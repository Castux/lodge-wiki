// Command checkrender renders every page of a content repo and reports
// suspicious output: leftover DokuWiki syntax and links to missing pages.
//
//	go run ./tools/checkrender ../lodge-data
package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Castux/lodge-wiki/internal/render"
	"github.com/Castux/lodge-wiki/internal/wiki"
	nethtml "golang.org/x/net/html"
)

var leftovers = map[string]*regexp.Regexp{
	"[[":       regexp.MustCompile(`\[\[`),
	"{{":       regexp.MustCompile(`\{\{`),
	"**":       regexp.MustCompile(`\*\*`),
	"//":       regexp.MustCompile(`(^|\s)//\S`),
	"''":       regexp.MustCompile(`''\S`),
	"====":     regexp.MustCompile(`====`),
	"<wrap":    regexp.MustCompile(`(?i)<wrap`),
	"\\\\":     regexp.MustCompile(`\\`),
	"&lt;tag":  regexp.MustCompile(`<[a-zA-Z]+[ >]`),
}

func main() {
	store, err := wiki.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	r := &render.Renderer{Store: store}
	idx := store.Index()
	ids := make([]string, 0, len(idx.Pages))
	for id := range idx.Pages {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	missing := map[string][]string{}
	found := map[string][]string{}
	for _, id := range ids {
		p, _ := store.ReadPage(id)
		html := r.Render(p).HTML
		for _, href := range regexp.MustCompile(`class="[^"]*wikilink2[^"]*"`).FindAllString(html, -1) {
			_ = href
		}
		for _, m := range regexp.MustCompile(`<a href="([^"]*)"[^>]*wikilink2`).FindAllStringSubmatch(html, -1) {
			missing[m[1]] = append(missing[m[1]], id)
		}
		text := visibleText(html)
		for name, re := range leftovers {
			if re.MatchString(text) {
				found[name] = append(found[name], id)
			}
		}
	}
	fmt.Printf("%d pages\n\nLeftover syntax in visible text:\n", len(ids))
	for name, pages := range found {
		fmt.Printf("  %-8s %3d pages, e.g. %s\n", name, len(pages), strings.Join(pages[:min(4, len(pages))], ", "))
	}
	type kv struct {
		k string
		v []string
	}
	var ms []kv
	for k, v := range missing {
		ms = append(ms, kv{k, v})
	}
	sort.Slice(ms, func(i, j int) bool { return len(ms[i].v) > len(ms[j].v) })
	fmt.Printf("\nLinks to missing pages: %d distinct targets\n", len(ms))
	for _, m := range ms[:min(40, len(ms))] {
		fmt.Printf("  %3d × %s  (from %s)\n", len(m.v), m.k, m.v[0])
	}
}

func visibleText(src string) string {
	z := nethtml.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case nethtml.ErrorToken:
			return b.String()
		case nethtml.StartTagToken:
			n, _ := z.TagName()
			if s := string(n); s == "pre" || s == "code" || s == "script" || s == "style" {
				skip++
			}
		case nethtml.EndTagToken:
			n, _ := z.TagName()
			if s := string(n); (s == "pre" || s == "code" || s == "script" || s == "style") && skip > 0 {
				skip--
			}
		case nethtml.TextToken:
			if skip == 0 {
				b.WriteString(nethtml.UnescapeString(string(z.Text())))
				b.WriteByte('\n')
			}
		}
	}
}
