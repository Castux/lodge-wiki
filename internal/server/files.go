package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"log"
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

// isPrivate reports whether a repo path is in a private folder (_config):
// only admins may see it at all, in Files, history and diffs.
func isPrivate(file string) bool {
	return file == "_config" || strings.HasPrefix(file, "_config/")
}

// hidePrivate answers 404 to non-admins for private files.
func (s *Server) hidePrivate(w http.ResponseWriter, r *http.Request, id string) bool {
	if isPrivate(fileOf(id)) && !s.can(r, RoleAdmin) {
		http.NotFound(w, r)
		return true
	}
	return false
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
		"Managed": managedFiles[file].Note, "ReadOnly": managedFiles[file].ReadOnly,
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
		title := strings.TrimSpace(r.URL.Query().Get("title"))
		if title == "" {
			title = strings.ReplaceAll(wiki.Name(id), "-", " ")
		}
		raw = []byte("# " + title + "\n\n")
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
// Managed files are written by the server itself. Read-only ones cannot
// be saved from the editor at all; the others show a warning.
type managedFile struct {
	ReadOnly bool
	Note     string
}

var managedFiles = map[string]managedFile{
	MediaManifestFile: {true, "The wiki keeps this list of media files up to date by itself, after every upload, move and delete. It is read-only: changing it by hand would only make it wrong, and backups rely on it."},
	UsersFile:         {false, "The accounts. Normally changed on the Users page; edit by hand only to repair something. Saving checks that every account still has a valid password hash and role."},
}

func validate(file string, content []byte) string {
	if file == UsersFile {
		var users map[string]*User
		if err := json.Unmarshal(content, &users); err != nil {
			return "This is not a valid accounts file, so it was not saved: " + err.Error()
		}
		for name, u := range users {
			if u == nil || !strings.HasPrefix(u.Hash, "$2") || u.Role < RolePending || u.Role > RoleAdmin {
				return "The account " + name + " needs a bcrypt password hash and a role from 0 to 3, so nothing was saved."
			}
		}
	}
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
	if managedFiles[file].ReadOnly {
		http.Error(w, "this file is maintained by the wiki and cannot be edited", http.StatusForbidden)
		return
	}
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
	before := s.store.Index().Pages[id]
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
	if file == UsersFile {
		if err := s.auth.Reload(); err != nil {
			log.Printf("reloading accounts: %v", err)
		}
	}
	if isFile(id) {
		http.Redirect(w, r, "/_/files?dir="+url.QueryEscape(path.Dir(file))+"&saved="+url.QueryEscape(path.Base(file)), http.StatusSeeOther)
		return
	}
	s.store.UpdateIndex(id, s.user(r), time.Now())
	s.pageChanged(r, changeType(before != nil, content != nil), id, summary, before)
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
	Movable  bool
	Media    bool // a file or folder under _media
	Image    bool // an image, shown as a thumbnail
	Uses     int  // pages using this media file or folder
	Managed  bool // written by the server (see managedFiles)
	ReadOnly bool
}

// files is a browser for the whole content repo (except media).
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	dir := strings.Trim(path.Clean("/"+r.URL.Query().Get("dir")), "/")
	inMedia := dir == "_media" || strings.HasPrefix(dir, "_media/")
	if inMedia && dir != "_media" {
		if ok, _ := fileAccess(strings.TrimPrefix(dir, "_media/")); !ok {
			http.Error(w, "bad folder", http.StatusBadRequest)
			return
		}
	} else if dir != "" && !inMedia {
		if ok, _ := fileAccess(dir); !ok || (isPrivate(dir) && !s.can(r, RoleAdmin)) {
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
	idx := s.store.Index()
	var rows []FileEntry
	for _, e := range entries {
		rel := path.Join(dir, e.Name())
		if inMedia {
			if strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".upload") {
				continue
			}
			m := strings.TrimPrefix(rel, "_media/")
			fe := FileEntry{Name: e.Name(), Path: rel, Dir: e.IsDir(), Media: true, Movable: true}
			if info, err := e.Info(); err == nil {
				fe.Size, fe.Modified = info.Size(), info.ModTime()
			}
			ext := strings.ToLower(path.Ext(e.Name()))
			fe.Image = !fe.Dir && (ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".svg")
			for k, us := range idx.MediaUse {
				if k == m || (fe.Dir && strings.HasPrefix(k, m+"/")) {
					fe.Uses += len(us)
				}
			}
			rows = append(rows, fe)
			continue
		}
		if dir == "" && e.Name() == "_media" {
			rows = append(rows, FileEntry{Name: "_media", Path: "_media", Dir: true, Media: true})
			continue
		}
		ok, admin := fileAccess(rel)
		if !ok || (isPrivate(rel) && !isAdmin) {
			continue
		}
		fe := FileEntry{Name: e.Name(), Path: rel, Dir: e.IsDir(), Admin: admin}
		fe.Movable = !admin && (fe.Dir || strings.HasSuffix(rel, ".md"))
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
			if m, ok := managedFiles[rel]; ok {
				fe.Managed, fe.ReadOnly = true, m.ReadOnly
			}
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
			"CanCreate": (!dirAdmin || isAdmin) && !inMedia, "IsAdmin": isAdmin,
			"InMedia": inMedia, "MediaNS": strings.TrimPrefix(strings.TrimPrefix(dir, "_media"), "/"),
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

// createForm asks for a new page's title and folder. With a title it
// goes straight to the editor (or to the page, if it already exists).
func (s *Server) createForm(w http.ResponseWriter, r *http.Request) {
	ns := strings.Trim(r.URL.Query().Get("ns"), "/")
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	if title != "" {
		var segs []string
		for _, seg := range strings.Split(ns, "/") {
			if c := wiki.CleanID(seg); c != "" {
				segs = append(segs, c)
			}
		}
		name := wiki.CleanID(strings.ReplaceAll(title, "/", "-"))
		if name == "" {
			name = "new-page"
		}
		id := path.Join(append(segs, name)...)
		if s.store.Exists(id) {
			http.Redirect(w, r, wiki.PageURL(id)+"?exists=1", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/_/edit/"+id+"?title="+url.QueryEscape(title), http.StatusSeeOther)
		return
	}
	// Offer the existing folders for autocompletion.
	seen := map[string]bool{}
	var folders []string
	for id := range s.store.Index().Pages {
		for ns := wiki.Namespace(id); ns != "" && !seen[ns]; ns = wiki.Namespace(ns) {
			seen[ns] = true
			folders = append(folders, ns)
		}
	}
	sort.Strings(folders)
	s.show(w, r, &View{ID: "_create", Title: "New page", Mode: "create",
		Content: s.ui_("create.html", map[string]any{"NS": ns, "Folders": folders})})
}
