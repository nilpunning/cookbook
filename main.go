package main

import (
	"bytes"
	"database/sql"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/fsnotify/fsnotify"

	"hallertau/internal/database"
	"hallertau/internal/markdown"
)

type State struct {
	db          *sql.DB
	recipesPath string
	recipeExt   string
}

func (s *State) isRecipe(entry fs.FileInfo) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), s.recipeExt)
}

func (s *State) upsertRecipe(filename string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		var name = strings.TrimSuffix(entry.Name(), s.recipeExt)
		var title = cases.Title(language.English, cases.Compact).String(name)
		gMarkdown := goldmark.New(
			goldmark.WithExtensions(extension.GFM, markdown.Tags),
			goldmark.WithRendererOptions(html.WithHardWraps()),
		)

		file, err := os.DirFS(s.recipesPath).Open(filename)
		if err != nil {
			log.Println("Error opening recipe file:", err)
			return
		}
		var md bytes.Buffer
		if _, err = md.ReadFrom(file); err != nil {
			log.Println("Error reading recipe file:", err)
			return
		}
		var html bytes.Buffer
		pc := parser.NewContext()
		if err := gMarkdown.Convert(md.Bytes(), &html, parser.WithContext(pc)); err != nil {
			log.Println("Error converting recipe file:", err)
			return
		}

		tags := []string{}
		if t := pc.Get(markdown.TagsContextKey); t != nil {
			tags = t.([]string)
		}

		if len(tags) == 0 {
			tags = []string{"Other"}
		}

		webpath := strings.ReplaceAll(title, " ", "") + ".html"

		database.UpsertRecipe(s.db, filename, name, webpath, html.String(), tags)
	}
}

func (s *State) loadRecipes() {
	entries, err := os.ReadDir(s.recipesPath)
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range entries {
		var info, err = entry.Info()
		if err != nil {
			log.Fatal(err)
		}
		s.upsertRecipe(entry.Name(), info)
	}
}

func (s *State) monitorRecipesDirectory() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = watcher.Add(s.recipesPath)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			filename := filepath.Base(event.Name)
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				entry, err := os.Stat(event.Name)
				if err != nil {
					log.Fatal(err)
				}
				s.upsertRecipe(filename, entry)
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				database.DeleteRecipe(s.db, filename)
			}
			log.Println("Event:", event)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("Error:", err)
		}
	}
}

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.
	log.Println("Recipes path:", os.Args[1])

	var state = State{
		db:          database.Setup(),
		recipesPath: os.Args[1],
		recipeExt:   ".md",
	}
	defer state.db.Close()

	state.loadRecipes()
	go state.monitorRecipesDirectory()

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	indexTemplate := template.Must(template.ParseFiles("templates/base.html", "templates/index.html"))
	recipeTemplate := template.Must(template.ParseFiles("templates/base.html", "templates/recipe.html"))

	// Serve the Go template
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		tags, err := database.GetRecipesGroupedByTag(state.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := indexTemplate.Execute(w, map[string]any{
			"Title": "Recipes",
			"Tags":  tags,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		var name, html, err = database.GetRecipe(state.db, webpath)
		switch err {
		case sql.ErrNoRows:
			http.Error(w, "Recipe not found", http.StatusNotFound)
		case nil:
			data := struct {
				Title string
				Name  string
				Body  template.HTML
			}{
				Title: "Recipes",
				Name:  name,
				Body:  template.HTML(html),
			}
			if err := recipeTemplate.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Start the web server
	log.Println("Server starting on http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
