package main

import (
	"database/sql"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/fsnotify/fsnotify"
	_ "github.com/mattn/go-sqlite3"
)

type State struct {
	recipesPath string
	recipes     map[string]string
	recipeExt   string
}

func (s *State) isRecipe(entry fs.FileInfo) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), s.recipeExt)
}

func (s *State) addRecipe(path string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		s.recipes[path] = strings.TrimSuffix(entry.Name(), s.recipeExt)
	}
}

func (s *State) deleteRecipe(path string) {
	delete(s.recipes, path)
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

	err = watcher.Add(".")
	if err != nil {
		log.Fatal(err)
	}

	err = watcher.Add(s.recipesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Fatal(err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name == s.recipesPath {
				log.Println("Recipes path event:", event)
				if event.Has(fsnotify.Create) {
					err = watcher.Add(s.recipesPath)
					if err != nil {
						log.Fatal(err)
					}
				}
				if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					watcher.Remove(s.recipesPath)
				}
			} else {
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
	log.Println("Recipes path:", os.Args[1])
	var state = State{
		recipesPath: os.Args[1],
		recipes:     map[string]string{},
		recipeExt:   ".md",
	}

	// Open SQLite database
	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	state.loadRecipes()
	go state.monitorRecipesDirectory()

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	// Serve the Go template
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("templates/index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Println(state.recipes)
		recipeList := make([]string, 0, len(state.recipes))
		for _, name := range state.recipes {
			recipeList = append(recipeList, name)
		}
		sort.Strings(recipeList)

		err = tmpl.Execute(w, recipeList)
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
