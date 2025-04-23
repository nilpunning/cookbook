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

type makeBaseContext func(r *http.Request) baseContext

func makeHandleIndex(state core.State, makeBaseContext makeBaseContext) http.HandlerFunc {
	indexTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/index.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func makeHandleRecipePath(state core.State, makeBaseContext makeBaseContext) http.HandlerFunc {
	recipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipe.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func handleRecipe(r *http.Request, state core.State, makeBaseContext makeBaseContext) recipeTemplateData {
	bc := makeBaseContext(r)
	if !bc.IsAuthenticated {
		return recipeResponseError(bc, "Unauthorized", "", http.StatusUnauthorized)
	}

	var resp recipeResponse
	switch r.Method {
	case "GET":
		resp = handleRecipeGet(state, r)
	case "POST":
		resp = handleRecipePost(state, r, "")
	default:
		resp = recipeResponse{
			Error:      "Method Not Allowed",
			StatusCode: http.StatusMethodNotAllowed,
		}
	}

	return recipeTemplateData{
		baseContext:    bc,
		recipeResponse: resp,
		CsrfField:      csrf.TemplateField(r),
		Title:          "Add Recipe",
		CancelUrl:      "/",
	}
}

func makeHandleRecipe(state core.State, makeBaseContext makeBaseContext, recipeFormTemplate *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeRecipeResponse(w, r, recipeFormTemplate, handleRecipe(r, state, makeBaseContext))
	}
}

func handleRecipePathEdit(r *http.Request, state core.State, makeBaseContext makeBaseContext) recipeTemplateData {
	bc := makeBaseContext(r)
	if !bc.IsAuthenticated {
		return recipeResponseError(bc, "Unauthorized", "", http.StatusUnauthorized)
	}

	webpath := r.PathValue("path")
	filename, name, _, err := search.GetRecipe(state.Index, webpath)

	if err == search.ErrNotFound {
		return recipeResponseError(bc, "Not Found", "", http.StatusNotFound)
	}
	if err != nil {
		slog.Error(err.Error())
		return recipeResponseError(bc, "Unexpected Error", err.Error(), http.StatusInternalServerError)
	}

	var resp recipeResponse
	switch r.Method {
	case "GET":
		fp := filepath.Join(state.Config.Server.RecipesPath, filename)
		md, err := os.ReadFile(fp)
		if err != nil {
			slog.Error(err.Error())
			resp = recipeResponse{
				Error:      "Unexpected Error: " + err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		} else {
			resp = recipeResponse{
				Name: name,
				Body: string(md),
			}
		}
	case "POST":
		resp = handleRecipePost(state, r, filename)
	default:
		resp = recipeResponse{
			Error:      "Method Not Allowed",
			StatusCode: http.StatusMethodNotAllowed,
		}
	}

	return recipeTemplateData{
		recipeResponse: resp,
		CsrfField:      csrf.TemplateField(r),
		Title:          "Edit " + name,
		CancelUrl:      "/recipe/" + webpath,
		DeleteUrl:      "/recipe/" + webpath + "/delete",
	}
}

func makeHandleRecipePathEdit(state core.State, makeBaseContext makeBaseContext, recipeFormTemplate *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeRecipeResponse(w, r, recipeFormTemplate, handleRecipePathEdit(r, state, makeBaseContext))
	}
}

func makeHandleImport(makeBaseContext makeBaseContext) http.HandlerFunc {
	importTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/import.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func makeHandleRecipePathDelete(state core.State, makeBaseContext makeBaseContext) http.HandlerFunc {
	deleteRecipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/deleteRecipe.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
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
	}
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

	serveMux.HandleFunc("/{$}", makeHandleIndex(state, makeBaseContext))
	serveMux.HandleFunc("/recipe/{path}", makeHandleRecipePath(state, makeBaseContext))

	recipeFormTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipeForm.html",
	))
	serveMux.HandleFunc("/recipe", makeHandleRecipe(state, makeBaseContext, recipeFormTemplate))
	serveMux.HandleFunc("/recipe/{path}/edit", makeHandleRecipePathEdit(state, makeBaseContext, recipeFormTemplate))

	if state.Config.Server.LLM != nil {
		serveMux.HandleFunc("/import", makeHandleImport(makeBaseContext))
	}

	serveMux.HandleFunc("/recipe/{path}/delete", makeHandleRecipePathDelete(state, makeBaseContext))
}
