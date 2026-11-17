package server

import "testing"

func TestRewriteRefs(t *testing.T) {
	pages := map[string]string{"a/old": "b/new", "a/sub/start": "c/start"}
	body := `[x](/a/old) [y](/a/old#part) [z](old) [keep](/a/other) [ns](/a/sub/) <a href="/a/old">h</a> ![i](/_media/a/pic.jpg?w=20) <img src="/_media/a/sub/p.png">`

	// Another page: only links to moved pages change.
	got := string(rewriteRefs("a/page", []byte(body), pages, false, "a", "z"))
	want := `[x](/b/new) [y](/b/new#part) [z](/b/new) [keep](/a/other) [ns](/c/) <a href="/b/new">h</a> ![i](/_media/z/pic.jpg?w=20) <img src="/_media/z/sub/p.png">`
	if got != want {
		t.Errorf("rewrite for other page:\n got %s\nwant %s", got, want)
	}

	// The moving page itself: relative links become absolute.
	got = string(rewriteRefs("a/old", []byte(`[r](other) [s](../x/y) [t](/q)`), pages, true, "", ""))
	if want := `[r](/a/other) [s](/x/y) [t](/q)`; got != want {
		t.Errorf("rewrite for moved page:\n got %s\nwant %s", got, want)
	}

	// Media prefix must match whole path segments.
	got = string(rewriteRefs("p", []byte(`![a](/_media/ab/x.png) ![b](/_media/a/x.png)`), nil, false, "a", "z"))
	if want := `![a](/_media/ab/x.png) ![b](/_media/z/x.png)`; got != want {
		t.Errorf("media prefix:\n got %s\nwant %s", got, want)
	}
}

func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		"foolslanding/War Room 2":      "foolslanding/war-room-2",
		"/a/b/Kirrix the Inventor.JPG": "a/b/kirrix-the-inventor.jpg",
		`a\b\c`:                        "a/b/c",
		"foolslanding/story/new-name":  "foolslanding/story/new-name",
	}
	for in, want := range cases {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}
