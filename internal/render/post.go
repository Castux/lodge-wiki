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

// SectionID turns a heading text into an anchor id, like DokuWiki's
// sectionID.
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

// postProcess reshapes goldmark's HTML into DokuWiki-like markup and
// returns the headings found at the top level.
func postProcess(pageID, src string, store *wiki.Store) (string, []Heading) {
	z := nethtml.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	var heads []Heading
	used := map[string]int{}

	depth := 0
	sectionOpen := false
	inLink := 0

	// heading buffering
	var headTok *nethtml.Token
	var headBuf strings.Builder
	var headText strings.Builder
	headDepth := 0

	// lists: stack of "li has an open div.li"
	var liStack []bool
	listLevel := 0

	write := func(s string) {
		if headTok != nil {
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
			if headTok != nil {
				headText.WriteString(html.UnescapeString(raw))
			}
			write(raw)
			continue
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			t := z.Token()
			name := t.Data
			isVoid := voidTags[name] || tt == nethtml.SelfClosingTagToken

			switch {
			case depth == 0 && len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6' && headTok == nil:
				tok := t
				headTok = &tok
				headBuf.Reset()
				headText.Reset()
				headDepth = depth
				depth++
				continue
			case name == "a":
				inLink++
				fixLink(pageID, &t, store)
				write(t.String())
			case name == "img":
				src, _ := attr(&t, "src")
				mp := wiki.MediaPath(src)
				if mp == "" {
					write(raw)
					break
				}
				full := "/_media/" + mp
				fixImage(&t)
				if inLink == 0 {
					write(`<a href="` + html.EscapeString(full) + `" class="media" target="_blank">` + t.String() + `</a>`)
				} else {
					write(t.String())
				}
			case name == "table" && !hasAttr(&t, "class"):
				addClass(&t, "inline")
				write(`<div class="table">` + t.String())
			case name == "ul" || name == "ol":
				if n := len(liStack); n > 0 && liStack[n-1] {
					write("</div>")
					liStack[n-1] = false
				}
				listLevel++
				write(raw)
			case name == "li":
				addClass(&t, fmt.Sprintf("level%d", listLevel))
				write(t.String() + `<div class="li">`)
				liStack = append(liStack, true)
			default:
				write(raw)
			}
			if !isVoid {
				depth++
			}
		case nethtml.EndTagToken:
			name, _ := z.TagName()
			n := string(name)
			if !voidTags[n] {
				depth--
			}
			if headTok != nil && depth == headDepth && n == headTok.Data {
				text := strings.TrimSpace(headText.String())
				level := int(headTok.Data[1] - '0')
				id, ok := attr(headTok, "id")
				if !ok || id == "" {
					id = SectionID(text)
				}
				if c, seen := used[id]; seen {
					used[id] = c + 1
					id = fmt.Sprintf("%s%d", id, c+1)
				} else {
					used[id] = 0
				}
				if sectionOpen {
					out.WriteString("</div>\n")
				}
				setAttr(headTok, "id", id)
				out.WriteString(headTok.String() + headBuf.String() + "</" + headTok.Data + ">\n")
				out.WriteString(fmt.Sprintf(`<div class="level%d">`, level))
				sectionOpen = true
				heads = append(heads, Heading{Level: level, ID: id, Text: text})
				headTok = nil
				continue
			}
			switch n {
			case "a":
				if inLink > 0 {
					inLink--
				}
				write(raw)
			case "table":
				write(raw + "</div>")
			case "li":
				if k := len(liStack); k > 0 {
					if liStack[k-1] {
						write("</div>")
					}
					liStack = liStack[:k-1]
				}
				write(raw)
			case "ul", "ol":
				listLevel--
				write(raw)
			default:
				write(raw)
			}
		default:
			write(raw)
		}
	}
	if sectionOpen {
		out.WriteString("\n</div>\n")
	}
	return out.String(), heads
}

func hasAttr(t *nethtml.Token, key string) bool {
	_, ok := attr(t, key)
	return ok
}

func fixLink(pageID string, t *nethtml.Token, store *wiki.Store) {
	href, ok := attr(t, "href")
	if !ok || href == "" || strings.HasPrefix(href, "#") {
		return
	}
	if c, _ := attr(t, "class"); strings.Contains(c, "footnote") {
		return
	}
	if strings.HasPrefix(href, "mailto:") {
		addClass(t, "mail")
		return
	}
	if wiki.MediaPath(href) != "" {
		addClass(t, "media mediafile")
		return
	}
	u, err := url.Parse(href)
	if err != nil {
		return
	}
	if u.Scheme != "" || u.Host != "" {
		addClass(t, "urlextern")
		if !hasAttr(t, "title") {
			setAttr(t, "title", href)
		}
		setAttr(t, "rel", "ugc nofollow")
		return
	}
	id, frag, ok := wiki.LinkTarget(pageID, href)
	if !ok {
		return
	}
	// Normalise the URL so relative links work from start pages too.
	clean := wiki.PageURL(id)
	if frag != "" {
		clean += "#" + frag
	}
	setAttr(t, "href", clean)
	if store != nil && store.Exists(id) {
		addClass(t, "wikilink1")
		if !hasAttr(t, "title") {
			setAttr(t, "title", id)
		}
	} else {
		addClass(t, "wikilink2")
		setAttr(t, "rel", "nofollow")
		if !hasAttr(t, "title") {
			setAttr(t, "title", id+" (page does not exist)")
		}
	}
}

// fixImage adds DokuWiki's media classes, turns align into a class and
// asks the server for a scaled copy when a size is given.
func fixImage(t *nethtml.Token) {
	src, _ := attr(t, "src")
	cls := "media"
	if a, ok := attr(t, "align"); ok {
		switch strings.ToLower(a) {
		case "right":
			cls = "mediaright"
		case "left":
			cls = "medialeft"
		case "center", "middle":
			cls = "mediacenter"
		}
		delAttr(t, "align")
	}
	addClass(t, cls)
	if !hasAttr(t, "loading") {
		setAttr(t, "loading", "lazy")
	}
	u, err := url.Parse(src)
	if err != nil || u.RawQuery != "" {
		return
	}
	q := url.Values{}
	if w, ok := attr(t, "width"); ok && isNum(w) {
		q.Set("w", w)
	}
	if h, ok := attr(t, "height"); ok && isNum(h) {
		q.Set("h", h)
	}
	if len(q) > 0 && isRaster(u.Path) {
		setAttr(t, "src", u.Path+"?"+q.Encode())
	}
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

func buildTOC(heads []Heading) string {
	var b strings.Builder
	b.WriteString(`<div id="dw__toc" class="dw__toc"><h3 class="toggle">Table of Contents</h3><div><ul class="toc">`)
	cur := 0
	open := []bool{}
	for _, h := range heads {
		if h.Level > 3 {
			continue
		}
		if cur == 0 {
			cur = h.Level
			open = append(open, false)
		}
		for h.Level > cur {
			b.WriteString(`<ul class="toc">`)
			cur++
			open = append(open, false)
		}
		for h.Level < cur && len(open) > 1 {
			if open[len(open)-1] {
				b.WriteString("</li>")
			}
			b.WriteString("</ul>")
			open = open[:len(open)-1]
			cur--
		}
		if open[len(open)-1] {
			b.WriteString("</li>")
		}
		fmt.Fprintf(&b, `<li class="level%d"><div class="li"><a href="#%s">%s</a></div>`, h.Level, html.EscapeString(h.ID), html.EscapeString(h.Text))
		open[len(open)-1] = true
	}
	for i := len(open) - 1; i >= 0; i-- {
		if open[i] {
			b.WriteString("</li>")
		}
		if i > 0 {
			b.WriteString("</ul>")
		}
	}
	b.WriteString("</ul></div></div>")
	return b.String()
}
