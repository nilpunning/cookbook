package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"hallertau/internal/core"
	"hallertau/internal/database"
)

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.
	var state = core.State{
		DB:          database.Setup(),
		RecipesPath: os.Args[1],
		RecipeExt:   ".md",
	}
	defer state.DB.Close()

	state.LoadRecipes()
	go state.MonitorRecipesDirectory()

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

			recipes, err := database.SearchRecipes(state.DB, query)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			context["Recipes"] = recipes
		} else {
			tags, err := database.GetRecipesGroupedByTag(state.DB)
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

		var name, html, err = database.GetRecipe(state.DB, webpath)
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

		core.HandleEditRecipe(state, w, r, "")
	})

	http.HandleFunc("/edit/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		name, filename, err := database.GetRecipeName(state.DB, webpath)
		if err == sql.ErrNoRows {
			http.Error(w, "Recipe not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fp := filepath.Join(state.RecipesPath, filename)

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

		core.HandleEditRecipe(state, w, r, filename)
	})

	deleteRecipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/deleteRecipe.html",
	))

	http.HandleFunc("/delete/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		if r.Method == "GET" {
			name, html, err := database.GetRecipe(state.DB, webpath)
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

		_, filename, err := database.GetRecipeName(state.DB, webpath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fp := filepath.Join(state.RecipesPath, filename)
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
