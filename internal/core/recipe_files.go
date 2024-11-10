package core

import (
	"bytes"
	"hallertau/internal/database"
	"hallertau/internal/markdown"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var recipeExt = ".md"

func nameToWebpath(name string) string {
	title := cases.Title(language.English, cases.Compact).String(name)
	return strings.ReplaceAll(title, " ", "") + ".html"
}

func (s *State) isRecipe(entry fs.FileInfo) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), recipeExt)
}

func (s *State) upsertRecipe(filename string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		var name = strings.TrimSuffix(entry.Name(), recipeExt)

		file, err := os.DirFS(s.Config.Server.RecipesPath).Open(filename)
		if err != nil {
			log.Println("Error opening recipe file:", err)
			return
		}
		var md bytes.Buffer
		if _, err = md.ReadFrom(file); err != nil {
			log.Println("Error reading recipe file:", err)
			return
		}
		html, tags, err := markdown.Convert(md.Bytes())
		if err != nil {
			log.Println("Error converting recipe file:", err)
			return
		}
		database.UpsertRecipe(s.DB, filename, name, nameToWebpath(name), html, tags)
	}
}

func (s *State) LoadRecipes() {
	entries, err := os.ReadDir(s.Config.Server.RecipesPath)
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

func (s *State) MonitorRecipesDirectory() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = watcher.Add(s.Config.Server.RecipesPath)
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
				database.DeleteRecipe(s.DB, filename)
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
