package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

// Everything in the content repo is edited through /_/edit/<id>:
//   - pages: <id> without extension, stored as <id>.md
//   - other text files (data, layouts, widgets): <id> with its extension
//
// Files under a folder starting with "_" (_layout, _widgets) run in every
// reader's browser, so only admins may change them.

var textExts = map[string]string{
	".json": "json", ".css": "css", ".js": "javascript", ".html": "html",
	".txt": "text", ".svg": "svg", ".yaml": "yaml", ".yml": "yaml", ".md": "markdown",
}

// isFile reports whether an id names a file with an extension (not a page).
func isFile(id string) bool {
	_, ok := textExts[strings.ToLower(path.Ext(id))]
	return ok && !strings.HasSuffix(id, ".md")
}

// fileOf maps an id to its repo-relative file.
func fileOf(id string) string {
	if isFile(id) {
		return id
	}
	return id + ".md"
}

// fileAccess checks a repo path: ok=false for paths that are never
// editable (.git, _media, dot files); admin=true when only admins may
// change it.
func fileAccess(file string) (ok, admin bool) {
	if file == "" || strings.Contains(file, "..") {
		return false, false
	}
	for i, seg := range strings.Split(file, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false, false
		}
		if i == 0 && seg == "_media" {
			return false, false
		}
		if strings.HasPrefix(seg, "_") {
			admin = true
		}
	}
	return true, admin
}

func (s *Server) checkEditable(w http.ResponseWriter, r *http.Request, id string) bool {
	ok, admin := fileAccess(fileOf(id))
	if !ok {
		http.Error(w, "this file cannot be edited here", http.StatusBadRequest)
		return false
	}
	if admin && !s.can(r, RoleAdmin) {
		http.Error(w, "only admins can change layouts and widgets", http.StatusForbidden)
		return false
	}
	return true
}

// editData is what ui/edit.html receives.
func (s *Server) editData(id, text, base string, exists bool) map[string]any {
	file := fileOf(id)
	back := wiki.PageURL(id)
	if isFile(id) {
		back = "/_/files?dir=" + url.QueryEscape(path.Dir(file))
	}
	return map[string]any{
		"ID": id, "File": file, "URL": back, "Text": text, "Base": base, "Exists": exists,
		"IsFile": isFile(id), "Lang": textExts[strings.ToLower(path.Ext(file))], "NS": wiki.Namespace(id),
	}
}

func (s *Server) editForm(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/edit/")
	if !s.checkEditable(w, r, id) {
		return
	}
	file := fileOf(id)
	raw, err := os.ReadFile(s.store.File(file))
	exists := err == nil
	switch {
	case !exists && path.Ext(file) == ".json":
		raw = []byte("{}\n")
	case !exists && isFile(id):
		raw = nil
	case !exists:
		raw = []byte("# " + strings.ReplaceAll(wiki.Name(id), "-", " ") + "\n\n")
	case path.Ext(file) == ".json" && bytes.Count(bytes.TrimSpace(raw), []byte("\n")) == 0 && len(raw) > 200:
		// One long line: show it indented so it can be edited by hand.
		var buf bytes.Buffer
		if json.Indent(&buf, raw, "", "  ") == nil {
			raw = append(buf.Bytes(), '\n')
		}
	}
	title := "Editing " + s.titleOr(id)
	if isFile(id) {
		title = "Editing " + file
	}
	s.show(w, r, &View{ID: id, Title: title, Mode: "edit", Exists: exists,
		Content: s.ui_("edit.html", s.editData(id, string(raw), s.store.Git.Head(file), exists))})
}

// validate refuses content that would break the site.
func validate(file string, content []byte) string {
	switch path.Ext(file) {
	case ".json":
		if !json.Valid(content) {
			return "This is not valid JSON, so it was not saved. Check for missing commas or quotes."
		}
	case ".html":
		if path.Base(file) == "page.html" {
			if _, err := template.New("page").Funcs(funcs).Parse(string(content)); err != nil {
				return "This layout template has an error, so it was not saved: " + err.Error()
			}
		}
	}
	return ""
}

func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	id := idFrom(r, "/_/edit/")
	if !s.checkEditable(w, r, id) {
		return
	}
	file := fileOf(id)
	text := strings.ReplaceAll(r.FormValue("text"), "\r\n", "\n")
	summary := strings.TrimSpace(r.FormValue("summary"))
	var content []byte
	if strings.TrimSpace(text) != "" {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		content = []byte(text)
	}
	again := func(status int, extra map[string]any) {
		w.WriteHeader(status)
		d := s.editData(id, text, r.FormValue("base"), true)
		d["Summary"] = summary
		for k, v := range extra {
			d[k] = v
		}
		s.show(w, r, &View{ID: id, Title: "Editing " + file, Mode: "edit", Exists: true, Content: s.ui_("edit.html", d)})
	}
	if content != nil {
		if msg := validate(file, content); msg != "" {
			again(http.StatusBadRequest, map[string]any{"Error": msg})
			return
		}
	}
	msg := summary
	if msg == "" {
		if content == nil {
			msg = "delete " + file
		} else {
			msg = "edit " + strings.TrimSuffix(file, ".md")
		}
	}
	_, err := s.store.Git.Write(file, content, r.FormValue("base"), s.user(r), msg)
	var conflict *wiki.ErrConflict
	if errors.As(err, &conflict) {
		theirs, _ := os.ReadFile(s.store.File(file))
		again(http.StatusConflict, map[string]any{"Base": conflict.Current, "Conflict": string(theirs)})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if isFile(id) {
		http.Redirect(w, r, "/_/files?dir="+url.QueryEscape(path.Dir(file))+"&saved="+url.QueryEscape(path.Base(file)), http.StatusSeeOther)
		return
	}
	s.store.UpdateIndex(id, s.user(r), time.Now())
	http.Redirect(w, r, wiki.PageURL(id), http.StatusSeeOther)
}

// FileEntry is a row of the file browser.
type FileEntry struct {
	Name     string
	Path     string // repo-relative
	Dir      bool
	Size     int64
	Modified time.Time
	Page     string // page id, for .md files
	Editable bool
	Admin    bool // only admins may edit
	Title    string
}

// files is a browser for the whole content repo (except media).
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	dir := strings.Trim(path.Clean("/"+r.URL.Query().Get("dir")), "/")
	if dir != "" {
		if ok, _ := fileAccess(dir); !ok {
			http.Error(w, "bad folder", http.StatusBadRequest)
			return
		}
	}
	entries, err := os.ReadDir(s.store.File(dir))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	isAdmin := s.can(r, RoleAdmin)
	var rows []FileEntry
	for _, e := range entries {
		rel := path.Join(dir, e.Name())
		ok, admin := fileAccess(rel)
		if !ok {
			continue
		}
		fe := FileEntry{Name: e.Name(), Path: rel, Dir: e.IsDir(), Admin: admin}
		if info, err := e.Info(); err == nil {
			fe.Size, fe.Modified = info.Size(), info.ModTime()
		}
		if !fe.Dir {
			ext := strings.ToLower(path.Ext(rel))
			if ext == ".md" {
				fe.Page = strings.TrimSuffix(rel, ".md")
				fe.Title = s.store.Title(fe.Page)
			}
			_, text := textExts[ext]
			fe.Editable = text && (!admin || isAdmin)
		}
		rows = append(rows, fe)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Dir != rows[j].Dir {
			return rows[i].Dir
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	var crumbs []Crumb
	acc := ""
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" {
			continue
		}
		acc = path.Join(acc, seg)
		crumbs = append(crumbs, Crumb{Title: seg, URL: "/_/files?dir=" + url.QueryEscape(acc)})
	}
	_, dirAdmin := fileAccess(path.Join(dir, "x"))
	s.show(w, r, &View{ID: "_files", Title: "Files", Mode: "files",
		Content: s.ui_("files.html", map[string]any{
			"Dir": dir, "Crumbs": crumbs, "Rows": rows, "Saved": r.URL.Query().Get("saved"),
			"CanCreate": !dirAdmin || isAdmin, "IsAdmin": isAdmin,
		})})
}

// newFile turns the "new file" form into an editor URL.
func (s *Server) newFile(w http.ResponseWriter, r *http.Request) {
	dir := strings.Trim(r.URL.Query().Get("dir"), "/")
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Redirect(w, r, "/_/files?dir="+url.QueryEscape(dir), http.StatusSeeOther)
		return
	}
	ext := strings.ToLower(path.Ext(name))
	base := strings.TrimSuffix(name, path.Ext(name))
	// Clean each folder level, keeping a leading "_" (_layout, _widgets).
	var segs []string
	for _, seg := range strings.Split(strings.ReplaceAll(base, "\\", "/"), "/") {
		c := wiki.CleanID(seg)
		if c == "" {
			continue
		}
		if strings.HasPrefix(seg, "_") {
			c = "_" + c
		}
		segs = append(segs, c)
	}
	if len(segs) == 0 {
		http.Redirect(w, r, "/_/files?dir="+url.QueryEscape(dir), http.StatusSeeOther)
		return
	}
	id := path.Join(append([]string{dir}, segs...)...)
	if _, ok := textExts[ext]; ok && ext != ".md" {
		id += ext
	} else if ext != "" && ext != ".md" {
		http.Error(w, "only text files can be created here (.md, .json, .css, .js, .html, ...); upload images from the editor", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/_/edit/"+id, http.StatusSeeOther)
}

func (s *Server) titleOr(id string) string {
	if t := s.store.Title(id); t != "" {
		return t
	}
	return wiki.Name(id)
}
