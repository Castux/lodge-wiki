package main

import (
	"strings"
	"testing"
)

var testSite = &Site{Titles: map[string]string{
	"foolslanding/start":             "Fool's Landing",
	"foolslanding/notice/missingpet": "A Missing Pet",
	"foolslanding/story/start":       "Stories",
	"skarnholm/start":                "Skarnholm",
	"rules/combat":                   "",
	"fool-s-landing":                 "",
}}

func conv1(t *testing.T, id, in string) string {
	t.Helper()
	return Convert(id, in, testSite).Text
}

func TestConvert(t *testing.T) {
	cases := []struct{ name, id, in, want string }{
		{"heading", "a/b", "====== Big Title ======\n\n==== Sub ====", "# Big Title\n\n### Sub\n"},
		{"emphasis", "a/b", "**bold** //it// __un__ ''mono''", "**bold** *it* <u>un</u> `mono`\n"},
		{"emphasis spaces", "a/b", "a ** bold ** b", "a  **bold**  b\n"},
		{"url not italic", "a/b", "see http://x.com/a//b and //it//", "see <http://x.com/a//b> and *it*\n"},
		{"link label", "foolslanding/notice/x", "[[foolslanding:story:start|Stories!]]", "[Stories!](/foolslanding/story/)\n"},
		{"link no label uses title", "foolslanding/notice/x", "[[missingpet]]", "[A Missing Pet](/foolslanding/notice/missingpet)\n"},
		{"link ns", "x", "[[skarnholm:]]", "[Skarnholm](/skarnholm/)\n"},
		{"link to ns dir", "x", "[[foolslanding:story]]", "[Stories](/foolslanding/story/)\n"},
		{"link anchor", "a/b", "[[rules:combat#Attack_Rolls|roll]]", "[roll](/rules/combat#attack-rolls)\n"},
		{"link missing page", "a/b", "[[nothing here]]", "[nothing-here](/a/nothing-here)\n"},
		{"external", "a/b", "[[https://x.org/a b|X]]", "[X](<https://x.org/a%20b>)\n"},
		{"interwiki", "a/b", "[[wp>Kobold]]", "[Kobold](https://en.wikipedia.org/wiki/Kobold)\n"},
		{"image plain", "foolslanding/x", "{{map.jpg|The map}}", "![The map](/_media/foolslanding/map.jpg)\n"},
		{"image sized right", "foolslanding/x", "{{ foolslanding:map.jpg?200|Map}}", `<img src="/_media/foolslanding/map.jpg" width="200" align="right" alt="Map">` + "\n"},
		{"image link", "a/b", "[[skarnholm:start|{{skarnholm:logo.png}}]]", "[![](/_media/skarnholm/logo.png)](/skarnholm/)\n"},
		{"audio", "a/b", "{{song.mp3}}", `<audio controls src="/_media/a/song.mp3"></audio>` + "\n"},
		{"pdf", "a/b", "{{doc.pdf|Rules}}", "[Rules](/_media/a/doc.pdf)\n"},
		{"list", "a/b", "  * one\n  * two\n    * nested\n  - num", "- one\n- two\n  - nested\n1. num\n"},
		{"code", "a/b", "<code lua>\nprint(1)\n</code>", "```lua\nprint(1)\n```\n"},
		{"pre", "a/b", "text\n\n  indented code\n  more", "text\n\n```\nindented code\nmore\n```\n"},
		{"quote", "a/b", "> hello\n>> deeper", "> hello\n> > deeper\n"},
		{"hr", "a/b", "a\n----\nb", "a\n\n---\n\nb\n"},
		{"footnote", "a/b", "Text((a note)) more.", "Text[^1] more.\n\n[^1]: a note\n"},
		{"linebreak", "a/b", `one\\ two\\`, "one<br>two<br>\n"},
		{"entities", "a/b", "a -> b (c) wait... ''x->y''", "a → b © wait… `x->y`\n"},
		{"smiley", "a/b", "nice :-) ok", "nice 🙂 ok\n"},
		{"escape", "a/b", "5 * 3 [x] <b> 1. item", "5 \\* 3 \\[x\\] \\<b> 1. item\n"},
		{"line start", "a/b", "# not a heading\n1. not a list", "\\# not a heading\n1\\. not a list\n"},
		{"wrap", "a/b", "<WRAP noticeboard wood1>\ntext\n</WRAP>", "<div class=\"noticeboard wood1\">\n\ntext\n\n</div>\n"},
		{"wrap span", "a/b", "<wrap badaawi>Hi</wrap>", "<span class=\"badaawi\">Hi</span>\n"},
		{"notoc", "a/b", "~~NOTOC~~\n====== T ======", "---\ntoc: false\n---\n\n# T\n"},
		{"nspages", "foolslanding/start", "<nspages foolslanding:notice -usePictures -textPages=\"\" -sortByCreationDate>", "```list\nns: foolslanding/notice\npictures: true\nsort: created\n```\n"},
		{"nspages default", "skarnholm/location/start", "<nspages -h1 -exclude -textPages=\"\" -nbCol=2>", "```list\nexclude: [self]\ncolumns: 2\n```\n"},
		{"gfm table", "a/b", "^ A ^ B ^\n| 1 | **2** |", "| A | B |\n| --- | --- |\n| 1 | **2** |\n"},
		{"html table", "a/b", "^ A ^^\n| 1 | 2 |", "<table>\n<tr><th colspan=\"2\">A</th></tr>\n<tr><td>1</td><td>2</td></tr>\n</table>\n"},
		{"html block", "a/b", "<html>\n<div>\n\nx</div>\n</html>", "<div>\nx</div>\n"},
		{"json", "skarnholm/mapdata", ` {"a": 1} `, "{\"a\": 1}\n"},
		{"img first line", "a/b", "{{ x.jpg?100}}\nText **b**", "<img src=\"/_media/a/x.jpg\" width=\"100\" align=\"right\" alt=\"\">\n\nText **b**\n"},
		{"del sub", "a/b", "<del>gone</del> H<sub>2</sub>O", "~~gone~~ H<sub>2</sub>O\n"},
		{"nowiki", "a/b", "%%**raw**%% <nowiki>[[x]]</nowiki>", "\\*\\*raw\\*\\* \\[\\[x\\]\\]\n"},
	}
	for _, c := range cases {
		got := conv1(t, c.id, c.in)
		if got != c.want {
			t.Errorf("%s:\n in: %q\ngot: %q\nwant: %q", c.name, c.in, got, c.want)
		}
	}
}

func TestIssues(t *testing.T) {
	r := Convert("a/b", "~~RIP~~ <foo>x</foo>", testSite)
	if len(r.Issues) != 2 || !strings.Contains(strings.Join(r.Issues, ";"), "RIP") {
		t.Errorf("issues = %v", r.Issues)
	}
}

func TestSplitCrossItalics(t *testing.T) {
	cases := map[string]string{
		"//one\n\ntwo// three":          "//one//\n\n//two// three",
		"//a\\\\\n\n  * item\n\nb//":    "//a//\\\\\n\n  * //item//\n\n//b//",
		"x //open\n\nrest":              "x //open//\n\n//rest//",
		"//t//\n\n==== H ====\n\n//u//": "//t//\n\n==== H ====\n\n//u//",
		"//a\n\n==== H ====\n\nb//":     "//a//\n\n==== H ====\n\n//b//",
		"see http://x.org/a and //b//":  "see http://x.org/a and //b//",
	}
	for in, want := range cases {
		if got := splitCrossItalics(in); got != want {
			t.Errorf("splitCrossItalics(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarkdownPassthrough(t *testing.T) {
	in := "<!DOCTYPE markdown>\n\nTitle\n=====\n\n![War](foolslanding:story:war.jpg) [pet](foolslanding:notice:missingpet#Day_one) [x](https://e.org) [y](/a/b)\n\n```\n[k](a:b)\n```\n"
	want := "Title\n=====\n\n![War](/_media/foolslanding/story/war.jpg) [pet](/foolslanding/notice/missingpet#day-one) [x](https://e.org) [y](/a/b)\n\n```\n[k](a:b)\n```\n"
	if got := conv1(t, "foolslanding/story/x", in); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
