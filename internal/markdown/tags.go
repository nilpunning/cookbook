package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type TagsNode struct {
	ast.BaseInline
	Tags []string
}

var KindTagsNode = ast.NewNodeKind("TagsNode")

func (n *TagsNode) Kind() ast.NodeKind {
	return KindTagsNode
}

func (n *TagsNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type tagsParser struct{}

func NewTagsParser() parser.InlineParser {
	return &tagsParser{}
}

func (t *tagsParser) Trigger() []byte {
	return []byte{' '}
}

var TagsContextKey = parser.NewContextKey()

func (t *tagsParser) Parse(parent ast.Node, reader text.Reader, pc parser.Context) ast.Node {
	line, _ := reader.PeekLine()

	if parent != nil && !bytes.HasPrefix(line, []byte("tags:")) {
		return nil
	}

	reader.AdvanceLine()

	tags := strings.Split(string(line[5:]), ",")
	for i := range tags {
		tags[i] = strings.TrimSpace(tags[i])
	}

	pc.Set(TagsContextKey, tags)

	return &TagsNode{
		BaseInline: ast.BaseInline{},
		Tags:       tags,
	}
}

type TagsRenderer struct {
	html.Config
}

func NewTagRenderer() renderer.NodeRenderer {
	return &TagsRenderer{
		Config: html.NewConfig(),
	}
}

func (r *TagsRenderer) renderTags(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<div class=\"tags\">")
		for _, tag := range n.(*TagsNode).Tags {
			_, _ = w.WriteString("<span class=\"tag\">" + tag + " </span>")
		}
	} else {
		_, _ = w.WriteString("</div>")
	}
	return ast.WalkSkipChildren, nil
}

func (r *TagsRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindTagsNode, r.renderTags)
}

type tags struct{}

var Tags = &tags{}

func (e *tags) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(util.Prioritized(NewTagsParser(), 100)),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(util.Prioritized(NewTagRenderer(), 100)),
	)
}
