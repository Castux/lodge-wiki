// Command htmltables rewrites the plain HTML tables the DokuWiki import
// left in pages (character sheets, lists) as Markdown tables, lined up as
// the editor would. The first row becomes the header; a cell spanning
// columns is followed by empty ones; <strong>/<b> and <em>/<i> become
// ** and *, <br> stays (Markdown tables allow it), empty lists and
// cells past the header's width go when they are empty.
//
// Left as they are: tables holding pictures, lists, paragraphs or other
// tables (the Domination glyph grids), which Markdown cannot write.
// -skip lists pages to leave alone (comma-separated ids). Without -w it
// prints what it would write.
//
//	go run ./migrate/fixes/htmltables ../lodge-data      # show
//	go run ./migrate/fixes/htmltables -w -skip skarnholm/vault/start ../lodge-data
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	tableRe   = regexp.MustCompile(`(?is)[ \t]*<table[^>]*>.*?</table>[ \t]*`)
	rowRe     = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellRe    = regexp.MustCompile(`(?is)<t[dh]([^>]*)>(.*?)</t[dh]>`)
	colspanRe = regexp.MustCompile(`(?i)colspan="?(\d+)`)
	blockRe   = regexp.MustCompile(`(?i)<(img|ul|ol|li|p|div|table|h\d)\b`)
	emptyList = regexp.MustCompile(`(?is)<ul>\s*<li>\s*</li>\s*</ul>`)
	strongRe  = regexp.MustCompile(`(?is)<(strong|b)>(.*?)</(strong|b)>`)
	emRe      = regexp.MustCompile(`(?is)<(em|i)>(.*?)</(em|i)>`)
	brRe      = regexp.MustCompile(`(?i)\s*<br\s*/?>\s*`)
	spaceRe   = regexp.MustCompile(`\s+`)
	wholeBold = regexp.MustCompile(`^\*\*([^*]+)\*\*$`)
)

func cell(s string) (string, bool) {
	s = emptyList.ReplaceAllString(s, "")
	if blockRe.MatchString(s) {
		return "", false
	}
	s = strongRe.ReplaceAllString(s, "**$2**")
	s = emRe.ReplaceAllString(s, "*$2*")
	s = brRe.ReplaceAllString(s, "<br>")
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	for strings.HasSuffix(s, "<br>") {
		s = strings.TrimSpace(strings.TrimSuffix(s, "<br>"))
	}
	for strings.HasPrefix(s, "<br>") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "<br>"))
	}
	s = strings.ReplaceAll(s, "&amp;", "&")
	return strings.ReplaceAll(s, "|", `\|`), true
}

// convert returns the Markdown table, or false if the table must stay HTML.
func convert(table string) (string, bool) {
	var rows [][]string
	for _, r := range rowRe.FindAllStringSubmatch(table, -1) {
		var row []string
		for _, c := range cellRe.FindAllStringSubmatch(r[1], -1) {
			text, ok := cell(c[2])
			if !ok {
				return "", false
			}
			row = append(row, text)
			if m := colspanRe.FindStringSubmatch(c[1]); m != nil {
				n, _ := strconv.Atoi(m[1])
				for i := 1; i < n; i++ {
					row = append(row, "")
				}
			}
		}
		rows = append(rows, row)
	}
	if len(rows) < 2 {
		return "", false
	}
	n := len(rows[0]) // the header's width, or more if later rows have text further right
	for _, r := range rows {
		for i, c := range r {
			if c != "" {
				n = max(n, i+1)
			}
		}
	}
	width := make([]int, n)
	for i := range width {
		width[i] = 3
	}
	for ri, r := range rows {
		for len(r) < n {
			r = append(r, "")
		}
		r = r[:n]
		if ri == 0 { // the header is bold already
			for i, c := range r {
				r[i] = wholeBold.ReplaceAllString(c, "$1")
			}
		}
		rows[ri] = r
		for i, c := range r {
			width[i] = max(width[i], utf8.RuneCountInString(c))
		}
	}
	line := func(cells []string) string {
		var b strings.Builder
		b.WriteString("|")
		for i, c := range cells {
			b.WriteString(" " + c + strings.Repeat(" ", width[i]-utf8.RuneCountInString(c)) + " |")
		}
		return b.String()
	}
	out := []string{line(rows[0])}
	delim := make([]string, n)
	for i := range delim {
		delim[i] = strings.Repeat("-", width[i])
	}
	out = append(out, line(delim))
	for _, r := range rows[1:] {
		out = append(out, line(r))
	}
	return strings.Join(out, "\n"), true
}

func main() {
	write := flag.Bool("w", false, "write the changes")
	skip := flag.String("skip", "", "page ids to leave alone, comma-separated")
	flag.Parse()
	root := flag.Arg(0)
	kept := map[string]int{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() && (d.Name() == ".git" || d.Name() == "_media") || !strings.HasSuffix(p, ".md") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		for _, id := range strings.Split(*skip, ",") {
			if id != "" && filepath.ToSlash(rel) == id+".md" {
				return nil
			}
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		changed := 0
		out := tableRe.ReplaceAllStringFunc(src, func(t string) string {
			md, ok := convert(t)
			if !ok {
				kept[p]++
				return t
			}
			changed++
			if !*write {
				fmt.Printf("--- %s\n%s\n\n", p, md)
			}
			return md
		})
		if changed == 0 {
			return nil
		}
		// A Markdown table needs a blank line before and after it.
		out = regexp.MustCompile(`([^\n])\n(\|[^\n]*\|\n\| -)`).ReplaceAllString(out, "$1\n\n$2")
		out = regexp.MustCompile(`(\n\|[^\n]*\|)\n([^|\n])`).ReplaceAllString(out, "$1\n\n$2")
		fmt.Printf("%s: %d tables\n", p, changed)
		if *write {
			return os.WriteFile(p, []byte(out), 0o644)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	for p, n := range kept {
		fmt.Printf("kept as HTML: %s (%d)\n", p, n)
	}
}
