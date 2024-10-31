package main

import (
	"bytes"
	"database/sql"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/fsnotify/fsnotify"
	_ "github.com/mattn/go-sqlite3"

	"hallertau/internal/markdown"
)

type State struct {
	db          *sql.DB
	recipesPath string
	recipeExt   string
}

func (s *State) isRecipe(entry fs.FileInfo) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), s.recipeExt)
}

func (s *State) upsertRecipe(filename string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		var name = strings.TrimSuffix(entry.Name(), s.recipeExt)
		var title = cases.Title(language.English, cases.Compact).String(name)
		gMarkdown := goldmark.New(
			goldmark.WithExtensions(extension.GFM, markdown.Tags),
			goldmark.WithRendererOptions(html.WithHardWraps()),
		)

		file, err := os.DirFS(s.recipesPath).Open(filename)
		if err != nil {
			log.Println("Error opening recipe file:", err)
			return
		}
		var md bytes.Buffer
		if _, err = md.ReadFrom(file); err != nil {
			log.Println("Error reading recipe file:", err)
			return
		}
		var html bytes.Buffer
		pc := parser.NewContext()
		if err := gMarkdown.Convert(md.Bytes(), &html, parser.WithContext(pc)); err != nil {
			log.Println("Error converting recipe file:", err)
			return
		}

		tags := []string{}
		if t := pc.Get(markdown.TagsContextKey); t != nil {
			tags = t.([]string)
		}

		webpath := strings.ReplaceAll(title, " ", "") + ".html"

		tx, err := s.db.Begin()
		if err != nil {
			log.Println("Error beginning transaction:", err)
			return
		}
		if _, err := tx.Exec(`
				INSERT INTO recipe (filename, name, webpath, html)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(filename) DO UPDATE SET name = ?, webpath = ?, html = ?;
				DELETE FROM recipe_tag WHERE recipe_filename = ?
			`,
			filename, name, webpath, html.String(),
			name, webpath, html.String(),
			filename); err != nil {
			log.Printf("Error inserting recipe %s: %v", filename, err)
			tx.Rollback()
			return
		}

		for _, tag := range tags {
			if _, err := tx.Exec(`
				INSERT INTO recipe_tag (recipe_filename, tag_name) VALUES (?, ?)
				ON CONFLICT(recipe_filename, tag_name) DO NOTHING
			`, filename, tag); err != nil {
				log.Printf("Error inserting recipe tag %s: %v", tag, err)
				tx.Rollback()
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Error committing transaction for recipe %s: %v", filename, err)
		}
	}
}

func (s *State) deleteRecipe(path string) {
	_, err := s.db.Exec(`
		DELETE FROM recipe WHERE filename = ?
	`, path)
	if err != nil {
		log.Printf("Error deleting recipe %s: %v", path, err)
	}
}

func (s *State) loadRecipes() {
	entries, err := os.ReadDir(s.recipesPath)
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range entries {
		var info, err = entry.Info()
		if err != nil {
			log.Fatal(err)
		}
		s.upsertRecipe(entry.Name(), info)
	}
}

func (s *State) monitorRecipesDirectory() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = watcher.Add(s.recipesPath)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			filename := filepath.Base(event.Name)
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				entry, err := os.Stat(event.Name)
				if err != nil {
					log.Fatal(err)
				}
				s.upsertRecipe(filename, entry)
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				s.deleteRecipe(filename)
			}
			log.Println("Event:", event)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("Error:", err)
		}
	}
}

func main() {
	// Recipes path must be a folder that exists, if it doesn't exist or is deleted after the
	// program starts, recipe changes will not be monitored.
	log.Println("Recipes path:", os.Args[1])

	// Open SQLite database
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create recipe table
	_, err = db.Exec(`
		CREATE TABLE recipe (
			filename TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			webpath TEXT NOT NULL,
			html TEXT NOT NULL
		);
		CREATE INDEX idx_recipe_webpath ON recipe (webpath);
		CREATE TABLE recipe_tag (
			recipe_filename TEXT NOT NULL,
			tag_name TEXT NOT NULL,
			PRIMARY KEY (recipe_filename, tag_name)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	var state = State{
		db:          db,
		recipesPath: os.Args[1],
		recipeExt:   ".md",
	}

	state.loadRecipes()
	go state.monitorRecipesDirectory()

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	indexTemplate := template.Must(template.ParseFiles("templates/base.html", "templates/index.html"))
	recipeTemplate := template.Must(template.ParseFiles("templates/base.html", "templates/recipe.html"))

	// Serve the Go template
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		tx, err := state.db.Begin()
		if err != nil {
			log.Println("Error beginning transaction:", err)
			return
		}
		defer tx.Commit()

		tagRows, err := tx.Query(`
			SELECT DISTINCT tag_name FROM recipe_tag ORDER BY tag_name
		`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tagRows.Close()

		type tag struct {
			Tag     string
			Recipes []map[string]string
		}
		var tags []tag
		for tagRows.Next() {
			t := tag{}
			err := tagRows.Scan(&t.Tag)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			tags = append(tags, t)
		}
		if err = tagRows.Err(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i, tag := range tags {
			recipeRows, err := tx.Query(`
				SELECT name, webpath FROM recipe
				JOIN recipe_tag ON recipe.filename = recipe_tag.recipe_filename
				WHERE recipe_tag.tag_name = ?
				ORDER BY name
			`, tag.Tag)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer recipeRows.Close()

			tag.Recipes = []map[string]string{}
			for recipeRows.Next() {
				var name, webpath string
				err := recipeRows.Scan(&name, &webpath)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				tag.Recipes = append(tag.Recipes, map[string]string{
					"Name":    name,
					"Webpath": webpath,
				})
			}
			if err = recipeRows.Err(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			tags[i] = tag
		}

		if err := indexTemplate.Execute(w, map[string]any{
			"Title": "Recipes",
			"Tags":  tags,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/recipe/{path}", func(w http.ResponseWriter, r *http.Request) {
		webpath := r.PathValue("path")

		row := state.db.QueryRow(`
			SELECT filename, name, html FROM recipe WHERE webpath = ?
		`, webpath)

		var filename, name, html string
		switch err := row.Scan(&filename, &name, &html); err {
		case sql.ErrNoRows:
			http.Error(w, "Recipe not found", http.StatusNotFound)
		case nil:
			data := struct {
				Title string
				Name  string
				Body  template.HTML
			}{
				Title: "Recipes",
				Name:  name,
				Body:  template.HTML(html),
			}
			if err := recipeTemplate.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Start the web server
	log.Println("Server starting on http://localhost:8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
