package main

import (
	"path"
	"regexp"
	"strings"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

// Pages starting with "<!DOCTYPE markdown>" were written in markdown for
// DokuWiki's commonmark plugin. They pass through unchanged apart from
// their link and image targets, which use DokuWiki ids (ns:page,
// ns:file.jpg) and are rewritten to wiki paths.

var (
	reMDLink = regexp.MustCompile(`(!?)\[((?:[^\[\]]|\[[^\[\]]*\])*)\]\(\s*([^)\s]+)((?:\s+"[^"]*")?\s*)\)`)
	reMDRef  = regexp.MustCompile(`(?m)^(\s{0,3}\[[^\]]+\]:\s*)(\S+)`)
	reFence  = regexp.MustCompile("(?m)^(```|~~~)")
)

func (c *conv) markdownTargets(md string) string {
	// Leave fenced code blocks alone.
	parts := reFence.Split(md, -1)
	fences := reFence.FindAllString(md, -1)
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 0 {
			p = c.mixedDokuWiki(p)
			p = reMDLink.ReplaceAllStringFunc(p, func(m string) string {
				g := reMDLink.FindStringSubmatch(m)
				target := c.mdTarget(g[3], g[1] == "!")
				label := g[2]
				if strings.TrimSpace(label) == "" && g[1] == "" && target != g[3] {
					// DokuWiki showed the target page's title for empty labels.
					page, _, _ := strings.Cut(g[3], "#")
					id := c.resolvePage(page)
					if t := c.site.Titles[id]; t != "" {
						label = escapeText(t, false)
					} else {
						label = escapeText(wiki.Name(id), false)
					}
				}
				return g[1] + "[" + label + "](" + target + g[4] + ")"
			})
			p = reMDRef.ReplaceAllStringFunc(p, func(m string) string {
				g := reMDRef.FindStringSubmatch(m)
				return g[1] + c.mdTarget(g[2], false)
			})
		}
		b.WriteString(p)
		if i < len(fences) {
			b.WriteString(fences[i])
		}
	}
	return b.String()
}

// mdTarget rewrites a DokuWiki-style target; URLs, absolute paths and
// anchors are kept.
func (c *conv) mdTarget(t string, image bool) string {
	if reScheme.MatchString(t) || strings.HasPrefix(t, "/") || strings.HasPrefix(t, "#") ||
		strings.HasPrefix(t, "mailto:") || strings.HasPrefix(t, "<") {
		return t
	}
	page, anchor, _ := strings.Cut(t, "#")
	ext := strings.ToLower(path.Ext(page))
	if image || imageExt[ext] || audioExt[ext] || videoExt[ext] || ext == ".pdf" {
		return "/_media/" + wiki.Resolve(c.ns, page)
	}
	slug := ""
	if anchor != "" {
		slug = sectionSlug(anchor)
	}
	return pageURL(c.resolvePage(page), slug)
}

var (
	reDWLink  = regexp.MustCompile(`\[\[([^\]]*)\]\]`)
	reDWMedia = regexp.MustCompile(`\{\{([^}]*)\}\}`)
	reDWWrap  = regexp.MustCompile(`<wrap(\s[^>]*)?>`)
	// A DokuWiki heading: the same run of '=' on both sides, on one line.
	reDWHeading = regexp.MustCompile(`(?m)^[ \t]*(={2,})[ \t]*([^=\s][^\n]*?)[ \t]*={2,}[ \t]*$`)
)

// mixedDokuWiki converts the DokuWiki inline syntax that the commonmark
// plugin also rendered inside markdown pages: links, media, <wrap> spans
// and //italics//.
func (c *conv) mixedDokuWiki(s string) string {
	s = reDWHeading.ReplaceAllStringFunc(s, func(m string) string {
		g := reDWHeading.FindStringSubmatch(m)
		return strings.Repeat("#", 7-min(len(g[1]), 6)) + " " + g[2]
	})
	s = dwItalics(s)
	s = reDWLink.ReplaceAllStringFunc(s, func(m string) string { return c.link(m[2 : len(m)-2]) })
	s = reDWMedia.ReplaceAllStringFunc(s, func(m string) string { return c.media(m[2 : len(m)-2]) })
	s = reDWWrap.ReplaceAllStringFunc(s, func(m string) string {
		var classes []string
		for f := range strings.FieldsSeq(reDWWrap.FindStringSubmatch(m)[1]) {
			if reClassTok.MatchString(f) {
				classes = append(classes, strings.ToLower(f))
			}
		}
		return `<span class="` + strings.Join(classes, " ") + `">`
	})
	s = strings.ReplaceAll(s, "</wrap>", "</span>")
	return s
}

// dwItalics turns //x// pairs on one line into *x*, skipping URL schemes.
func dwItalics(s string) string {
	lines := strings.Split(s, "\n")
	for li, l := range lines {
		var marks []int
		for i := 0; i+1 < len(l); i++ {
			if l[i] == '/' && l[i+1] == '/' {
				if i > 0 && l[i-1] == ':' && i+2 < len(l) && l[i+2] != ' ' {
					i++
					continue
				}
				marks = append(marks, i)
				i++
			}
		}
		if len(marks) < 2 {
			continue
		}
		var b strings.Builder
		last := 0
		for k := 0; k+1 < len(marks); k += 2 {
			b.WriteString(l[last:marks[k]])
			b.WriteString("*" + l[marks[k]+2:marks[k+1]] + "*")
			last = marks[k+1] + 2
		}
		b.WriteString(l[last:])
		lines[li] = b.String()
	}
	return strings.Join(lines, "\n")
}
