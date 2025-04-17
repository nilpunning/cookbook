package handlers

import (
	"cookbook/internal/auth"
	"cookbook/internal/core"
	"cookbook/internal/search"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gorilla/csrf"
)

type baseContext struct {
	ShowAuth        bool
	ShowImport      bool
	IsAuthenticated bool
	LoginUrl        string
	LogoutUrl       string
}

func htmx(r *http.Request) (bool, string) {
	isHtmx := r.Header.Get("Hx-Request") == "true"
	htmxTarget := r.Header.Get("Hx-Target")
	return isHtmx, htmxTarget
}

func AddHandlers(serveMux *http.ServeMux, state core.State, loginURL string, logoutURL string) {

	serveMux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(core.Version))
	})

	makeBaseContext := func(r *http.Request) baseContext {
		return baseContext{
			ShowAuth:        loginURL != "" && logoutURL != "",
			ShowImport:      state.Config.Server.LLM != nil,
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

		if query == "" && r.URL.RawQuery != "" || r.URL.Query().Has("clear") {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		context := struct {
			baseContext
			Recipes []search.SearchResult
			Tags    []search.RecipesGroupedByTag
			Title   string
			Query   string
		}{
			baseContext: makeBaseContext(r),
			Title:       "Recipes",
			Query:       query,
		}

		if query != "" {
			recipes, err := search.SearchRecipes(state.Index, query)
			if err != nil {
				slog.Error(err.Error())
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			context.Recipes = recipes
		} else {
			tags, err := search.GetRecipesGroupedByTag(state.Index)
			if err != nil {
				slog.Error(err.Error())
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			context.Tags = tags
		}

		isHtmx, htmxTarget := htmx(r)

		templateName := "base.html"
		if isHtmx {
			if htmxTarget == "recipes" {
				templateName = "recipesBody"
			}
			if htmxTarget == "body" {
				templateName = "body"
			}
		}

		w.Header().Set("Vary", "HX-Request")

		if err := indexTemplate.ExecuteTemplate(w, templateName, context); err != nil {
			slog.Error(err.Error())
		}
	})

	recipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipe.html",
	))

	serveMux.HandleFunc("/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		_, name, html, err := search.GetRecipe(state.Index, webpath)
		switch err {
		case search.ErrNotFound:
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusNotFound)
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
				slog.Error(err.Error())
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
		if !bc.IsAuthenticated {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method == "GET" {
			name := ""
			body := ""
			if state.Config.Server.LLM != nil {
				importURL := r.URL.Query().Get("import")

				if importURL != "" {
					llm, err := core.LLMModel(r.Context(), state.Config)
					if err != nil {
						slog.Error(err.Error())
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}

					recipe, err := core.Import(r.Context(), llm, core.HTTPRequest, importURL)
					if err != nil {
						slog.Error(err.Error())
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					if recipe != nil {
						name = recipe.Name
						body = recipe.Body
					}
				}
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
				Title:       "Add Recipe",
				Name:        name,
				Body:        body,
				CancelUrl:   "/",
			}
			if err := recipeFormTemplate.Execute(w, data); err != nil {
				slog.Error(err.Error())
			}
			return
		}

		handleEditRecipe(state, w, r, "")
	})

	importTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/import.html",
	))

	if state.Config.Server.LLM != nil {
		serveMux.HandleFunc("/import", func(w http.ResponseWriter, r *http.Request) {
			bc := makeBaseContext(r)
			if !bc.IsAuthenticated {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if r.Method == "GET" {
				data := struct {
					baseContext
					CsrfField template.HTML
					Title     string
					CancelUrl string
				}{
					baseContext: bc,
					CsrfField:   csrf.TemplateField(r),
					Title:       "Import Recipe",
					CancelUrl:   "/",
				}
				if err := importTemplate.Execute(w, data); err != nil {
					slog.Error(err.Error())
				}
				return
			}

			if err := r.ParseForm(); err != nil {
				slog.Error(err.Error())
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			u := r.FormValue("url")
			escapedURL := url.QueryEscape(u)
			w.Header().Set("HX-Redirect", "/recipe?import="+escapedURL)
			w.WriteHeader(http.StatusOK)
		})
	}

	serveMux.HandleFunc("/recipe/{path}/edit", func(w http.ResponseWriter, r *http.Request) {
		bc := makeBaseContext(r)
		if !bc.IsAuthenticated {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		webpath := r.PathValue("path")

		filename, name, _, err := search.GetRecipe(state.Index, webpath)
		if err == search.ErrNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err != nil {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fp := filepath.Join(state.Config.Server.RecipesPath, filename)

		if r.Method == "GET" {
			md, err := os.ReadFile(fp)
			if err != nil {
				slog.Error(err.Error())
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
				slog.Error(err.Error())
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
		if !bc.IsAuthenticated {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		webpath := r.PathValue("path")

		if r.Method == "GET" {
			_, name, _, err := search.GetRecipe(state.Index, webpath)
			if err == search.ErrNotFound {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if err != nil {
				slog.Error(err.Error())
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
				slog.Error(err.Error())
			}
			return
		}

		filename, _, _, err := search.GetRecipe(state.Index, webpath)
		if err == search.ErrNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err != nil {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fp := filepath.Join(state.Config.Server.RecipesPath, filename)

		if err := os.Remove(fp); err != nil {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}
