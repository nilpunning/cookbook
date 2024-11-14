package handlers

import (
	"database/sql"
	"hallertau/internal/auth"
	"hallertau/internal/core"
	"hallertau/internal/database"
	"html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/csrf"
)

type baseContext struct {
	IsAuthenticated bool
	LoginUrl        string
	LogoutUrl       string
}

func AddHandlers(serveMux *http.ServeMux, state core.State, loginURL string, logoutURL string) {

	makeBaseContext := func(r *http.Request) baseContext {
		return baseContext{
			IsAuthenticated: auth.IsAuthenticated(state.SessionStore, r),
			LoginUrl:        loginURL,
			LogoutUrl:       logoutURL,
		}
	}

	indexTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/index.html",
	))

	serveMux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")

		if query == "" && r.URL.RawQuery != "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		context := struct {
			baseContext
			Recipes []database.SearchResult
			Tags    []database.RecipesGroupedByTag
			Title   string
			Query   string
		}{
			baseContext: makeBaseContext(r),
			Title:       "Recipes",
			Query:       query,
		}

		if query != "" {
			recipes, err := database.SearchRecipes(state.DB, query)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			context.Recipes = recipes
		} else {
			tags, err := database.GetRecipesGroupedByTag(state.DB)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			context.Tags = tags
		}

		isHtmx := r.Header.Get("Hx-Request") == "true"
		htmxTarget := r.Header.Get("Hx-Target")

		templateName := "base.html"
		if isHtmx && htmxTarget == "recipes" {
			templateName = "recipesBody"
		}

		if err := indexTemplate.ExecuteTemplate(w, templateName, context); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	recipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipe.html",
	))

	serveMux.HandleFunc("/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		var name, html, err = database.GetRecipe(state.DB, webpath)
		switch err {
		case sql.ErrNoRows:
			http.Error(w, "Recipe not found", http.StatusNotFound)
		case nil:
			data := struct {
				baseContext
				Title   string
				Name    string
				Webpath string
				Body    template.HTML
			}{
				baseContext: makeBaseContext(r),
				Title:       name,
				Name:        name,
				Webpath:     webpath,
				Body:        template.HTML(html),
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

	serveMux.HandleFunc("/recipe", func(w http.ResponseWriter, r *http.Request) {
		bc := makeBaseContext(r)
		if bc.IsAuthenticated == false {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method == "GET" {
			data := struct {
				baseContext
				CsrfField template.HTML
				Title     string
				Name      string
				Body      string
				CancelUrl string
				DeleteUrl string
			}{
				baseContext: bc,
				CsrfField:   csrf.TemplateField(r),
				Title:       "Add Recipe",
				CancelUrl:   "/",
			}
			if err := recipeFormTemplate.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		handleEditRecipe(state, w, r, "")
	})

	serveMux.HandleFunc("/recipe/{path}/edit", func(w http.ResponseWriter, r *http.Request) {
		bc := makeBaseContext(r)
		if bc.IsAuthenticated == false {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

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
		fp := filepath.Join(state.Config.Server.RecipesPath, filename)

		if r.Method == "GET" {
			md, err := os.ReadFile(fp)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			data := struct {
				baseContext
				CsrfField template.HTML
				Title     string
				Name      string
				Body      string
				CancelUrl string
				DeleteUrl string
			}{
				baseContext: bc,
				CsrfField:   csrf.TemplateField(r),
				Title:       "Edit " + name,
				Name:        name,
				Body:        string(md),
				CancelUrl:   "/recipe/" + webpath,
				DeleteUrl:   "/recipe/" + webpath + "/delete",
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

	serveMux.HandleFunc("/recipe/{path}/delete", func(w http.ResponseWriter, r *http.Request) {
		bc := makeBaseContext(r)
		if bc.IsAuthenticated == false {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		webpath := r.PathValue("path")

		if r.Method == "GET" {
			name, _, err := database.GetRecipe(state.DB, webpath)
			if err == sql.ErrNoRows {
				http.Error(w, "Recipe not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data := struct {
				baseContext
				CsrfField template.HTML
				Title     string
				Name      string
				Webpath   string
			}{
				baseContext: bc,
				CsrfField:   csrf.TemplateField(r),
				Title:       "Delete " + name + "?",
				Name:        name,
				Webpath:     "/recipe/" + webpath,
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

		fp := filepath.Join(state.Config.Server.RecipesPath, filename)
		if err := os.Remove(fp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}
