// Command banners lists the pictures around page titles that may want to
// change role, for a person to check (it changes nothing):
//
//   - promote: a landscape picture right before the "# Title" that has a
//     size or side (so it is not shown as the banner), or one right after
//     the title (move it before, without size or side);
//
//   - demote: a notice whose picture is shown as a banner (move it after
//     the title: notices are read whole).
//
//     go run ./migrate/fixes/banners ../lodge-data
//
// With -w it applies the second case: the picture right after the title
// moves before it, without size or side, unless it is a notice picture or
// would be stretched more than 1.5 times (narrower than 564 px), except for
// sized pictures (someone chose those) and pages listed in -skip.
//
//	go run ./migrate/fixes/banners -w -skip foolslanding/npc/servant ../lodge-data
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

var (
	// a line that is only a picture, maybe inside a link
	reMd  = regexp.MustCompile(`^\[?!\[[^\]]*\]\(<?([^)>\s]+)>?(?:\s+"[^"]*")?\)(?:\]\([^)]*\))?$`)
	reTag = regexp.MustCompile(`^<img\b[^>]*>$`)
	reSrc = regexp.MustCompile(`src="([^"]*)"`)
	reW   = regexp.MustCompile(`width="?(\d+)`)
	reAl  = regexp.MustCompile(`align="?(left|right)`)
)

type pic struct {
	src, preset string
	w, h        int
}

func main() {
	write := flag.Bool("w", false, "move the pictures after the titles before them")
	skipList := flag.String("skip", "", "comma-separated page ids to leave alone")
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("usage: banners [-w] [-skip id,id] <content repo>")
	}
	root := flag.Arg(0)
	skip := map[string]bool{}
	for _, id := range strings.Split(*skipList, ",") {
		skip[strings.TrimSpace(id)] = true
	}
	moved := 0
	var promote, move, demote []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		id := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		blocks := blocksOf(string(b))
		t := -1
		for i, bl := range blocks {
			if strings.HasPrefix(bl, "# ") {
				t = i
				break
			}
		}
		if t < 0 {
			return nil
		}
		notice := strings.Contains("/"+id+"/", "/notice/")
		page := "http://127.0.0.1:8097/" + strings.TrimSuffix(strings.TrimSuffix(id, "start"), "/")
		if t == 1 { // a block before the title, and it is the first thing on the page
			if pc, ok := picOf(root, blocks[0]); ok {
				switch {
				case notice && pc.preset == "":
					demote = append(demote, fmt.Sprintf("%s  %s %s", page, pc.src, pc.size()))
				case !notice && pc.preset != "" && pc.landscape():
					promote = append(promote, fmt.Sprintf("%s  %s #%s %s", page, pc.src, pc.preset, pc.size()))
				}
			}
		}
		if t == 0 && len(blocks) > 1 && !notice {
			if pc, ok := picOf(root, blocks[1]); ok && pc.landscape() {
				pr := ""
				if pc.preset != "" {
					pr = "#" + pc.preset + " "
				}
				move = append(move, fmt.Sprintf("%s  %s %s%s", page, pc.src, pr, pc.size()))
				if *write && !skip[id] && !strings.Contains(pc.src, "/notice/") && (pc.preset != "" || pc.w >= 564) {
					if out, ok := moveBefore(string(b)); ok {
						if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
							return err
						}
						moved++
					}
				}
			}
		}
		return nil
	})
	for _, l := range [][]string{promote, move, demote} {
		sort.Strings(l)
	}
	show("1. Before the title, landscape, but with a size or side: drop it to make the banner", promote)
	show("2. Right after the title, landscape: move it before the title (without size or side)", move)
	show("3. Notices shown with a banner: move the picture after the title", demote)
	if *write {
		fmt.Printf("\nmoved %d pictures before their titles\n", moved)
	}
}

var reFrag = regexp.MustCompile(`(\]\(<?[^)#>\s]+)#[a-z-]+`)

// moveBefore puts the page's second block (a one-line picture, right after
// the title) before the title, without its size or side.
func moveBefore(src string) (string, bool) {
	lines := strings.Split(src, "\n")
	ti := 0
	for ti < len(lines) && strings.TrimSpace(lines[ti]) == "" {
		ti++
	}
	pi := ti + 1
	for pi < len(lines) && strings.TrimSpace(lines[pi]) == "" {
		pi++
	}
	if pi >= len(lines) || (pi+1 < len(lines) && strings.TrimSpace(lines[pi+1]) != "") {
		return "", false // not a one-line block
	}
	pic := reFrag.ReplaceAllString(strings.TrimSpace(lines[pi]), "$1")
	rest := append(append([]string{}, lines[ti:pi]...), lines[pi+1:]...)
	// the blank line that followed the picture now follows the title twice
	out := strings.Join(append([]string{pic, ""}, rest...), "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.Replace(out, "\n\n\n", "\n\n", 1)
	}
	return out, true
}

func show(title string, l []string) {
	fmt.Printf("\n## %s (%d)\n", title, len(l))
	for _, s := range l {
		fmt.Println("  " + s)
	}
}

func (p pic) landscape() bool { return p.h > 0 && float64(p.w)/float64(p.h) >= 1.3 }
func (p pic) size() string {
	if p.h == 0 {
		return "(size unknown)"
	}
	if p.w < 800 {
		return fmt.Sprintf("(%d×%d, small: blurry as a banner)", p.w, p.h)
	}
	return fmt.Sprintf("(%d×%d)", p.w, p.h)
}

// blocksOf returns the page's blocks (separated by blank lines), outside
// fenced code, trimmed.
func blocksOf(s string) []string {
	var out []string
	cur := []string{}
	inFence := false
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = cur[:0]
		}
	}
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
		}
		if t == "" && !inFence {
			flush()
			continue
		}
		cur = append(cur, t)
	}
	flush()
	return out
}

// picOf reads a block that is a single picture: its address, layout
// preset (or legacy width/align) and pixel size.
func picOf(root, block string) (pic, bool) {
	var p pic
	switch {
	case reMd.MatchString(block):
		u, err := url.Parse(reMd.FindStringSubmatch(block)[1])
		if err != nil {
			return p, false
		}
		p.src, p.preset = u.Path, u.Fragment
	case reTag.MatchString(block):
		m := reSrc.FindStringSubmatch(block)
		if m == nil {
			return p, false
		}
		p.src = m[1]
		var parts []string
		if w := reW.FindStringSubmatch(block); w != nil {
			n, _ := strconv.Atoi(w[1])
			parts = append(parts, "width="+strconv.Itoa(n))
		}
		if a := reAl.FindStringSubmatch(block); a != nil {
			parts = append(parts, a[1])
		}
		p.preset = strings.Join(parts, "-")
	default:
		return p, false
	}
	if rest, ok := strings.CutPrefix(p.src, "/_media/"); ok {
		if f, err := os.Open(filepath.Join(root, "_media", filepath.FromSlash(rest))); err == nil {
			if c, _, err := image.DecodeConfig(f); err == nil {
				p.w, p.h = c.Width, c.Height
			}
			f.Close()
		}
	}
	return p, true
}
