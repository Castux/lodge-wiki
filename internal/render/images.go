package render

import (
	"bytes"
	"html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

// Image layout. Authors write plain markdown images; the layout comes
// from where the image is and from an optional preset in the address's
// fragment (see presetClasses):
//
//	![Alt](/_media/a.jpg)               alone in its paragraph: <p class="pic">
//	![Alt](/_media/a.jpg "Caption")     alone, with a title: <figure> with <figcaption>
//	![A](/_media/a.jpg) ![B](/_media/b.jpg)   images only: <p class="gallery">
//	![Alt](/_media/a.jpg#small-right)   presets: small, medium, full, left, right

// imagePresets are the layout presets allowed in an image's #fragment.
var imagePresets = map[string]bool{"small": true, "medium": true, "full": true, "left": true, "right": true}

// presetClasses parses a fragment such as "small-right" into classes; ok
// is false when it is not made only of presets (then it is left alone).
func presetClasses(frag string) (classes []string, ok bool) {
	if frag == "" {
		return nil, false
	}
	for _, p := range strings.Split(strings.ToLower(frag), "-") {
		if !imagePresets[p] {
			return nil, false
		}
		classes = append(classes, p)
	}
	return classes, true
}

// paragraphImages returns the images of a paragraph made only of images
// (and spaces or line breaks between them), or nil.
func paragraphImages(n ast.Node, src []byte) []*ast.Image {
	var imgs []*ast.Image
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Image:
			imgs = append(imgs, c)
		case *ast.Text:
			if len(bytes.TrimSpace(c.Segment.Value(src))) != 0 {
				return nil
			}
		default:
			return nil
		}
	}
	return imgs
}

func (b *blockRenderer) paragraph(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	imgs := paragraphImages(n, src)
	if len(imgs) == 1 && len(imgs[0].Title) > 0 {
		if entering {
			img := imgs[0]
			w.WriteString(`<figure><img src="` + string(util.EscapeHTML(util.URLEscape(img.Destination, true))) +
				`" alt="` + html.EscapeString(string(nodeText(img, src))) + `"><figcaption>` +
				html.EscapeString(string(img.Title)) + "</figcaption></figure>\n")
		}
		return ast.WalkSkipChildren, nil
	}
	if !entering {
		w.WriteString("</p>\n")
		return ast.WalkContinue, nil
	}
	switch {
	case len(imgs) == 1:
		w.WriteString(`<p class="pic">`)
	case len(imgs) > 1:
		w.WriteString(`<p class="gallery">`)
	default:
		w.WriteString("<p>")
	}
	return ast.WalkContinue, nil
}

// nodeText is the plain text inside a node (an image's alt text).
func nodeText(n ast.Node, src []byte) []byte {
	var b bytes.Buffer
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := c.(*ast.Text); ok && entering {
			b.Write(t.Segment.Value(src))
		}
		return ast.WalkContinue, nil
	})
	return b.Bytes()
}
