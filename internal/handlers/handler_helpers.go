package handlers

import (
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"cookbook/internal/core"
)

func htmx(r *http.Request) (bool, string) {
	isHtmx := r.Header.Get("Hx-Request") == "true"
	htmxTarget := r.Header.Get("Hx-Target")
	return isHtmx, htmxTarget
}

func ExclusiveWriteFile(name string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err1 := f.Close(); err1 != nil && err == nil {
		err = err1
	}
	return err
}

type recipeResponse struct {
	Name         string
	Body         string
	Error        string
	StatusCode   int
	RedirectPath string
}

func handleRecipeGet(state core.State, r *http.Request) recipeResponse {
	name := ""
	body := ""
	if state.Config.Server.LLM != nil {
		importURL := r.URL.Query().Get("import")

		if importURL != "" {
			llm, err := core.LLMModel(r.Context(), state.Config)
			if err != nil {
				slog.Error(err.Error())
				return recipeResponse{Error: err.Error(), StatusCode: http.StatusInternalServerError}
			}

			recipe, err := core.Import(r.Context(), llm, core.HTTPRequest, importURL)
			if err != nil {
				slog.Error(err.Error())
				return recipeResponse{Error: err.Error(), StatusCode: http.StatusInternalServerError}
			}
			if recipe != nil {
				name = recipe.Name
				body = recipe.Body
			}
		}
	}
	return recipeResponse{Name: name, Body: body}
}

func handleRecipePost(s core.State, r *http.Request, prevFilename string) recipeResponse {
	if err := r.ParseForm(); err != nil {
		slog.Error(err.Error())
		return recipeResponse{Error: "Bad Request: " + err.Error(), StatusCode: http.StatusBadRequest}
	}

	name := filepath.Base(r.FormValue("name"))
	body := r.FormValue("body")

	if name == "." || name == string(filepath.Separator) {
		return recipeResponse{Body: body, Error: "Name is required", StatusCode: http.StatusBadRequest}
	}

	filename := name + core.RecipeExt
	fp := filepath.Join(s.Config.Server.RecipesPath, filename)

	writeFn := ExclusiveWriteFile
	if filename == prevFilename {
		writeFn = os.WriteFile
	}

	if err := writeFn(fp, []byte(body), 0644); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return recipeResponse{
				Name:       name,
				Body:       body,
				Error:      "A recipe with the name already exists.",
				StatusCode: http.StatusConflict}
		} else {
			slog.Error(err.Error())
			return recipeResponse{
				Name:       name,
				Body:       body,
				Error:      "Unexpected Error: " + err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		}
	}

	if prevFilename != "" && prevFilename != filename {
		prevFp := filepath.Join(s.Config.Server.RecipesPath, prevFilename)
		if err := os.Remove(prevFp); err != nil {
			slog.Error(err.Error())
			return recipeResponse{
				Name:       name,
				Body:       body,
				Error:      "Unexpected Error: " + err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		}
	}

	escapedPath := url.PathEscape(core.NameToWebpath(name))

	return recipeResponse{RedirectPath: escapedPath}
}

type recipeTemplateData struct {
	baseContext
	recipeResponse
	CsrfField template.HTML
	Title     string
	CancelUrl string
	DeleteUrl string
}

func writeRecipeResponse(
	w http.ResponseWriter,
	r *http.Request,
	recipeFormTemplate *template.Template,
	data recipeTemplateData,
) {
	isHtmx, _ := htmx(r)
	switch {
	case isHtmx && data.Error != "":
		http.Error(w, data.Error, data.StatusCode)
	case isHtmx && data.RedirectPath != "":
		w.Header().Set("HX-Location", "/recipe/"+data.RedirectPath)
		w.WriteHeader(http.StatusOK)
	case !isHtmx && data.RedirectPath != "":
		w.Header().Set("Location", "/recipe/"+data.RedirectPath)
		w.WriteHeader(http.StatusSeeOther)
	default:
		if err := recipeFormTemplate.Execute(w, data); err != nil {
			slog.Error(err.Error())
		}
	}
}

func recipeResponseError(bc baseContext, e string, msg string, statusCode int) recipeTemplateData {
	err := e
	if msg != "" {
		err = err + ": " + msg
	}
	return recipeTemplateData{
		baseContext: bc,
		recipeResponse: recipeResponse{
			Error:      err,
			StatusCode: statusCode,
		},
		Title: e,
	}
}
