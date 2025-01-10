package search

import (
	"fmt"
	"html/template"
	"log"
	"sort"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"

	index "github.com/blevesearch/bleve_index_api"
)

// todo: is this even necessary?
type Recipe struct {
	Filename string   `json:"filename"`
	Name     string   `json:"name"`
	Webpath  string   `json:"webpath"`
	HTML     string   `json:"html"`
	Markdown string   `json:"markdown"`
	Tags     []string `json:"tags"`
}

func NewIndex() bleve.Index {
	recipeMapping := bleve.NewDocumentMapping()

	keywordMapping := bleve.NewKeywordFieldMapping()
	recipeMapping.AddFieldMappingsAt("filename", keywordMapping)
	recipeMapping.AddFieldMappingsAt("webpath", keywordMapping)
	recipeMapping.AddFieldMappingsAt("html", keywordMapping)
	recipeMapping.AddFieldMappingsAt("tags", keywordMapping)

	englishMapping := bleve.NewTextFieldMapping()
	englishMapping.Analyzer = "en"
	recipeMapping.AddFieldMappingsAt("name", englishMapping)
	recipeMapping.AddFieldMappingsAt("markdown", englishMapping)

	mapping := bleve.NewIndexMapping()
	mapping.AddDocumentMapping("recipe", recipeMapping)

	idx, err := bleve.NewMemOnly(mapping)
	if err != nil {
		log.Fatal(err)
	}
	return idx
}

func UpsertRecipe(index bleve.Index, filename, name, webpath, html, markdown string, tags []string) error {
	recipe := Recipe{
		Filename: filename,
		Name:     name,
		Webpath:  webpath,
		HTML:     html,
		Markdown: markdown,
		Tags:     tags,
	}
	return index.Index(webpath, recipe)
}

func DeleteRecipe(index bleve.Index, webpath string) error {
	fmt.Println("Deleting", webpath)
	return index.Delete(webpath)
}

// todo: refactor?
func GetRecipe(idx bleve.Index, webpath string) (string, string, string, error) {
	doc, err := idx.Document(webpath)
	if err != nil {
		return "", "", "", err
	}

	var filename, name, html string

	doc.VisitFields(func(field index.Field) {
		switch field.Name() {
		case "filename":
			filename = string(field.Value())
		case "name":
			name = string(field.Value())
		case "html":
			html = string(field.Value())
		}
	})

	return filename, name, html, nil
}

type RecipesGroupedByTag struct {
	TagName string
	Recipes []map[string]string
}

func GetRecipesGroupedByTag(index bleve.Index) ([]RecipesGroupedByTag, error) {
	query := bleve.NewMatchAllQuery()
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Fields = []string{"name", "webpath", "tags"}
	searchRequest.Size = 1000

	searchResults, err := index.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	// Group recipes by tags
	tagMap := make(map[string][]map[string]string)
	addTag := func(hit *search.DocumentMatch, tag string) {
		tagName := strings.TrimSpace(tag)
		recipe := map[string]string{
			"Name":    hit.Fields["name"].(string),
			"Webpath": hit.Fields["webpath"].(string),
		}
		tagMap[tagName] = append(tagMap[tagName], recipe)
	}

	for _, hit := range searchResults.Hits {
		switch tags := hit.Fields["tags"].(type) {
		case string:
			addTag(hit, tags)
		case []interface{}:
			for _, tag := range tags {
				addTag(hit, tag.(string))
			}
		}
	}

	// Convert map to slice and sort
	// TODO: can this be done in the original query?
	var result []RecipesGroupedByTag
	for tag, recipes := range tagMap {
		result = append(result, RecipesGroupedByTag{
			TagName: tag,
			Recipes: recipes,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TagName < result[j].TagName
	})

	return result, nil
}

type SearchResult struct {
	Name    template.HTML
	Webpath string
	Snippet template.HTML
}

func cleanSnippet(snippet template.HTML) template.HTML {
	lines := strings.Split(string(snippet), "\n")

	trimmedLines := []string{}
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l != "" {
			trimmedLines = append(trimmedLines, l)
		}
	}

	n := 4
	start := 0
	end := len(trimmedLines) - 1

	for start < len(trimmedLines) && len(trimmedLines[start]) < n {
		start++
	}

	for end >= 0 && len(trimmedLines[end]) < n {
		end--
	}

	if start <= end {
		return template.HTML(strings.Join(trimmedLines[start:end+1], "\n"))
	}

	return snippet
}

func SearchRecipes(index bleve.Index, query string) ([]SearchResult, error) {
	searchQuery := bleve.NewQueryStringQuery(query)
	searchRequest := bleve.NewSearchRequest(searchQuery)
	searchRequest.Fields = []string{"name", "webpath", "markdown"}
	// todo: make this highligher prettier, like cleanSnippet, but using bleve's interfaces
	highlight := bleve.NewHighlight()
	highlight.AddField("name")
	highlight.AddField("markdown")

	searchRequest.Highlight = highlight

	results, err := index.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	searchResults := make([]SearchResult, 0, len(results.Hits))
	for _, hit := range results.Hits {
		name := hit.Fields["name"].(string)
		if fragments, exists := hit.Fragments["name"]; exists && len(fragments) > 0 {
			name = fragments[0]
		}

		snippet := ""
		if fragments, exists := hit.Fragments["markdown"]; exists && len(fragments) > 0 {
			snippet = fragments[0]
		}

		searchResults = append(searchResults, SearchResult{
			Name:    template.HTML(name),
			Webpath: hit.Fields["webpath"].(string),
			Snippet: template.HTML(snippet),
		})
	}

	return searchResults, nil
}
