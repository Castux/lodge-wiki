package server

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

func (s *Server) user(r *http.Request) string {
	if u := s.auth.User(r); u != nil {
		return u.Name
	}
	return "anonymous"
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/preview/")
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	res := s.render.RenderSnippet(id, body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, res.TOC+res.HTML)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/history/")
	if s.hidePrivate(w, r, id) {
		return
	}
	revs, err := s.store.Git.Log(fileOf(id), 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i := range revs {
		revs[i].Message = tidySummary(revs[i].Message)
	}
	s.show(w, r, &View{ID: id, Title: "History of " + s.titleOr(id), Mode: "history", Exists: s.store.Exists(id),
		Content: s.ui_("history.html", map[string]any{"ID": id, "URL": wiki.PageURL(id), "Path": strings.TrimPrefix(wiki.PageURL(id), "/"), "Revs": revs, "CanEdit": s.can(r, RoleEditor)})})
}

func (s *Server) revision(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/rev/")
	if s.hidePrivate(w, r, id) {
		return
	}
	rev := r.URL.Query().Get("rev")
	raw, err := s.store.Git.Show(rev, fileOf(id))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if isFile(id) {
		raw = []byte("# " + id + "\n\n```" + textExts[strings.ToLower(path.Ext(id))] + "\n" + string(raw) + "```\n")
	}
	pg := wiki.ParsePage(id, raw)
	res := s.render.Render(pg)
	note := s.ui_("revnote.html", map[string]any{"URL": wiki.PageURL(id), "Path": strings.TrimPrefix(wiki.PageURL(id), "/"), "Rev": rev})
	s.show(w, r, &View{ID: id, Title: pg.Title, Mode: "revision", Exists: true,
		Content: note + template.HTML(res.HTML), TOC: template.HTML(res.TOC), Widgets: res.Widgets})
}

// DiffLine is one line of a rendered diff.
type DiffLine struct {
	Kind string // "add", "del", "ctx", "hunk"
	Text string
}

func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/diff/")
	if s.hidePrivate(w, r, id) {
		return
	}
	rev := r.URL.Query().Get("rev")
	if rev == "" {
		http.Error(w, "missing rev", http.StatusBadRequest)
		return
	}
	out, err := s.store.Git.Diff(rev+"^", rev, fileOf(id))
	if err != nil {
		// first commit of the file: compare with nothing
		out, err = s.store.Git.Diff("4b825dc642cb6eb9a060e54bf8d69288fbee4904", rev, fileOf(id))
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var lines []DiffLine
	started := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			started = true
			lines = append(lines, DiffLine{"hunk", l})
		case !started:
		case strings.HasPrefix(l, "+"):
			lines = append(lines, DiffLine{"add", l[1:]})
		case strings.HasPrefix(l, "-"):
			lines = append(lines, DiffLine{"del", l[1:]})
		case strings.HasPrefix(l, " "):
			lines = append(lines, DiffLine{"ctx", l[1:]})
		}
	}
	revs, _ := s.store.Git.Log(fileOf(id), 0)
	var info wiki.Revision
	for _, rv := range revs {
		if strings.HasPrefix(rv.Hash, rev) {
			info = rv
		}
	}
	s.show(w, r, &View{ID: id, Title: "Changes to " + s.titleOr(id), Mode: "diff", Exists: s.store.Exists(id),
		Content: s.ui_("diff.html", map[string]any{"ID": id, "URL": wiki.PageURL(id), "Path": strings.TrimPrefix(wiki.PageURL(id), "/"), "Rev": info, "Lines": lines, "CanEdit": s.can(r, RoleEditor)})})
}

func (s *Server) revert(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/revert/")
	if !s.checkEditable(w, r, id) {
		return
	}
	rev := r.FormValue("rev")
	raw, err := s.store.Git.Show(rev, fileOf(id))
	if err != nil {
		http.Error(w, "revision not found", http.StatusNotFound)
		return
	}
	short := rev
	if len(short) > 8 {
		short = short[:8]
	}
	if _, err := s.store.Git.Write(fileOf(id), raw, "-", s.user(r), "revert "+id+" to "+short); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if isFile(id) {
		http.Redirect(w, r, "/_/history/"+id, http.StatusSeeOther)
		return
	}
	s.store.UpdateIndex(id, s.user(r), time.Now())
	http.Redirect(w, r, wiki.PageURL(id), http.StatusSeeOther)
}

func (s *Server) backlinks(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/backlinks/")
	idx := s.store.Index()
	var pages []*wiki.PageInfo
	for _, b := range idx.Backlinks[id] {
		pages = append(pages, idx.Pages[b])
	}
	s.show(w, r, &View{ID: id, Title: "Pages linking to " + s.titleOr(id), Mode: "backlinks", Exists: s.store.Exists(id),
		Content: s.ui_("pagelist.html", map[string]any{"Heading": "Pages linking to " + s.titleOr(id), "Pages": pages, "Empty": "No page links here."})})
}

// Change is one entry of the recent changes list.
type Change struct {
	wiki.Revision
	Pages []string
}

func (s *Server) recent(w http.ResponseWriter, r *http.Request) {
	revs, files, err := s.store.Git.Recent(200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var changes []Change
	for i, rv := range revs {
		rv.Message = tidySummary(rv.Message)
		c := Change{Revision: rv}
		for _, f := range files[i] {
			c.Pages = append(c.Pages, strings.TrimSuffix(f, ".md"))
		}
		changes = append(changes, c)
	}
	s.show(w, r, &View{ID: "_recent", Title: "Recent changes", Mode: "recent",
		Content: s.ui_("recent.html", map[string]any{"Changes": changes, "Store": s.store})})
}

// SearchHit is a search result.
type SearchHit struct {
	Page    *wiki.PageInfo
	Snippet string
	Score   int
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	idx := s.store.Index()
	var hits []SearchHit
	if q != "" {
		lq := strings.ToLower(q)
		words := strings.Fields(lq)
		for id, text := range idx.Text {
			p := idx.Pages[id]
			lt := strings.ToLower(p.Title)
			score := 0
			if strings.Contains(lt, lq) {
				score += 100
			}
			all := true
			for _, wd := range words {
				n := strings.Count(text, wd)
				if n == 0 && !strings.Contains(lt, wd) {
					all = false
					break
				}
				score += n
			}
			if !all {
				continue
			}
			hits = append(hits, SearchHit{Page: p, Snippet: snippet(text, words[0]), Score: score})
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].Score != hits[j].Score {
				return hits[i].Score > hits[j].Score
			}
			return hits[i].Page.ID < hits[j].Page.ID
		})
		if r.URL.Query().Get("all") == "" {
			for _, h := range hits {
				if strings.EqualFold(h.Page.Title, q) {
					http.Redirect(w, r, wiki.PageURL(h.Page.ID), http.StatusFound)
					return
				}
			}
		}
		if len(hits) > 200 {
			hits = hits[:200]
		}
	}
	s.show(w, r, &View{ID: "_search", Title: "Search", Mode: "search",
		Content: s.ui_("search.html", map[string]any{"Q": q, "Hits": hits})})
}

func snippet(text, word string) string {
	i := strings.Index(text, word)
	if i < 0 {
		i = 0
	}
	lo, hi := max(0, i-80), min(len(text), i+120)
	for lo > 0 && lo < len(text) && text[lo]&0xC0 == 0x80 {
		lo--
	}
	for hi < len(text) && text[hi]&0xC0 == 0x80 {
		hi++
	}
	sn := strings.Join(strings.Fields(text[lo:hi]), " ")
	if lo > 0 {
		sn = "…" + sn
	}
	if hi < len(text) {
		sn += "…"
	}
	return sn
}

// IndexNode is a namespace in the site index.
type IndexNode struct {
	NS    string
	Title string
	Pages []*wiki.PageInfo
	Subs  []*IndexNode
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	idx := s.store.Index()
	var build func(ns string) *IndexNode
	build = func(ns string) *IndexNode {
		pages, subs := idx.Children(ns)
		n := &IndexNode{NS: ns, Title: path.Base("/" + ns)}
		if p, ok := idx.Pages[strings.TrimPrefix(ns+"/"+wiki.StartPage, "/")]; ok {
			n.Title = p.Title
		}
		sort.Slice(pages, func(i, j int) bool { return strings.ToLower(pages[i].Title) < strings.ToLower(pages[j].Title) })
		n.Pages = pages
		for _, sub := range subs {
			n.Subs = append(n.Subs, build(sub))
		}
		return n
	}
	s.show(w, r, &View{ID: "_index", Title: "Index", Mode: "index",
		Content: s.ui_("index.html", build(""))})
}

// rawPath validates a /_/raw/ path: pages (.md) and data files (.json)
// outside special folders.
func rawPath(r *http.Request) (string, bool) {
	p := path.Clean(strings.TrimPrefix(r.URL.Path, "/_/raw/"))
	if p == "." || strings.Contains(p, "..") {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, "_") || strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	switch path.Ext(p) {
	case ".md", ".json":
		return p, true
	}
	return "", false
}

func (s *Server) rawGet(w http.ResponseWriter, r *http.Request) {
	p, ok := rawPath(r)
	if !ok {
		http.Error(w, "only .md and .json files", http.StatusBadRequest)
		return
	}
	b, err := os.ReadFile(s.store.File(p))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Revision", s.store.Git.Head(p))
	if path.Ext(p) == ".json" {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	}
	w.Write(b)
}

// rawPut writes a page or data file. Every write is a commit; the
// X-Base-Revision header must name the revision the change started from.
func (s *Server) rawPut(w http.ResponseWriter, r *http.Request) {
	p, ok := rawPath(r)
	if !ok {
		http.Error(w, "only .md and .json files", http.StatusBadRequest)
		return
	}
	if !s.limiter.allow(s.user(r)) {
		http.Error(w, "too many writes, slow down", http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if path.Ext(p) == ".json" && !json.Valid(body) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	// The revision the change started from; "new" when creating a file.
	base := r.Header.Get("X-Base-Revision")
	if base == "" {
		http.Error(w, `missing X-Base-Revision header (use "new" to create a file)`, http.StatusPreconditionRequired)
		return
	}
	if base == "new" {
		base = ""
	}
	msg := r.Header.Get("X-Summary")
	if msg == "" {
		msg = "edit " + p
	}
	if wd := r.Header.Get("X-Widget"); wd != "" {
		msg += " (via widget " + wd + ")"
	}
	rev, err := s.store.Git.Write(p, body, base, s.user(r), msg)
	var conflict *wiki.ErrConflict
	if errors.As(err, &conflict) {
		w.Header().Set("X-Revision", conflict.Current)
		http.Error(w, "conflict: the file changed since your revision", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if path.Ext(p) == ".md" {
		s.store.UpdateIndex(strings.TrimSuffix(p, ".md"), s.user(r), time.Now())
	}
	w.Header().Set("X-Revision", rev)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listJSON(w http.ResponseWriter, r *http.Request) {
	ns := strings.Trim(r.URL.Query().Get("ns"), "/")
	idx := s.store.Index()
	pages, subs := idx.Children(ns)
	type item struct {
		ID       string         `json:"id"`
		Title    string         `json:"title"`
		URL      string         `json:"url"`
		Meta     map[string]any `json:"meta,omitempty"`
		Created  time.Time      `json:"created"`
		Modified time.Time      `json:"modified"`
		Author   string         `json:"author"`
		Image    string         `json:"image,omitempty"`
	}
	out := struct {
		Pages      []item   `json:"pages"`
		Namespaces []string `json:"namespaces"`
	}{Namespaces: subs}
	for _, p := range pages {
		out.Pages = append(out.Pages, item{p.ID, p.Title, wiki.PageURL(p.ID), p.Meta, p.Created, p.Modified, p.Author, p.Image})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// logJSON returns the commit history (all files) as JSON, for widgets
// such as statistics. ?since=YYYY-MM-DD limits it.
func (s *Server) logJSON(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "since must be YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		since = t
	}
	commits, err := s.store.Git.LogAll(since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(commits)
}

var idPrefixRe = regexp.MustCompile(`^[a-z0-9_.:/-]+: `)

// tidySummary drops the "page/id: " prefix the migrated history carries
// in every message, and DokuWiki's "edited" placeholder.
func tidySummary(msg string) string {
	msg = idPrefixRe.ReplaceAllString(msg, "")
	if msg == "edited" || msg == "external edit" {
		return ""
	}
	return msg
}
