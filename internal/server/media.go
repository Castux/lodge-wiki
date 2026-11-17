package server

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Castux/lodge-wiki/internal/wiki"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// media serves files from _media/, scaling raster images down when
// ?w= or ?h= is given. Scaled copies are cached in <state>/cache.
func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	if !s.can(r, RoleReader) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	rel := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if strings.Contains(rel, "..") || !strings.HasPrefix(rel, "_media/") || strings.Contains(rel, "/.") {
		http.NotFound(w, r)
		return
	}
	full := s.store.File(rel)
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	wq, _ := strconv.Atoi(r.URL.Query().Get("w"))
	hq, _ := strconv.Atoi(r.URL.Query().Get("h"))
	ext := strings.ToLower(path.Ext(rel))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if (wq <= 0 && hq <= 0) || (ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp") {
		http.ServeFile(w, r, full)
		return
	}
	// Request at most 2x for sharp images on high-density screens.
	wq, hq = wq*2, hq*2
	key := sha1.Sum([]byte(rel + "|" + strconv.Itoa(wq) + "x" + strconv.Itoa(hq) + "|" + st.ModTime().String()))
	outExt := ".jpg"
	if ext == ".png" {
		outExt = ".png"
	}
	cached := filepath.Join(s.cfg.State, "cache", hex.EncodeToString(key[:])+outExt)
	if _, err := os.Stat(cached); err == nil {
		http.ServeFile(w, r, cached)
		return
	}
	if err := scaleLock(cached, func() error { return scale(full, cached, wq, hq) }); err != nil {
		http.ServeFile(w, r, full) // fall back to the original
		return
	}
	http.ServeFile(w, r, cached)
}

var scaling sync.Map // cache path -> *sync.Mutex

func scaleLock(key string, f func() error) error {
	m, _ := scaling.LoadOrStore(key, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if _, err := os.Stat(key); err == nil {
		return nil
	}
	return f()
}

func scale(src, dst string, w, h int) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	switch {
	case w > 0 && h > 0:
	case w > 0:
		h = sh * w / sw
	default:
		w = sw * h / sh
	}
	if w >= sw || h >= sh || w <= 0 || h <= 0 {
		w, h = sw, sh
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Over, nil)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	of, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if strings.HasSuffix(dst, ".png") {
		err = png.Encode(of, out)
	} else {
		err = jpeg.Encode(of, out, &jpeg.Options{Quality: 85})
	}
	of.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

var _ = gif.Decode

// limiter caps writes per user per minute.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
}

func (l *limiter) allow(user string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	now := time.Now()
	recent := l.hits[user][:0]
	for _, t := range l.hits[user] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.hits[user] = recent
		return false
	}
	l.hits[user] = append(recent, now)
	return true
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/_/")
	b, err := uiFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch path.Ext(name) {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}

var mediaExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".svg": true,
	".pdf": true, ".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".txt": true,
}

// mediaNS validates a media namespace ("foolslanding/story").
func mediaNS(ns string) (string, bool) {
	ns = strings.Trim(path.Clean("/"+ns), "/")
	if strings.Contains(ns, "..") {
		return "", false
	}
	for _, seg := range strings.Split(ns, "/") {
		if strings.HasPrefix(seg, ".") || strings.HasPrefix(seg, "_") {
			return "", false
		}
	}
	return ns, true
}

// mediaList returns the files and folders of a media namespace as JSON.
func (s *Server) mediaList(w http.ResponseWriter, r *http.Request) {
	ns, ok := mediaNS(r.URL.Query().Get("ns"))
	if !ok {
		http.Error(w, "bad namespace", http.StatusBadRequest)
		return
	}
	type file struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Size  int64  `json:"size"`
		Image bool   `json:"image"`
	}
	out := struct {
		NS      string   `json:"ns"`
		Folders []string `json:"folders"`
		Files   []file   `json:"files"`
	}{NS: ns, Folders: []string{}, Files: []file{}}
	dir := s.store.File(path.Join("_media", ns))
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			out.Folders = append(out.Folders, e.Name())
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		ext := strings.ToLower(path.Ext(e.Name()))
		out.Files = append(out.Files, file{e.Name(), "/" + path.Join("_media", ns, e.Name()), info.Size(),
			ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".svg"})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// mediaUpload stores an uploaded file in _media/<ns>/. Media files are
// not versioned in git; an existing file is only replaced with overwrite=1.
func (s *Server) mediaUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		http.Error(w, "upload too large or malformed: "+err.Error(), http.StatusBadRequest)
		return
	}
	ns, ok := mediaNS(r.FormValue("ns"))
	if !ok {
		http.Error(w, "bad namespace", http.StatusBadRequest)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer f.Close()
	ext := strings.ToLower(path.Ext(hdr.Filename))
	if !mediaExts[ext] {
		http.Error(w, "file type not allowed: "+ext, http.StatusBadRequest)
		return
	}
	name := wiki.CleanID(strings.TrimSuffix(hdr.Filename, path.Ext(hdr.Filename)))
	name = strings.ReplaceAll(name, "/", "-") + ext
	rel := path.Join("_media", ns, name)
	full := s.store.File(rel)
	if _, err := os.Stat(full); err == nil && r.FormValue("overwrite") != "1" {
		http.Error(w, "a file named "+name+" already exists here", http.StatusConflict)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	replaced := false
	if _, err := os.Stat(full); err == nil {
		replaced = true
		old := s.trashPath(path.Join(ns, name))
		if err := os.MkdirAll(filepath.Dir(old), 0o755); err == nil {
			if err := os.Rename(full, old); err == nil {
				log.Printf("replaced %s; previous version kept in %s", rel, old)
			}
		}
	}
	tmp := full + ".upload"
	out, err := os.Create(tmp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(out, f)
	out.Close()
	if err == nil {
		err = os.Rename(tmp, full)
	}
	if err != nil {
		os.Remove(tmp)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("upload %s by %s (%d bytes)", rel, s.user(r), hdr.Size)
	verb := "upload "
	if replaced {
		verb = "replace "
	}
	s.mediaChanged(s.user(r), "media: "+verb+path.Join(ns, name))
	if back := r.FormValue("back"); strings.HasPrefix(back, "/_/files") {
		http.Redirect(w, r, back+"&saved="+url.QueryEscape(name+" uploaded"), http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": "/" + rel, "name": name})
}
