package matrix

import (
	"bytes"
	"html"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// markdown renders the Bot's Markdown as the HTML a Matrix message carries
// beside its plain body. Tables, strikethrough and bare links are what models
// write beyond CommonMark; a single newline is a line break, as in any chat.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.Linkify),
	goldmark.WithRendererOptions(
		gmhtml.WithHardWraps(),
		renderer.WithNodeRenderers(util.Prioritized(literalHTML{}, 100)),
	),
)

// literalHTML writes HTML found in the Markdown as the text it is. Nothing the
// model writes reaches a client as markup of its own, and "use <name> here"
// keeps its words, which leaving raw HTML out (goldmark's default) would drop.
type literalHTML struct{}

func (literalHTML) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		block := n.(*ast.HTMLBlock)
		var text []byte
		for i := 0; i < block.Lines().Len(); i++ {
			line := block.Lines().At(i)
			text = append(text, line.Value(src)...)
		}
		if block.HasClosure() {
			text = append(text, block.ClosureLine.Value(src)...)
		}
		_, _ = w.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(strings.TrimSpace(string(text))), "\n", "<br>\n") + "</p>\n")
		return ast.WalkSkipChildren, nil
	})
	reg.Register(ast.KindRawHTML, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		segs := n.(*ast.RawHTML).Segments
		for i := 0; i < segs.Len(); i++ {
			seg := segs.At(i)
			_, _ = w.WriteString(html.EscapeString(string(seg.Value(src))))
		}
		return ast.WalkSkipChildren, nil
	})
}

// toHTML is a message's formatted_body. Text that cannot be rendered goes as
// itself, escaped.
func toHTML(md string) string {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(md), &buf); err != nil {
		return strings.ReplaceAll(html.EscapeString(md), "\n", "<br>")
	}
	return strings.TrimSpace(buf.String())
}
