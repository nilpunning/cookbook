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

type baseData struct {
	ShowAuth        bool
	ShowImport      bool
	IsAuthenticated bool
	LoginUrl        string
	LogoutUrl       string
}

type makeBaseData func(r *http.Request) baseData

func makeHandleIndex(state core.State, makeBaseData makeBaseData) http.HandlerFunc {
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

		data := struct {
			baseData
			Recipes []search.SearchResult
			Tags    []search.RecipesGroupedByTag
			Title   string
			Query   string
		}{
			baseData: makeBaseData(r),
			Title:    "Recipes",
			Query:    query,
		}

		if query != "" {
			recipes, err := search.SearchRecipes(state.Index, query)
			if err != nil {
				slog.Error(err.Error())
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data.Recipes = recipes
		} else {
			tags, err := search.GetRecipesGroupedByTag(state.Index)
			if err != nil {
				slog.Error(err.Error())
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data.Tags = tags
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

		if err := indexTemplate.ExecuteTemplate(w, templateName, data); err != nil {
			slog.Error(err.Error())
		}
	}
}

func makeHandleRecipePath(state core.State, makeBaseData makeBaseData) http.HandlerFunc {
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
				baseData
				Title   string
				Name    string
				Webpath string
				Body    template.HTML
			}{
				baseData: makeBaseData(r),
				Title:    name,
				Name:     name,
				Webpath:  webpath,
				Body:     template.HTML(html),
			}
			if err := recipeTemplate.Execute(w, data); err != nil {
				slog.Error(err.Error())
			}
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func handleRecipe(r *http.Request, state core.State, makeBaseData makeBaseData) recipeTemplateData {
	data := makeBaseData(r)
	if !data.IsAuthenticated {
		return recipeTemplateDataError(data, http.StatusUnauthorized, "Unauthorized", "")
	}

	var resp recipeResponse
	switch r.Method {
	case "GET":
		resp = handleRecipeGet(state, r)
	case "POST":
		resp = handleRecipePost(state, r, "")
	default:
		resp = recipeResponse{
			response: response{
				Error:      "Method Not Allowed",
				StatusCode: http.StatusMethodNotAllowed,
			},
		}
	}

	return recipeTemplateData{
		baseData:       data,
		recipeResponse: resp,
		CsrfField:      csrf.TemplateField(r),
		Title:          "Add Recipe",
		CancelUrl:      "/",
	}
}

func makeHandleRecipe(state core.State, makeBaseData makeBaseData, recipeFormTemplate *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, r, recipeFormTemplate, handleRecipe(r, state, makeBaseData))
	}
}

func handleRecipePathEdit(r *http.Request, state core.State, makeBaseData makeBaseData) recipeTemplateData {
	data := makeBaseData(r)
	if !data.IsAuthenticated {
		return recipeTemplateDataError(data, http.StatusUnauthorized, "Unauthorized", "")
	}

	webpath := r.PathValue("path")
	filename, name, _, err := search.GetRecipe(state.Index, webpath)

	if err == search.ErrNotFound {
		return recipeTemplateDataError(data, http.StatusNotFound, "Not Found", "")
	}
	if err != nil {
		slog.Error(err.Error())
		return recipeTemplateDataError(data, http.StatusInternalServerError, "Unexpected Error", err.Error())
	}

	var resp recipeResponse
	switch r.Method {
	case "GET":
		fp := filepath.Join(state.Config.Server.RecipesPath, filename)
		md, err := os.ReadFile(fp)
		if err != nil {
			slog.Error(err.Error())
			resp = recipeResponse{
				response: response{
					Error:      "Unexpected Error: " + err.Error(),
					StatusCode: http.StatusInternalServerError,
				},
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
			response: response{
				Error:      "Method Not Allowed",
				StatusCode: http.StatusMethodNotAllowed,
			},
		}
	}

	return recipeTemplateData{
		baseData:       data,
		recipeResponse: resp,
		CsrfField:      csrf.TemplateField(r),
		Title:          "Edit " + name,
		CancelUrl:      "/recipe/" + webpath,
		DeleteUrl:      "/recipe/" + webpath + "/delete",
	}
}

func makeHandleRecipePathEdit(state core.State, makeBaseData makeBaseData, recipeFormTemplate *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, r, recipeFormTemplate, handleRecipePathEdit(r, state, makeBaseData))
	}
}

func handleImport(r *http.Request, makeBaseData makeBaseData) importTemplateData {
	data := makeBaseData(r)
	if !data.IsAuthenticated {
		return importTemplateDataError(data, http.StatusUnauthorized, "Unauthorized", "")
	}

	switch r.Method {
	case "GET":
		return importTemplateData{
			baseData:  data,
			CsrfField: csrf.TemplateField(r),
			Title:     "Import Recipe",
			CancelUrl: "/",
		}
	case "POST":
		if err := r.ParseForm(); err != nil {
			slog.Error(err.Error())
			return importTemplateDataError(data, http.StatusBadRequest, "Bad Request", err.Error())
		}

		u := r.FormValue("url")
		escapedURL := url.QueryEscape(u)

		return importTemplateData{
			baseData: data,
			response: response{
				RedirectPath: "/recipe?import=" + escapedURL,
			},
		}
	default:
		return importTemplateDataError(data, http.StatusMethodNotAllowed, "Method Not Allowed", "")
	}
}

func makeHandleImport(makeBaseData makeBaseData) http.HandlerFunc {
	importTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/import.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, r, importTemplate, handleImport(r, makeBaseData))
	}
}

func makeHandleRecipePathDelete(state core.State, makeBaseData makeBaseData) http.HandlerFunc {
	deleteRecipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/deleteRecipe.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
		data := makeBaseData(r)
		if !data.IsAuthenticated {
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
				baseData
				CsrfField template.HTML
				Title     string
				Name      string
				Webpath   string
			}{
				baseData:  data,
				CsrfField: csrf.TemplateField(r),
				Title:     "Delete " + name + "?",
				Name:      name,
				Webpath:   "/recipe/" + webpath,
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

	makeBaseData := func(r *http.Request) baseData {
		return baseData{
			ShowAuth:        loginURL != "" && logoutURL != "",
			ShowImport:      state.Config.Server.LLM != nil,
			IsAuthenticated: auth.IsAuthenticated(state.SessionStore, r),
			LoginUrl:        loginURL,
			LogoutUrl:       logoutURL,
		}
	}

	serveMux.HandleFunc("/{$}", makeHandleIndex(state, makeBaseData))
	serveMux.HandleFunc("/recipe/{path}", makeHandleRecipePath(state, makeBaseData))

	recipeFormTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipeForm.html",
	))
	serveMux.HandleFunc("/recipe", makeHandleRecipe(state, makeBaseData, recipeFormTemplate))
	serveMux.HandleFunc("/recipe/{path}/edit", makeHandleRecipePathEdit(state, makeBaseData, recipeFormTemplate))

	if state.Config.Server.LLM != nil {
		serveMux.HandleFunc("/import", makeHandleImport(makeBaseData))
	}

	serveMux.HandleFunc("/recipe/{path}/delete", makeHandleRecipePathDelete(state, makeBaseData))
}
