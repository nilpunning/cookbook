package main

import (
	"bytes"
	"database/sql"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
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

func nameToWebpath(name string) string {
	title := cases.Title(language.English, cases.Compact).String(name)
	return strings.ReplaceAll(title, " ", "") + ".html"
}

func (s *State) upsertRecipe(filename string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		var name = strings.TrimSuffix(entry.Name(), s.recipeExt)

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
		database.UpsertRecipe(s.db, filename, name, nameToWebpath(name), html, tags)
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

func handleEditRecipe(state State, w http.ResponseWriter, r *http.Request, prevFilename string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := filepath.Base(r.FormValue("name"))
	body := r.FormValue("body")

	if name == "." || name == string(filepath.Separator) {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	filename := name + state.recipeExt
	fp := filepath.Join(state.recipesPath, filename)

	if err := os.WriteFile(fp, []byte(body), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if prevFilename != "" && prevFilename != filename {
		prevFp := filepath.Join(state.recipesPath, prevFilename)
		if err := os.Remove(prevFp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	escapedPath := url.PathEscape(nameToWebpath(name))
	http.Redirect(w, r, "/recipe/"+escapedPath, http.StatusSeeOther)
}

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.
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
				Title   string
				Name    string
				Webpath string
				Body    template.HTML
			}{
				Title:   name,
				Name:    name,
				Webpath: webpath,
				Body:    template.HTML(html),
			}
			if err := recipeTemplate.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	recipeFormTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipeForm.html",
	))

	http.HandleFunc("/recipe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			data := struct {
				Title     string
				Name      string
				Body      string
				CancelUrl string
				DeleteUrl string
			}{
				Title:     "Add Recipe",
				CancelUrl: "/",
			}
			if err := recipeFormTemplate.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		handleEditRecipe(state, w, r, "")
	})

	http.HandleFunc("/edit/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		name, filename, err := database.GetRecipeName(state.db, webpath)
		if err == sql.ErrNoRows {
			http.Error(w, "Recipe not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fp := filepath.Join(state.recipesPath, filename)

		if r.Method == "GET" {
			md, err := os.ReadFile(fp)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			data := struct {
				Title     string
				Name      string
				Body      string
				CancelUrl string
				DeleteUrl string
			}{
				Title:     "Edit " + name,
				Name:      name,
				Body:      string(md),
				CancelUrl: "/recipe/" + webpath,
				DeleteUrl: "/delete/recipe/" + webpath,
			}
			if err := recipeFormTemplate.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		handleEditRecipe(state, w, r, filename)
	})

	deleteRecipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/deleteRecipe.html",
	))

	http.HandleFunc("/delete/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		if r.Method == "GET" {
			name, html, err := database.GetRecipe(state.db, webpath)
			if err == sql.ErrNoRows {
				http.Error(w, "Recipe not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data := struct {
				Title   string
				Name    string
				Body    template.HTML
				Webpath string
			}{
				Title:   "Delete " + name + "?",
				Name:    name,
				Body:    template.HTML(html),
				Webpath: "/recipe/" + webpath,
			}
			if err := deleteRecipeTemplate.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		_, filename, err := database.GetRecipeName(state.db, webpath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fp := filepath.Join(state.recipesPath, filename)
		if err := os.Remove(fp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// Start the web server
	log.Println("Server starting on http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
