package wiki

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
}

func (g *Git) run(stdin []byte, env []string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", g.Dir, "-c", "core.quotepath=off"}, args...)...)
	cmd.Env = append(os.Environ(), env...)
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

// FileHistory is the first and last commit of a file.
type FileHistory struct {
	Created, Modified time.Time
	Author            string
}

// FileTimes walks the whole history once and returns, per file, when it
// was created and last modified, and by whom.
func (g *Git) FileTimes() (map[string]FileHistory, error) {
	out, err := g.run(nil, nil, "log", "--format=\x01%at\x02%an", "--name-only", "--no-renames")
	if err != nil {
		return nil, err
	}
	res := map[string]FileHistory{}
	var when time.Time
	var author string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\x01") {
			parts := strings.SplitN(line[1:], "\x02", 2)
			sec, _ := strconv.ParseInt(parts[0], 10, 64)
			when = time.Unix(sec, 0)
			author = ""
			if len(parts) > 1 {
				author = parts[1]
			}
			continue
		}
		if line == "" {
			continue
		}
		h, ok := res[line]
		if !ok {
			h = FileHistory{Modified: when, Author: author}
		}
		h.Created = when // log is newest first: the last seen is the oldest
		res[line] = h
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

// Show returns a file's content at a revision.
func (g *Git) Show(rev, file string) ([]byte, error) {
	return g.run(nil, nil, "show", rev+":"+file)
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
	args := []string{"diff", "--no-color", "-U3", from}
	if to != "" {
		args = append(args, to)
	}
	out, err := g.run(nil, nil, append(args, "--", file)...)
	return string(out), err
}

// ErrConflict is returned when a write was based on an outdated revision.
type ErrConflict struct{ Current string }

func (e *ErrConflict) Error() string { return "conflict: file changed since revision was loaded" }

// Write writes (or deletes, when content is nil) a file and commits it,
// all under the write lock. base is the revision the edit started from;
// if the file has changed since, ErrConflict is returned and nothing is
// written. base == "-" skips the check.
func (g *Git) Write(file string, content []byte, base, author, message string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if base != "-" {
		if cur := g.Head(file); cur != base {
			return "", &ErrConflict{Current: cur}
		}
	}
	abs := g.Dir + string(os.PathSeparator) + strings.ReplaceAll(file, "/", string(os.PathSeparator))
	if content == nil {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	} else {
		if err := os.MkdirAll(dirOf(abs), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(abs, content, 0o644); err != nil {
			return "", err
		}
	}
	if _, err := g.run(nil, nil, "add", "-A", "--", file); err != nil {
		return "", err
	}
	if out, _ := g.run(nil, nil, "status", "--porcelain", "--", file); len(bytes.TrimSpace(out)) == 0 {
		return g.Head(file), nil // nothing changed
	}
	if message == "" {
		message = "edit " + file
	}
	env := []string{
		"GIT_AUTHOR_NAME=" + author, "GIT_AUTHOR_EMAIL=" + author + "@lodge.invalid",
		"GIT_COMMITTER_NAME=lodge-wiki", "GIT_COMMITTER_EMAIL=wiki@lodge.invalid",
	}
	if _, err := g.run(nil, env, "commit", "-q", "-m", message, "--", file); err != nil {
		return "", err
	}
	return g.Head(file), nil
}

func dirOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[:i]
	}
	return "."
}
