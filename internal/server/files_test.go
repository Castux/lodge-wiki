package server

import "testing"

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
