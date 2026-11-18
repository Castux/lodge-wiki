package server

import (
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
  "editor": {"hash": "$2a$10$secrethashsecrethashsecrethashsecrethashsecrethashsec", "role": 2}
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
