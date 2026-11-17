package server

import (
	"net/http/httptest"
	"testing"
)

func TestFileAccess(t *testing.T) {
	cases := []struct {
		path      string
		ok, admin bool
	}{
		{"foolslanding/start.md", true, false},
		{"skarnholm/mapdata.json", true, false},
		{"_layout/style.css", true, true},
		{"skarnholm/_layout/style.css", true, true},
		{"_widgets/map/widget.js", true, true},
		{"_media/a.json", false, false},
		{".git/config", false, false},
		{"a/../b.md", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		ok, admin := fileAccess(c.path)
		if ok != c.ok || (ok && admin != c.admin) {
			t.Errorf("fileAccess(%q) = %v, %v; want %v, %v", c.path, ok, admin, c.ok, c.admin)
		}
	}
	if fileOf("a/b") != "a/b.md" || fileOf("a/b.json") != "a/b.json" || fileOf("_layout/page.html") != "_layout/page.html" {
		t.Error("fileOf")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.5:4000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Errorf("forwarded header trusted from a remote address: %s", got)
	}
	r.RemoteAddr = "127.0.0.1:4000"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.7")
	if got := clientIP(r); got != "198.51.100.7" {
		t.Errorf("behind the local proxy: %s", got)
	}
}
