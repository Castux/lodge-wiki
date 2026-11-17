package main

import (
	"regexp"
	"sort"
	"strings"
)

// CrossItalicPages are pages where "//" italics run across paragraph
// breaks (or are never closed). DokuWiki toggles italics over the whole
// document, so everything between the markers was italic. The converter
// only pairs markers within a paragraph, so for these pages the source is
// first rewritten to close and reopen the italics around every paragraph
// break inside a span.
var CrossItalicPages = map[string]bool{}

var (
	reListLine  = regexp.MustCompile(`^((?: {2,}|\t)[ \t]*[*-]\s?)`)
	reQuoteLine = regexp.MustCompile(`^(>+\s?)`)
	reBlockish  = regexp.MustCompile(`^\s*(={2,}|[\^|]|-{4,}|<(?i:code|file|html|WRAP|/WRAP|nspages))`)
	reEndBreak  = regexp.MustCompile(`(\s*\\\\\s*)$`)
)

// italicMarkers returns the byte offsets of the "//" markers DokuWiki
// treats as italics toggles.
func italicMarkers(s string) []int {
	var out []int
	skip := func(i int, open, close string) (int, bool) {
		if !strings.HasPrefix(strings.ToLower(s[i:]), open) {
			return i, false
		}
		j := strings.Index(strings.ToLower(s[i+len(open):]), close)
		if j < 0 {
			return i, false
		}
		return i + len(open) + j + len(close), true
	}
	for i := 0; i < len(s); {
		if j, ok := skip(i, "%%", "%%"); ok {
			i = j
			continue
		}
		if j, ok := skip(i, "<nowiki>", "</nowiki>"); ok {
			i = j
			continue
		}
		if j, ok := skip(i, "<code", "</code>"); ok {
			i = j
			continue
		}
		if j, ok := skip(i, "<file", "</file>"); ok {
			i = j
			continue
		}
		if j, ok := skip(i, "<html>", "</html>"); ok {
			i = j
			continue
		}
		if j, ok := skip(i, "[[", "]]"); ok {
			i = j
			continue
		}
		if j, ok := skip(i, "{{", "}}"); ok {
			i = j
			continue
		}
		if strings.HasPrefix(s[i:], "''") {
			if j := strings.Index(s[i+2:], "''"); j >= 0 && !strings.Contains(s[i+2:i+2+j], "\n") {
				i += j + 4
				continue
			}
		}
		if strings.HasPrefix(s[i:], "//") {
			if i > 0 && s[i-1] == ':' && i+2 < len(s) && s[i+2] != ' ' && s[i+2] != '\n' && s[i+2] != '\\' {
				i += 2
				continue
			}
			out = append(out, i)
			i += 2
			continue
		}
		i++
	}
	return out
}

// splitCrossItalics rewrites text so that no italic span crosses a blank
// line: each span is closed before every paragraph break it contains and
// reopened at the start of the next paragraph. An unclosed span runs to
// the end of the page, as in DokuWiki.
func splitCrossItalics(s string) string {
	marks := italicMarkers(s)
	type ins struct {
		at   int
		text string
	}
	var inserts []ins
	dels := map[int]bool{}
	for k := 0; k < len(marks); k += 2 {
		open := marks[k] + 2
		closed := k+1 < len(marks)
		end := len(strings.TrimRight(s, "\n \t"))
		if closed {
			end = marks[k+1]
		}
		lines := strings.SplitAfter(s[open:end], "\n")
		pos := open
		inPara := true // inside the paragraph that opened the span
		for li, l := range lines {
			lineStart := pos
			pos += len(l)
			body := strings.TrimRight(l, "\n")
			if li == 0 {
				continue
			}
			if strings.TrimSpace(body) == "" {
				if inPara {
					prev := strings.TrimRight(lines[li-1], "\n")
					prevStart := lineStart - len(lines[li-1])
					at := prevStart + len(prev)
					if m := reEndBreak.FindStringIndex(prev); m != nil {
						at = prevStart + m[0]
					}
					if strings.TrimSpace(prev) != "" {
						inserts = append(inserts, ins{at, "//"})
					}
					inPara = false
				}
				continue
			}
			if !inPara && !reBlockish.MatchString(body) {
				at := lineStart
				if m := reListLine.FindString(body); m != "" {
					at += len(m)
				} else if m := reQuoteLine.FindString(body); m != "" {
					at += len(m)
				} else {
					at += len(body) - len(strings.TrimLeft(body, " \t"))
				}
				inserts = append(inserts, ins{at, "//"})
				inPara = true
			}
		}
		switch {
		case !closed && inPara:
			inserts = append(inserts, ins{end, "//"})
		case closed && !inPara:
			dels[end] = true // the original closing marker has nothing left to close
		}
	}
	sort.SliceStable(inserts, func(i, j int) bool { return inserts[i].at < inserts[j].at })
	var b strings.Builder
	k := 0
	for i := 0; i <= len(s); i++ {
		for k < len(inserts) && inserts[k].at == i {
			b.WriteString(inserts[k].text)
			k++
		}
		if i == len(s) {
			break
		}
		if dels[i] {
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func init() {
	for _, id := range []string{
		"foolslanding/item/privilege-beyond",
		"foolslanding/log/playing-the-empress",
		"foolslanding/lore/deity/lathander",
		"foolslanding/pc/abraxas",
		"foolslanding/pc/quenya",
		"foolslanding/spell/improved-hexes-and-curses",
		"rules/alchemist-s-supplies",
		"skarnholm/item/book-of-spider-lady",
		"skarnholm/log/2026-01-13-goblincave",
		"skarnholm/log/emmett-ramblings-no.2",
		"skarnholm/log/first-song",
		"skarnholm/log/for-whom-the-bell-tolls",
		"skarnholm/log/from-a-shit-whirlpool-to-casual-murder",
		"skarnholm/npc/filistrom-warston-leopold-swiftbonk-vunderkundoodle",
	} {
		CrossItalicPages[id] = true
	}
}

// DedentPages are poems whose authors indented some lines with spaces.
// DokuWiki turned those lines into code boxes in the middle of the poem;
// the indent is kept with em spaces instead, so the lines stay text.
var DedentPages = map[string]bool{
	"skarnholm/log/first-song":              true,
	"skarnholm/log/for-whom-the-bell-tolls": true,
}

var reIndented = regexp.MustCompile(`(?m)^( {2,})(\S)`)

func dedentLines(s string) string {
	return reIndented.ReplaceAllStringFunc(s, func(m string) string {
		n := len(m) - 1
		if m[n] == '*' || m[n] == '-' {
			return m // list item
		}
		return strings.Repeat(" ", n/2) + m[n:]
	})
}
