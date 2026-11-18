// Command imgtags rewrites the HTML image tags the DokuWiki import left in
// pages (<img src="…" width="300" align="right" alt="">) as Markdown images
// with the wiki's layout presets (![](…#medium-right)):
//
//	width ≤ 250 → small, ≤ 500 → medium, > 700 → full, otherwise no size
//	align left/right → left/right; center or none → nothing (the default)
//
// Left as they are: tags in HTML blocks and tables (the Domination glyphs,
// where Markdown is not read), and tags with other attributes. Portraits in
// a character tracker scene (<div class="statusboard …">) get no size: the
// scene lays them out. Without -w it lists what it would do.
//
//	go run ./migrate/fixes/imgtags ../lodge-data      # list
//	go run ./migrate/fixes/imgtags -w ../lodge-data   # write
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	reImg  = regexp.MustCompile(`<img\b[^>]*>`)
	reAttr = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
	known  = map[string]bool{"src": true, "alt": true, "width": true, "align": true, "loading": true}
)

func main() {
	write := flag.Bool("w", false, "write the changes")
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("usage: imgtags [-w] <content repo>")
	}
	root := flag.Arg(0)
	counts := map[string]int{}
	pages := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out, changed := convert(filepath.ToSlash(rel), string(b), counts)
		if changed == 0 {
			return nil
		}
		pages++
		if *write {
			return os.WriteFile(p, []byte(out), 0o644)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, k := range []string{"converted", "  #small", "  #medium", "  #full", "  no size", "  in a tracker scene",
		"skipped: HTML block or table", "skipped: other attributes"} {
		fmt.Printf("%-32s %d\n", k, counts[k])
	}
	verb := "would change"
	if *write {
		verb = "changed"
	}
	fmt.Printf("%s %d pages\n", verb, pages)
}

// convert rewrites the image tags of one page.
func convert(page, src string, counts map[string]int) (string, int) {
	lines := strings.SplitAfter(src, "\n")
	inFence, inScene, inBlock := false, false, false
	changed := 0
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			inFence = !inFence
			continue
		case inFence:
			continue
		case t == "":
			inBlock = false // a blank line ends an HTML block
			continue
		}
		if strings.HasPrefix(t, `<div class="statusboard`) {
			inScene = true
		}
		// An HTML block starts with a tag other than a lone image, and
		// lasts until the next blank line: Markdown is not read there.
		if strings.HasPrefix(t, "<") && !strings.HasPrefix(t, "<img") && !strings.HasPrefix(t, "</div") {
			inBlock = true
		}
		if strings.HasPrefix(t, "</div") {
			inScene = false
		}
		if !strings.Contains(line, "<img") {
			continue
		}
		lines[i] = reImg.ReplaceAllStringFunc(line, func(tag string) string {
			if inBlock || strings.Contains(line, "<td") {
				counts["skipped: HTML block or table"]++
				return tag
			}
			attrs := map[string]string{}
			for _, m := range reAttr.FindAllStringSubmatch(tag, -1) {
				attrs[strings.ToLower(m[1])] = m[2]
			}
			for a := range attrs {
				if !known[a] {
					counts["skipped: other attributes"]++
					fmt.Printf("%s: left as is (has %s): %s\n", page, a, tag)
					return tag
				}
			}
			if attrs["src"] == "" {
				return tag
			}
			var preset []string
			w, _ := strconv.Atoi(attrs["width"])
			switch {
			case inScene:
				counts["  in a tracker scene"]++
			case w > 0 && w <= 250:
				preset = append(preset, "small")
				counts["  #small"]++
			case w > 250 && w <= 500:
				preset = append(preset, "medium")
				counts["  #medium"]++
			case w > 700:
				preset = append(preset, "full")
				counts["  #full"]++
			default:
				counts["  no size"]++
			}
			if a := strings.ToLower(attrs["align"]); a == "left" || a == "right" {
				preset = append(preset, a)
			}
			url := attrs["src"]
			if len(preset) > 0 {
				url += "#" + strings.Join(preset, "-")
			}
			if strings.ContainsAny(url, " ()<>") {
				url = "<" + url + ">"
			}
			alt := strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(attrs["alt"])
			counts["converted"]++
			changed++
			return "![" + alt + "](" + url + ")"
		})
	}
	return strings.Join(lines, ""), changed
}
