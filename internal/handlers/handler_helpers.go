package handlers

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"hallertau/internal/core"
)

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

func handleEditRecipe(s core.State, w http.ResponseWriter, r *http.Request, prevFilename string) {
	if err := r.ParseForm(); err != nil {
		slog.Error(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := filepath.Base(r.FormValue("name"))
	body := r.FormValue("body")

	if name == "." || name == string(filepath.Separator) {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	filename := name + core.RecipeExt
	fp := filepath.Join(s.Config.Server.RecipesPath, filename)

	writeFn := ExclusiveWriteFile
	if filename == prevFilename {
		writeFn = os.WriteFile
	}

	if err := writeFn(fp, []byte(body), 0644); err != nil {
		if errors.Is(err, fs.ErrExist) {
			http.Error(w, "A recipe with the name already exists.", http.StatusConflict)
		} else {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if prevFilename != "" && prevFilename != filename {
		prevFp := filepath.Join(s.Config.Server.RecipesPath, prevFilename)
		if err := os.Remove(prevFp); err != nil {
			slog.Error(err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	escapedPath := url.PathEscape(core.NameToWebpath(name))

	w.Header().Set("HX-Redirect", "/recipe/"+escapedPath)
	w.WriteHeader(http.StatusOK)
}
