package wiki

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	mdLinkRe  = regexp.MustCompile(`(!?)\[(?:[^\]\\]|\\.)*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	htmlHref  = regexp.MustCompile(`(?i)<a\s[^>]*href="([^"]+)"`)
	htmlImg   = regexp.MustCompile(`(?i)<img\s[^>]*src="([^"]+)"`)
	fenceLine = regexp.MustCompile("^\\s*(```|~~~)")
)

// PageURL returns the URL path of a page id.
func PageURL(id string) string {
	if id == StartPage {
		return "/"
	}
	if strings.HasSuffix(id, "/"+StartPage) {
		return "/" + strings.TrimSuffix(id, StartPage)
	}
	return "/" + id
}

// IDFromURL maps a URL path ("/a/b", "/a/", "a/b") to a page id.
func IDFromURL(p string) string {
	p = strings.TrimPrefix(p, "/")
	if p == "" || strings.HasSuffix(p, "/") {
		return p + StartPage
	}
	return strings.TrimSuffix(p, ".md")
}

// LinkTarget resolves a link href found in page `from` to an internal page
// id. ok is false for external links, media, anchors and special routes.
func LinkTarget(from, href string) (id, fragment string, ok bool) {
	u, err := url.Parse(href)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", "", false
	}
	p := u.Path
	if !strings.HasPrefix(p, "/") {
		base := "/" + Namespace(from) + "/"
		if Namespace(from) == "" {
			base = "/"
		}
		trailing := strings.HasSuffix(p, "/")
		p = path.Join(base, p)
		if trailing && p != "/" {
			p += "/"
		}
	}
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if strings.HasPrefix(seg, "_") {
			return "", "", false
		}
	}
	return IDFromURL(p), u.Fragment, true
}

// MediaPath returns the media-relative path of a /_media/ URL, or "" (also
// for paths that would leave the media folder, such as "/_media/../x").
func MediaPath(src string) string {
	u, err := url.Parse(src)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return ""
	}
	p, ok := strings.CutPrefix(u.Path, "/_media/")
	if !ok || strings.ContainsRune(p, '\\') || !filepath.IsLocal(filepath.FromSlash(p)) {
		return ""
	}
	return p
}

// scanLinks finds internal page links and the first image of a page body.
func scanLinks(id string, body []byte) (links []string, image string) {
	seen := map[string]bool{}
	add := func(href string) {
		if t, _, ok := LinkTarget(id, href); ok && t != id && !seen[t] {
			seen[t] = true
			links = append(links, t)
		}
	}
	inFence := false
	for _, line := range strings.Split(string(body), "\n") {
		if fenceLine.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range mdLinkRe.FindAllStringSubmatch(line, -1) {
			if m[1] == "!" {
				if image == "" && MediaPath(m[2]) != "" {
					image = m[2]
				}
				continue
			}
			add(m[2])
		}
		for _, m := range htmlHref.FindAllStringSubmatch(line, -1) {
			add(m[1])
		}
		if image == "" {
			for _, m := range htmlImg.FindAllStringSubmatch(line, -1) {
				if MediaPath(m[1]) != "" {
					image = m[1]
					break
				}
			}
		}
	}
	return links, image
}

// mediaRefRe matches media references anywhere in a page, including
// widget settings inside fenced blocks.
var mediaRefRe = regexp.MustCompile(`/_media/([^\s"'()?#<>\]\[]+)`)

// scanMedia lists the media files (paths under _media/) a page refers to.
func scanMedia(body []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mediaRefRe.FindAllSubmatch(body, -1) {
		p, err := url.PathUnescape(string(m[1]))
		if err != nil {
			p = string(m[1])
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
