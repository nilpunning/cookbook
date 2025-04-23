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

type response struct {
	Error        string
	StatusCode   int
	RedirectPath string
}

type recipeResponse struct {
	response
	Name string
	Body string
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
				return recipeResponse{response: response{Error: err.Error(), StatusCode: http.StatusInternalServerError}}
			}

			recipe, err := core.Import(r.Context(), llm, core.HTTPRequest, importURL)
			if err != nil {
				slog.Error(err.Error())
				return recipeResponse{response: response{Error: err.Error(), StatusCode: http.StatusInternalServerError}}
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
		return recipeResponse{response: response{Error: "Bad Request: " + err.Error(), StatusCode: http.StatusBadRequest}}
	}

	name := filepath.Base(r.FormValue("name"))
	body := r.FormValue("body")

	if name == "." || name == string(filepath.Separator) {
		return recipeResponse{response: response{Error: "Name is required", StatusCode: http.StatusBadRequest}, Body: body}
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
				response: response{
					Error:      "A recipe with the name already exists.",
					StatusCode: http.StatusConflict,
				},
				Name: name,
				Body: body,
			}
		} else {
			slog.Error(err.Error())
			return recipeResponse{
				response: response{
					Error:      "Unexpected Error: " + err.Error(),
					StatusCode: http.StatusInternalServerError,
				},
				Name: name,
				Body: body,
			}
		}
	}

	if prevFilename != "" && prevFilename != filename {
		prevFp := filepath.Join(s.Config.Server.RecipesPath, prevFilename)
		if err := os.Remove(prevFp); err != nil {
			slog.Error(err.Error())
			return recipeResponse{
				response: response{
					Error:      "Unexpected Error: " + err.Error(),
					StatusCode: http.StatusInternalServerError,
				},
				Name: name,
				Body: body,
			}
		}
	}

	escapedPath := url.PathEscape(core.NameToWebpath(name))

	return recipeResponse{response: response{RedirectPath: "/recipe/" + escapedPath}}
}

type templateData interface {
	getResponse() response
}

type recipeTemplateData struct {
	baseData
	recipeResponse
	CsrfField template.HTML
	Title     string
	CancelUrl string
	DeleteUrl string
}

func (d recipeTemplateData) getResponse() response {
	return d.recipeResponse.response
}

type importTemplateData struct {
	baseData
	response
	CsrfField template.HTML
	Title     string
	CancelUrl string
}

func (d importTemplateData) getResponse() response {
	return d.response
}

func writeResponse(
	w http.ResponseWriter,
	r *http.Request,
	template *template.Template,
	data templateData,
) {
	isHtmx, _ := htmx(r)
	resp := data.getResponse()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		http.Error(w, resp.Error, resp.StatusCode)
	case isHtmx && resp.Error != "":
		http.Error(w, resp.Error, resp.StatusCode)
	case isHtmx && resp.RedirectPath != "":
		w.Header().Set("HX-Location", resp.RedirectPath)
		w.WriteHeader(http.StatusOK)
	case !isHtmx && resp.RedirectPath != "":
		w.Header().Set("Location", resp.RedirectPath)
		w.WriteHeader(http.StatusSeeOther)
	default:
		if err := template.Execute(w, data); err != nil {
			slog.Error(err.Error())
		}
	}
}

func errMsg(e string, msg string) string {
	errMsg := e
	if msg != "" {
		errMsg = errMsg + ": " + msg
	}
	return errMsg
}

func recipeTemplateDataError(data baseData, statusCode int, e string, msg string) recipeTemplateData {
	return recipeTemplateData{
		baseData: data,
		recipeResponse: recipeResponse{
			response: response{
				Error:      errMsg(e, msg),
				StatusCode: statusCode,
			},
		},
		Title: e,
	}
}

func importTemplateDataError(data baseData, statusCode int, e string, msg string) importTemplateData {
	return importTemplateData{
		baseData: data,
		response: response{
			Error:      errMsg(e, msg),
			StatusCode: statusCode,
		},
		Title: e,
	}
}
