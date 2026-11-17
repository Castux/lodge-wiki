package server

import (
	"crypto/sha1"
	"encoding/hex"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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
	if strings.Contains(rel, "..") || !strings.HasPrefix(rel, "_media/") {
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
