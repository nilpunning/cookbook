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
	Title        string
	Error        string
	StatusCode   int
	RedirectPath string
}

type recipeResponse struct {
	response
	Name string
	Body string
}

func errorResponse(statusCode int, msg string) response {
	statusText := http.StatusText(statusCode)
	errMsg := statusText
	if msg != "" {
		errMsg = errMsg + ": " + msg
	}

	return response{
		Title:      statusText,
		Error:      errMsg,
		StatusCode: statusCode,
	}
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
				return recipeResponse{response: errorResponse(http.StatusInternalServerError, err.Error())}
			}

			recipe, err := core.Import(r.Context(), llm, core.HTTPRequest, importURL)
			if err != nil {
				slog.Error(err.Error())
				return recipeResponse{response: errorResponse(http.StatusInternalServerError, err.Error())}
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
		return recipeResponse{response: errorResponse(http.StatusBadRequest, err.Error())}
	}

	name := filepath.Base(r.FormValue("name"))
	body := r.FormValue("body")
	delete := r.Form.Has("delete")

	prevFp := filepath.Join(s.Config.Server.RecipesPath, prevFilename)

	if delete {
		if err := os.Remove(prevFp); err != nil {
			slog.Error(err.Error())
			return recipeResponse{response: errorResponse(http.StatusInternalServerError, err.Error()), Name: name, Body: body}
		}
		return recipeResponse{response: response{RedirectPath: "/"}}
	}

	if name == "." || name == string(filepath.Separator) {
		return recipeResponse{response: errorResponse(http.StatusBadRequest, "name is required"), Body: body}
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
				response: errorResponse(http.StatusConflict, "A recipe with the name already exists."),
				Name:     name,
				Body:     body,
			}
		} else {
			slog.Error(err.Error())
			return recipeResponse{
				response: errorResponse(http.StatusInternalServerError, err.Error()),
				Name:     name,
				Body:     body,
			}
		}
	}

	if prevFilename != "" && prevFilename != filename {
		if err := os.Remove(prevFp); err != nil {
			slog.Error(err.Error())
			return recipeResponse{
				response: errorResponse(http.StatusInternalServerError, err.Error()),
				Name:     name,
				Body:     body,
			}
		}
	}

	escapedPath := url.PathEscape(core.NameToWebpath(name))

	return recipeResponse{response: response{RedirectPath: "/recipe/" + escapedPath}}
}

type responser interface {
	getResponse() response
}

func (r response) getResponse() response {
	return r
}

type recipeTemplateData struct {
	stateData
	recipeResponse
	CsrfField  template.HTML
	CancelUrl  string
	ShowDelete bool
}

type importTemplateData struct {
	stateData
	response
	CsrfField template.HTML
	CancelUrl string
}

type recipePathDeleteTemplateData struct {
	stateData
	response
	CsrfField template.HTML
	Name      string
	Webpath   string
}

func writeResponse(
	w http.ResponseWriter,
	r *http.Request,
	template *template.Template,
	data responser,
) {
	isHtmx, _ := htmx(r)
	resp := data.getResponse()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
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
