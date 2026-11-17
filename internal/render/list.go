package render

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Castux/lodge-wiki/internal/wiki"
	"gopkg.in/yaml.v3"
)

// ListOptions are the options of a ```list block (see docs/syntax.md).
type ListOptions struct {
	NS         *string  `yaml:"ns"`
	Exclude    []string `yaml:"exclude"`
	Sort       string   `yaml:"sort"`
	Natural    bool     `yaml:"natural"`
	Reverse    bool     `yaml:"reverse"`
	Columns    int      `yaml:"columns"`
	Numbered   bool     `yaml:"numbered"`
	Pictures   bool     `yaml:"pictures"`
	Recursive  bool     `yaml:"recursive"`
	Namespaces bool     `yaml:"namespaces"`
	Tree       bool     `yaml:"tree"`
	Heading    string   `yaml:"heading"`
	NSHeading  string   `yaml:"namespaces_heading"`
}

type listItem struct {
	id, title, image string
	info             *wiki.PageInfo
	isNS             bool
}

func (pc *pageCtx) renderList(body string) string {
	var o ListOptions
	if err := yaml.Unmarshal([]byte(body), &o); err != nil {
		return `<div class="error">list: ` + html.EscapeString(err.Error()) + `</div>`
	}
	idx := pc.r.Store.Index()
	ns := wiki.Namespace(pc.id)
	if o.NS != nil {
		r := wiki.Resolve(ns, *o.NS+"/")
		ns = strings.TrimSuffix(r, "/"+wiki.StartPage)
		if r == wiki.StartPage {
			ns = ""
		}
		if *o.NS == "" || *o.NS == "/" {
			ns = ""
		}
	}
	excluded := map[string]bool{}
	for _, e := range o.Exclude {
		if e == "self" {
			excluded[pc.id] = true
		} else {
			excluded[strings.Trim(ns+"/"+wiki.CleanID(e), "/")] = true
		}
	}

	if o.Tree {
		return `<div class="plugin_nspages">` + pc.tree(idx, ns, excluded, o) + `</div>`
	}

	var items, nsItems []listItem
	var collect func(n string)
	collect = func(n string) {
		pages, subs := idx.Children(n)
		for _, p := range pages {
			if excluded[p.ID] {
				continue
			}
			items = append(items, listItem{id: p.ID, title: p.Title, image: p.Image, info: p})
		}
		for _, s := range subs {
			if o.Namespaces && n == ns {
				start := s + "/" + wiki.StartPage
				t := wiki.Name(s)
				if p, ok := idx.Pages[start]; ok {
					t = p.Title
				}
				nsItems = append(nsItems, listItem{id: start, title: t, isNS: true, info: idx.Pages[start]})
			}
			if o.Recursive {
				collect(s)
			}
		}
	}
	collect(ns)
	sortItems(items, o)
	sortItems(nsItems, ListOptions{Sort: "title"})

	var b strings.Builder
	b.WriteString(`<div class="plugin_nspages">`)
	if len(nsItems) > 0 {
		if o.NSHeading != "" {
			b.WriteString(`<p class="catpageheadline">` + html.EscapeString(o.NSHeading) + `</p>`)
		}
		pc.columns(&b, nsItems, o)
	}
	if o.Heading != "" {
		b.WriteString(`<p class="catpageheadline">` + html.EscapeString(o.Heading) + `</p>`)
	}
	if o.Pictures {
		for _, it := range items {
			img := it.image
			if img == "" {
				img = "/_layout/default-picture.png"
			} else if !strings.Contains(img, "?") {
				img += "?w=200"
			}
			fmt.Fprintf(&b, `<a href="%s" class="media" title="%s"><img src="%s" class="media" loading="lazy" alt="" width="200"></a>`,
				html.EscapeString(wiki.PageURL(it.id)), html.EscapeString(it.title), html.EscapeString(img))
		}
	} else {
		pc.columns(&b, items, o)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func (pc *pageCtx) columns(b *strings.Builder, items []listItem, o ListOptions) {
	cols := o.Columns
	if cols <= 0 {
		cols = 3
	}
	if len(items) < cols {
		cols = max(1, len(items))
	}
	per := (len(items) + cols - 1) / cols
	tag := "ul"
	if o.Numbered {
		tag = "ol"
	}
	width := 100 / cols
	for c := 0; c < cols; c++ {
		lo, hi := c*per, min((c+1)*per, len(items))
		if lo >= hi {
			break
		}
		fmt.Fprintf(b, `<div class="catpagecol" style="width: %d%%"><%s`, width, tag)
		if o.Numbered && lo > 0 {
			fmt.Fprintf(b, ` start="%d"`, lo+1)
		}
		b.WriteString(">")
		for _, it := range items[lo:hi] {
			fmt.Fprintf(b, `<li><a href="%s" title="%s">%s</a></li>`,
				html.EscapeString(wiki.PageURL(it.id)), html.EscapeString(it.id), html.EscapeString(it.title))
		}
		fmt.Fprintf(b, "</%s></div>", tag)
	}
	b.WriteString(`<div class="catpageeofidx"></div>`)
}

func (pc *pageCtx) tree(idx *wiki.Index, ns string, excluded map[string]bool, o ListOptions) string {
	pages, subs := idx.Children(ns)
	var items []listItem
	for _, p := range pages {
		if !excluded[p.ID] && wiki.Name(p.ID) != wiki.StartPage {
			items = append(items, listItem{id: p.ID, title: p.Title, info: p})
		}
	}
	sortItems(items, o)
	var b strings.Builder
	b.WriteString("<ul>")
	for _, s := range subs {
		start := s + "/" + wiki.StartPage
		t := wiki.Name(s)
		if p, ok := idx.Pages[start]; ok {
			t = p.Title
		}
		fmt.Fprintf(&b, `<li class="node"><a href="%s">%s</a>%s</li>`,
			html.EscapeString(wiki.PageURL(start)), html.EscapeString(t), pc.tree(idx, s, excluded, o))
	}
	for _, it := range items {
		fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`,
			html.EscapeString(wiki.PageURL(it.id)), html.EscapeString(it.title))
	}
	b.WriteString("</ul>")
	return b.String()
}

func sortItems(items []listItem, o ListOptions) {
	key := func(it listItem) string {
		switch o.Sort {
		case "id":
			return it.id
		}
		return strings.ToLower(it.title)
	}
	less := func(a, b listItem) bool {
		switch o.Sort {
		case "created", "modified":
			var ta, tb int64
			if a.info != nil {
				if o.Sort == "created" {
					ta = a.info.Created.Unix()
				} else {
					ta = a.info.Modified.Unix()
				}
			}
			if b.info != nil {
				if o.Sort == "created" {
					tb = b.info.Created.Unix()
				} else {
					tb = b.info.Modified.Unix()
				}
			}
			return ta < tb
		}
		if o.Natural {
			return naturalLess(key(a), key(b))
		}
		return key(a) < key(b)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if o.Reverse {
			return less(items[j], items[i])
		}
		return less(items[i], items[j])
	})
}

// naturalLess compares strings with digit runs compared by value.
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		da, db := unicode.IsDigit(rune(a[0])), unicode.IsDigit(rune(b[0]))
		if da && db {
			na, ra := leadingNum(a)
			nb, rb := leadingNum(b)
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func leadingNum(s string) (int, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n, s[i:]
}
