package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

func testStore(t *testing.T, pages map[string]string) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	for id, body := range pages {
		p := filepath.Join(dir, filepath.FromSlash(id)+".md")
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("git", "init", "-q", dir).Run(); err != nil {
		t.Skip("git not available")
	}
	s, err := wiki.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func render(t *testing.T, s *wiki.Store, id, body string) Result {
	t.Helper()
	return (&Renderer{Store: s}).Render(wiki.ParsePage(id, []byte(body)))
}

func contains(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Errorf("missing %s in\n%s", w, html)
		}
	}
}

func TestHeadingsAndTOC(t *testing.T) {
	s := testStore(t, nil)
	res := render(t, s, "a", "# Title\n\nIntro\n\n## One\n\nx\n\n### Deep\n\n## One\n\ny\n")
	contains(t, res.HTML, `<h1 id="title">`, `<h2 id="one">`, `<h2 id="one1">`)
	if strings.Contains(res.HTML, "level1") || strings.Contains(res.HTML, `class="li"`) {
		t.Errorf("DokuWiki wrappers left in:\n%s", res.HTML)
	}
	want := `<nav class="toc" aria-label="Contents"><h2>Contents</h2><ul><li><a href="#title">Title</a><ul><li><a href="#one">One</a><ul><li><a href="#deep">Deep</a></li></ul></li><li><a href="#one1">One</a></li></ul></li></ul></nav>`
	if res.TOC != want {
		t.Errorf("TOC:\n got %s\nwant %s", res.TOC, want)
	}
	if res := render(t, s, "a", "# A\n## B\n"); res.TOC != "" {
		t.Errorf("TOC shown with fewer than 3 headings")
	}
}

func TestLinks(t *testing.T) {
	s := testStore(t, map[string]string{"ns/there": "# There\n", "ns/sub/x": "# X\n"})
	html := render(t, s, "ns/page", "[a](/ns/there) [b](/ns/missing) [c](there) [d](https://example.com) [e](/ns/sub/)").HTML
	contains(t, html,
		`<a href="/ns/there">a</a>`,
		`<a href="/ns/missing" class="missing"`,
		`<a href="/ns/there">c</a>`, // relative link resolved
		`class="external"`,
		`<a href="/ns/sub/">e</a>`, // namespace without start page
	)
}

func TestImagesAndTables(t *testing.T) {
	s := testStore(t, nil)
	html := render(t, s, "a", `<img src="/_media/a/b.jpg" width="200" align="right">`+"\n\n![x](/_media/a/c.png)\n\n| a |\n| - |\n| 1 |\n").HTML
	contains(t, html, `src="/_media/a/b.jpg?w=200"`, `class="right"`, `<a href="/_media/a/c.png" class="image"`, `<div class="table-wrap"><table>`, `</table></div>`)
}

func TestListAndWidget(t *testing.T) {
	s := testStore(t, map[string]string{"n/b": "# Bravo\n", "n/a": "# Alpha\n", "n/sub/c": "# C\n"})
	html := render(t, s, "n/start", "```list\nns: /n\ncolumns: 2\n```\n\n```widget map\ndata: x.json\n```\n").HTML
	if i, j := strings.Index(html, "Alpha"), strings.Index(html, "Bravo"); i < 0 || j < 0 || i > j {
		t.Errorf("list order wrong:\n%s", html)
	}
	contains(t, html, `<div class="pagelist"><ul style="--columns: 2">`, `data-widget="map"`, `{&#34;data&#34;:&#34;x.json&#34;}`)
	if strings.Contains(html, ">C<") {
		t.Errorf("sub-namespace page listed without recursive")
	}
}

func TestNaturalLess(t *testing.T) {
	if !naturalLess("part 2", "part 10") || naturalLess("part 10", "part 2") {
		t.Error("natural order")
	}
}

func TestImageLayout(t *testing.T) {
	s := testStore(t, nil)
	cases := []struct{ in, want string }{
		// alone: a picture paragraph, capped width
		{"![Marak](/_media/a/marak.jpg)", `<p class="pic"><a href="/_media/a/marak.jpg" class="image" target="_blank"><img src="/_media/a/marak.jpg?w=800" alt="Marak" loading="lazy"></a></p>`},
		// presets in the fragment become classes and a matching size
		{"![Crest](/_media/a/crest.png#small-right)", `<img src="/_media/a/crest.png?w=200" alt="Crest" loading="lazy" class="small right">`},
		{"![M](/_media/a/m.jpg#medium)", `<img src="/_media/a/m.jpg?w=400" alt="M" loading="lazy" class="medium">`},
		// not a preset: no class (the fragment means nothing on an image)
		{"![M](/_media/a/m.jpg#top)", `<img src="/_media/a/m.jpg?w=800" alt="M" loading="lazy">`},
		// a title makes a figure with a caption
		{`![Marak](/_media/a/marak.jpg "Marak at the siege")`, `<figure><a href="/_media/a/marak.jpg" class="image" target="_blank"><img src="/_media/a/marak.jpg?w=800" alt="Marak" loading="lazy"></a><figcaption>Marak at the siege</figcaption></figure>`},
		// images only: a gallery
		{"![A](/_media/a/1.jpg)\n![B](/_media/a/2.jpg)", `<p class="gallery">`},
		// with text: a plain paragraph
		{"Text ![A](/_media/a/1.jpg#right) more", `<p>Text <a`},
		// explicit HTML sizes still win; svg is never scaled
		{`<img src="/_media/a/g.png" width="40" align="left">`, `src="/_media/a/g.png?w=40"`},
		{"![S](/_media/a/s.svg)", `src="/_media/a/s.svg"`},
	}
	for _, c := range cases {
		contains(t, render(t, s, "a", c.in+"\n").HTML, c.want)
	}
}
