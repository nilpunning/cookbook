package main

import (
	"database/sql"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/fsnotify/fsnotify"
	_ "github.com/mattn/go-sqlite3"
)

type Recipe struct {
	Name string
	URL  string
}

type State struct {
	recipesPath string
	recipes     map[string]Recipe
	recipeExt   string
}

func (s *State) isRecipe(entry fs.FileInfo) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), s.recipeExt)
}

func (s *State) addRecipe(path string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		var name = strings.TrimSuffix(entry.Name(), s.recipeExt)
		var title = cases.Title(language.English, cases.Compact).String(name)
		s.recipes[path] = Recipe{
			Name: name,
			URL:  strings.ReplaceAll(title, " ", "") + ".html",
		}
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
	var state = State{
		recipesPath: os.Args[1],
		recipes:     map[string]Recipe{},
		recipeExt:   ".md",
	}

	// Open SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	state.loadRecipes()
	go state.monitorRecipesDirectory()

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	baseTemplate := template.Must(template.ParseFiles("templates/base.html"))
	indexTemplate := template.Must(baseTemplate.ParseFiles("templates/index.html"))

	// Serve the Go template
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		recipeList := make([]Recipe, 0, len(state.recipes))
		for _, recipe := range state.recipes {
			recipeList = append(recipeList, recipe)
		}
		sort.Slice(recipeList, func(i, j int) bool {
			return recipeList[i].Name < recipeList[j].Name
		})

		err = indexTemplate.Execute(w, map[string]any{
			"Title":   "Recipes",
			"Recipes": recipeList,
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
