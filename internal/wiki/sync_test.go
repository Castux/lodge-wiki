package wiki

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=admin", "-c", "user.email=a@x", "-c", "init.defaultBranch=main"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeT(t *testing.T, dir, file, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(file))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readT(t *testing.T, dir, file string) string {
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// TestSync: a wiki clone and an admin clone of one remote. Changes to
// different files merge; a file changed on both sides keeps the wiki's
// version; everything ends up on the remote.
func TestSync(t *testing.T) {
	root := t.TempDir()
	remote, wikiDir, admin := filepath.Join(root, "remote.git"), filepath.Join(root, "wiki"), filepath.Join(root, "admin")
	gitT(t, root, "init", "-q", "--bare", remote)
	gitT(t, root, "clone", "-q", remote, admin)
	writeT(t, admin, "a.md", "# A\n\none\ntwo\nthree\n")
	writeT(t, admin, "b.md", "# B\n")
	writeT(t, admin, "gone.md", "# Gone\n")
	gitT(t, admin, "add", "-A")
	gitT(t, admin, "commit", "-qm", "start")
	gitT(t, admin, "push", "-q", "-u", "origin", "HEAD")
	gitT(t, root, "clone", "-q", remote, wikiDir)

	g := &Git{Dir: wikiDir}
	if g.Upstream() == "" {
		t.Fatal("no upstream")
	}
	// The wiki edits a.md (line one), deletes gone.md; the admin edits
	// b.md, a.md (line one too: a conflict) and gone.md.
	if _, err := g.Write("a.md", []byte("# A\n\nONE (wiki)\ntwo\nthree\n"), "-", "player", "edit a"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Write("gone.md", nil, "-", "player", "delete gone"); err != nil {
		t.Fatal(err)
	}
	writeT(t, admin, "a.md", "# A\n\nONE (admin)\ntwo\nthree\n")
	writeT(t, admin, "b.md", "# B\n\nadmin edit\n")
	writeT(t, admin, "gone.md", "# Gone\n\nadmin edit\n")
	writeT(t, admin, "new.md", "# New\n")
	gitT(t, admin, "add", "-A")
	gitT(t, admin, "commit", "-qm", "admin edits")
	gitT(t, admin, "push", "-q")

	changed, err := g.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if got := readT(t, wikiDir, "a.md"); !strings.Contains(got, "ONE (wiki)") {
		t.Errorf("conflict: wiki's a.md not kept:\n%s", got)
	}
	if got := readT(t, wikiDir, "b.md"); !strings.Contains(got, "admin edit") {
		t.Errorf("admin's b.md not merged:\n%s", got)
	}
	if got := readT(t, wikiDir, "gone.md"); got != "<missing>" {
		t.Errorf("deleted in the wiki, edited upstream: should stay deleted, got:\n%s", got)
	}
	if !slices.Contains(changed, "b.md") || !slices.Contains(changed, "new.md") {
		t.Errorf("changed = %v", changed)
	}
	st := g.SyncStatus()
	slices.Sort(st.Overridden)
	if !slices.Equal(st.Overridden, []string{"a.md", "gone.md"}) || st.Err != "" {
		t.Errorf("status = %+v", st)
	}
	if s := gitT(t, wikiDir, "status", "--porcelain"); s != "" {
		t.Errorf("working tree not clean:\n%s", s)
	}
	// Everything is on the remote; the admin's version is in the history.
	gitT(t, admin, "pull", "-q", "--no-rebase")
	if got := readT(t, admin, "a.md"); !strings.Contains(got, "ONE (wiki)") {
		t.Errorf("remote lacks the merge:\n%s", got)
	}
	if !strings.Contains(gitT(t, admin, "log", "--all", "--full-history", "-p", "--", "a.md"), "ONE (admin)") {
		t.Error("admin's version lost from history")
	}

	// Nothing new: a no-op.
	if changed, err := g.Sync(); err != nil || len(changed) != 0 {
		t.Errorf("idle sync: %v %v", changed, err)
	}
}
