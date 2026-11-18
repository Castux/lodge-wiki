// Command linktitles replaces the text of links that the DokuWiki import
// filled in with the target's page name ("[mudrow-blues](/skarnholm/log/
// mudrow-blues)") with the target's title. DokuWiki showed the title of a
// page for links written without text; the import used the page name when
// it did not know the title.
//
// A link is changed only when its text is exactly the target's page name
// (or, for a namespace start page, the namespace name), the target has a
// real title, and the page's DokuWiki source linked that target without a
// label ([[ns:page]] or [[ns:page|]]) and never with that same text as an
// explicit label: people did type "[[creature:chimera|chimera]]" on purpose.
// Without -w it lists the changes.
//
//	go run ./migrate/fixes/linktitles ../lodge-data ../public_html/data/pages      # list
//	go run ./migrate/fixes/linktitles -w ../lodge-data ../public_html/data/pages   # write
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

// [text](dest) or [text](<dest>), not preceded by "!" (images). The text
// holds no "]" except escaped ones.
var reLink = regexp.MustCompile(`(^|[^!\\])\[((?:[^\]\\\n]|\\.)*)\]\((<[^>\n]*>|[^)\s]*)\)`)

func main() {
	write := flag.Bool("w", false, "write the changes")
	flag.Parse()
	if flag.NArg() != 2 {
		log.Fatal("usage: linktitles [-w] <content repo> <dokuwiki data/pages>")
	}
	root, dwPages := flag.Arg(0), flag.Arg(1)
	store, err := wiki.Open(root)
	if err != nil {
		log.Fatal(err)
	}
	links, files := 0, 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		from := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(filepath.Join(dwPages, filepath.FromSlash(from)+".txt"))
		if err != nil {
			return nil // a page made after the import: nothing was auto-filled
		}
		auto, labelled := dokuLinks(string(src))
		out, n := fix(store, from, string(b), auto, labelled)
		if n == 0 {
			return nil
		}
		links += n
		files++
		if *write {
			return os.WriteFile(p, []byte(out), 0o644)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	verb := "would change"
	if *write {
		verb = "changed"
	}
	fmt.Printf("%s %d links in %d pages\n", verb, links, files)
}

var reDokuLink = regexp.MustCompile(`\[\[([^\]|]*)(?:\|([^\]]*))?\]\]`)

// dokuLinks reads the internal links of a DokuWiki page: the page names
// linked without a label, and "name|label" for those linked with one.
func dokuLinks(src string) (auto, labelled map[string]bool) {
	auto, labelled = map[string]bool{}, map[string]bool{}
	for _, m := range reDokuLink.FindAllStringSubmatch(src, -1) {
		target, _, _ := strings.Cut(m[1], "#")
		if strings.Contains(target, "://") || strings.Contains(target, ">") {
			continue
		}
		segs := strings.FieldsFunc(target, func(r rune) bool { return r == ':' || r == '/' })
		if len(segs) == 0 {
			continue
		}
		name := segs[len(segs)-1]
		if name == wiki.StartPage && len(segs) > 1 {
			name = segs[len(segs)-2]
		}
		name = strings.NewReplacer(" ", "-", "_", "-").Replace(strings.ToLower(strings.TrimSpace(name)))
		if label := strings.TrimSpace(m[2]); label == "" {
			auto[name] = true
		} else {
			labelled[name+"|"+label] = true
		}
	}
	return auto, labelled
}

// fix rewrites the links of one page, outside fenced code blocks.
func fix(store *wiki.Store, from, src string, auto, labelled map[string]bool) (string, int) {
	lines := strings.SplitAfter(src, "\n")
	inFence, n := false, 0
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = reLink.ReplaceAllStringFunc(line, func(m string) string {
			g := reLink.FindStringSubmatch(m)
			lead, text, dest := g[1], g[2], strings.Trim(g[3], "<>")
			id, _, ok := wiki.LinkTarget(from, dest)
			if !ok || !store.Exists(id) {
				return m
			}
			plain := strings.ReplaceAll(text, `\`, "")
			name := wiki.Name(id)
			if name == wiki.StartPage {
				name = wiki.Name(wiki.Namespace(id))
			}
			title := store.Title(id)
			if plain != name || title == "" || title == name || title == plain ||
				!auto[name] || labelled[name+"|"+plain] {
				return m
			}
			n++
			fmt.Printf("%s: [%s] -> [%s]  (%s)\n", from, text, title, dest)
			return lead + "[" + escape(title) + "](" + g[3] + ")"
		})
	}
	return strings.Join(lines, ""), n
}

// escape backslash-escapes the characters that could make a title read as
// markdown inside link text (any ASCII punctuation may be escaped).
func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`,
		"[", `\[`, "]", `\]`, "<", `\<`, "|", `\|`).Replace(s)
}
