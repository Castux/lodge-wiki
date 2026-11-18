package server

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testWiki makes a content repo (one page, two accounts) and a state
// folder next to it, and returns a server acting as the given account.
func testWiki(t *testing.T, as string) (srv *Server, root, state string) {
	t.Helper()
	dir := t.TempDir()
	root, state = filepath.Join(dir, "data"), filepath.Join(dir, "state")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(filepath.Join(root, "_config"), 0o755)
	os.MkdirAll(filepath.Join(root, "_media", "a"), 0o755)
	os.WriteFile(filepath.Join(root, "start.md"), []byte("# Home\n"), 0o644)
	os.WriteFile(filepath.Join(root, "_media", "a", "pic.png"), []byte("not really a png"), 0o644)
	os.WriteFile(filepath.Join(root, "_config", "users.json"), []byte(`{
  "reader": {"hash": "$2a$10$secrethashsecrethashsecrethashsecrethashsecrethashsec", "role": 1},
  "editor": {"hash": "$2a$10$secrethashsecrethashsecrethashsecrethashsecrethashsec", "role": 2},
  "tinkerer": {"hash": "$2a$10$secrethashsecrethashsecrethashsecrethashsecrethashsec", "role": 3}
}
`), 0o644)
	exec.Command("git", "init", "-q", root).Run()
	git("add", "-A")
	git("commit", "-qm", "start")
	srv, err := New(Config{Root: root, State: state, Title: "t", DevUser: as})
	if err != nil {
		t.Fatal(err)
	}
	return srv, root, state
}

func do(srv *Server, method, target string, form url.Values, header map[string]string) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func headOf(t *testing.T, root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// Revisions from requests must never reach git as options.
func TestRevisionInjection(t *testing.T) {
	srv, root, _ := testWiki(t, "reader")
	for _, target := range []string{
		"/_/diff/start?rev=--output=pwned",
		"/_/rev/start?rev=--output=pwned",
		"/_/diff/start?rev=HEAD",
	} {
		if w := do(srv, "GET", target, nil, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", target, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "pwned")); err == nil {
		t.Error("git wrote the file named in --output")
	}
	if w := do(srv, "GET", "/_/diff/start?rev="+headOf(t, root), nil, nil); w.Code != http.StatusOK {
		t.Errorf("a real revision: status %d", w.Code)
	}
}

// Private files must not leak through git's pathspec patterns.
func TestPrivatePathspec(t *testing.T) {
	srv, root, _ := testWiki(t, "reader")
	rev := headOf(t, root)
	for _, p := range []string{"*config/users.json", ":(top)_config/users.json", "_config/users.json", "%5Fconfig/users.json"} {
		w := do(srv, "GET", "/_/diff/"+p+"?rev="+rev, nil, nil)
		if strings.Contains(w.Body.String(), "secrethash") {
			t.Errorf("%s: the accounts file leaked (status %d)", p, w.Code)
		}
	}
}

// An editor must not move files from outside the media folder into it.
func TestMediaMoveEscape(t *testing.T) {
	srv, root, state := testWiki(t, "editor")
	key := filepath.Join(state, "session.key")
	for from, to := range map[string]string{
		"_media/../../state/session.key": "session.key", // same extension: only the path check stops it
		"_media/../_config/users.json":   "users.json",
		"_media/.trash":                  "bin",
		"../state/session.key.md":        "x",
	} {
		w := do(srv, "POST", "/_/mv", url.Values{"path": {from}, "to": {to}}, nil)
		if w.Code == http.StatusSeeOther {
			t.Errorf("move %s was accepted", from)
		}
	}
	if _, err := os.Stat(key); err != nil {
		t.Errorf("the session key moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "_config", "users.json")); err != nil {
		t.Errorf("the accounts file moved: %v", err)
	}
	// A legitimate media move still works.
	if w := do(srv, "POST", "/_/mv", url.Values{"path": {"_media/a/pic.png"}, "to": {"a/picture.png"}}, nil); w.Code != http.StatusSeeOther {
		t.Errorf("media move: status %d: %s", w.Code, w.Body)
	}
}

// Widgets reach ordinary files only; media serving stays in _media.
func TestRawAndMediaPaths(t *testing.T) {
	srv, _, _ := testWiki(t, "editor")
	for _, target := range []string{"/_/raw/_config/users.json", "/_/raw/_layout/x.json", "/_/raw/.git/config.json"} {
		if w := do(srv, "GET", target, nil, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", target, w.Code)
		}
	}
	for _, target := range []string{"/_media/.trash/x.png", "/_media/a/../../_config/users.json"} {
		if w := do(srv, "GET", target, nil, nil); strings.Contains(w.Body.String(), "secrethash") || w.Code == http.StatusOK {
			t.Errorf("%s: status %d", target, w.Code)
		}
	}
}

// Logged out, a private wiki serves its look (so the login page is styled)
// and nothing else.
func TestLayoutPublic(t *testing.T) {
	srv, root, _ := testWiki(t, "")
	os.MkdirAll(filepath.Join(root, "_layout"), 0o755)
	os.WriteFile(filepath.Join(root, "_layout", "style.css"), []byte("body{}"), 0o644)
	os.MkdirAll(filepath.Join(root, "_widgets", "w"), 0o755)
	os.WriteFile(filepath.Join(root, "_widgets", "w", "w.js"), []byte("//"), 0o644)
	if w := do(srv, "GET", "/_layout/style.css", nil, nil); w.Code != http.StatusOK {
		t.Errorf("layout: status %d, want 200", w.Code)
	}
	if w := do(srv, "GET", "/_/login", nil, nil); w.Code != http.StatusOK {
		t.Errorf("login page: status %d", w.Code)
	}
	os.WriteFile(filepath.Join(root, "_layout", "sidebar.md"), []byte("- [Secret](/secret)\n"), 0o644)
	os.WriteFile(filepath.Join(root, "_layout", "page.html"), []byte("{{.Sidebar}}{{range .Tools}}{{.Name}} {{end}}{{.Content}}"), 0o644)
	if body := do(srv, "GET", "/_/login", nil, nil).Body.String(); strings.Contains(body, "Secret") || strings.Contains(body, "recent") {
		t.Errorf("the login page shows the sidebar or recent changes to a visitor:\n%s", body)
	}
	for _, target := range []string{"/start", "/", "/_widgets/w/w.js", "/_layout/../_config/users.json", "/_layout/sidebar.md",
		"/_config/_layout/x", "/a/_layout", "/_media/a/pic.png", "/_/raw/start.md"} {
		w := do(srv, "GET", target, nil, nil)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "secrethash") {
			t.Errorf("%s: status %d while logged out", target, w.Code)
		}
	}
}

// A form posted from another site with the visitor's cookie is refused.
func TestCrossOriginRefused(t *testing.T) {
	srv, _, _ := testWiki(t, "editor")
	w := do(srv, "POST", "/_/edit/start", url.Values{"text": {"# Hacked"}, "base": {"-"}}, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-site POST: status %d, want 403", w.Code)
	}
	w = do(srv, "POST", "/_/edit/start", url.Values{"text": {"# Home again"}, "base": {"-"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if w.Code != http.StatusSeeOther {
		t.Errorf("same-origin POST: status %d, want 303", w.Code)
	}
}

// The nearest _layout/sidebar.md is rendered into the layout.
func TestSidebar(t *testing.T) {
	srv, root, _ := testWiki(t, "reader")
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(s), 0o644)
	}
	write("_layout/page.html", "<nav>{{.Sidebar}}</nav>{{.Content}}")
	write("_layout/sidebar.md", "- [Wiki help](/wiki/)\n")
	write("camp/_layout/sidebar.md", "**People**\n\n- [Characters](/camp/pc/)\n")
	write("camp/a.md", "# A\n")
	write("other/b.md", "# B\n")
	srv.store.Reindex()
	for target, want := range map[string]string{
		"/":        "Wiki help",
		"/other/b": "Wiki help",
		"/camp/a":  "Characters",
	} {
		body := do(srv, "GET", target, nil, nil).Body.String()
		if !strings.Contains(body, "<nav>") || !strings.Contains(body, want) {
			t.Errorf("%s: sidebar without %q:\n%s", target, want, body)
		}
	}
	if body := do(srv, "GET", "/camp/a", nil, nil).Body.String(); strings.Contains(body, "Wiki help") {
		t.Error("/camp/a: got the root sidebar too")
	}
	// Editing a sidebar goes back to Files, with or without ".md".
	for _, u := range []string{"/_/edit/camp/_layout/sidebar.md", "/_/edit/camp/_layout/sidebar"} {
		r := httptest.NewRequest("GET", u, nil)
		if id := idFrom(r, "/_/edit/"); id != "camp/_layout/sidebar.md" || !isFile(id) {
			t.Errorf("%s: id %q", u, id)
		}
	}
	// Markdown in "_" folders is a file to edit in Files, not a page.
	for id, want := range map[string]bool{"_layout/sidebar.md": true, "camp/_layout/sidebar.md": true,
		"_widgets/map/README.md": true, "camp/a": false, "camp/a.md": false, "camp/data.json": true} {
		if isFile(id) != want {
			t.Errorf("isFile(%q) = %v", id, !want)
		}
	}
}

// Built-in files are linked with their version and kept by browsers; the
// page list and unversioned files are revalidated with an ETag.
func TestCaching(t *testing.T) {
	srv, _, _ := testWiki(t, "reader")
	body := do(srv, "GET", "/start", nil, nil).Body.String()
	if !strings.Contains(body, staticURL("core.js")) {
		t.Fatalf("page does not link the versioned core.js:\n%s", body)
	}
	w := do(srv, "GET", staticURL("core.js"), nil, nil)
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("versioned core.js: Cache-Control %q", cc)
	}
	for _, target := range []string{"/_/static/core.js", "/_/titles"} {
		first := do(srv, "GET", target, nil, nil)
		etag := first.Header().Get("ETag")
		if etag == "" || first.Code != http.StatusOK {
			t.Fatalf("%s: status %d, ETag %q", target, first.Code, etag)
		}
		if again := do(srv, "GET", target, nil, map[string]string{"If-None-Match": etag}); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Errorf("%s with its ETag: status %d, %d bytes", target, again.Code, again.Body.Len())
		}
	}
}

// The backup holds the pages, the media, the history and instructions, for
// admins only.
func TestBackup(t *testing.T) {
	srv, root, _ := testWiki(t, "editor")
	if w := do(srv, "GET", "/_/backup", nil, nil); w.Code == http.StatusOK {
		t.Fatal("an editor got the backup")
	}
	os.WriteFile(filepath.Join(root, "_config", "users.json"), []byte(`{"boss": {"hash": "$2a$10$secrethashsecrethashsecrethashsecrethashsecrethashsec", "role": 4}}`), 0o644)
	srv, err := New(Config{Root: root, State: t.TempDir(), Title: "t", DevUser: "boss"})
	if err != nil {
		t.Fatal(err)
	}
	w := do(srv, "GET", "/_/backup", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin: status %d: %s", w.Code, w.Body)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range zr.File {
		have[f.Name[strings.Index(f.Name, "/")+1:]] = true
	}
	for _, want := range []string{"README.txt", "lodge-data.git.bundle", "lodge-data/start.md", "lodge-data/_media/a/pic.png", "lodge-data/_config/users.json"} {
		if !have[want] {
			t.Errorf("missing %s in %v", want, have)
		}
	}
	for name := range have {
		if strings.HasPrefix(name, "lodge-data/.git/") {
			t.Fatalf("the .git folder is in the zip: %s", name)
		}
	}
}

// Layouts and widgets: tinkerers and admins only; settings: admins only.
func TestTinkerer(t *testing.T) {
	try := func(as, file string) int {
		srv, _, _ := testWiki(t, as)
		return do(srv, "POST", "/_/edit/"+file, url.Values{"text": {"x"}}, nil).Code
	}
	for _, c := range []struct {
		as, file string
		ok       bool
	}{
		{"editor", "newpage", true},
		{"editor", "_layout/style.css", false},
		{"editor", "_widgets/dice/widget.js", false},
		{"tinkerer", "_layout/style.css", true},
		{"tinkerer", "_widgets/dice/widget.js", true},
		{"tinkerer", "_config/notifications.json", false},
		{"tinkerer", "_config/users.json", false},
	} {
		if code := try(c.as, c.file); (code == http.StatusSeeOther) != c.ok {
			t.Errorf("%s saving %s: status %d", c.as, c.file, code)
		}
	}
	srv, _, _ := testWiki(t, "tinkerer")
	if w := do(srv, "GET", "/_/users", nil, nil); w.Code != http.StatusForbidden {
		t.Errorf("tinkerer on the users page: status %d", w.Code)
	}
}

// The running version is shown to admins only, with newer commits if known.
func TestVersion(t *testing.T) {
	srv, root, _ := testWiki(t, "editor")
	srv.cfg.Version = "0123456789abcdef0123456789abcdef01234567 2026-11-18"
	if strings.Contains(do(srv, "GET", "/start", nil, nil).Body.String(), "lodge-wiki 0123456") {
		t.Error("an editor sees the version")
	}
	os.WriteFile(filepath.Join(root, "_config", "users.json"), []byte(`{"boss": {"hash": "$2a$10$secrethashsecrethashsecrethashsecrethashsecrethashsec", "role": 4}}`), 0o644)
	srv, err := New(Config{Root: root, State: t.TempDir(), Title: "t", DevUser: "boss", Version: srv.cfg.Version})
	if err != nil {
		t.Fatal(err)
	}
	srv.newer.Store(3)
	body := do(srv, "GET", "/start", nil, nil).Body.String()
	for _, want := range []string{"lodge-wiki 0123456, 2026-11-18", "3 new", "/compare/0123456789abcdef0123456789abcdef01234567...main"} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page lacks %q", want)
		}
	}
}
