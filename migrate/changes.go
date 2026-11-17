package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// listChanges prints what changed on the DokuWiki side after `since`
// (YYYY-MM-DD), for catching up lodge-data after a new download of the
// live wiki (see migrate/SYNC.md):
//   - pages, from the last line of each data/meta/<id>.changes file
//     (unlike _dokuwiki.changes, these are never trimmed);
//   - media files modified after the date (lftp mirror keeps the dates),
//     and media deletions from data/meta/_media.changes.
//
// With copyMedia, the modified media files are copied into mediaOut
// (only those: files moved in the new wiki do not come back).
func listChanges(data, sinceStr string, copyMedia bool, mediaOut string) {
	since, err := time.ParseInLocation("2006-01-02", sinceStr, time.Local)
	check(err)
	cut := since.Unix()

	type entry struct {
		ts                 int64
		typ, id, user, sum string
	}
	lastLine := func(path string) []string {
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		var last string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) != "" {
				last = sc.Text()
			}
		}
		return strings.Split(last, "\t")
	}

	var pages []entry
	metaDir := filepath.Join(data, "meta")
	_ = filepath.WalkDir(metaDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".changes") || strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		f := lastLine(p)
		if len(f) < 4 {
			return nil
		}
		ts, _ := strconv.ParseInt(f[0], 10, 64)
		if ts <= cut {
			return nil
		}
		e := entry{ts: ts, typ: f[2], id: strings.ReplaceAll(f[3], ":", "/")}
		if len(f) > 4 {
			e.user = f[4]
		}
		if len(f) > 5 {
			e.sum = f[5]
		}
		pages = append(pages, e)
		return nil
	})
	sort.Slice(pages, func(i, j int) bool { return pages[i].ts < pages[j].ts })

	kinds := map[string]string{"C": "created", "E": "edited", "e": "edited (minor)", "D": "deleted", "R": "reverted"}
	fmt.Printf("Pages changed since %s (last change of each):\n", sinceStr)
	var convert, deleted []string
	for _, e := range pages {
		k := kinds[e.typ]
		if k == "" {
			k = e.typ
		}
		fmt.Printf("  %s  %-15s %-50s %s  %s\n", time.Unix(e.ts, 0).Format("2006-01-02 15:04"), k, e.id, e.user, e.sum)
		if e.typ == "D" {
			deleted = append(deleted, e.id)
		} else {
			convert = append(convert, e.id)
		}
	}
	if len(pages) == 0 {
		fmt.Println("  none")
	}

	fmt.Printf("\nMedia files modified since %s:\n", sinceStr)
	var media []string
	mediaDir := filepath.Join(data, "media")
	_ = filepath.WalkDir(mediaDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Unix() > cut {
			rel, _ := filepath.Rel(mediaDir, p)
			media = append(media, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(media)
	for _, m := range media {
		fmt.Println("  " + m)
		if copyMedia {
			dst := filepath.Join(mediaOut, filepath.FromSlash(m))
			check(os.MkdirAll(filepath.Dir(dst), 0o755))
			src := filepath.Join(mediaDir, filepath.FromSlash(m))
			b, err := os.ReadFile(src)
			check(err)
			check(os.WriteFile(dst, b, 0o644))
			if info, err := os.Stat(src); err == nil {
				check(os.Chtimes(dst, info.ModTime(), info.ModTime()))
			}
		}
	}
	if copyMedia && len(media) > 0 {
		fmt.Printf("  (copied into %s)\n", mediaOut)
	}
	if len(media) == 0 {
		fmt.Println("  none")
	}
	if f, err := os.Open(filepath.Join(metaDir, "_media.changes")); err == nil {
		sc := bufio.NewScanner(f)
		var gone []string
		for sc.Scan() {
			x := strings.Split(sc.Text(), "\t")
			if len(x) > 3 && x[2] == "D" {
				if ts, _ := strconv.ParseInt(x[0], 10, 64); ts > cut {
					gone = append(gone, strings.ReplaceAll(x[3], ":", "/"))
				}
			}
		}
		f.Close()
		if len(gone) > 0 {
			fmt.Printf("\nMedia files deleted since %s (the media log may be trimmed: older deletions are not listed):\n", sinceStr)
			for _, g := range gone {
				fmt.Println("  " + g)
			}
		}
	}

	if len(convert) > 0 {
		fmt.Printf("\nTo re-convert the changed pages into lodge-data:\n  go run ./migrate --pages %s\n", strings.Join(convert, ","))
	}
	if len(deleted) > 0 {
		fmt.Printf("\nDeleted on the old wiki (remove by hand if they should go):\n  %s\n", strings.Join(deleted, "\n  "))
	}
}
