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
	_ "github.com/mattn/go-sqlite3"

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

		tags := pc.Get(markdown.TagsContextKey).([]string)
		log.Println(tags)

		webpath := strings.ReplaceAll(title, " ", "") + ".html"
		_, err = s.db.Exec(`
			INSERT INTO recipe (filename, name, webpath, html)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(filename) DO UPDATE SET name = ?, webpath = ?, html = ?
		`, filename, name, webpath, html.String(), name, webpath, html.String())

		if err != nil {
			log.Printf("Error inserting recipe %s: %v", filename, err)
			return
		}
	}
}

func (s *State) deleteRecipe(path string) {
	_, err := s.db.Exec(`
		DELETE FROM recipe WHERE filename = ?
	`, path)
	if err != nil {
		log.Printf("Error deleting recipe %s: %v", path, err)
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
				s.deleteRecipe(filename)
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

	// Open SQLite database
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create recipe table
	_, err = db.Exec(`
		CREATE TABLE recipe (
			filename TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			webpath TEXT NOT NULL,
			html TEXT NOT NULL
		);
		CREATE INDEX idx_recipe_webpath ON recipe (webpath);
		CREATE TABLE tag (
			name TEXT NOT NULL PRIMARY KEY
		);
		CREATE TABLE recipe_tag (
			recipe_filename TEXT NOT NULL,
			tag_name TEXT NOT NULL,
			PRIMARY KEY (recipe_filename, tag_name)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	var state = State{
		db:          db,
		recipesPath: os.Args[1],
		recipeExt:   ".md",
	}

	state.loadRecipes()
	go state.monitorRecipesDirectory()

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	indexTemplate := template.Must(template.ParseFiles("templates/base.html", "templates/index.html"))
	recipeTemplate := template.Must(template.ParseFiles("templates/base.html", "templates/recipe.html"))

	// Serve the Go template
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		rows, err := state.db.Query(`
			SELECT name, webpath FROM recipe order by name
		`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		defer rows.Close()

		var recipes []map[string]string
		for rows.Next() {
			var name, webpath string
			err := rows.Scan(&name, &webpath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			recipes = append(recipes, map[string]string{
				"Name":    name,
				"Webpath": webpath,
			})
		}
		if err = rows.Err(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = indexTemplate.Execute(w, map[string]any{
			"Title":   "Recipes",
			"Recipes": recipes,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		row := state.db.QueryRow(`
			SELECT filename, name, html FROM recipe WHERE webpath = ?
		`, webpath)

		var filename, name, html string
		switch err := row.Scan(&filename, &name, &html); err {
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
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
