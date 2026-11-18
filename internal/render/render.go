// Package render turns page markdown into HTML shaped like DokuWiki's
// output (section divs, table wrappers, link classes), so the existing
// layouts' CSS applies unchanged.
package render

import (
	"bytes"
	"html"
	"strings"

	"github.com/Castux/lodge-wiki/internal/wiki"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
	"gopkg.in/yaml.v3"
)

// Heading is a section heading of a rendered page.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// Result is a rendered page.
type Result struct {
	HTML     string
	TOC      string // empty when no table of contents is shown
	Headings []Heading
	Widgets  []string // widget names used on the page
}

// Renderer renders pages of one store.
type Renderer struct {
	Store *wiki.Store
}

type ctxKey struct{}

type pageCtx struct {
	r       *Renderer
	id      string
	widgets []string
}

// Render renders a page body.
func (r *Renderer) Render(p *wiki.Page) Result {
	pc := &pageCtx{r: r, id: p.ID}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote),
		goldmark.WithParserOptions(parser.WithAttribute()),
		goldmark.WithRendererOptions(
			gmhtml.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(&blockRenderer{pc: pc}, 100)),
		),
	)
	var buf bytes.Buffer
	ctx := parser.NewContext()
	if err := md.Convert(p.Body, &buf, parser.WithContext(ctx)); err != nil {
		return Result{HTML: "<p>Render error: " + html.EscapeString(err.Error()) + "</p>"}
	}
	out, heads := postProcess(p.ID, buf.String(), r.Store)
	res := Result{HTML: out, Headings: heads, Widgets: pc.widgets}
	if len(heads) >= 3 {
		res.TOC = buildTOC(heads)
	}
	return res
}

// RenderSnippet renders markdown in the context of a page (for previews).
func (r *Renderer) RenderSnippet(id string, body []byte) Result {
	return r.Render(wiki.ParsePage(id, body))
}

// blockRenderer handles fenced code blocks: `list` and `widget` blocks
// become wiki features; everything else becomes DokuWiki-style <pre>.
type blockRenderer struct{ pc *pageCtx }

func (b *blockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, b.fenced)
	reg.Register(ast.KindCodeBlock, b.indented)
	reg.Register(ast.KindParagraph, b.paragraph)
}

func codeText(n ast.Node, src []byte) string {
	var sb strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(src))
	}
	return sb.String()
}

func (b *blockRenderer) fenced(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	fc := n.(*ast.FencedCodeBlock)
	info := ""
	if fc.Info != nil {
		info = strings.TrimSpace(string(fc.Info.Segment.Value(src)))
	}
	lang, rest, _ := strings.Cut(info, " ")
	body := codeText(n, src)
	switch lang {
	case "list":
		w.WriteString(b.pc.renderList(body))
		return ast.WalkSkipChildren, nil
	case "widget":
		name := strings.TrimSpace(rest)
		if name == "" {
			break
		}
		var cfg map[string]any
		_ = yaml.Unmarshal([]byte(body), &cfg)
		js, _ := jsonMarshal(cfg)
		b.pc.widgets = append(b.pc.widgets, name)
		w.WriteString(`<div class="widget widget-` + html.EscapeString(name) + `" data-widget="` + html.EscapeString(name) + `" data-config="` + html.EscapeString(js) + `"></div>` + "\n")
		return ast.WalkSkipChildren, nil
	}
	cls := "code"
	if lang != "" {
		cls += " " + html.EscapeString(lang)
	}
	w.WriteString(`<pre class="` + cls + `">` + html.EscapeString(body) + "</pre>\n")
	return ast.WalkSkipChildren, nil
}

func (b *blockRenderer) indented(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		w.WriteString(`<pre class="code">` + html.EscapeString(codeText(n, src)) + "</pre>\n")
	}
	return ast.WalkSkipChildren, nil
}
