package main

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// recovery finds content for revisions that have no attic file of their
// own. The move plugin renames a page's attic files to its new id, and
// "external edit" entries never had an attic copy.
type recovery struct {
	data      string
	attic     map[string][]int64 // page path -> available revision timestamps
	movedTo   map[string]string  // page path -> path it was moved to
	movedFrom map[string]string  // page path -> path it was moved from
	current   map[string]string
	last      map[string]string // page path -> latest replayed text
}

var reMoved = regexp.MustCompile(`moved (?:and renamed )?from (\S+) to (\S+)`)

func newRecovery(data string, changes []change, current map[string]string) *recovery {
	r := &recovery{
		data: data, attic: map[string][]int64{}, movedTo: map[string]string{},
		movedFrom: map[string]string{}, current: current, last: map[string]string{},
	}
	atticDir := filepath.Join(data, "attic")
	_ = filepath.WalkDir(atticDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".txt.gz") {
			return nil
		}
		rel, _ := filepath.Rel(atticDir, p)
		rel = strings.TrimSuffix(filepath.ToSlash(rel), ".txt.gz")
		k := strings.LastIndexByte(rel, '.')
		if k < 0 {
			return nil
		}
		ts, err := strconv.ParseInt(rel[k+1:], 10, 64)
		if err != nil {
			return nil
		}
		r.attic[rel[:k]] = append(r.attic[rel[:k]], ts)
		return nil
	})
	for _, list := range r.attic {
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
	}
	for _, ch := range changes {
		if m := reMoved.FindStringSubmatch(ch.summary); m != nil {
			from := strings.ReplaceAll(m[1], ":", "/")
			to := strings.ReplaceAll(m[2], ":", "/")
			if from != to {
				r.movedTo[from] = to
				r.movedFrom[to] = from
			}
		}
	}
	return r
}

// chain returns path followed by every later location it was moved to.
func (r *recovery) chain(path string) []string {
	out := []string{path}
	seen := map[string]bool{path: true}
	for p := path; len(out) < 20; {
		next, ok := r.movedTo[p]
		if !ok || seen[next] {
			break
		}
		seen[next] = true
		out = append(out, next)
		p = next
	}
	return out
}

func (r *recovery) recover(path string, ts int64, _ int) (string, bool) {
	chain := r.chain(path)
	// 1. The same revision, renamed to a later id by a move.
	for _, p := range chain[1:] {
		if t, ok := readAttic(r.data, p, ts); ok {
			return t, true
		}
	}
	// 2. A page created by a move: the source's latest text.
	if src, ok := r.movedFrom[path]; ok {
		if t, ok := r.last[src]; ok {
			return t, true
		}
	}
	// 3. The nearest later revision of this page, along its moves.
	for _, p := range chain {
		list := r.attic[p]
		k := sort.Search(len(list), func(i int) bool { return list[i] >= ts })
		if k < len(list) {
			if t, ok := readAttic(r.data, p, list[k]); ok {
				return t, true
			}
		}
	}
	// 4. The current text at the end of the chain.
	for i := len(chain) - 1; i >= 0; i-- {
		if t, ok := r.current[chain[i]]; ok {
			return t, true
		}
	}
	// 5. The last text we replayed for this page.
	if t, ok := r.last[path]; ok {
		return t, true
	}
	return "", false
}
