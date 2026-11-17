// Package wiki holds the content model shared by the server and the
// migration scripts: page ids, path resolution and the on-disk layout.
package wiki

import (
	"path"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// StartPage is the name of a namespace's front page.
const StartPage = "start"

var special = map[rune]string{
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'đ': "d", 'ð': "d",
	'þ': "th", 'ł': "l", 'ı': "i",
}

// CleanID normalises a page or media id the way DokuWiki did (with the
// '-' separator and accent stripping), using '/' as namespace separator.
// ':' and ';' are accepted as separators so old ids keep working.
func CleanID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = deaccent(id)

	var b strings.Builder
	for _, r := range id {
		switch {
		case r == ':' || r == ';' || r == '/':
			b.WriteByte('/')
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	parts := strings.Split(b.String(), "/")
	out := parts[:0]
	for _, p := range parts {
		p = collapse(p, '-')
		p = strings.Trim(p, "._-")
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

func collapse(s string, c byte) string {
	var b strings.Builder
	prev := false
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			if prev {
				continue
			}
			prev = true
		} else {
			prev = false
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func deaccent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if rep, ok := special[r]; ok {
			b.WriteString(rep)
		} else {
			b.WriteRune(r)
		}
	}
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, b.String())
	if err != nil {
		return b.String()
	}
	return out
}

// Namespace returns the namespace of a page id ("" for the root).
func Namespace(id string) string {
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		return id[:i]
	}
	return ""
}

// Name returns the last segment of an id.
func Name(id string) string {
	return id[strings.LastIndexByte(id, '/')+1:]
}

// Resolve turns a link target written in a page of namespace ns into a
// clean absolute id. See docs/guide.md (Links) for the rules. A trailing '/'
// points at the namespace's start page.
func Resolve(ns, target string) string {
	target = strings.TrimSpace(target)
	target = strings.NewReplacer(":", "/", ";", "/").Replace(target)
	trailing := strings.HasSuffix(target, "/")

	var abs string
	switch {
	case strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || target == "." || target == "..":
		abs = path.Join("/"+ns, target)
	case strings.HasPrefix(target, "/"):
		abs = target
	case strings.Contains(target, "/"):
		abs = target
	default:
		if ns == "" {
			abs = target
		} else {
			abs = ns + "/" + target
		}
	}

	id := CleanID(abs)
	if trailing || id == "" || target == "." || target == ".." {
		if id == "" {
			return StartPage
		}
		return id + "/" + StartPage
	}
	return id
}
