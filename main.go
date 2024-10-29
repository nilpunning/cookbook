package main

import (
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
	_ "github.com/mattn/go-sqlite3"
)

type State struct {
	db          *sql.DB
	recipesPath string
	recipeExt   string
}

func (s *State) isRecipe(entry fs.FileInfo) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), s.recipeExt)
}

func (s *State) addRecipe(path string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		var name = strings.TrimSuffix(entry.Name(), s.recipeExt)
		var title = cases.Title(language.English, cases.Compact).String(name)
		_, err := s.db.Exec(`
			INSERT INTO recipe (filepath, name, url)
			VALUES (?, ?, ?)
		`, path, name, strings.ReplaceAll(title, " ", "")+".html")
		if err != nil {
			log.Printf("Error inserting recipe %s: %v", path, err)
			return
		}
	}
}

func (s *State) deleteRecipe(path string) {
	_, err := s.db.Exec(`
		DELETE FROM recipe WHERE filepath = ?
	`, path)
	if err != nil {
		log.Printf("Error deleting recipe %s: %v", path, err)
	}
}

func (s *State) loadRecipes() {
	entries, err := os.ReadDir("recipes")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Fatal(err)
	}

	for _, entry := range entries {
		var info, err = entry.Info()
		if err != nil {
			log.Fatal(err)
		}
		s.addRecipe(filepath.Join("recipes", entry.Name()), info)
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
			if event.Has(fsnotify.Create) {
				entry, err := os.Stat(event.Name)
				if err != nil {
					log.Fatal(err)
				}
				s.addRecipe(event.Name, entry)
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				s.deleteRecipe(event.Name)
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
	db, err := sql.Open("sqlite3", ":memory:?cache=shared")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create recipe table
	_, err = db.Exec(`
		CREATE TABLE recipe (
			filepath TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			url TEXT NOT NULL
		)
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

	baseTemplate := template.Must(template.ParseFiles("templates/base.html"))
	indexTemplate := template.Must(baseTemplate.ParseFiles("templates/index.html"))

	// Serve the Go template
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		rows, err := state.db.Query(`
			SELECT filepath, name, url FROM recipe order by name
		`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		defer rows.Close()

		var recipes []map[string]string
		for rows.Next() {
			var filepath, name, url string
			err := rows.Scan(&filepath, &name, &url)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			recipes = append(recipes, map[string]string{
				"Filepath": filepath,
				"Name":     name,
				"Url":      url,
			})
		}
		if err = rows.Err(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Println(recipes)

		err = indexTemplate.Execute(w, map[string]any{
			"Title":   "Recipes",
			"Recipes": recipes,
		})
		if err != nil {
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
