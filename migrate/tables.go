package main

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

type cell struct {
	header  bool
	raw     string
	align   string
	colspan int
	rowspan int
	skip    bool // covered by a rowspan from above
}

// splitRow splits a DokuWiki table row into cells, ignoring delimiters
// inside links, media, monospace and nowiki.
func splitRow(line string) []cell {
	line = strings.TrimRight(strings.TrimLeft(line, " \t"), " \t")
	var cells []cell
	var cur *cell
	var buf strings.Builder
	finish := func() {
		if cur != nil {
			cur.raw = buf.String()
			cells = append(cells, *cur)
		}
		buf.Reset()
	}
	skipTo := func(i int, open, close string) (int, bool) {
		if !strings.HasPrefix(line[i:], open) {
			return i, false
		}
		j := strings.Index(line[i+len(open):], close)
		if j < 0 {
			return i, false
		}
		end := i + len(open) + j + len(close)
		buf.WriteString(line[i:end])
		return end, true
	}
	for i := 0; i < len(line); {
		if j, ok := skipTo(i, "[[", "]]"); ok {
			i = j
			continue
		}
		if j, ok := skipTo(i, "{{", "}}"); ok {
			i = j
			continue
		}
		if j, ok := skipTo(i, "''", "''"); ok {
			i = j
			continue
		}
		if j, ok := skipTo(i, "%%", "%%"); ok {
			i = j
			continue
		}
		if j, ok := skipTo(i, "<nowiki>", "</nowiki>"); ok {
			i = j
			continue
		}
		if line[i] == '^' || line[i] == '|' {
			finish()
			cur = &cell{header: line[i] == '^', colspan: 1, rowspan: 1}
			i++
			continue
		}
		buf.WriteByte(line[i])
		i++
	}
	// The final delimiter opens an empty trailing cell: drop it.
	if cur != nil && strings.TrimSpace(buf.String()) == "" {
		cur = nil
	}
	finish()

	// Empty cells extend the previous cell's colspan.
	var out []cell
	for _, c := range cells {
		if c.raw == "" && len(out) > 0 {
			out[len(out)-1].colspan++
			continue
		}
		lead := len(c.raw) - len(strings.TrimLeft(c.raw, " "))
		trail := len(c.raw) - len(strings.TrimRight(c.raw, " "))
		switch {
		case lead >= 2 && trail >= 2:
			c.align = "center"
		case lead >= 2:
			c.align = "right"
		case trail >= 2:
			c.align = "left"
		}
		c.raw = strings.TrimSpace(c.raw)
		out = append(out, c)
	}
	return out
}

func (c *conv) table(lines []string) []string {
	rows := make([][]cell, len(lines))
	for i, l := range lines {
		rows[i] = splitRow(l)
	}

	if c.simpleTable(rows) {
		return c.gfmTable(rows)
	}
	return c.htmlTable(rows)
}

func (c *conv) simpleTable(rows [][]cell) bool {
	if len(rows) == 0 {
		return false
	}
	n := len(rows[0])
	for ri, r := range rows {
		if len(r) != n {
			return false
		}
		for _, cl := range r {
			if cl.colspan > 1 || cl.raw == ":::" {
				return false
			}
			if cl.header != (ri == 0) {
				return false
			}
		}
	}
	return true
}

func (c *conv) gfmTable(rows [][]cell) []string {
	var out []string
	cellText := func(cl cell) string {
		t := c.inline(cl.raw, true)
		return strings.ReplaceAll(t, "\n", " ")
	}
	line := func(r []cell) string {
		parts := make([]string, len(r))
		for i, cl := range r {
			parts[i] = cellText(cl)
		}
		return "| " + strings.Join(parts, " | ") + " |"
	}
	out = append(out, line(rows[0]))
	alignRow := rows[0]
	if len(rows) > 1 {
		alignRow = rows[1]
	}
	delim := make([]string, len(rows[0]))
	for i := range delim {
		switch alignRow[i].align {
		case "center":
			delim[i] = ":---:"
		case "right":
			delim[i] = "---:"
		case "left":
			delim[i] = ":---"
		default:
			delim[i] = "---"
		}
	}
	out = append(out, "| "+strings.Join(delim, " | ")+" |")
	for _, r := range rows[1:] {
		out = append(out, line(r))
	}
	return out
}

var mdInline = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// inlineHTML renders a cell's markdown to HTML without the <p> wrapper.
func inlineHTML(md string) string {
	var buf bytes.Buffer
	if err := mdInline.Convert([]byte(md), &buf); err != nil {
		return md
	}
	s := strings.TrimSpace(buf.String())
	if strings.HasPrefix(s, "<p>") && strings.HasSuffix(s, "</p>") && strings.Count(s, "<p>") == 1 {
		s = s[3 : len(s)-4]
	}
	return strings.ReplaceAll(s, "\n", " ")
}

func (c *conv) htmlTable(rows [][]cell) []string {
	// Resolve rowspans: a ":::" cell extends the cell above in the same
	// column position.
	type pos struct{ r, i int }
	colOwner := map[int]pos{} // column -> cell that last occupied it
	for ri := range rows {
		col := 0
		for ci := range rows[ri] {
			cl := &rows[ri][ci]
			if cl.raw == ":::" {
				if p, ok := colOwner[col]; ok {
					rows[p.r][p.i].rowspan++
				}
				cl.skip = true
			} else {
				for k := 0; k < cl.colspan; k++ {
					colOwner[col+k] = pos{ri, ci}
				}
			}
			col += cl.colspan
		}
	}

	out := []string{"<table>"}
	for _, r := range rows {
		var b strings.Builder
		b.WriteString("<tr>")
		for _, cl := range r {
			if cl.skip {
				continue
			}
			tag := "td"
			if cl.header {
				tag = "th"
			}
			b.WriteString("<" + tag)
			if cl.colspan > 1 {
				b.WriteString(` colspan="` + strconv.Itoa(cl.colspan) + `"`)
			}
			if cl.rowspan > 1 {
				b.WriteString(` rowspan="` + strconv.Itoa(cl.rowspan) + `"`)
			}
			if cl.align != "" {
				b.WriteString(` style="text-align: ` + cl.align + `"`)
			}
			b.WriteString(">")
			b.WriteString(inlineHTML(c.inline(cl.raw, false)))
			b.WriteString("</" + tag + ">")
		}
		b.WriteString("</tr>")
		out = append(out, b.String())
	}
	out = append(out, "</table>")
	return out
}
