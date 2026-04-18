// Package render turns Markdown bodies into safe HTML using goldmark and
// assembles HTML response pages via html/template.
package render

import (
	"bytes"
	"html/template"

	chtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
)

// Markdown wraps a configured goldmark instance; reusable across renders.
type Markdown struct {
	md goldmark.Markdown
}

// NewMarkdown builds the default Markdown renderer: GFM, footnotes, linkify,
// typographer, and chroma code highlighting with a friendly (classes-only)
// theme so CSS owns the actual colours.
func NewMarkdown() *Markdown {
	m := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
			extension.DefinitionList,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithFormatOptions(
					chtml.WithClasses(true),
					chtml.WithLineNumbers(false),
				),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(
			ghtml.WithXHTML(),
		),
	)
	return &Markdown{md: m}
}

// ToHTML renders Markdown source to safe HTML. The output is wrapped in
// template.HTML so templates can inject it without re-escaping — goldmark's
// default renderer escapes HTML in the source, so the resulting string is
// safe for the CSP our middleware enforces.
func (m *Markdown) ToHTML(src string) (template.HTML, error) {
	var buf bytes.Buffer
	if err := m.md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil //nolint:gosec // goldmark output is already HTML-safe
}
