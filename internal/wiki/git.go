package wiki

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Git runs git commands in the content repository. Writes are serialised
// by mu: this is the single write queue.
type Git struct {
	Dir string
	mu  sync.Mutex

	syncs    chan struct{} // non-nil when syncing with the upstream (see StartSyncing)
	syncMu   sync.Mutex    // one sync at a time
	statusMu sync.Mutex
	status   SyncStatus
}

// gitTimeout bounds every git command (a fetch from an unreachable remote
// must not hang the wiki).
const gitTimeout = 2 * time.Minute

// gitEnv makes every git command safe to run with user input and never
// wait for a person: file arguments are literal paths, never patterns
// (so "*config/users.json" cannot match a private file), and git and
// ssh never prompt.
var gitEnv = []string{"GIT_LITERAL_PATHSPECS=1", "GIT_TERMINAL_PROMPT=0"}

func init() {
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		gitEnv = append(gitEnv, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
}

func (g *Git) run(stdin []byte, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.Dir, "-c", "core.quotepath=off"}, args...)...)
	cmd.Env = slices.Concat(os.Environ(), gitEnv, env)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Revision is one commit touching a file.
type Revision struct {
	Hash    string
	Author  string
	Time    time.Time
	Message string
}

var dokuwikiMove = regexp.MustCompile(`Page moved (?:and renamed )?from (\S+) to `)

// FileHistory is what the history says about one file.
type FileHistory struct {
	Created, Modified time.Time
	Creator           string // author of the commit that created it
	Author            string // author of the last commit
}

// FileTimes walks the whole history once, oldest first, and returns per
// file when it was created and by whom, and when it was last changed and
// by whom. Renames (git mv, and the wiki's own moves) carry the creator
// and creation date along. So do the moves in the history imported from
// DokuWiki, which were a deletion and a creation: their commit message
// says "↷ Page moved from old:id to new:id".
func (g *Git) FileTimes() (map[string]FileHistory, error) {
	out, err := g.run(nil, nil, "log", "--reverse", "-M", "--name-status", "--format=\x01%at\x02%an\x02%s")
	if err != nil {
		return nil, err
	}
	res := map[string]FileHistory{}
	deleted := map[string]FileHistory{} // for moves imported from DokuWiki
	var when time.Time
	var author, movedFrom string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\x01") {
			parts := strings.SplitN(line[1:], "\x02", 3)
			sec, _ := strconv.ParseInt(parts[0], 10, 64)
			when = time.Unix(sec, 0)
			author, movedFrom = "", ""
			if len(parts) > 1 {
				author = parts[1]
			}
			if len(parts) > 2 {
				if m := dokuwikiMove.FindStringSubmatch(parts[2]); m != nil {
					movedFrom = strings.ReplaceAll(m[1], ":", "/") + ".md"
				}
			}
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 || f[0] == "" {
			continue
		}
		switch path, status := f[len(f)-1], f[0][0]; status {
		case 'D':
			deleted[path] = res[path]
			delete(res, path)
		case 'R', 'C':
			h, ok := res[f[1]]
			if !ok {
				h = FileHistory{Created: when, Creator: author}
			}
			if status == 'R' {
				delete(res, f[1])
			}
			h.Modified, h.Author = when, author
			res[path] = h
		default: // A, M, T
			h, ok := res[path]
			if !ok {
				h = FileHistory{Created: when, Creator: author}
				if movedFrom != "" && movedFrom != path {
					if prev, ok := res[movedFrom]; ok && prev.Creator != "" {
						h.Created, h.Creator = prev.Created, prev.Creator
					} else if prev, ok := deleted[movedFrom]; ok && prev.Creator != "" {
						h.Created, h.Creator = prev.Created, prev.Creator
					}
				}
			}
			h.Modified, h.Author = when, author
			res[path] = h
		}
	}
	return res, sc.Err()
}

// Log returns the commits touching a file, newest first.
func (g *Git) Log(file string, limit int) ([]Revision, error) {
	args := []string{"log", "--format=%H\x02%an\x02%at\x02%s", "--follow"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := g.run(nil, nil, append(args, "--", file)...)
	if err != nil {
		return nil, err
	}
	return parseRevs(out), nil
}

// Recent returns the latest commits across the repository with the files
// they touched.
func (g *Git) Recent(limit int) ([]Revision, [][]string, error) {
	out, err := g.run(nil, nil, "log", "--format=\x01%H\x02%an\x02%at\x02%s", "--name-only", "-n", strconv.Itoa(limit))
	if err != nil {
		return nil, nil, err
	}
	var revs []Revision
	var files [][]string
	for _, block := range strings.Split(string(out), "\x01") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		r := parseRevs([]byte(lines[0]))
		if len(r) == 0 {
			continue
		}
		revs = append(revs, r[0])
		var fs []string
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l != "" {
				fs = append(fs, l)
			}
		}
		files = append(files, fs)
	}
	return revs, files, nil
}

func parseRevs(out []byte) []Revision {
	var revs []Revision
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		p := strings.SplitN(line, "\x02", 4)
		if len(p) < 4 {
			continue
		}
		sec, _ := strconv.ParseInt(p[2], 10, 64)
		revs = append(revs, Revision{Hash: p[0], Author: p[1], Time: time.Unix(sec, 0), Message: p[3]})
	}
	return revs
}

// validRev matches a commit hash, full or abbreviated, optionally with
// "^" (its parent). Revisions from requests must match it: anything else
// could be read by git as an option ("--output=…").
var validRev = regexp.MustCompile(`^[0-9a-f]{4,64}\^?$`)

// ValidRev reports whether s is a commit hash that may be passed to git.
func ValidRev(s string) bool { return validRev.MatchString(s) }

// Show returns a file's content at a revision.
func (g *Git) Show(rev, file string) ([]byte, error) {
	if !ValidRev(rev) {
		return nil, fmt.Errorf("invalid revision %q", rev)
	}
	return g.run(nil, nil, "show", "--end-of-options", rev+":"+file)
}

// Info returns one commit's hash, author, time and message.
func (g *Git) Info(rev string) (Revision, error) {
	if !ValidRev(rev) {
		return Revision{}, fmt.Errorf("invalid revision %q", rev)
	}
	out, err := g.run(nil, nil, "log", "-1", "--format=%H\x02%an\x02%at\x02%s", "--end-of-options", rev)
	if err != nil {
		return Revision{}, err
	}
	if revs := parseRevs(out); len(revs) == 1 {
		return revs[0], nil
	}
	return Revision{}, fmt.Errorf("revision %q not found", rev)
}

// Head returns the hash of the last commit that touched a file ("" if none).
func (g *Git) Head(file string) string {
	revs, err := g.Log(file, 1)
	if err != nil || len(revs) == 0 {
		return ""
	}
	return revs[0].Hash
}

// Diff returns a unified diff of a file between two revisions
// (to == "" means the working tree).
func (g *Git) Diff(from, to, file string) (string, error) {
	if !ValidRev(from) || (to != "" && !ValidRev(to)) {
		return "", fmt.Errorf("invalid revision %q..%q", from, to)
	}
	args := []string{"diff", "--no-color", "-U3", "--end-of-options", from}
	if to != "" {
		args = append(args, to)
	}
	out, err := g.run(nil, nil, append(args, "--", file)...)
	return string(out), err
}

// ConflictError is returned when a write was based on an outdated revision.
type ConflictError struct{ Current string }

func (e *ConflictError) Error() string { return "conflict: file changed since revision was loaded" }

// Write writes (or deletes, when content is nil) a file and commits it,
// all under the write lock. base is the revision the edit started from;
// if the file has changed since, ConflictError is returned and nothing is
// written. base == "-" skips the check. It returns the file's revision.
func (g *Git) Write(file string, content []byte, base, author, message string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if base != "-" {
		if cur := g.Head(file); cur != base {
			return "", &ConflictError{Current: cur}
		}
	}
	if message == "" {
		message = "edit " + file
	}
	if err := g.writeLocked(map[string][]byte{file: content}, author, message); err != nil {
		return "", err
	}
	return g.Head(file), nil
}

// WriteMany writes and deletes (nil content) several files in one commit,
// under the write lock.
func (g *Git) WriteMany(files map[string][]byte, author, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.writeLocked(files, author, message)
}

// writeLocked writes the files and commits them (nothing when nothing
// changed). If anything fails, the files are put back as they were in
// HEAD, so a failed write leaves nothing behind. Called with g.mu held.
func (g *Git) writeLocked(files map[string][]byte, author, message string) (err error) {
	paths := slices.Sorted(maps.Keys(files))
	for _, f := range paths {
		if !filepath.IsLocal(filepath.FromSlash(f)) {
			return fmt.Errorf("write %q: not a path inside the repository", f)
		}
	}
	defer func() {
		if err != nil {
			g.restore(paths)
		}
	}()
	for _, f := range paths {
		abs := filepath.Join(g.Dir, filepath.FromSlash(f))
		if files[f] == nil {
			if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, files[f], 0o644); err != nil {
			return err
		}
	}
	if _, err := g.run(nil, nil, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return err
	}
	out, err := g.run(nil, nil, append([]string{"status", "--porcelain", "--"}, paths...)...)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil // nothing changed
	}
	env := []string{
		"GIT_AUTHOR_NAME=" + author, "GIT_AUTHOR_EMAIL=" + author + "@lodge.invalid",
		"GIT_COMMITTER_NAME=lodge-wiki", "GIT_COMMITTER_EMAIL=wiki@lodge.invalid",
	}
	if _, err := g.run(nil, env, append([]string{"commit", "-q", "-m", message, "--"}, paths...)...); err != nil {
		return err
	}
	g.syncSoon()
	return nil
}

// restore puts files back as they are in HEAD (removing the ones HEAD
// does not have), after a failed write.
func (g *Git) restore(paths []string) {
	g.run(nil, nil, append([]string{"reset", "-q", "--"}, paths...)...)
	for _, f := range paths {
		if _, err := g.run(nil, nil, "cat-file", "-e", "HEAD:"+f); err == nil {
			g.run(nil, nil, "checkout", "-q", "HEAD", "--", f)
		} else {
			os.Remove(filepath.Join(g.Dir, filepath.FromSlash(f)))
		}
	}
}

// FileChange is one file touched by a commit.
type FileChange struct {
	Status string `json:"status"` // A added, M modified, D deleted, R renamed
	Path   string `json:"path"`
	From   string `json:"from,omitempty"` // for renames
}

// Commit is a commit with the files it touched.
type Commit struct {
	Hash    string       `json:"hash"`
	Author  string       `json:"author"`
	Time    time.Time    `json:"time"`
	Message string       `json:"message"`
	Files   []FileChange `json:"files"`
}

// LogAll returns every commit since a date (zero = all), oldest first,
// with renames detected.
func (g *Git) LogAll(since time.Time) ([]Commit, error) {
	args := []string{"log", "--reverse", "-M", "--name-status", "--format=\x01%H\x02%an\x02%at\x02%s"}
	if !since.IsZero() {
		args = append(args, "--since="+since.Format(time.RFC3339))
	}
	out, err := g.run(nil, nil, args...)
	if err != nil {
		return nil, err
	}
	var res []Commit
	for _, block := range strings.Split(string(out), "\x01") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		revs := parseRevs([]byte(lines[0]))
		if len(revs) == 0 {
			continue
		}
		c := Commit{Hash: revs[0].Hash, Author: revs[0].Author, Time: revs[0].Time, Message: revs[0].Message}
		for _, l := range lines[1:] {
			f := strings.Split(strings.TrimSpace(l), "\t")
			if len(f) < 2 || f[0] == "" {
				continue
			}
			fc := FileChange{Status: f[0][:1], Path: f[len(f)-1]}
			if fc.Status == "R" && len(f) == 3 {
				fc.From = f[1]
			}
			c.Files = append(c.Files, fc)
		}
		res = append(res, c)
	}
	return res, nil
}

