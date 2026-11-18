// Command check is a verbose variant of tools/checkrender used while
// fixing conversion problems: it prints every leftover with its line of
// visible text, and every missing link with all its sources.
//
//	go run ./migrate/fixes/check ../lodge-data [kind]
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
	"[[":      regexp.MustCompile(`\[\[`),
	"{{":      regexp.MustCompile(`\{\{`),
	"**":      regexp.MustCompile(`\*\*`),
	"//":      regexp.MustCompile(`(^|\s)//\S`),
	"''":      regexp.MustCompile(`''\S`),
	"====":    regexp.MustCompile(`====`),
	"<wrap":   regexp.MustCompile(`(?i)<wrap`),
	"\\\\":    regexp.MustCompile(`\\`),
	"&lt;tag": regexp.MustCompile(`<[a-zA-Z]+[ >]`),
	"oldsite": regexp.MustCompile(`the-chasers-lodge\.net`),
}

func main() {
	store, err := wiki.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	only := ""
	if len(os.Args) > 2 {
		only = os.Args[2]
	}
	r := &render.Renderer{Store: store}
	idx := store.Index()
	ids := make([]string, 0, len(idx.Pages))
	for id := range idx.Pages {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	missing := map[string][]string{}
	reMissing := regexp.MustCompile(`<a href="([^"]*)"[^>]*wikilink2`)
	reOld := regexp.MustCompile(`href="(https?://(?:www\.)?the-chasers-lodge\.net[^"]*)"`)
	for _, id := range ids {
		p, _ := store.ReadPage(id)
		html := r.Render(p).HTML
		for _, m := range reMissing.FindAllStringSubmatch(html, -1) {
			missing[m[1]] = append(missing[m[1]], id)
		}
		if only == "" || only == "oldsite" {
			for _, m := range reOld.FindAllStringSubmatch(html, -1) {
				fmt.Printf("oldsite\t%s\t%s\n", id, m[1])
			}
		}
		for line := range strings.SplitSeq(visibleText(html), "\n") {
			for name, re := range leftovers {
				if (only == "" || only == name) && re.MatchString(line) {
					l := line
					if len(l) > 160 {
						loc := re.FindStringIndex(l)
						s := max(0, loc[0]-70)
						l = l[s:min(len(l), loc[0]+90)]
					}
					fmt.Printf("%s\t%s\t%s\n", name, id, l)
				}
			}
		}
	}
	if only == "" || only == "missing" {
		keys := make([]string, 0, len(missing))
		for k := range missing {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("missing\t%s\t%s\n", k, strings.Join(uniq(missing[k]), ", "))
		}
	}
}

func uniq(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
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
			switch string(n) {
			case "pre", "code", "script", "style":
				skip++
			case "p", "li", "br", "div", "h1", "h2", "h3", "h4", "h5", "h6", "tr", "td", "th":
				b.WriteByte('\n')
			}
		case nethtml.EndTagToken:
			n, _ := z.TagName()
			if s := string(n); (s == "pre" || s == "code" || s == "script" || s == "style") && skip > 0 {
				skip--
			}
		case nethtml.TextToken:
			if skip == 0 {
				b.WriteString(nethtml.UnescapeString(string(z.Text())))
			}
		}
	}
}
