// Command scan looks for conversion failures in the markdown source of a
// content repo, beyond what tools/checkrender sees in the rendered text.
//
//	go run ./migrate/fixes/scan ../lodge-data ../public_html/data/pages
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type check struct {
	name string
	re   *regexp.Regexp
}

var checks = []check{
	{"line starts with \\<", regexp.MustCompile(`^\s*\\<`)},
	{"escape runs", regexp.MustCompile(`(\\[\[\]*_]\S*){3,}|\\\[\\\[|\\\]\\\]`)},
	{"DokuWiki table row (^)", regexp.MustCompile(`^\s*\^.*\^\s*$`)},
	{"DokuWiki * list item", regexp.MustCompile(`^  +\* `)},
	{"{{ media", regexp.MustCompile(`\{\{`)},
	{"[[ link", regexp.MustCompile(`\[\[`)},
	{"<WRAP / <nspages / <code / <file", regexp.MustCompile(`(?i)<(WRAP|nspages|code|file)[\s>]`)},
	{"%% nowiki", regexp.MustCompile(`%%`)},
	{"==== heading", regexp.MustCompile(`={4,}`)},
	{"empty heading", regexp.MustCompile(`^#{1,6}\s*$`)},
	{"colon target", regexp.MustCompile(`\]\((?:[^)\s:/#][^)\s]*:[^)\s]*|/[^)\s]*:[^)\s]*)\)`)},
	{"empty link", regexp.MustCompile(`(^|[^!\\])\[\]\(`)},
	{".md link", regexp.MustCompile(`\]\([^)\s]*\.md(#[^)]*)?\)`)},
	{"entity soup", regexp.MustCompile(`&amp;(lt|gt|amp|quot|#\d+);`)},
	{"// italics marker", regexp.MustCompile(`(^|\s)//\S`)},
}

var (
	reMedia = regexp.MustCompile(`(?:\]\(|src=")(/_media/[^)"\s?#]+)`)
	reFence = regexp.MustCompile("^\\s*(```|~~~)")
)

func main() {
	root, src := os.Args[1], os.Args[2]
	hits := map[string][]string{}
	var short []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), "_") || d.Name() == ".git" || rel == "calendar" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".md") || rel == "README.md" {
			return nil
		}
		id := strings.TrimSuffix(rel, ".md")
		b, _ := os.ReadFile(p)
		inFence := false
		for n, line := range strings.Split(string(b), "\n") {
			if reFence.MatchString(line) {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			for _, c := range checks {
				if c.re.MatchString(line) {
					hits[c.name] = append(hits[c.name], fmt.Sprintf("%s:%d: %s", id, n+1, clip(line)))
				}
			}
			for _, m := range reMedia.FindAllStringSubmatch(line, -1) {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(m[1][1:]))); err != nil {
					hits["missing media"] = append(hits["missing media"], fmt.Sprintf("%s:%d: %s", id, n+1, m[1]))
				}
			}
		}
		if sb, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(id)+".txt")); err == nil {
			if len(sb) > 600 && len(b)*2 < len(sb) {
				short = append(short, fmt.Sprintf("%s: md %d bytes, source %d bytes", id, len(b), len(sb)))
			}
		}
		return nil
	})
	hits["much shorter than source"] = short
	names := make([]string, 0, len(hits))
	for k := range hits {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("== %s (%d)\n", k, len(hits[k]))
		for _, h := range hits[k] {
			fmt.Println("  " + h)
		}
	}
}

func clip(s string) string {
	if len(s) > 150 {
		return s[:150] + "…"
	}
	return s
}
