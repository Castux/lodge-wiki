package wiki

import "testing"

func TestCleanID(t *testing.T) {
	cases := map[string]string{
		"Fool's Landing":            "fool-s-landing",
		"foolslanding:notice:Start": "foolslanding/notice/start",
		"  Élan  Vital ":            "elan-vital",
		"a::b":                      "a/b",
		"_hidden:-x-":               "hidden/x",
		"Straße":                    "strasse",
		"20180113-siege-of-kobold":  "20180113-siege-of-kobold",
	}
	for in, want := range cases {
		if got := CleanID(in); got != want {
			t.Errorf("CleanID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	cases := []struct{ ns, target, want string }{
		{"foolslanding/notice", "missingpet", "foolslanding/notice/missingpet"},
		{"foolslanding/notice", "foolslanding:story:x", "foolslanding/story/x"},
		{"foolslanding/notice", "/rules/combat", "rules/combat"},
		{"foolslanding/notice", "..:story:x", "foolslanding/story/x"},
		{"foolslanding/notice", ".:x", "foolslanding/notice/x"},
		{"foolslanding", "skarnholm:", "skarnholm/start"},
		{"", "Fool's Landing", "fool-s-landing"},
		{"a", "..", "start"},
	}
	for _, c := range cases {
		if got := Resolve(c.ns, c.target); got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c.ns, c.target, got, c.want)
		}
	}
}

func TestFirstHeading(t *testing.T) {
	cases := map[string]string{
		"# Title\n\ntext": "Title",
		"intro\n\nA Witch's Council\n=====\n\n## Later": "A Witch's Council",
		"Sub title\n---------\n":                        "Sub title",
		"Para\n\n---\n\n# Real":                         "Real",
		"```\n# not\n```\n## Yes":                       "Yes",
	}
	for in, want := range cases {
		if got := FirstHeading([]byte(in)); got != want {
			t.Errorf("FirstHeading(%q) = %q, want %q", in, got, want)
		}
	}
}
