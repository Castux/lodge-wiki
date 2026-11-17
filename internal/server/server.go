// Package server is the HTTP side of the wiki.
package server

import (
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Castux/lodge-wiki/internal/render"
	"github.com/Castux/lodge-wiki/internal/wiki"
)

// Config holds the server settings.
type Config struct {
	Root    string // content repository
	State   string // runtime state: users, sessions key, image cache
	Title   string
	Public  bool   // allow reading without logging in
	DevUser string // development only: act as this account on every request
	Notify  bool   // send notifications (otherwise they are only logged)
	Push    bool   // push the content repo to its remote after commits
}

// Server serves one wiki.
type Server struct {
	cfg        Config
	store      *wiki.Store
	render     *render.Renderer
	auth       *Auth
	mux        *http.ServeMux
	ui         *template.Template
	limiter    limiter
	loginLimit limiter
}

// New creates a server.
func New(cfg Config) (*Server, error) {
	store, err := wiki.Open(cfg.Root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.State, 0o755); err != nil {
		return nil, err
	}
	if cfg.Push {
		store.Git.StartPushing(log.Printf)
	}
	auth, err := NewAuth(cfg.State, store.Git)
	if err != nil {
		return nil, err
	}
	auth.devUser = cfg.DevUser
	s := &Server{cfg: cfg, store: store, render: &render.Renderer{Store: store}, auth: auth, limiter: limiter{max: 30}, loginLimit: limiter{max: 8}}
	if s.ui, err = loadUI(); err != nil {
		return nil, err
	}
	s.routes()
	return s, nil
}

// Store returns the content store.
func (s *Server) Store() *wiki.Store { return s.store }

func (s *Server) routes() {
	m := http.NewServeMux()
	m.HandleFunc("GET /_/static/", s.static)
	m.HandleFunc("GET /_media/", s.media)
	m.HandleFunc("GET /_/login", s.loginForm)
	m.HandleFunc("POST /_/login", s.login)
	m.HandleFunc("POST /_/logout", s.logout)
	m.HandleFunc("GET /_/account", s.accountPage)
	m.HandleFunc("POST /_/account", s.accountSave)
	m.Handle("GET /_/users", s.need(RoleAdmin, s.usersPage))
	m.Handle("POST /_/users", s.need(RoleAdmin, s.usersSave))

	m.Handle("GET /_/edit/", s.need(RoleEditor, s.editForm))
	m.Handle("POST /_/edit/", s.need(RoleEditor, s.save))
	m.Handle("POST /_/preview/", s.need(RoleEditor, s.preview))
	m.Handle("GET /_/history/", s.need(RoleReader, s.history))
	m.Handle("GET /_/rev/", s.need(RoleReader, s.revision))
	m.Handle("GET /_/diff/", s.need(RoleReader, s.diff))
	m.Handle("POST /_/revert/", s.need(RoleEditor, s.revert))
	m.Handle("GET /_/move/", s.need(RoleEditor, s.moveForm))
	m.Handle("GET /_/mv", s.need(RoleEditor, s.mvForm))
	m.Handle("POST /_/mv", s.need(RoleEditor, s.mv))
	m.Handle("POST /_/rm", s.need(RoleEditor, s.mediaRemove))
	m.Handle("GET /_/used", s.need(RoleReader, s.mediaUsers))
	m.Handle("GET /_/backlinks/", s.need(RoleReader, s.backlinks))
	m.Handle("GET /_/files", s.need(RoleEditor, s.files))
	m.Handle("GET /_/new", s.need(RoleEditor, s.newFile))
	m.Handle("GET /_/create", s.need(RoleEditor, s.createForm))
	m.Handle("GET /_/help", s.need(RoleReader, s.help))
	m.Handle("GET /_/recent", s.need(RoleReader, s.recent))
	m.Handle("GET /_/search", s.need(RoleReader, s.search))
	m.Handle("GET /_/index", s.need(RoleReader, s.index))
	m.Handle("GET /_/raw/", s.need(RoleReader, s.rawGet))
	m.Handle("PUT /_/raw/", s.need(RoleEditor, s.rawPut))
	m.Handle("GET /_/list", s.need(RoleReader, s.listJSON))
	m.Handle("GET /_/log", s.need(RoleReader, s.logJSON))
	m.Handle("GET /_/media", s.need(RoleReader, s.mediaList))
	m.Handle("POST /_/media", s.need(RoleEditor, s.mediaUpload))
	m.Handle("GET /", s.need(RoleReader, s.page))
	s.mux = m
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.mux.ServeHTTP(w, r)
	if !strings.HasPrefix(r.URL.Path, "/_media/") && !strings.HasPrefix(r.URL.Path, "/_/static/") {
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	}
}

// page serves wiki pages, legacy DokuWiki URLs and files from _layout
// and _widgets folders.
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path

	// Old DokuWiki URLs: /doku.php?id=a:b and /a:b
	if p == "/doku.php" {
		http.Redirect(w, r, wiki.PageURL(wiki.Resolve("", r.URL.Query().Get("id"))), http.StatusMovedPermanently)
		return
	}
	if strings.Contains(p, ":") {
		http.Redirect(w, r, wiki.PageURL(wiki.Resolve("", strings.TrimPrefix(p, "/"))), http.StatusMovedPermanently)
		return
	}

	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if seg == "_layout" || seg == "_widgets" {
			s.repoFile(w, r)
			return
		}
		if strings.HasPrefix(seg, "_") || strings.HasPrefix(seg, ".") {
			http.NotFound(w, r)
			return
		}
	}

	id := wiki.IDFromURL(p)
	if clean := wiki.CleanID(id); clean != id && clean != "" {
		http.Redirect(w, r, wiki.PageURL(clean), http.StatusMovedPermanently)
		return
	}
	// /ns (a namespace without its trailing slash) -> /ns/
	if !s.store.Exists(id) && s.store.Exists(id+"/"+wiki.StartPage) {
		http.Redirect(w, r, wiki.PageURL(id+"/"+wiki.StartPage), http.StatusFound)
		return
	}

	pg, err := s.store.ReadPage(id)
	if errors.Is(err, fs.ErrNotExist) && wiki.Name(id) == wiki.StartPage && s.store.HasNamespace(wiki.Namespace(id)) {
		// A namespace without a start page: list what it holds.
		ns := wiki.Namespace(id)
		title := strings.ReplaceAll(wiki.Name(ns), "-", " ")
		if title != "" {
			title = strings.ToUpper(title[:1]) + title[1:]
		}
		body := "# " + title + "\n\n```list\nns: /" + ns + "\nnamespaces: true\n```\n"
		res := s.render.Render(wiki.ParsePage(id, []byte(body)))
		s.show(w, r, &View{ID: id, Title: title, Mode: "show", Content: template.HTML(res.HTML)})
		return
	}
	if errors.Is(err, fs.ErrNotExist) {
		w.WriteHeader(http.StatusNotFound)
		s.show(w, r, &View{ID: id, Title: wiki.Name(id), Mode: "missing",
			Content: s.ui_("missing.html", map[string]any{"ID": id, "CanEdit": s.can(r, RoleEditor)})})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res := s.render.Render(pg)
	v := &View{ID: id, Title: pg.Title, Mode: "show", Exists: true,
		Content: template.HTML(res.HTML), TOC: template.HTML(res.TOC), Widgets: res.Widgets}
	s.show(w, r, v)
}

// repoFile serves static files from _layout and _widgets folders.
func (s *Server) repoFile(w http.ResponseWriter, r *http.Request) {
	rel := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	full := s.store.File(rel)
	if st, err := os.Stat(full); err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, full)
}

// layoutDirs returns the _layout folders that apply to a page: the
// nearest one holding page.html, then every one below it down to the
// page's namespace. Paths are repo-relative.
func (s *Server) layoutDirs(id string) []string {
	ns := wiki.Namespace(id)
	var chain []string
	for {
		dir := "_layout"
		if ns != "" {
			dir = ns + "/_layout"
		}
		if st, err := os.Stat(s.store.File(dir)); err == nil && st.IsDir() {
			chain = append([]string{dir}, chain...)
			if _, err := os.Stat(s.store.File(dir + "/page.html")); err == nil {
				return chain
			}
		}
		if ns == "" {
			return chain
		}
		ns = wiki.Namespace(ns)
	}
}

func (s *Server) can(r *http.Request, role Role) bool {
	u := s.auth.User(r)
	if u == nil {
		return role == RoleReader && s.cfg.Public
	}
	return u.Role >= role
}

// need wraps a handler with a permission check.
func (s *Server) need(role Role, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.can(r, role) {
			u := s.auth.User(r)
			if u == nil {
				http.Redirect(w, r, "/_/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
				return
			}
			if u.Role == RolePending {
				http.Error(w, "Your account is waiting for an admin to approve it.", http.StatusForbidden)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	})
}

// idFrom extracts the page id after a route prefix like /_/edit/.
func idFrom(r *http.Request, prefix string) string {
	return wiki.IDFromURL(strings.TrimPrefix(r.URL.Path, prefix))
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
