package markdown

import (
	"io"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
)

func NewTextRenderer() renderer.Renderer {
	return &TextRenderer{
		// lineWidth: 80, // default line width
	}
}

type TextRenderer struct {
	// lineWidth int
}

func (r *TextRenderer) Render(w io.Writer, source []byte, node ast.Node) error {
	return ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			return ast.WalkContinue, nil
		}

		switch n.Type() {
		case ast.TypeBlock:
			if n.Lines().Len() > 0 {
				if _, err := w.Write([]byte("\n")); err != nil {
					return ast.WalkStop, err
				}
			}
			if n.Kind() == ast.KindFencedCodeBlock {
				for i := 0; i < n.Lines().Len(); i++ {
					line := n.Lines().At(i)
					if _, err := w.Write(line.Value(source)); err != nil {
						return ast.WalkStop, err
					}
				}
			}
		case ast.TypeInline:
			switch n.Kind() {
			case ast.KindText:
				text := n.(*ast.Text)
				if _, err := w.Write(text.Segment.Value(source)); err != nil {
					return ast.WalkStop, err
				}
				if text.SoftLineBreak() {
					if _, err := w.Write([]byte("\n")); err != nil {
						return ast.WalkStop, err
					}
				}
			case ast.KindLink:
				link := n.(*ast.Link)

				if _, err := w.Write([]byte(" (" + string(link.Destination) + ")")); err != nil {
					return ast.WalkStop, err
				}
			}
		}

		return ast.WalkContinue, nil
	})
}

func (r *TextRenderer) AddOptions(...renderer.Option) {
	// No options needed for basic text rendering
}

// RegisterFuncs implements renderer.NodeRenderer.RegisterFuncs
func (r *TextRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// Registration is handled in the Render method
}
