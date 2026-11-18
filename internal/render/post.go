package render

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"path"
	"strings"

	"github.com/Castux/lodge-wiki/internal/wiki"
	nethtml "golang.org/x/net/html"
)

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// SectionID turns a heading text into an anchor id (lowercase, '-' for
// spaces and punctuation), the same slugs the migrated links use.
func SectionID(text string) string {
	id := strings.NewReplacer("/", "", ".", "").Replace(wiki.CleanID(text))
	if id == "" {
		id = "section"
	}
	return id
}

var voidTags = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

func attr(t *nethtml.Token, key string) (string, bool) {
	for _, a := range t.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func hasAttr(t *nethtml.Token, key string) bool {
	_, ok := attr(t, key)
	return ok
}

func setAttr(t *nethtml.Token, key, val string) {
	for i, a := range t.Attr {
		if a.Key == key {
			t.Attr[i].Val = val
			return
		}
	}
	t.Attr = append(t.Attr, nethtml.Attribute{Key: key, Val: val})
}

func delAttr(t *nethtml.Token, key string) {
	out := t.Attr[:0]
	for _, a := range t.Attr {
		if a.Key != key {
			out = append(out, a)
		}
	}
	t.Attr = out
}

func addClass(t *nethtml.Token, cls string) {
	if c, ok := attr(t, "class"); ok && c != "" {
		setAttr(t, "class", c+" "+cls)
	} else {
		setAttr(t, "class", cls)
	}
}

func hasClass(t *nethtml.Token, cls string) bool {
	c, _ := attr(t, "class")
	for _, f := range strings.Fields(c) {
		if f == cls {
			return true
		}
	}
	return false
}

// postProcess finishes goldmark's HTML: heading ids, link classes
// (missing pages, external sites), scaled and linked images, scrollable
// tables. It returns the headings found at the top level, for the TOC.
func postProcess(pageID, src string, store *wiki.Store) (string, []Heading) {
	z := nethtml.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	var heads []Heading
	used := map[string]int{}

	depth, inLink := 0, 0
	var head *nethtml.Token // top-level heading being read
	var headBuf, headText strings.Builder

	write := func(s string) {
		if head != nil {
			headBuf.WriteString(s)
		} else {
			out.WriteString(s)
		}
	}

	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			break
		}
		raw := string(z.Raw())
		switch tt {
		case nethtml.TextToken:
			if head != nil {
				headText.WriteString(html.UnescapeString(raw))
			}
			write(raw)

		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			t := z.Token()
			name := t.Data
			switch {
			case depth == 0 && head == nil && len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6':
				tok := t
				head = &tok
				headBuf.Reset()
				headText.Reset()
			case name == "a":
				inLink++
				fixLink(pageID, &t, store)
				write(t.String())
			case name == "img" && wiki.MediaPath(attrOr(&t, "src")) != "":
				full := "/_media/" + wiki.MediaPath(attrOr(&t, "src"))
				fixImage(&t)
				if inLink == 0 {
					// Clicking an image opens the full-size file.
					write(`<a href="` + html.EscapeString(full) + `" class="image" target="_blank">` + t.String() + `</a>`)
				} else {
					write(t.String())
				}
			case name == "table":
				write(`<div class="table-wrap">` + raw)
			default:
				write(raw)
			}
			if !voidTags[name] && tt != nethtml.SelfClosingTagToken {
				depth++
			}

		case nethtml.EndTagToken:
			name, _ := z.TagName()
			n := string(name)
			if !voidTags[n] {
				depth--
			}
			switch {
			case head != nil && depth == 0 && n == head.Data:
				text := strings.TrimSpace(headText.String())
				id := attrOr(head, "id")
				if id == "" {
					id = SectionID(text)
				}
				if c, seen := used[id]; seen {
					used[id] = c + 1
					id = fmt.Sprintf("%s%d", id, c+1)
				} else {
					used[id] = 0
				}
				setAttr(head, "id", id)
				out.WriteString(head.String() + headBuf.String() + "</" + head.Data + ">")
				heads = append(heads, Heading{Level: int(head.Data[1] - '0'), ID: id, Text: text})
				head = nil
			case n == "a":
				if inLink > 0 {
					inLink--
				}
				write(raw)
			case n == "table":
				write(raw + "</div>")
			default:
				write(raw)
			}

		default:
			write(raw)
		}
	}
	return out.String(), heads
}

func attrOr(t *nethtml.Token, key string) string {
	v, _ := attr(t, key)
	return v
}

// fixLink classifies a link: external sites get "external" (and open
// with rel=nofollow), links to missing pages get "missing", internal links
// are normalised to the page's canonical URL.
func fixLink(pageID string, t *nethtml.Token, store *wiki.Store) {
	href, ok := attr(t, "href")
	if !ok || href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") {
		return
	}
	if c, _ := attr(t, "class"); strings.Contains(c, "footnote") {
		return
	}
	if wiki.MediaPath(href) != "" {
		addClass(t, "file")
		return
	}
	u, err := url.Parse(href)
	if err != nil {
		return
	}
	if u.Scheme != "" || u.Host != "" {
		addClass(t, "external")
		setAttr(t, "rel", "ugc nofollow")
		return
	}
	id, frag, ok := wiki.LinkTarget(pageID, href)
	if !ok {
		return
	}
	clean := wiki.PageURL(id)
	if frag != "" {
		clean += "#" + frag
	}
	setAttr(t, "href", clean)
	if store != nil && !store.Linkable(id) {
		addClass(t, "missing")
		setAttr(t, "rel", "nofollow")
		if !hasAttr(t, "title") {
			setAttr(t, "title", "This page does not exist yet")
		}
	}
}

// Widths (CSS pixels) asked of the server for scaled copies (it sends
// twice as many pixels, for sharp images on high-density screens):
// the size presets, and a cap for images shown at the column's width.
var presetWidths = map[string]string{"small": "200", "medium": "400"}

const maxImageWidth = "800"

// fixImage turns align into a class (left, right, center), turns a
// layout preset in the fragment (#small-right) into classes, lazy-loads
// the image and asks the server for a scaled copy: of the given size,
// of the preset's size, or capped at the column's width.
func fixImage(t *nethtml.Token) {
	if a, ok := attr(t, "align"); ok {
		switch strings.ToLower(a) {
		case "right", "left":
			addClass(t, strings.ToLower(a))
		case "center", "middle":
			addClass(t, "center")
		}
		delAttr(t, "align")
	}
	if !hasAttr(t, "loading") {
		setAttr(t, "loading", "lazy")
	}
	u, err := url.Parse(attrOr(t, "src"))
	if err != nil {
		return
	}
	if classes, ok := presetClasses(u.Fragment); ok {
		for _, c := range classes {
			addClass(t, c)
		}
		u.Fragment = ""
		setAttr(t, "src", u.String())
	}
	if u.RawQuery != "" || !isRaster(u.Path) {
		return
	}
	q := url.Values{}
	if w, ok := attr(t, "width"); ok && isNum(w) {
		q.Set("w", w)
	}
	if h, ok := attr(t, "height"); ok && isNum(h) {
		q.Set("h", h)
	}
	if len(q) == 0 {
		w := maxImageWidth
		for c, pw := range presetWidths {
			if hasClass(t, c) {
				w = pw
			}
		}
		q.Set("w", w)
	}
	setAttr(t, "src", u.Path+"?"+q.Encode())
}

func isNum(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isRaster(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}

// buildTOC lists headings of levels 1 to 3 as nested lists.
func buildTOC(heads []Heading) string {
	var b strings.Builder
	b.WriteString(`<nav class="toc" aria-label="Contents"><h2>Contents</h2>`)
	var stack []int // heading level of each open <ul>
	for _, h := range heads {
		if h.Level > 3 {
			continue
		}
		switch {
		case len(stack) == 0:
			b.WriteString("<ul>")
			stack = append(stack, h.Level)
		case h.Level > stack[len(stack)-1]:
			b.WriteString("<ul>") // nested inside the open <li>
			stack = append(stack, h.Level)
		default:
			for len(stack) > 1 && h.Level < stack[len(stack)-1] {
				b.WriteString("</li></ul>")
				stack = stack[:len(stack)-1]
			}
			b.WriteString("</li>")
		}
		fmt.Fprintf(&b, `<li><a href="#%s">%s</a>`, html.EscapeString(h.ID), html.EscapeString(h.Text))
	}
	for i := len(stack); i > 0; i-- {
		b.WriteString("</li></ul>")
	}
	b.WriteString("</nav>")
	return b.String()
}
