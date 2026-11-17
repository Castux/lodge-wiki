package main

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

var entities = []struct{ from, to string }{
	{"<->", "↔"}, {"<=>", "⇔"}, {"->", "→"}, {"<-", "←"}, {"=>", "⇒"}, {"<=", "⇐"},
	{">>", "»"}, {"<<", "«"}, {"---", "—"}, {"--", "–"},
	{"(c)", "©"}, {"(tm)", "™"}, {"(r)", "®"}, {"...", "…"},
}

var smileys = []struct{ from, to string }{
	{"8-)", "😎"}, {"8-O", "😲"}, {"8-o", "😲"}, {":-(", "🙁"}, {":-)", "🙂"}, {"=)", "🙂"},
	{":-/", "😕"}, {`:-\`, "😕"}, {":-?", "😕"}, {":-D", "😀"}, {":-P", "😛"}, {":-o", "😮"},
	{":-O", "😮"}, {":-x", "🤐"}, {":-X", "🤐"}, {":-|", "😐"}, {";-)", "😉"}, {"m(", "🤦"},
	{"^_^", "😊"}, {":?:", "❓"}, {":!:", "❗"}, {"LOL", "😆"},
}

var (
	reURL      = regexp.MustCompile(`^(?:(?:https?|ftp)://|www\.)[^\s<>\[\]"|{}\\]+`)
	reEmailTag = regexp.MustCompile(`^<([\w.+-]+@[\w-]+(?:\.[\w-]+)+)>`)
	reAnyTag   = regexp.MustCompile(`^</?([A-Za-z][\w-]*)[^>]*>`)
	reWrapSpan = regexp.MustCompile(`^<wrap(\s[^>]*)?>`)
	reInlCode  = regexp.MustCompile(`(?i)^<(code|file)(\s[^>]*)?>`)
	reSize     = regexp.MustCompile(`^(\d+)?(?:x(\d+))?$`)
	reScheme   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
	reInterwik = regexp.MustCompile(`^([A-Za-z0-9_]+)>(.*)$`)
	reEmail    = regexp.MustCompile(`^[\w.+-]+@[\w-]+(?:\.[\w-]+)+$`)
)

// inline converts inline DokuWiki markup. inTable escapes '|'.
func (c *conv) inline(s string, inTable bool) string {
	var out, plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			out.WriteString(escapeText(plain.String(), inTable))
			plain.Reset()
		}
	}
	prevRune := func(i int) rune {
		if i == 0 {
			return ' '
		}
		r, _ := utf8.DecodeLastRuneInString(s[:i])
		return r
	}
	nextRune := func(i int) rune {
		if i >= len(s) {
			return ' '
		}
		r, _ := utf8.DecodeRuneInString(s[i:])
		return r
	}

	for i := 0; i < len(s); {
		rest := s[i:]
		low := strings.ToLower(rest)

		switch {
		case strings.HasPrefix(rest, "%%"):
			if j := strings.Index(rest[2:], "%%"); j >= 0 {
				flush()
				out.WriteString(escapeLiteral(rest[2:2+j], inTable))
				i += 4 + j
				continue
			}
		case strings.HasPrefix(low, "<nowiki>"):
			if j := strings.Index(low[8:], "</nowiki>"); j >= 0 {
				flush()
				out.WriteString(escapeLiteral(rest[8:8+j], inTable))
				i += 8 + j + 9
				continue
			}
		case strings.HasPrefix(rest, "''"):
			if j := strings.Index(rest[2:], "''"); j >= 0 {
				flush()
				out.WriteString(codeSpan(rest[2 : 2+j]))
				i += 4 + j
				continue
			}
		case reInlCode.MatchString(rest):
			m := reInlCode.FindStringSubmatch(rest)
			closeTag := "</" + strings.ToLower(m[1]) + ">"
			if j := strings.Index(low[len(m[0]):], closeTag); j >= 0 {
				flush()
				out.WriteString(codeSpan(rest[len(m[0]) : len(m[0])+j]))
				i += len(m[0]) + j + len(closeTag)
				continue
			}
		case strings.HasPrefix(low, "<html>"):
			if j := strings.Index(low[6:], "</html>"); j >= 0 {
				flush()
				out.WriteString(rest[6 : 6+j])
				i += 6 + j + 7
				continue
			}
		case strings.HasPrefix(rest, "[["):
			if j := findLinkEnd(rest); j >= 0 {
				flush()
				out.WriteString(c.link(rest[2:j]))
				i += j + 2
				continue
			}
		case strings.HasPrefix(rest, "{{"):
			if j := strings.Index(rest, "}}"); j >= 0 {
				flush()
				out.WriteString(c.media(rest[2:j]))
				i += j + 2
				continue
			}
		case strings.HasPrefix(rest, "(("):
			if j := strings.Index(rest, "))"); j >= 0 {
				flush()
				c.footnotes = append(c.footnotes, strings.ReplaceAll(c.inline(strings.TrimSpace(rest[2:j]), false), "\n", " "))
				fmt.Fprintf(&out, "[^%d]", len(c.footnotes))
				i += j + 2
				continue
			}
		case strings.HasPrefix(rest, "**"), strings.HasPrefix(rest, "__"),
			strings.HasPrefix(rest, "//") && prevRune(i) != ':':
			marker := rest[:2]
			if j := findClose(rest, marker); j > 2 {
				flush()
				inner := c.inline(rest[2:j], inTable)
				out.WriteString(emphasis(marker, inner, prevRune(i), nextRune(i+j+2)))
				i += j + 2
				continue
			}
		case strings.HasPrefix(low, "<del>"):
			if j := strings.Index(low, "</del>"); j >= 0 {
				flush()
				out.WriteString("~~" + c.inline(rest[5:j], inTable) + "~~")
				i += j + 6
				continue
			}
		case strings.HasPrefix(low, "<sub>"), strings.HasPrefix(low, "<sup>"):
			tag := low[1:4]
			if j := strings.Index(low, "</"+tag+">"); j >= 0 {
				flush()
				out.WriteString("<" + tag + ">" + c.inline(rest[5:j], inTable) + "</" + tag + ">")
				i += j + 6
				continue
			}
		case reWrapSpan.MatchString(rest):
			m := reWrapSpan.FindStringSubmatch(rest)
			if j := strings.Index(rest, "</wrap>"); j >= 0 {
				flush()
				var classes []string
				for _, f := range strings.Fields(m[1]) {
					if reClassTok.MatchString(f) {
						classes = append(classes, strings.ToLower(f))
					}
				}
				out.WriteString(`<span class="` + strings.Join(classes, " ") + `">` + c.inline(rest[len(m[0]):j], inTable) + "</span>")
				i += j + 7
				continue
			}
		case strings.HasPrefix(rest, `\\`) && (i+2 >= len(s) || s[i+2] == ' ' || s[i+2] == '\n' || s[i+2] == '\t' ||
			(c.looseBreaks && s[i+2] != '\\')):
			flush()
			out.WriteString("<br>")
			i += 2
			if i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			continue
		case reEmailTag.MatchString(rest):
			m := reEmailTag.FindString(rest)
			flush()
			out.WriteString(m)
			i += len(m)
			continue
		case strings.HasPrefix(rest, "<"):
			if m := reAnyTag.FindStringSubmatch(rest); m != nil {
				c.issue("unknown tag <%s> kept as text", strings.ToLower(m[1]))
			}
		}

		// bare URLs
		if r := prevRune(i); !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if m := reURL.FindString(rest); m != "" {
				m = strings.TrimRight(m, ".,;:!?)'")
				flush()
				if id, anchor, ok := c.oldSiteID(m); ok {
					out.WriteString(c.link(id + anchor))
				} else if strings.HasPrefix(m, "www.") {
					out.WriteString(m)
				} else {
					out.WriteString("<" + m + ">")
				}
				i += len(m)
				continue
			}
		}

		// smileys: need whitespace (or edges) around
		if r := prevRune(i); unicode.IsSpace(r) || i == 0 {
			matched := false
			for _, sm := range smileys {
				if strings.HasPrefix(rest, sm.from) {
					if n := nextRune(i + len(sm.from)); unicode.IsSpace(n) || i+len(sm.from) == len(s) {
						flush()
						out.WriteString(sm.to)
						i += len(sm.from)
						matched = true
						break
					}
				}
			}
			if matched {
				continue
			}
		}

		// typography entities
		matched := false
		for _, e := range entities {
			if strings.HasPrefix(rest, e.from) {
				plain.WriteString(e.to)
				i += len(e.from)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		r, size := utf8.DecodeRuneInString(rest)
		plain.WriteRune(r)
		i += size
	}
	flush()
	return out.String()
}

// findLinkEnd returns the index of the closing "]]" of a link starting
// at s[0:2]=="[[", skipping media embeds in the label.
func findLinkEnd(s string) int {
	for i := 2; i < len(s)-1; i++ {
		if strings.HasPrefix(s[i:], "{{") {
			if j := strings.Index(s[i:], "}}"); j >= 0 {
				i += j + 1
				continue
			}
		}
		if strings.HasPrefix(s[i:], "]]") {
			return i
		}
	}
	return -1
}

// findClose finds the closing marker of an emphasis opened at s[0:2],
// skipping links, media, monospace and URL schemes.
func findClose(s, marker string) int {
	for i := 2; i < len(s)-1; i++ {
		switch {
		case strings.HasPrefix(s[i:], "[["):
			if j := findLinkEnd(s[i:]); j >= 0 {
				i += j + 1
				continue
			}
		case strings.HasPrefix(s[i:], "{{"):
			if j := strings.Index(s[i:], "}}"); j >= 0 {
				i += j + 1
				continue
			}
		case strings.HasPrefix(s[i:], "''"):
			if j := strings.Index(s[i+2:], "''"); j >= 0 {
				i += j + 3
				continue
			}
		case strings.HasPrefix(s[i:], "%%"):
			if j := strings.Index(s[i+2:], "%%"); j >= 0 {
				i += j + 3
				continue
			}
		case strings.HasPrefix(s[i:], "\n\n"):
			return -1
		}
		if strings.HasPrefix(s[i:], marker) {
			// "://" is a URL scheme only when a non-space follows
			// ("http://x"); "Others://" closes the italics, as in DokuWiki.
			if marker == "//" && s[i-1] == ':' && i+2 < len(s) && s[i+2] != ' ' && s[i+2] != '\n' && s[i+2] != '\\' {
				continue
			}
			return i
		}
	}
	return -1
}

func isPunct(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// emphasis wraps inner text, moving surrounding spaces outside the
// markers and falling back to HTML when CommonMark flanking rules would
// not recognise the markers.
func emphasis(marker, inner string, before, after rune) string {
	core := strings.TrimSpace(inner)
	if core == "" {
		return inner
	}
	lead := inner[:strings.Index(inner, core)]
	trail := inner[len(lead)+len(core):]
	first, _ := utf8.DecodeRuneInString(core)
	last, _ := utf8.DecodeLastRuneInString(core)

	if marker == "__" {
		return lead + "<u>" + core + "</u>" + trail
	}
	md, tag := "**", "strong"
	if marker == "//" {
		md, tag = "*", "em"
	}
	risky := (isPunct(first) && (isWordRune(before) && lead == "")) ||
		(isPunct(last) && (isWordRune(after) && trail == "")) ||
		strings.HasPrefix(core, "*") && md == "*" || strings.HasSuffix(core, "*") && md == "*"
	if risky {
		return lead + "<" + tag + ">" + core + "</" + tag + ">" + trail
	}
	return lead + md + core + md + trail
}

func codeSpan(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || (strings.HasPrefix(s, " ") && strings.HasSuffix(s, " ") && strings.TrimSpace(s) != "") {
		return f + " " + s + " " + f
	}
	return f + s + f
}

var reEntityLike = regexp.MustCompile(`^&(?:[A-Za-z][A-Za-z0-9]*|#\d+|#[xX][0-9a-fA-F]+);`)

// escapeText escapes markdown-special characters in plain text.
func escapeText(s string, inTable bool) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		prev, next := ' ', ' '
		if i > 0 {
			prev = runes[i-1]
		}
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		switch r {
		case '\\', '`', '*', '[', ']':
			b.WriteRune('\\')
		case '_':
			if !isWordRune(prev) || !isWordRune(next) {
				b.WriteRune('\\')
			}
		case '<':
			if unicode.IsLetter(next) || next == '/' || next == '!' || next == '?' {
				b.WriteRune('\\')
			}
		case '~':
			if next == '~' || prev == '~' {
				b.WriteRune('\\')
			}
		case '|':
			if inTable {
				b.WriteRune('\\')
			}
		case '&':
			if reEntityLike.MatchString(string(runes[i:min(len(runes), i+12)])) {
				b.WriteString("&amp;")
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeLiteral escapes text that must show exactly as written.
func escapeLiteral(s string, inTable bool) string {
	return escapeText(s, inTable)
}

func pageURL(id, anchor string) string {
	u := "/" + id
	switch {
	case id == wiki.StartPage:
		u = "/"
	case strings.HasSuffix(id, "/"+wiki.StartPage):
		u = "/" + strings.TrimSuffix(id, wiki.StartPage)
	}
	if anchor != "" {
		u += "#" + anchor
	}
	return u
}

// sectionSlug turns a DokuWiki section anchor into the slug the new
// renderer uses for headings (lowercase, '-' separated).
func sectionSlug(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	a = strings.ReplaceAll(a, "_", "-")
	return wiki.CleanID(strings.ReplaceAll(a, "/", "-"))
}

// resolvePage applies DokuWiki's namespace fallbacks to a link target.
func (c *conv) resolvePage(target string) string {
	id := wiki.Resolve(c.ns, target)
	if c.site.exists(id) {
		return id
	}
	if c.site.exists(id + "/" + wiki.StartPage) {
		return id + "/" + wiki.StartPage
	}
	if strings.HasSuffix(id, "/"+wiki.StartPage) {
		parent := strings.TrimSuffix(id, "/"+wiki.StartPage)
		if c.site.exists(parent + "/" + wiki.Name(parent)) {
			return parent + "/" + wiki.Name(parent)
		}
		if c.site.exists(parent) {
			return parent
		}
	}
	return id
}

var interwiki = map[string]string{
	"wp":     "https://en.wikipedia.org/wiki/",
	"wpfr":   "https://fr.wikipedia.org/wiki/",
	"doku":   "https://www.dokuwiki.org/",
	"google": "https://www.google.com/search?q=",
}

// link converts the inside of [[...]].
func (c *conv) link(inner string) string {
	target, label, hasLabel := splitOutside(inner, '|')
	target = strings.TrimSpace(target)
	label = strings.TrimSpace(label)

	labelMD := ""
	if hasLabel && label != "" {
		labelMD = c.inline(label, false)
	}

	if id, anchor, ok := c.oldSiteID(target); ok {
		target = id + anchor
	}

	var url string
	switch {
	case reScheme.MatchString(target) || strings.HasPrefix(target, "www."):
		url = target
		if strings.HasPrefix(url, "www.") {
			url = "http://" + url
		}
		if labelMD == "" {
			labelMD = escapeText(target, false)
		}
	case reEmail.MatchString(target):
		url = "mailto:" + target
		if labelMD == "" {
			labelMD = escapeText(target, false)
		}
	case reInterwik.MatchString(target):
		m := reInterwik.FindStringSubmatch(target)
		base, ok := interwiki[strings.ToLower(m[1])]
		if !ok {
			c.issue("unknown interwiki %s>", m[1])
			base = ""
		}
		url = base + strings.ReplaceAll(m[2], " ", "_")
		if labelMD == "" {
			labelMD = escapeText(m[2], false)
		}
	default:
		page, anchor, _ := strings.Cut(target, "#")
		page, _, _ = strings.Cut(page, "?") // e.g. ?s[]=word search highlighting
		page = dwSlash(page)
		slug := ""
		if anchor != "" {
			slug = sectionSlug(anchor)
		}
		if strings.TrimSpace(page) == "" {
			url = "#" + slug
			if labelMD == "" {
				labelMD = escapeText(anchor, false)
			}
			break
		}
		id := c.resolvePage(page)
		url = pageURL(id, slug)
		if labelMD == "" {
			if t := c.site.Titles[id]; t != "" {
				labelMD = escapeText(t, false)
			} else {
				labelMD = escapeText(wiki.Name(id), false)
			}
		}
	}
	return "[" + labelMD + "](" + linkDest(url) + ")"
}

// linkDest wraps a URL in <> when it contains characters that would
// break a markdown link destination.
func linkDest(u string) string {
	if strings.ContainsAny(u, " ()<>") {
		u = strings.NewReplacer(" ", "%20", "<", "%3C", ">", "%3E").Replace(u)
		return "<" + u + ">"
	}
	return u
}

// splitOutside splits at the first sep that is not inside {{...}}.
func splitOutside(s string, sep byte) (string, string, bool) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "{{"):
			depth++
			i++
		case strings.HasPrefix(s[i:], "}}"):
			depth--
			i++
		case s[i] == sep && depth == 0:
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

var (
	imageExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".svg": true, ".bmp": true}
	audioExt = map[string]bool{".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".flac": true}
	videoExt = map[string]bool{".mp4": true, ".webm": true, ".ogv": true}
)

func attr(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// media converts the inside of {{...}}.
func (c *conv) media(inner string) string {
	if m := reInterwik.FindStringSubmatch(strings.TrimSpace(inner)); m != nil && !strings.Contains(m[1], ".") {
		c.issue("plugin syntax {{%s>...}}", m[1])
		if strings.EqualFold(m[1], "rss") {
			u := strings.Fields(m[2])
			if len(u) > 0 {
				return "[RSS feed](" + linkDest(u[0]) + ")"
			}
		}
		return escapeText("{{"+inner+"}}", false)
	}

	src, caption, _ := strings.Cut(inner, "|")
	left := strings.HasPrefix(src, " ")
	right := strings.HasSuffix(src, " ")
	align := ""
	switch {
	case left && right:
		align = "center"
	case left:
		align = "right"
	case right:
		align = "left"
	}
	caption = strings.TrimSpace(caption)
	src = strings.TrimSpace(src)

	query := ""
	if k := strings.Index(src, "?"); k >= 0 {
		query = src[k+1:]
		src = src[:k]
	}
	var width, height string
	linkOnly := false
	for _, p := range strings.FieldsFunc(query, func(r rune) bool { return r == '&' }) {
		p = strings.ToLower(strings.TrimSpace(p))
		switch {
		case p == "linkonly":
			linkOnly = true
		case p == "nolink", p == "direct", p == "nocache", p == "recache", p == "cache", p == "":
		case reSize.MatchString(p):
			m := reSize.FindStringSubmatch(p)
			width, height = m[1], m[2]
		default:
			c.issue("media parameter ?%s ignored", p)
		}
	}

	var url string
	if reScheme.MatchString(src) {
		url = src
	} else {
		url = "/_media/" + wiki.Resolve(c.ns, dwSlash(src))
	}
	ext := strings.ToLower(path.Ext(src))
	name := path.Base(src)
	if i := strings.LastIndexAny(name, ":"); i >= 0 {
		name = name[i+1:]
	}

	switch {
	case linkOnly || (!imageExt[ext] && !audioExt[ext] && !videoExt[ext]):
		label := caption
		if label == "" {
			label = name
		}
		return "[" + escapeText(label, false) + "](" + linkDest(url) + ")"
	case audioExt[ext]:
		return `<audio controls src="` + attr(url) + `"></audio>`
	case videoExt[ext]:
		s := `<video controls src="` + attr(url) + `"`
		if width != "" {
			s += ` width="` + width + `"`
		}
		return s + `></video>`
	}

	if width == "" && height == "" && align == "" {
		return "![" + escapeText(caption, false) + "](" + linkDest(url) + ")"
	}
	s := `<img src="` + attr(url) + `"`
	if width != "" {
		s += ` width="` + width + `"`
	}
	if height != "" {
		s += ` height="` + height + `"`
	}
	if align != "" {
		s += ` align="` + align + `"`
	}
	s += ` alt="` + attr(caption) + `">`
	return s
}

var _ = strconv.Itoa

var reOldSite = regexp.MustCompile(`^(?:https?://)?(?:www\.)?the-chasers-lodge\.net/(.*)$`)

// oldSiteID turns an absolute link to a page of the old DokuWiki site
// (https://the-chasers-lodge.net/ns:page or /doku.php?id=ns:page) into
// an absolute DokuWiki id (leading ':') and anchor.
func (c *conv) oldSiteID(u string) (string, string, bool) {
	m := reOldSite.FindStringSubmatch(u)
	if m == nil {
		return "", "", false
	}
	p := m[1]
	anchor := ""
	if k := strings.IndexByte(p, '#'); k >= 0 {
		p, anchor = p[:k], p[k:]
	}
	explicit := false
	switch {
	case strings.HasPrefix(p, "doku.php?"):
		q := p[len("doku.php?"):]
		p = ""
		for _, kv := range strings.Split(q, "&") {
			if v, ok := strings.CutPrefix(kv, "id="); ok {
				p = v
			}
		}
		explicit = true
	case strings.HasPrefix(p, "doku.php/"):
		p = p[len("doku.php/"):]
		explicit = true
	default:
		if k := strings.IndexByte(p, '?'); k >= 0 {
			p = p[:k]
		}
	}
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return ":start", anchor, explicit || m[1] == ""
	}
	if !explicit && !strings.Contains(p, ":") && !c.site.exists(wiki.CleanID(p)) {
		return "", "", false
	}
	return ":" + strings.ReplaceAll(p, "/", ":"), anchor, true
}

// dwSlash applies DokuWiki's useslash=0: a '/' in a page or media id is
// not a namespace separator but an ordinary character, cleaned to '-'.
func dwSlash(id string) string {
	return strings.ReplaceAll(id, "/", "-")
}
