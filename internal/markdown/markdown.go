package markdown

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

func New() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM, Tags),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
}

func ConvertToHtml(md []byte) (string, []string, error) {
	var html bytes.Buffer
	pc := parser.NewContext()
	if err := New().Convert(md, &html, parser.WithContext(pc)); err != nil {
		return "", nil, err
	}

	tags := []string{}
	if t := pc.Get(TagsContextKey); t != nil {
		tags = t.([]string)
	}

	if len(tags) == 0 {
		tags = []string{"Other"}
	}

	return html.String(), tags, nil
}
