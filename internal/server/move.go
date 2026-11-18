package server

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

// Moving things around: a page, a folder of pages (optionally with its
// media folder), or media files and folders. Every link and image
// reference to what moved is rewritten in the same commit, so nothing
// breaks. Media files are not in git: they are renamed on disk, and
// deleting one moves it to _media/.trash instead of erasing it.

var (
	linkHrefRe  = regexp.MustCompile(`(\]\(\s*<?|href=")([^)\s>"]+)`)
	mediaURLRe  = regexp.MustCompile(`/_media/[^\s"'()?#<>\]\[]+`)
	errNotFound = errors.New("not found")
)

// rewriteRefs rewrites a page body:
//   - links to pages in `pages` (old id -> new id) point to the new URL;
//   - when `absolute` is set (the page itself moves), relative links are
//     made absolute so they keep pointing at the same pages;
//   - media references under mediaFrom are moved to mediaTo.
func rewriteRefs(page string, body []byte, pages map[string]string, absolute bool, mediaFrom, mediaTo string) []byte {
	if len(pages) > 0 || absolute {
		body = linkHrefRe.ReplaceAllFunc(body, func(m []byte) []byte {
			sub := linkHrefRe.FindSubmatch(m)
			href := string(sub[2])
			id, frag, ok := wiki.LinkTarget(page, href)
			if !ok {
				return m
			}
			nu, moved := pages[id]
			if !moved {
				if !absolute || strings.HasPrefix(href, "/") {
					return m
				}
				nu = id
			}
			u := wiki.PageURL(nu)
			if frag != "" {
				u += "#" + frag
			}
			return append(append([]byte{}, sub[1]...), u...)
		})
	}
	if mediaFrom != "" {
		body = mediaURLRe.ReplaceAllFunc(body, func(m []byte) []byte {
			p := strings.TrimPrefix(string(m), "/_media/")
			if dec, err := url.PathUnescape(p); err == nil {
				p = dec
			}
			if p == mediaFrom || strings.HasPrefix(p, mediaFrom+"/") {
				return []byte("/_media/" + mediaTo + p[len(mediaFrom):])
			}
			return m
		})
	}
	return body
}

// movePlan collects the git changes of a move.
type movePlan struct {
	files    map[string][]byte // repo file -> new content (nil = delete)
	pages    map[string]string // moved pages: old id -> new id
	updated  int               // other pages whose links were updated
	mediaOld string            // media path to rename on disk ("" = none)
	mediaNew string
}

// finish rewrites the pages that refer to what moves, renames media on
// disk and commits everything at once.
func (s *Server) finish(p *movePlan, author, msg string) error {
	idx := s.store.Index()
	others := map[string]bool{}
	for old := range p.pages {
		for _, b := range idx.Backlinks[old] {
			if _, moving := p.pages[b]; !moving {
				others[b] = true
			}
		}
	}
	if p.mediaOld != "" {
		for m, users := range idx.MediaUse {
			if m == p.mediaOld || strings.HasPrefix(m, p.mediaOld+"/") {
				for _, u := range users {
					if _, moving := p.pages[u]; !moving {
						others[u] = true
					}
				}
			}
		}
	}
	for id := range others {
		file := id + ".md"
		old, err := os.ReadFile(s.store.File(file))
		if err != nil {
			continue
		}
		if nu := rewriteRefs(id, old, p.pages, false, p.mediaOld, p.mediaNew); string(nu) != string(old) {
			p.files[file] = nu
			p.updated++
		}
	}
	if p.updated > 0 {
		msg += " (links updated in " + plural(p.updated, "page") + ")"
	}

	if p.mediaOld != "" {
		manifestMu.Lock()
		defer manifestMu.Unlock()
		from, to := s.store.File("_media/"+p.mediaOld), s.store.File("_media/"+p.mediaNew)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		log.Printf("media moved: %s -> %s by %s", p.mediaOld, p.mediaNew, author)
		if nu, err := s.manifestUpdate(); err == nil && nu != nil {
			p.files[MediaManifestFile] = nu
		}
		if len(p.files) > 0 {
			if err := s.store.Git.WriteMany(p.files, author, msg); err != nil {
				os.Rename(to, from) // undo, so pages and files stay in step
				return err
			}
		}
	} else if err := s.store.Git.WriteMany(p.files, author, msg); err != nil {
		return err
	}
	return s.store.Reindex()
}

// planPage renames one page.
func (s *Server) planPage(id, to string) (*movePlan, error) {
	if !s.store.Exists(id) {
		return nil, errNotFound
	}
	if to == "" || to == id || strings.HasPrefix(to, "_") {
		return nil, errors.New("choose a new, different name")
	}
	if s.store.Exists(to) || exists(s.store.PageFile(to)) {
		return nil, fmt.Errorf("a page named %s already exists", to)
	}
	raw, err := os.ReadFile(s.store.PageFile(id))
	if err != nil {
		return nil, err
	}
	p := &movePlan{files: map[string][]byte{}, pages: map[string]string{id: to}}
	p.files[id+".md"] = nil
	p.files[to+".md"] = rewriteRefs(id, raw, p.pages, true, "", "")
	return p, nil
}

// planFolder moves a folder of pages (and any other files in it), and
// optionally the media folder of the same name.
func (s *Server) planFolder(from, to string, withMedia bool) (*movePlan, error) {
	if from == "" || to == "" || from == to || strings.HasPrefix(to+"/", from+"/") {
		return nil, errors.New("choose a different folder, not inside the one being moved")
	}
	if exists(s.store.File(to)) {
		return nil, fmt.Errorf("the folder %s already exists", to)
	}
	p := &movePlan{files: map[string][]byte{}, pages: map[string]string{}}
	type item struct{ rel, nu string }
	var items []item
	err := filepath.WalkDir(s.store.File(from), func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(s.store.Root, fp)
		rel = filepath.ToSlash(rel)
		nu := to + strings.TrimPrefix(rel, from)
		items = append(items, item{rel, nu})
		if strings.HasSuffix(rel, ".md") {
			p.pages[strings.TrimSuffix(rel, ".md")] = strings.TrimSuffix(nu, ".md")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errNotFound
	}
	if withMedia && exists(s.store.File("_media/"+from)) {
		if exists(s.store.File("_media/" + to)) {
			return nil, fmt.Errorf("the media folder %s already exists", to)
		}
		p.mediaOld, p.mediaNew = from, to
	}
	for _, it := range items {
		raw, err := os.ReadFile(s.store.File(it.rel))
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(it.rel, ".md") {
			raw = rewriteRefs(strings.TrimSuffix(it.rel, ".md"), raw, p.pages, true, p.mediaOld, p.mediaNew)
		}
		p.files[it.rel] = nil
		p.files[it.nu] = raw
	}
	return p, nil
}

// planMedia renames a media file or folder.
func (s *Server) planMedia(from, to string) (*movePlan, error) {
	if !exists(s.store.File("_media/" + from)) {
		return nil, errNotFound
	}
	if to == "" || to == from || strings.HasPrefix(to+"/", from+"/") {
		return nil, errors.New("choose a different name, not inside the folder being moved")
	}
	if path.Ext(from) != "" && !strings.EqualFold(path.Ext(from), path.Ext(to)) {
		return nil, fmt.Errorf("keep the file extension (%s)", path.Ext(from))
	}
	if exists(s.store.File("_media/" + to)) {
		return nil, fmt.Errorf("%s already exists", to)
	}
	return &movePlan{files: map[string][]byte{}, pages: map[string]string{}, mediaOld: from, mediaNew: to}, nil
}

// cleanPath normalises a user-typed repo path, keeping file extensions.
func cleanPath(p string) string {
	p = strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/ ")
	ext := ""
	if e := path.Ext(p); e != "" && !strings.Contains(e, "/") && len(e) <= 6 {
		ext, p = strings.ToLower(e), strings.TrimSuffix(p, e)
	}
	var segs []string
	for _, seg := range strings.Split(p, "/") {
		if c := wiki.CleanID(seg); c != "" {
			segs = append(segs, c)
		}
	}
	return strings.Join(segs, "/") + ext
}

// kindOf says what a repo path is: "page", "folder", "media", or "" (also
// for paths that are not allowed: outside the repository, hidden, or in
// admin-only folders).
func (s *Server) kindOf(rel string) string {
	if m, ok := strings.CutPrefix(rel, "_media/"); ok {
		if ok, _ := fileAccess(m); ok {
			return "media"
		}
		return ""
	}
	if ok, admin := fileAccess(rel); !ok || admin {
		return ""
	}
	if strings.HasSuffix(rel, ".md") {
		return "page"
	}
	if st, err := os.Stat(s.store.File(rel)); err == nil && st.IsDir() {
		return "folder"
	}
	return ""
}

func (s *Server) mvShow(w http.ResponseWriter, r *http.Request, rel, to, errMsg string, status int) {
	kind := s.kindOf(rel)
	data := map[string]any{"Path": rel, "To": to, "Kind": kind, "Error": errMsg,
		"Back": "/_/files?dir=" + url.QueryEscape(path.Dir(rel))}
	idx := s.store.Index()
	switch kind {
	case "page":
		id := strings.TrimSuffix(rel, ".md")
		data["Back"] = wiki.PageURL(id)
		data["Uses"] = len(idx.Backlinks[id])
	case "folder":
		n := 0
		for id := range idx.Pages {
			if strings.HasPrefix(id, rel+"/") {
				n++
			}
		}
		data["Pages"] = n
		data["HasMedia"] = exists(s.store.File("_media/" + rel))
	case "media":
		m := strings.TrimPrefix(rel, "_media/")
		users := map[string]bool{}
		for k, us := range idx.MediaUse {
			if k == m || strings.HasPrefix(k, m+"/") {
				for _, u := range us {
					users[u] = true
				}
			}
		}
		data["Uses"] = len(users)
	}
	s.show(w, r, &View{ID: "_move", Title: "Move " + rel, Mode: "move", Status: status, Content: s.ui_("mv.html", data)})
}

// mvForm: GET /_/mv?path=<repo path>
func (s *Server) mvForm(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(r.URL.Query().Get("path"), "/")
	if s.kindOf(rel) == "" || !exists(s.store.File(rel)) {
		http.Error(w, "this cannot be moved here", http.StatusBadRequest)
		return
	}
	to := rel
	if s.kindOf(rel) == "page" {
		to = strings.TrimSuffix(rel, ".md")
	}
	s.mvShow(w, r, rel, strings.TrimPrefix(to, "_media/"), "", 0)
}

// mv: POST /_/mv (path, to, media=1)
func (s *Server) mv(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(r.FormValue("path"), "/")
	raw := strings.Trim(r.FormValue("to"), "/ ")
	if s.kindOf(rel) == "media" {
		raw = strings.TrimPrefix(raw, "_media/")
	}
	to := cleanPath(raw)
	var plan *movePlan
	var err error
	var msg, dest string
	switch s.kindOf(rel) {
	case "page":
		id := strings.TrimSuffix(rel, ".md")
		to = strings.TrimSuffix(to, ".md")
		plan, err = s.planPage(id, to)
		msg, dest = "rename "+id+" to "+to, wiki.PageURL(to)
	case "folder":
		plan, err = s.planFolder(rel, to, r.FormValue("media") == "1")
		msg, dest = "move folder "+rel+" to "+to, "/_/files?dir="+url.QueryEscape(to)
	case "media":
		from := strings.TrimPrefix(rel, "_media/")
		plan, err = s.planMedia(from, to)
		msg, dest = "move media "+from+" to "+to, "/_/files?dir="+url.QueryEscape(path.Dir("_media/"+to))
	default:
		http.Error(w, "this cannot be moved here", http.StatusBadRequest)
		return
	}
	if err == nil {
		err = s.finish(plan, s.user(r), msg)
	}
	if err != nil {
		s.mvShow(w, r, rel, r.FormValue("to"), err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// Old route, kept for the page tools: /_/move/<page id>.
func (s *Server) moveForm(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/_/mv?path="+url.QueryEscape(idFrom(r, "/_/move/")+".md"), http.StatusSeeOther)
}

// trashPath is where a deleted or replaced media file goes: next to the
// media (so media backups include it), in a dated folder that is never
// listed or served.
func (s *Server) trashPath(m string) string {
	return filepath.Join(s.store.File("_media/.trash"), time.Now().Format("2006-01-02_150405"), filepath.FromSlash(m))
}

// mediaRemove moves an unused media file or folder to _media/.trash.
func (s *Server) mediaRemove(w http.ResponseWriter, r *http.Request) {
	m := strings.Trim(strings.TrimPrefix(strings.Trim(r.FormValue("path"), "/"), "_media/"), "/")
	if ok, _ := fileAccess(m); !ok || m == "" {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	src := s.store.File("_media/" + m)
	if !exists(src) {
		http.NotFound(w, r)
		return
	}
	var users []string
	for k, us := range s.store.Index().MediaUse {
		if k == m || strings.HasPrefix(k, m+"/") {
			users = append(users, us...)
		}
	}
	users = append(users, s.otherMediaUsers(m)...)
	if len(users) > 0 {
		slices.Sort(users)
		http.Error(w, "still used by: "+strings.Join(users, ", "), http.StatusConflict)
		return
	}
	dst := s.trashPath(m)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(src, dst); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("media deleted (moved to %s) by %s", dst, s.user(r))
	s.mediaChanged(s.user(r), "media: delete "+m+" (kept in the bin)")
	http.Redirect(w, r, "/_/files?dir="+url.QueryEscape(path.Dir("_media/"+m))+"&saved="+url.QueryEscape(path.Base(m)+" moved to the bin"), http.StatusSeeOther)
}

// mediaUsers lists the pages using a media file or folder.
func (s *Server) mediaUsers(w http.ResponseWriter, r *http.Request) {
	m := strings.Trim(strings.TrimPrefix(strings.Trim(r.URL.Query().Get("path"), "/"), "_media/"), "/")
	idx := s.store.Index()
	seen := map[string]bool{}
	var pages []*wiki.PageInfo
	for k, us := range idx.MediaUse {
		if k == m || strings.HasPrefix(k, m+"/") {
			for _, u := range us {
				if !seen[u] && idx.Pages[u] != nil {
					seen[u] = true
					pages = append(pages, idx.Pages[u])
				}
			}
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].ID < pages[j].ID })
	s.show(w, r, &View{ID: "_used", Title: "Pages using " + m, Mode: "files",
		Content: s.ui_("pagelist.html", map[string]any{"Heading": "Pages using " + m, "Pages": pages,
			"Empty": "No page uses this file. It can be deleted safely."})})
}

// otherMediaUsers finds non-page text files (data, layouts, widgets) that
// mention a media path, so deleting it cannot break them either.
func (s *Server) otherMediaUsers(m string) []string {
	var out []string
	needle := []byte("/_media/" + m)
	filepath.WalkDir(s.store.Root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if fp != s.store.Root && (strings.HasPrefix(name, ".") || name == "_media") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(path.Ext(name))
		if _, text := textExts[ext]; !text || ext == ".md" {
			return nil
		}
		if b, err := os.ReadFile(fp); err == nil && bytes.Contains(b, needle) {
			rel, _ := filepath.Rel(s.store.Root, fp)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}
