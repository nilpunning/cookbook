package core

import (
	"bytes"
	"cookbook/internal/markdown"
	"cookbook/internal/search"
	"html/template"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var RecipeExt = ".md"

func NameToWebpath(name string) string {
	title := cases.Title(language.English, cases.Compact).String(name)
	return strings.ReplaceAll(title, " ", "")
}

func (s *State) isRecipe(entry fs.FileInfo) bool {
	return !entry.IsDir() && strings.HasSuffix(entry.Name(), RecipeExt)
}

func (s *State) upsertRecipe(filename string, entry fs.FileInfo) {
	if s.isRecipe(entry) {
		var name = strings.TrimSuffix(entry.Name(), RecipeExt)

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
		html, tags, err := markdown.ConvertToHtml(md.Bytes())
		if err != nil {
			log.Println("Error converting recipe file:", err)
			return
		}
		var escapedMarkdown bytes.Buffer
		template.HTMLEscape(&escapedMarkdown, md.Bytes())
		search.UpsertRecipe(s.Index, filename, name, NameToWebpath(name), html, escapedMarkdown.String(), tags)
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
				name := strings.TrimSuffix(filename, RecipeExt)
				search.DeleteRecipe(s.Index, NameToWebpath(name))
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
