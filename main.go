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
		html, tags, err := markdown.Convert(md.Bytes())
		if err != nil {
			log.Println("Error converting recipe file:", err)
			return
		}

		webpath := strings.ReplaceAll(title, " ", "") + ".html"
		database.UpsertRecipe(s.db, filename, name, webpath, html, tags)
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

	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	indexGroupedByTagTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipes.html",
		"templates/recipesGroupedByTag.html",
		"templates/index.html",
	))
	indexBySearchTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipes.html",
		"templates/recipesBySearch.html",
		"templates/index.html",
	))

	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")

		if query == "" && r.URL.RawQuery != "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		tmpl := indexGroupedByTagTemplate
		context := map[string]any{}

		if query != "" {
			tmpl = indexBySearchTemplate

			recipes, err := database.SearchRecipes(state.db, query)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			context["Recipes"] = recipes
		} else {
			tags, err := database.GetRecipesGroupedByTag(state.db)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			context["Tags"] = tags
		}

		isHtmx := r.Header.Get("Hx-Request") == "true"
		htmxTarget := r.Header.Get("Hx-Target")

		if isHtmx && htmxTarget == "recipes" {
			if err := tmpl.ExecuteTemplate(w, "recipesBody", context); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			context["Title"] = "Recipes"
			context["Query"] = query
			if err := tmpl.Execute(w, context); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
	})

	recipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipe.html",
	))

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
