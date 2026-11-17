package server

import (
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

func (s *Server) moveForm(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/move/")
	if !s.store.Exists(id) {
		http.NotFound(w, r)
		return
	}
	s.show(w, r, &View{ID: id, Title: "Rename " + s.titleOr(id), Mode: "move", Exists: true,
		Content: s.ui_("move.html", map[string]any{"ID": id, "URL": wiki.PageURL(id), "To": id,
			"Backlinks": len(s.store.Index().Backlinks[id])})})
}

// move renames a page and rewrites every link to it, in one commit.
func (s *Server) move(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/move/")
	to := wiki.CleanID(r.FormValue("to"))
	fail := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		s.show(w, r, &View{ID: id, Title: "Rename " + s.titleOr(id), Mode: "move", Exists: true,
			Content: s.ui_("move.html", map[string]any{"ID": id, "URL": wiki.PageURL(id), "To": r.FormValue("to"),
				"Backlinks": len(s.store.Index().Backlinks[id]), "Error": msg})})
	}
	if !s.store.Exists(id) {
		http.NotFound(w, r)
		return
	}
	if to == "" || to == id || strings.HasPrefix(to, "_") {
		fail("Choose a new, different name.")
		return
	}
	if s.store.Exists(to) {
		fail("A page named " + to + " already exists.")
		return
	}
	raw, err := os.ReadFile(s.store.PageFile(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	files := map[string][]byte{id + ".md": nil, to + ".md": raw}
	changed := 0
	for _, b := range s.store.Index().Backlinks[id] {
		file := b + ".md"
		if b == id {
			continue
		}
		old, err := os.ReadFile(s.store.File(file))
		if err != nil {
			continue
		}
		if nu := rewriteLinks(b, old, id, to); string(nu) != string(old) {
			files[file] = nu
			changed++
		}
	}
	msg := "rename " + id + " to " + to
	if changed > 0 {
		msg += " (links updated in " + plural(changed, "page") + ")"
	}
	if err := s.store.Git.WriteMany(files, s.user(r), msg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.Reindex(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, wiki.PageURL(to), http.StatusSeeOther)
}

var linkHrefRe = regexp.MustCompile(`(\]\(\s*<?|href=")([^)\s>"]+)`)

// rewriteLinks points every link in page `page` that resolves to `from`
// at `to` instead, keeping #fragments. Relative links become absolute.
func rewriteLinks(page string, body []byte, from, to string) []byte {
	return linkHrefRe.ReplaceAllFunc(body, func(m []byte) []byte {
		sub := linkHrefRe.FindSubmatch(m)
		id, frag, ok := wiki.LinkTarget(page, string(sub[2]))
		if !ok || id != from {
			return m
		}
		nu := wiki.PageURL(to)
		if frag != "" {
			nu += "#" + frag
		}
		return append(append([]byte{}, sub[1]...), nu...)
	})
}
