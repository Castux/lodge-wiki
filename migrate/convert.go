package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

// Site holds what the converter needs to know about the whole wiki:
// existing page ids and their titles (first heading of the current text).
type Site struct {
	Titles map[string]string // page id -> title ("" when the page has no heading)
}

func (s *Site) exists(id string) bool {
	_, ok := s.Titles[id]
	return ok
}

// Result of converting one page.
type Result struct {
	Text   string
	JSON   bool
	Issues []string
}

type conv struct {
	id, ns      string
	site        *Site
	footnotes   []string
	issues      map[string]bool
	noTOC       bool
	looseBreaks bool // treat a double backslash followed by any character as a line break
}

func (c *conv) issue(format string, args ...any) {
	c.issues[fmt.Sprintf(format, args...)] = true
}

var (
	reMacro    = regexp.MustCompile(`~~([A-Z]+)~~`)
	reHeading  = regexp.MustCompile(`^\s*(={2,})\s*(.*?)\s*={2,}\s*$`)
	reHR       = regexp.MustCompile(`^\s*-{4,}\s*$`)
	reList     = regexp.MustCompile(`^((?: {2,}|\t)[ \t]*)([*-])\s?(.*)$`)
	reQuote    = regexp.MustCompile(`^(>+)\s?(.*)$`)
	reCodeOpen = regexp.MustCompile(`(?i)^\s*<(code|file)(\s[^>]*)?>(.*)$`)
	reHTMLOpen = regexp.MustCompile(`(?i)^\s*<html>(.*)$`)
	reWrapOpen = regexp.MustCompile(`^\s*<WRAP(\s[^>]*)?>(.*)$`)
	reWrapEnd  = regexp.MustCompile(`^\s*</WRAP>(.*)$`)
	reNspages  = regexp.MustCompile(`^\s*<nspages(\s[^>]*)?>\s*$`)
	reTableRow = regexp.MustCompile(`^\s*[\^|]`)
	rePre      = regexp.MustCompile(`^(  |\t)`)
)

// Convert turns DokuWiki text of page id into markdown.
func Convert(id, text string, site *Site) Result {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if DedentPages[id] {
		text = dedentLines(text)
	}
	if CrossItalicPages[id] {
		text = splitCrossItalics(text)
	}
	if t := strings.TrimSpace(text); (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
		return Result{Text: t + "\n", JSON: true}
	}

	c := &conv{id: id, ns: wiki.Namespace(id), site: site, issues: map[string]bool{}, looseBreaks: LooseBreakPages[id]}

	if md, ok := strings.CutPrefix(strings.TrimLeft(text, " \t\n"), "<!DOCTYPE markdown>"); ok {
		return Result{Text: normalizeBlank(c.markdownTargets(md))}
	}

	text = reMacro.ReplaceAllStringFunc(text, func(m string) string {
		switch m {
		case "~~NOTOC~~":
			c.noTOC = true
			return ""
		case "~~NOCACHE~~":
			return ""
		}
		c.issue("macro %s kept as text", m)
		return m
	})

	body := c.blocks(strings.Split(text, "\n"))

	var b strings.Builder
	if c.noTOC {
		b.WriteString("---\ntoc: false\n---\n\n")
	}
	b.WriteString(body)
	if len(c.footnotes) > 0 {
		b.WriteString("\n")
		for i, f := range c.footnotes {
			fmt.Fprintf(&b, "[^%d]: %s\n", i+1, f)
		}
	}

	issues := make([]string, 0, len(c.issues))
	for k := range c.issues {
		issues = append(issues, k)
	}
	sort.Strings(issues)
	return Result{Text: normalizeBlank(b.String()), Issues: issues}
}

// normalizeBlank collapses runs of blank lines and trims the ends.
func normalizeBlank(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, l)
		blank = false
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n") + "\n"
}

// blocks converts a sequence of lines.
func (c *conv) blocks(lines []string) string {
	var out []string
	emit := func(s ...string) { out = append(out, s...) }
	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		emit(c.paragraph(para)...)
		emit("")
		para = nil
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// <code>/<file> blocks
		if m := reCodeOpen.FindStringSubmatch(line); m != nil {
			flush()
			tag := strings.ToLower(m[1])
			closeTag := "</" + tag + ">"
			rest := m[3]
			var content []string
			var after string
			if k := strings.Index(strings.ToLower(rest), closeTag); k >= 0 {
				content = append(content, rest[:k])
				after = rest[k+len(closeTag):]
			} else {
				if strings.TrimSpace(rest) != "" {
					content = append(content, rest)
				}
				for i++; i < len(lines); i++ {
					l := lines[i]
					if k := strings.Index(strings.ToLower(l), closeTag); k >= 0 {
						if strings.TrimSpace(l[:k]) != "" {
							content = append(content, l[:k])
						}
						after = l[k+len(closeTag):]
						break
					}
					content = append(content, l)
				}
			}
			lang := ""
			if f := strings.Fields(m[2]); len(f) > 0 && f[0] != "-" {
				lang = f[0]
			}
			emit("")
			emit(fence(strings.Join(content, "\n"), lang)...)
			emit("")
			if strings.TrimSpace(after) != "" {
				lines = insertLine(lines, i+1, after)
			}
			continue
		}

		// <html> blocks
		if m := reHTMLOpen.FindStringSubmatch(line); m != nil {
			flush()
			var content []string
			rest := m[1]
			var after string
			if k := strings.Index(strings.ToLower(rest), "</html>"); k >= 0 {
				content = append(content, rest[:k])
				after = rest[k+7:]
			} else {
				content = append(content, rest)
				for i++; i < len(lines); i++ {
					l := lines[i]
					if k := strings.Index(strings.ToLower(l), "</html>"); k >= 0 {
						content = append(content, l[:k])
						after = l[k+7:]
						break
					}
					content = append(content, l)
				}
			}
			raw := strings.Join(content, "\n")
			low := strings.ToLower(raw)
			for _, t := range []string{"<script", "<style", "<iframe"} {
				if strings.Contains(low, t) {
					c.issue("raw HTML contains %s>", t)
				}
			}
			emit("")
			emit(htmlBlock(content)...)
			emit("")
			if strings.TrimSpace(after) != "" {
				lines = insertLine(lines, i+1, after)
			}
			continue
		}

		// <WRAP> containers
		if m := reWrapOpen.FindStringSubmatch(line); m != nil {
			flush()
			emit("", wrapDiv(m[1]), "")
			if strings.TrimSpace(m[2]) != "" {
				lines = insertLine(lines, i+1, m[2])
			}
			continue
		}
		if m := reWrapEnd.FindStringSubmatch(line); m != nil {
			flush()
			emit("", "</div>", "")
			if strings.TrimSpace(m[1]) != "" {
				lines = insertLine(lines, i+1, m[1])
			}
			continue
		}

		// <nspages>
		if m := reNspages.FindStringSubmatch(line); m != nil {
			flush()
			emit("")
			emit(c.nspages(m[1])...)
			emit("")
			continue
		}

		if m := reHeading.FindStringSubmatch(line); m != nil {
			flush()
			n := len(m[1])
			if n > 6 {
				n = 6
			}
			level := 7 - n
			emit("", strings.Repeat("#", level)+" "+escapeText(m[2], false), "")
			continue
		}

		if reHR.MatchString(line) {
			flush()
			emit("", "---", "")
			continue
		}

		if reList.MatchString(line) {
			flush()
			var items []string
			for ; i < len(lines) && reList.MatchString(lines[i]); i++ {
				items = append(items, lines[i])
			}
			i--
			emit("")
			emit(c.list(items)...)
			emit("")
			continue
		}

		if reTableRow.MatchString(line) {
			flush()
			var rows []string
			for ; i < len(lines) && reTableRow.MatchString(lines[i]); i++ {
				rows = append(rows, lines[i])
			}
			i--
			emit("")
			emit(c.table(rows)...)
			emit("")
			continue
		}

		if reQuote.MatchString(line) {
			flush()
			for ; i < len(lines) && reQuote.MatchString(lines[i]); i++ {
				m := reQuote.FindStringSubmatch(lines[i])
				prefix := strings.Repeat("> ", len(m[1]))
				emit(strings.TrimRight(prefix+c.inline(m[2], false), " "))
			}
			i--
			emit("")
			continue
		}

		if rePre.MatchString(line) && strings.TrimSpace(line) != "" {
			flush()
			var content []string
			for ; i < len(lines) && rePre.MatchString(lines[i]) && strings.TrimSpace(lines[i]) != "" && !reList.MatchString(lines[i]); i++ {
				l := lines[i]
				if strings.HasPrefix(l, "\t") {
					l = l[1:]
				} else {
					l = l[2:]
				}
				content = append(content, l)
			}
			i--
			emit("")
			emit(fence(strings.Join(content, "\n"), "")...)
			emit("")
			continue
		}

		if strings.TrimSpace(line) == "" {
			flush()
			emit("")
			continue
		}

		para = append(para, strings.TrimLeft(line, " \t"))
	}
	flush()
	return strings.Join(out, "\n")
}

func insertLine(lines []string, at int, l string) []string {
	lines = append(lines, "")
	copy(lines[at+1:], lines[at:])
	lines[at] = l
	return lines
}

// fence wraps code in a backtick fence longer than any run inside it.
func fence(code, lang string) []string {
	longest := 0
	run := 0
	for _, r := range code {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := 3
	if longest >= n {
		n = longest + 1
	}
	f := strings.Repeat("`", n)
	code = strings.Trim(code, "\n")
	return []string{f + lang, code, f}
}

// htmlBlock keeps raw HTML verbatim but drops blank lines (outside <pre>),
// so CommonMark does not end the HTML block early and parse the rest as
// markdown.
func htmlBlock(content []string) []string {
	var out []string
	inPre := false
	for _, l := range content {
		low := strings.ToLower(l)
		if strings.Contains(low, "<pre") || strings.Contains(low, "<textarea") {
			inPre = true
		}
		if strings.Contains(low, "</pre") || strings.Contains(low, "</textarea") {
			inPre = false
		}
		if strings.TrimSpace(l) == "" && !inPre {
			continue
		}
		out = append(out, l)
	}
	return out
}

var reClassTok = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func wrapDiv(args string) string {
	var classes []string
	var style string
	for _, f := range strings.Fields(args) {
		switch {
		case strings.HasSuffix(f, "%") || strings.HasSuffix(f, "px") || strings.HasSuffix(f, "em"):
			style = "width: " + f
		case reClassTok.MatchString(f):
			classes = append(classes, strings.ToLower(f))
		}
	}
	s := "<div"
	if len(classes) > 0 {
		s += ` class="` + strings.Join(classes, " ") + `"`
	}
	if style != "" {
		s += ` style="` + style + `"`
	}
	return s + ">"
}

var (
	reLineEscNum = regexp.MustCompile(`^(\d+)([.)])(\s|$)`)
	reOnlyTags   = regexp.MustCompile(`^(\s*<[^>]+>)+\s*$`)
	reSetext     = regexp.MustCompile(`^(=+|-+)\s*$`)
)

// paragraph converts the lines of one paragraph.
func (c *conv) paragraph(lines []string) []string {
	text := c.inline(strings.Join(lines, "\n"), false)
	out := strings.Split(text, "\n")
	for i, l := range out {
		out[i] = escapeLineStart(l)
	}
	// An HTML-only first line would open a CommonMark HTML block that
	// swallows the rest of the paragraph: give it its own block.
	if len(out) > 1 && reOnlyTags.MatchString(out[0]) {
		out = append([]string{out[0], ""}, out[1:]...)
	}
	return out
}

func escapeLineStart(l string) string {
	t := strings.TrimLeft(l, " ")
	switch {
	case strings.HasPrefix(t, "#"):
		return `\` + t
	case strings.HasPrefix(t, "+ ") || strings.HasPrefix(t, "- ") || t == "-" || t == "+":
		return `\` + t
	case strings.HasPrefix(t, ">"):
		return `\` + t
	case reSetext.MatchString(t):
		return `\` + t
	}
	if m := reLineEscNum.FindStringSubmatchIndex(t); m != nil {
		return t[:m[3]] + `\` + t[m[4]:]
	}
	return t
}

// list converts consecutive DokuWiki list lines.
func (c *conv) list(items []string) []string {
	type level struct{ indent, width int }
	var stack []level
	var out []string
	prevDepth := 0
	for _, l := range items {
		m := reList.FindStringSubmatch(l)
		ws := strings.ReplaceAll(m[1], "\t", "  ")
		depth := len(ws) / 2
		if depth < 1 {
			depth = 1
		}
		if depth > prevDepth+1 {
			depth = prevDepth + 1
		}
		prevDepth = depth
		marker := "- "
		if m[2] == "-" {
			marker = "1. "
		}
		for len(stack) >= depth {
			stack = stack[:len(stack)-1]
		}
		indent := 0
		if len(stack) > 0 {
			indent = stack[len(stack)-1].indent + stack[len(stack)-1].width
		}
		stack = append(stack, level{indent, len(marker)})
		body := escapeLineStart(c.inline(strings.TrimSpace(m[3]), false))
		out = append(out, strings.Repeat(" ", indent)+marker+body)
	}
	return out
}

var reNsArg = regexp.MustCompile(`-?[A-Za-z][A-Za-z0-9]*(?::[^\s="]*)?(?:=(?:"[^"]*"|\S*))?|"[^"]*"|\S+`)

// nspages converts an <nspages ...> tag into a ```list block.
func (c *conv) nspages(args string) []string {
	var ns string
	var excludes []string
	opts := map[string]string{}
	var order []string
	set := func(k, v string) {
		if _, ok := opts[k]; !ok {
			order = append(order, k)
		}
		opts[k] = v
	}
	heading := "Pages in this namespace:"
	headingSet := false
	for _, tok := range reNsArg.FindAllString(args, -1) {
		if !strings.HasPrefix(tok, "-") {
			ns = strings.Trim(tok, `"`)
			continue
		}
		name, val, hasVal := strings.Cut(tok[1:], "=")
		val = strings.Trim(val, `"`)
		key, sub, hasSub := strings.Cut(name, ":")
		switch strings.ToLower(key) {
		case "exclude":
			if hasSub && sub != "" {
				excludes = append(excludes, sub)
			} else if !hasVal {
				excludes = append(excludes, "self")
			}
		case "nbcol":
			set("columns", val)
		case "sortid":
			set("sort", "id")
		case "sortbycreationdate":
			set("sort", "created")
		case "sortbydate":
			set("sort", "modified")
		case "naturalorder":
			set("natural", "true")
		case "reverse":
			set("reverse", "true")
		case "numberedlist":
			set("numbered", "true")
		case "usepictures":
			set("pictures", "true")
		case "tree":
			set("tree", "true")
		case "r":
			set("recursive", "true")
		case "subns":
			set("namespaces", "true")
		case "h1", "pagesinns", "nopages":
			if key == "nopages" {
				c.issue("nspages option -nopages ignored")
			}
		case "textpages":
			heading = val
			headingSet = true
		case "textns":
		case "simplelist":
			set("columns", "1")
		case "customtitle":
			set("label", yamlString(val))
		case "displaymodificationdates":
			set("dates", "true")
		default:
			c.issue("nspages option -%s ignored", key)
		}
	}
	_ = headingSet
	out := []string{"```list"}
	ns = strings.TrimSuffix(ns, ":")
	if ns != "" && ns != "." {
		out = append(out, "ns: "+wiki.Resolve(c.ns, ns))
	}
	if len(excludes) > 0 {
		out = append(out, "exclude: ["+strings.Join(excludes, ", ")+"]")
	}
	for _, k := range order {
		out = append(out, k+": "+opts[k])
	}
	if heading != "" {
		out = append(out, "heading: "+yamlString(heading))
	}
	out = append(out, "```")
	return out
}

func yamlString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// LooseBreakPages are pages whose authors wrote "\\" directly before the
// next word (usually a curly quote) and clearly meant a line break.
// DokuWiki only breaks when whitespace follows.
var LooseBreakPages = map[string]bool{
	"skarnholm/log/into-the-deep":                        true,
	"skarnholm/log/2026-06-24-the-dance":                 true,
	"skarnholm/log/2025-08-08-hrafnkefli-rift":           true,
	"skarnholm/log/making-money-off-some-fishy-business": true,
}
