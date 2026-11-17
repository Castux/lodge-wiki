// Command italics shows how DokuWiki paired the "//" italic markers of a
// page: markers toggle italics across the whole document (not just a
// paragraph), skipping URL schemes, monospace and nowiki. Each marker is
// printed with its line and whether it opens or closes.
//
//	go run ./migrate/fixes/italics ../public_html/data/pages/<id>.txt
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	s := string(b)
	line := 1
	open := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\n':
			line++
		case strings.HasPrefix(s[i:], "%%"):
			if j := strings.Index(s[i+2:], "%%"); j >= 0 {
				line += strings.Count(s[i:i+j+4], "\n")
				i += j + 3
			}
		case strings.HasPrefix(s[i:], "''"):
			if j := strings.Index(s[i+2:], "''"); j >= 0 && !strings.Contains(s[i:i+j+4], "\n") {
				i += j + 3
			}
		case strings.HasPrefix(s[i:], "//"):
			if i > 0 && s[i-1] == ':' && i+2 < len(s) && s[i+2] != ' ' && s[i+2] != '\n' && s[i+2] != '\\' {
				i++
				continue
			}
			open = !open
			kind := "close"
			if open {
				kind = "OPEN "
			}
			start := strings.LastIndexByte(s[:i], '\n') + 1
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				end = len(s) - i
			}
			ctx := s[start : i+end]
			if len(ctx) > 100 {
				k := i - start
				ctx = ctx[max(0, k-40):min(len(ctx), k+50)]
			}
			fmt.Printf("%4d %s %s\n", line, kind, ctx)
			i++
		}
	}
	if open {
		fmt.Println("     (italics still open at end of page)")
	}
}
