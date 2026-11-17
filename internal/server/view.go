package server

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

//go:embed ui/*.html static/*
var uiFS embed.FS

// View is what a layout's page.html receives.
type View struct {
	ID      string
	Title   string
	Mode    string // show, missing, edit, history, revision, diff, search, recent, index, login, backlinks
	Exists  bool
	Content template.HTML
	TOC     template.HTML
	Widgets []string

	// filled in by show()
	SiteTitle string
	URL       string
	Crumbs    []Crumb
	Modified  time.Time
	Author    string
	User      string
	CanEdit   bool
	IsAdmin   bool
	Styles    []string // stylesheet URLs, outermost first
	Layout    string   // URL of the layout folder holding page.html (its script and assets)
	Head      template.HTML
	Scripts   template.HTML
	Tools     []Tool
	Backlinks int
}

// Crumb is one step of the "you are here" trail.
type Crumb struct {
	Title string
	URL   string
	Last  bool
}

// Tool is a page or site action shown by the layout.
type Tool struct {
	Name, Label, URL string
	Post             bool // needs a POST form (logout)
	Active           bool
}

var funcs = template.FuncMap{
	"date": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format("2006/01/02 15:04")
	},
	"ago":     ago,
	"pageurl": wiki.PageURL,
	"size": func(n int64) string {
		switch {
		case n >= 1<<20:
			return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
		case n >= 1<<10:
			return fmt.Sprintf("%d KB", n>>10)
		}
		return fmt.Sprintf("%d B", n)
	},
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month") + " ago"
	}
	return plural(int(d.Hours()/24/365), "year") + " ago"
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return fmt.Sprintf("%d %ss", n, w)
}

func loadUI() (*template.Template, error) {
	return template.New("ui").Funcs(funcs).ParseFS(uiFS, "ui/*.html")
}

// ui_ renders one of the built-in UI fragments (edit form, history...).
func (s *Server) ui_(name string, data any) template.HTML {
	var b bytes.Buffer
	if err := s.ui.ExecuteTemplate(&b, name, data); err != nil {
		return template.HTML("<p>Template error: " + template.HTMLEscapeString(err.Error()) + "</p>")
	}
	return template.HTML(b.String())
}

var layoutCache sync.Map // path -> cachedTpl

type cachedTpl struct {
	mod time.Time
	t   *template.Template
}

func (s *Server) layoutTemplate(dir string) (*template.Template, error) {
	file := s.store.File(dir + "/page.html")
	st, err := os.Stat(file)
	if err != nil {
		return template.New("default-layout.html").Funcs(funcs).ParseFS(uiFS, "ui/default-layout.html")
	}
	if c, ok := layoutCache.Load(file); ok && c.(cachedTpl).mod.Equal(st.ModTime()) {
		return c.(cachedTpl).t, nil
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	t, err := template.New("page").Funcs(funcs).Parse(string(src))
	if err != nil {
		return nil, err
	}
	layoutCache.Store(file, cachedTpl{st.ModTime(), t})
	return t, nil
}

// show fills in the common parts of a view and renders it with the
// page's layout.
func (s *Server) show(w http.ResponseWriter, r *http.Request, v *View) {
	v.SiteTitle = s.cfg.Title
	v.URL = wiki.PageURL(v.ID)
	v.CanEdit = s.can(r, RoleEditor)
	v.IsAdmin = s.can(r, RoleAdmin)
	if u := s.auth.User(r); u != nil {
		v.User = u.Name
	}
	idx := s.store.Index()
	if p, ok := idx.Pages[v.ID]; ok {
		v.Modified, v.Author = p.Modified, p.Author
	}
	v.Backlinks = len(idx.Backlinks[v.ID])
	if isFile(v.ID) {
		v.Crumbs = fileCrumbs(v.ID, v.Title)
	} else {
		v.Crumbs = s.crumbs(v.ID, v.Title)
	}

	dirs := s.layoutDirs(v.ID)
	for _, d := range dirs {
		if exists(s.store.File(d + "/style.css")) {
			v.Styles = append(v.Styles, "/"+d+"/style.css")
		}
	}
	if len(dirs) > 0 {
		v.Layout = "/" + dirs[0] + "/"
	}
	v.Head = template.HTML(`<link rel="stylesheet" href="/_/static/core.css">`)
	var scripts strings.Builder
	scripts.WriteString(`<script src="/_/static/wiki.js"></script><script src="/_/static/core.js"></script>`)
	seen := map[string]bool{}
	for _, name := range v.Widgets {
		if seen[name] {
			continue
		}
		seen[name] = true
		base := "/_widgets/" + name + "/"
		if exists(s.store.File("_widgets/" + name + "/widget.css")) {
			v.Head += template.HTML(`<link rel="stylesheet" href="` + base + `widget.css">`)
		}
		scripts.WriteString(`<script src="` + base + `widget.js"></script>`)
	}
	v.Scripts = template.HTML(scripts.String())
	v.Tools = s.tools(v)

	layoutDir := ""
	if len(dirs) > 0 {
		layoutDir = dirs[0]
	}
	t, err := s.layoutTemplate(layoutDir)
	if err != nil {
		log.Printf("layout %s: %v", layoutDir, err)
		t, _ = template.New("default-layout.html").Funcs(funcs).ParseFS(uiFS, "ui/default-layout.html")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b bytes.Buffer
	if err := t.Execute(&b, v); err != nil {
		http.Error(w, "layout error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(b.Bytes())
}

// crumbs builds the namespace trail: Home › Fool's Landing › Notices › Page.
func (s *Server) crumbs(id, title string) []Crumb {
	idx := s.store.Index()
	home := "Home"
	if p, ok := idx.Pages[wiki.StartPage]; ok {
		home = p.Title
	}
	out := []Crumb{{Title: home, URL: "/"}}
	parts := strings.Split(id, "/")
	for i := 1; i < len(parts); i++ {
		ns := strings.Join(parts[:i], "/")
		start := ns + "/" + wiki.StartPage
		if start == id {
			break
		}
		t := parts[i-1]
		if p, ok := idx.Pages[start]; ok {
			t = p.Title
		}
		out = append(out, Crumb{Title: t, URL: wiki.PageURL(start)})
	}
	if id != wiki.StartPage {
		out = append(out, Crumb{Title: title, URL: wiki.PageURL(id)})
	}
	out[len(out)-1].Last = true
	return out
}

func (s *Server) tools(v *View) []Tool {
	var t []Tool
	pagePath := strings.TrimPrefix(wiki.PageURL(v.ID), "/")
	// Special pages (search, recent changes...) have no page tools.
	isPage := !strings.HasPrefix(v.ID, "_")
	if v.CanEdit && isPage {
		label := "Edit"
		if !v.Exists {
			label = "Create"
		}
		t = append(t, Tool{Name: "edit", Label: label, URL: "/_/edit/" + pagePath, Active: v.Mode == "edit"})
	}
	if isPage && (v.Exists || v.Mode == "history") {
		t = append(t, Tool{Name: "history", Label: "History", URL: "/_/history/" + pagePath, Active: v.Mode == "history"})
		if v.CanEdit && v.Exists {
			t = append(t, Tool{Name: "move", Label: "Rename", URL: "/_/move/" + pagePath, Active: v.Mode == "move"})
		}
		t = append(t, Tool{Name: "backlinks", Label: fmt.Sprintf("Backlinks (%d)", v.Backlinks), URL: "/_/backlinks/" + pagePath, Active: v.Mode == "backlinks"})
	}
	t = append(t,
		Tool{Name: "recent", Label: "Recent changes", URL: "/_/recent", Active: v.Mode == "recent"},
	)
	if v.CanEdit {
		ns := wiki.Namespace(v.ID)
		if strings.HasPrefix(v.ID, "_") || isFile(v.ID) {
			ns = ""
		}
		t = append(t,
			Tool{Name: "create", Label: "New page", URL: "/_/create?ns=" + ns, Active: v.Mode == "create"},
			Tool{Name: "files", Label: "Files", URL: "/_/files", Active: v.Mode == "files"},
		)
	}
	if v.User != "" || s.cfg.Public {
		t = append(t, Tool{Name: "help", Label: "Help", URL: "/_/help", Active: v.Mode == "help"})
	}
	if v.User != "" {
		t = append(t, Tool{Name: "account", Label: "Your account", URL: "/_/account", Active: v.Mode == "account"})
		if v.IsAdmin {
			t = append(t, Tool{Name: "users", Label: "Users", URL: "/_/users", Active: v.Mode == "users"})
		}
		t = append(t, Tool{Name: "logout", Label: "Log out (" + v.User + ")", URL: "/_/logout", Post: true})
	} else {
		t = append(t, Tool{Name: "login", Label: "Log in", URL: "/_/login?next=" + v.URL})
	}
	return t
}

// fileCrumbs is the trail for non-page files: Files › folder › … › title.
func fileCrumbs(file, title string) []Crumb {
	out := []Crumb{{Title: "Files", URL: "/_/files"}}
	acc := ""
	segs := strings.Split(file, "/")
	for _, seg := range segs[:len(segs)-1] {
		acc = strings.TrimPrefix(acc+"/"+seg, "/")
		out = append(out, Crumb{Title: seg, URL: "/_/files?dir=" + acc})
	}
	return append(out, Crumb{Title: title, Last: true})
}
