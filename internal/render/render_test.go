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
	cmd := exec.Command("git", "init", "-q", dir)
	if err := cmd.Run(); err != nil {
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

func TestSectionsAndTOC(t *testing.T) {
	s := testStore(t, nil)
	res := render(t, s, "a", "# Title\n\nIntro\n\n## One\n\nx\n\n## One\n\ny\n")
	for _, want := range []string{`<h1 id="title">`, `<div class="level1">`, `<h2 id="one">`, `<h2 id="one1">`, `<div class="level2">`} {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("missing %s in\n%s", want, res.HTML)
		}
	}
	if res.TOC == "" || !strings.Contains(res.TOC, `href="#one1"`) {
		t.Errorf("TOC: %s", res.TOC)
	}
	if res := render(t, s, "a", "---\ntoc: false\n---\n# A\n## B\n## C\n"); res.TOC != "" {
		t.Errorf("toc: false ignored")
	}
}

func TestLinks(t *testing.T) {
	s := testStore(t, map[string]string{"ns/there": "# There\n", "ns/sub/x": "# X\n"})
	html := render(t, s, "ns/page", "[a](/ns/there) [b](/ns/missing) [c](there) [d](https://example.com) [e](/ns/sub/)").HTML
	for _, want := range []string{
		`href="/ns/there" class="wikilink1"`,
		`href="/ns/missing" class="wikilink2"`,
		`href="/ns/there" class="wikilink1"`, // relative resolved
		`class="urlextern"`,
		`href="/ns/sub/" class="wikilink1"`, // namespace without start page
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in\n%s", want, html)
		}
	}
}

func TestImages(t *testing.T) {
	s := testStore(t, nil)
	html := render(t, s, "a", `<img src="/_media/a/b.jpg" width="200" align="right">`+"\n\n![x](/_media/a/c.png)\n").HTML
	for _, want := range []string{`src="/_media/a/b.jpg?w=200"`, `class="mediaright"`, `<a href="/_media/a/c.png" class="media"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in\n%s", want, html)
		}
	}
}

func TestListAndWidget(t *testing.T) {
	s := testStore(t, map[string]string{"n/b": "# Bravo\n", "n/a": "# Alpha\n", "n/sub/c": "# C\n"})
	html := render(t, s, "n/start", "```list\nns: /n\n```\n\n```widget map\ndata: x.json\n```\n").HTML
	if i, j := strings.Index(html, "Alpha"), strings.Index(html, "Bravo"); i < 0 || j < 0 || i > j {
		t.Errorf("list order wrong:\n%s", html)
	}
	if strings.Contains(html, ">C<") {
		t.Errorf("sub-namespace page listed without recursive")
	}
	if !strings.Contains(html, `data-widget="map"`) || !strings.Contains(html, `{&#34;data&#34;:&#34;x.json&#34;}`) {
		t.Errorf("widget block:\n%s", html)
	}
}

func TestNaturalLess(t *testing.T) {
	if !naturalLess("part 2", "part 10") || naturalLess("part 10", "part 2") {
		t.Error("natural order")
	}
}
