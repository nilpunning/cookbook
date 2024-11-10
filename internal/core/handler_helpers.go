package core

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

func handleEditRecipe(s State, w http.ResponseWriter, r *http.Request, prevFilename string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := filepath.Base(r.FormValue("name"))
	body := r.FormValue("body")

	if name == "." || name == string(filepath.Separator) {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	filename := name + recipeExt
	fp := filepath.Join(s.Config.Server.RecipesPath, filename)

	if err := os.WriteFile(fp, []byte(body), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if prevFilename != "" && prevFilename != filename {
		prevFp := filepath.Join(s.Config.Server.RecipesPath, prevFilename)
		if err := os.Remove(prevFp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	escapedPath := url.PathEscape(nameToWebpath(name))
	http.Redirect(w, r, "/recipe/"+escapedPath, http.StatusSeeOther)
}
