// Command migrate is the one-off DokuWiki → lodge-data migration script.
//
//	go run ./migrate --src ../public_html --out ../lodge-data
//
// After lodge-data became the source, it is only used to catch up with
// changes on the live DokuWiki: see SYNC.md.
//
// It recreates --out as a fresh git repository: the full page history is
// replayed with git fast-import (one commit per DokuWiki change), then a
// final commit makes the tree match the current pages, and media files
// are copied (git-ignored) into _media/.
package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var reTitle = regexp.MustCompile(`(?m)^\s*={2,}\s*(.*?)\s*={2,}\s*$`)

// First markdown heading: ATX ("# Title") or setext ("Title" over "===").
var reMDTitle = regexp.MustCompile(`(?m)^#{1,6}[ \t]+(.+?)[ \t#]*$|^([^\n]+)\n=+[ \t]*$`)

type change struct {
	ts      int64
	typ     string
	id      string
	user    string
	summary string
	seq     int
}

func main() {
	src := flag.String("src", "../public_html", "DokuWiki root (contains data/)")
	out := flag.String("out", "../lodge-data", "output repository")
	report := flag.String("report", "migrate/REPORT.md", "report file")
	noHistory := flag.Bool("no-history", false, "skip history replay")
	pages := flag.String("pages", "", "comma-separated page ids: only re-convert these into --out")
	force := flag.Bool("force", false, "recreate --out even if it has commits after the migration")
	copyNew := flag.Bool("copy-media", false, "with --changed-since: also copy the changed media files into --out/_media")
	changedSince := flag.String("changed-since", "", "only list the DokuWiki pages and media changed after this date (YYYY-MM-DD), then exit")
	flag.Parse()
	start := time.Now()

	data := filepath.Join(*src, "data")
	if *changedSince != "" {
		listChanges(data, *changedSince, *copyNew, filepath.Join(*out, "_media"))
		return
	}
	pagesDir := filepath.Join(data, "pages")

	// Current pages: ids, text, titles.
	current := map[string]string{}
	err := filepath.WalkDir(pagesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".txt") {
			return err
		}
		rel, _ := filepath.Rel(pagesDir, p)
		id := strings.TrimSuffix(filepath.ToSlash(rel), ".txt")
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		current[id] = string(b)
		return nil
	})
	check(err)
	site := &Site{Titles: map[string]string{}}
	for id, text := range current {
		t := ""
		if strings.HasPrefix(strings.TrimSpace(text), "<!DOCTYPE markdown>") {
			if m := reMDTitle.FindStringSubmatch(text); m != nil {
				t = strings.TrimSpace(m[1] + m[2])
			}
		} else if m := reTitle.FindStringSubmatch(text); m != nil {
			t = strings.TrimSpace(m[1])
		}
		site.Titles[id] = t
	}
	log.Printf("%d current pages", len(current))

	// --pages: re-convert only these pages into --out, touching nothing
	// else (used for ad hoc fixes after lodge-data became the source).
	if *pages != "" {
		for id := range strings.SplitSeq(*pages, ",") {
			id = strings.TrimSpace(id)
			text, ok := current[id]
			if !ok {
				log.Fatalf("no DokuWiki page %s", id)
			}
			res := Convert(id, text, site)
			ext := ".md"
			if res.JSON {
				ext = ".json"
			}
			check(os.WriteFile(filepath.Join(*out, filepath.FromSlash(id)+ext), []byte(res.Text), 0o644))
		}
		return
	}

	// Refuse to wipe a repository that has commits made after the
	// migration: it is the source of truth from then on.
	if last, err := exec.Command("git", "-C", *out, "log", "-1", "--format=%an").Output(); err == nil &&
		strings.TrimSpace(string(last)) != "" && strings.TrimSpace(string(last)) != "migration" && !*force {
		log.Fatalf("%s has commits after the migration (last by %s); refusing to recreate it (use --force)", *out, strings.TrimSpace(string(last)))
	}

	// Keep README/.gitignore and the remote, then reset the repo.
	readme, _ := os.ReadFile(filepath.Join(*out, "README.md"))
	gitignore, _ := os.ReadFile(filepath.Join(*out, ".gitignore"))
	if !strings.Contains(string(gitignore), "/_media/") {
		gitignore = append(gitignore, []byte("\n/_media/\n")...)
	}
	remote, _ := exec.Command("git", "-C", *out, "remote", "get-url", "origin").Output()
	check(os.MkdirAll(*out, 0o755))
	entries, err := os.ReadDir(*out)
	check(err)
	for _, e := range entries {
		if e.Name() == "_media" {
			continue
		}
		check(os.RemoveAll(filepath.Join(*out, e.Name())))
	}
	run(*out, "git", "init", "-q", "-b", "main")
	if r := strings.TrimSpace(string(remote)); r != "" {
		run(*out, "git", "remote", "add", "origin", r)
	}

	fi := newImporter(*out)
	issuesByPage := map[string][]string{}
	nCommits := 0
	missing := 0
	recovered := 0

	if !*noHistory {
		changes := readChanges(filepath.Join(data, "meta"))
		log.Printf("%d changes", len(changes))
		rec := newRecovery(data, changes, current)
		for _, ch := range changes {
			path := strings.ReplaceAll(ch.id, ":", "/")
			author := ch.user
			if author == "" {
				author = "anonymous"
			}
			msg := strings.TrimSpace(ch.summary)
			if ch.typ == "D" {
				if !fi.has(path) {
					continue
				}
				if msg == "" {
					msg = "deleted"
				}
				fi.commit(author, ch.ts, path+": "+msg, func() { fi.remove(path) })
				nCommits++
				continue
			}
			text, ok := readAttic(data, path, ch.ts)
			if !ok {
				text, ok = rec.recover(path, ch.ts, 0)
				if ok {
					recovered++
				}
			}
			if !ok {
				missing++
				continue
			}
			rec.last[path] = text
			res := Convert(path, text, site)
			if msg == "" {
				if ch.typ == "C" {
					msg = "created"
				} else {
					msg = "edited"
				}
			}
			fi.commit(author, ch.ts, path+": "+msg, func() { fi.write(path, res) })
			nCommits++
		}
	}

	// Final commit: tree = converted current pages.
	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	now := time.Now().Unix()
	jsonPages := 0
	fi.commit("migration", now, "Import current pages", func() {
		keep := map[string]bool{}
		for _, id := range ids {
			res := Convert(id, current[id], site)
			if len(res.Issues) > 0 {
				issuesByPage[id] = res.Issues
			}
			if res.JSON {
				jsonPages++
			}
			fi.write(id, res)
			keep[id] = true
		}
		for p := range fi.files {
			if !keep[p] {
				fi.remove(p)
			}
		}
	})
	nCommits++
	fi.commit("migration", now, "Add README and .gitignore", func() {
		fi.raw("README.md", readme)
		fi.raw(".gitignore", gitignore)
	})
	nCommits++
	check(fi.close())
	run(*out, "git", "reset", "-q", "--hard", "main")

	nMedia, copied := copyMedia(filepath.Join(data, "media"), filepath.Join(*out, "_media"))
	log.Printf("media: %d files (%d copied)", nMedia, copied)

	log.Printf("%d revisions recovered from moves or neighbouring revisions", recovered)
	writeReport(*report, len(current), jsonPages, nCommits, missing, recovered, nMedia, issuesByPage, time.Since(start))
	log.Printf("done: %d commits, %d revisions missing, %s", nCommits, missing, time.Since(start).Round(time.Second))
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func run(dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	check(cmd.Run())
}

// readChanges reads every per-page .changes file, sorted by time.
func readChanges(metaDir string) []change {
	var all []change
	seq := 0
	_ = filepath.WalkDir(metaDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".changes") || strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			fields := strings.Split(sc.Text(), "\t")
			if len(fields) < 6 {
				continue
			}
			ts, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				continue
			}
			seq++
			all = append(all, change{ts: ts, typ: fields[2], id: fields[3], user: fields[4], summary: fields[5], seq: seq})
		}
		return nil
	})
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ts != all[j].ts {
			return all[i].ts < all[j].ts
		}
		return all[i].seq < all[j].seq
	})
	return all
}

func readAttic(data, path string, ts int64) (string, bool) {
	p := filepath.Join(data, "attic", filepath.FromSlash(path)+"."+strconv.FormatInt(ts, 10)+".txt.gz")
	f, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", false
	}
	b, err := io.ReadAll(gz)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// importer writes a git fast-import stream.
type importer struct {
	cmd   *exec.Cmd
	w     *bufio.Writer
	pipe  io.WriteCloser
	files map[string]string // page id -> current file path in the tree
	mark  int
}

func newImporter(dir string) *importer {
	cmd := exec.Command("git", "fast-import", "--quiet")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdinPipe()
	check(err)
	check(cmd.Start())
	return &importer{cmd: cmd, pipe: pipe, w: bufio.NewWriterSize(pipe, 1<<20), files: map[string]string{}}
}

func (f *importer) has(id string) bool { _, ok := f.files[id]; return ok }

func (f *importer) commit(author string, ts int64, msg string, body func()) {
	f.mark++
	email := strings.ToLower(strings.ReplaceAll(author, " ", ".")) + "@lodge.invalid"
	fmt.Fprintf(f.w, "commit refs/heads/main\nmark :%d\n", f.mark)
	fmt.Fprintf(f.w, "author %s <%s> %d +0000\n", author, email, ts)
	fmt.Fprintf(f.w, "committer %s <%s> %d +0000\n", author, email, ts)
	fmt.Fprintf(f.w, "data %d\n%s\n", len(msg), msg)
	body()
	f.w.WriteString("\n")
}

func (f *importer) raw(path string, content []byte) {
	fmt.Fprintf(f.w, "M 100644 inline %s\ndata %d\n", path, len(content))
	f.w.Write(content)
	f.w.WriteString("\n")
}

func (f *importer) write(id string, res Result) {
	path := id + ".md"
	if res.JSON {
		path = id + ".json"
	}
	if old, ok := f.files[id]; ok && old != path {
		fmt.Fprintf(f.w, "D %s\n", old)
	}
	f.files[id] = path
	f.raw(path, []byte(res.Text))
}

func (f *importer) remove(id string) {
	if old, ok := f.files[id]; ok {
		fmt.Fprintf(f.w, "D %s\n", old)
		delete(f.files, id)
	}
}

func (f *importer) close() error {
	if err := f.w.Flush(); err != nil {
		return err
	}
	f.pipe.Close()
	return f.cmd.Wait()
}

func copyMedia(srcDir, dstDir string) (int, int) {
	n, copied := 0, 0
	_ = filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, p)
		dst := filepath.Join(dstDir, rel)
		n++
		si, err := d.Info()
		if err != nil {
			return nil
		}
		if di, err := os.Stat(dst); err == nil && di.Size() == si.Size() && di.ModTime().Unix() == si.ModTime().Unix() {
			return nil
		}
		check(os.MkdirAll(filepath.Dir(dst), 0o755))
		in, err := os.Open(p)
		check(err)
		o, err := os.Create(dst)
		check(err)
		_, err = io.Copy(o, in)
		check(err)
		in.Close()
		o.Close()
		check(os.Chtimes(dst, si.ModTime(), si.ModTime()))
		copied++
		return nil
	})
	return n, copied
}

func writeReport(path string, pages, jsonPages, commits, missing, recovered, media int, issues map[string][]string, took time.Duration) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Migration report\n\nGenerated %s by `go run ./migrate`.\n\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "| | |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| Pages | %d (%d as JSON data files) |\n", pages, jsonPages)
	fmt.Fprintf(&b, "| Commits | %d |\n", commits)
	fmt.Fprintf(&b, "| Revisions with no attic copy, content recovered from a move source or the nearest later revision | %d |\n", recovered)
	fmt.Fprintf(&b, "| Revisions with no recoverable content (skipped) | %d |\n", missing)
	fmt.Fprintf(&b, "| Media files | %d |\n", media)
	fmt.Fprintf(&b, "| Run time | %s |\n\n", took.Round(time.Second))

	byIssue := map[string][]string{}
	for id, list := range issues {
		for _, is := range list {
			byIssue[is] = append(byIssue[is], id)
		}
	}
	keys := make([]string, 0, len(byIssue))
	for k := range byIssue {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(&b, "## Constructs not converted cleanly (%d pages)\n\n", len(issues))
	for _, k := range keys {
		ids := byIssue[k]
		sort.Strings(ids)
		fmt.Fprintf(&b, "- **%s** (%d): %s\n", k, len(ids), strings.Join(ids, ", "))
	}
	check(os.WriteFile(path, []byte(b.String()), 0o644))
}
