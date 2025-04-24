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

type stateData struct {
	HasAuth         bool
	HasImport       bool
	IsAuthenticated bool
	LoginUrl        string
	LogoutUrl       string
}

func makeStateData(state core.State, r *http.Request) stateData {
	hasAuth := state.Auth.LoginUrl != "" && state.Auth.LogoutUrl != ""
	return stateData{
		HasAuth:         hasAuth,
		HasImport:       hasAuth && state.Config.Server.LLM != nil,
		IsAuthenticated: hasAuth && auth.IsAuthenticated(state.SessionStore, r),
		LoginUrl:        state.Auth.LoginUrl,
		LogoutUrl:       state.Auth.LogoutUrl,
	}
}

func makeHandleIndex(state core.State) http.HandlerFunc {
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
			stateData
			Recipes []search.SearchResult
			Tags    []search.RecipesGroupedByTag
			Title   string
			Query   string
		}{
			stateData: makeStateData(state, r),
			Title:     "Recipes",
			Query:     query,
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

func makeHandleRecipePath(state core.State) http.HandlerFunc {
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
				stateData
				Title   string
				Name    string
				Webpath string
				Body    template.HTML
			}{
				stateData: makeStateData(state, r),
				Title:     name,
				Name:      name,
				Webpath:   webpath,
				Body:      template.HTML(html),
			}
			if err := recipeTemplate.Execute(w, data); err != nil {
				slog.Error(err.Error())
			}
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func handleRecipe(state core.State, r *http.Request) recipeTemplateData {
	data := recipeTemplateData{stateData: makeStateData(state, r)}

	if !data.IsAuthenticated {
		data.response = errorResponse(http.StatusUnauthorized, "")
		return data
	}

	switch r.Method {
	case "GET":
		data.recipeResponse = handleRecipeGet(state, r)
	case "POST":
		data.recipeResponse = handleRecipePost(state, r, "")
	default:
		data.response = errorResponse(http.StatusMethodNotAllowed, r.Method)
	}

	data.Title = "Add Recipe"
	data.CsrfField = csrf.TemplateField(r)
	data.CancelUrl = "/"

	return data
}

func makeHandleRecipe(state core.State, recipeFormTemplate *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, r, recipeFormTemplate, handleRecipe(state, r))
	}
}

func handleRecipePathEdit(state core.State, r *http.Request) recipeTemplateData {
	data := recipeTemplateData{stateData: makeStateData(state, r)}

	if !data.IsAuthenticated {
		data.response = errorResponse(http.StatusUnauthorized, "")
		return data
	}

	webpath := r.PathValue("path")
	filename, name, _, err := search.GetRecipe(state.Index, webpath)

	if err == search.ErrNotFound {
		data.response = errorResponse(http.StatusNotFound, webpath)
		return data
	}
	if err != nil {
		slog.Error(err.Error())
		data.response = errorResponse(http.StatusInternalServerError, err.Error())
		return data
	}

	switch r.Method {
	case "GET":
		fp := filepath.Join(state.Config.Server.RecipesPath, filename)
		md, err := os.ReadFile(fp)
		if err != nil {
			slog.Error(err.Error())
			data.response = errorResponse(http.StatusInternalServerError, err.Error())
			return data
		} else {
			data.recipeResponse = recipeResponse{
				Name: name,
				Body: string(md),
			}
		}
		data.Title = "Edit " + name
		data.CsrfField = csrf.TemplateField(r)
		data.CancelUrl = "/recipe/" + webpath
		data.DeleteUrl = "/recipe/" + webpath + "/delete"
	case "POST":
		data.recipeResponse = handleRecipePost(state, r, filename)
	default:
		data.response = errorResponse(http.StatusMethodNotAllowed, r.Method)
		return data
	}

	return data
}

func makeHandleRecipePathEdit(state core.State, recipeFormTemplate *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, r, recipeFormTemplate, handleRecipePathEdit(state, r))
	}
}

func handleImport(state core.State, r *http.Request) importTemplateData {
	data := importTemplateData{stateData: makeStateData(state, r)}

	if !data.IsAuthenticated {
		data.response = errorResponse(http.StatusUnauthorized, "")
		return data
	}

	if !data.HasImport {
		data.response = errorResponse(http.StatusForbidden, "import not configured")
		return data
	}

	switch r.Method {
	case "GET":
		data.response = response{Title: "Import Recipe"}
		data.CsrfField = csrf.TemplateField(r)
		data.CancelUrl = "/"
	case "POST":
		if err := r.ParseForm(); err != nil {
			slog.Error(err.Error())
			data.response = errorResponse(http.StatusInternalServerError, err.Error())
			return data
		}

		u := r.FormValue("url")
		escapedURL := url.QueryEscape(u)

		data.RedirectPath = "/recipe?import=" + escapedURL
	default:
		data.response = errorResponse(http.StatusMethodNotAllowed, r.Method)
	}
	return data
}

func makeHandleImport(state core.State) http.HandlerFunc {
	importTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/import.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, r, importTemplate, handleImport(state, r))
	}
}

func handleRecipePathDelete(state core.State, r *http.Request) recipePathDeleteTemplateData {
	data := recipePathDeleteTemplateData{stateData: makeStateData(state, r)}

	if !data.IsAuthenticated {
		data.response = errorResponse(http.StatusUnauthorized, "")
		return data
	}

	webpath := r.PathValue("path")

	switch r.Method {
	case "GET":
		_, name, _, err := search.GetRecipe(state.Index, webpath)
		if err == search.ErrNotFound {
			data.response = errorResponse(http.StatusNotFound, webpath)
			return data
		}
		if err != nil {
			slog.Error(err.Error())
			data.response = errorResponse(http.StatusInternalServerError, err.Error())
			return data
		}
		data.response = response{Title: "Delete " + name + "?"}
		data.CsrfField = csrf.TemplateField(r)
		data.Name = name
		data.Webpath = "/recipe/" + webpath
	case "POST":
		filename, _, _, err := search.GetRecipe(state.Index, webpath)
		if err == search.ErrNotFound {
			data.response = errorResponse(http.StatusNotFound, webpath)
			return data
		}
		if err != nil {
			slog.Error(err.Error())
			data.response = errorResponse(http.StatusInternalServerError, err.Error())
			return data
		}

		fp := filepath.Join(state.Config.Server.RecipesPath, filename)

		if err := os.Remove(fp); err != nil {
			slog.Error(err.Error())
			data.response = errorResponse(http.StatusInternalServerError, err.Error())
		}

		data.RedirectPath = "/"
	default:
		data.response = errorResponse(http.StatusMethodNotAllowed, r.Method)
	}

	return data
}

func makeHandleRecipePathDelete(state core.State) http.HandlerFunc {
	deleteRecipeTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/deleteRecipe.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, r, deleteRecipeTemplate, handleRecipePathDelete(state, r))
	}
}

func AddHandlers(state core.State, serveMux *http.ServeMux) {
	serveMux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(core.Version))
	})
	serveMux.HandleFunc("/{$}", makeHandleIndex(state))
	serveMux.HandleFunc("/recipe/{path}", makeHandleRecipePath(state))
	recipeFormTemplate := template.Must(template.ParseFiles(
		"templates/base.html",
		"templates/recipeForm.html",
	))
	serveMux.HandleFunc("/recipe", makeHandleRecipe(state, recipeFormTemplate))
	serveMux.HandleFunc("/recipe/{path}/edit", makeHandleRecipePathEdit(state, recipeFormTemplate))
	serveMux.HandleFunc("/import", makeHandleImport(state))
	serveMux.HandleFunc("/recipe/{path}/delete", makeHandleRecipePathDelete(state))
}
