package wiki

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Page is a parsed page file.
type Page struct {
	ID    string
	Meta  map[string]any // frontmatter
	Body  []byte         // markdown without frontmatter
	Raw   []byte         // the whole file
	Title string         // frontmatter title, else first heading, else name
}

// Store gives access to the content repository on disk.
type Store struct {
	Root string
	Git  *Git

	mu    sync.RWMutex
	index *Index
}

// Open opens a content repository.
func Open(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return nil, errors.New("content root not found: " + abs)
	}
	s := &Store{Root: abs, Git: &Git{Dir: abs}}
	return s, s.Reindex()
}

// PageFile returns the path of a page's markdown file.
func (s *Store) PageFile(id string) string {
	return filepath.Join(s.Root, filepath.FromSlash(id)+".md")
}

// File returns the absolute path of a repo-relative file path.
func (s *Store) File(rel string) string {
	return filepath.Join(s.Root, filepath.FromSlash(rel))
}

// ReadPage loads and parses a page. It returns fs.ErrNotExist for missing pages.
func (s *Store) ReadPage(id string) (*Page, error) {
	raw, err := os.ReadFile(s.PageFile(id))
	if err != nil {
		return nil, err
	}
	return ParsePage(id, raw), nil
}

// ParsePage splits frontmatter from body and works out the title.
func ParsePage(id string, raw []byte) *Page {
	p := &Page{ID: id, Raw: raw, Body: raw, Meta: map[string]any{}}
	if bytes.HasPrefix(raw, []byte("---\n")) || bytes.HasPrefix(raw, []byte("---\r\n")) {
		rest := raw[bytes.IndexByte(raw, '\n')+1:]
		if end := findFrontmatterEnd(rest); end >= 0 {
			_ = yaml.Unmarshal(rest[:end], &p.Meta)
			body := rest[end:]
			body = body[bytes.IndexByte(body, '\n')+1:]
			p.Body = body
		}
	}
	if t, ok := p.Meta["title"].(string); ok && t != "" {
		p.Title = t
	} else if h := FirstHeading(p.Body); h != "" {
		p.Title = h
	} else {
		p.Title = Name(id)
	}
	return p
}

func findFrontmatterEnd(b []byte) int {
	pos := 0
	for pos < len(b) {
		line := b[pos:]
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		if strings.TrimRight(string(line), "\r") == "---" {
			return pos
		}
		pos += len(line) + 1
	}
	return -1
}

// FirstHeading returns the plain text of the first heading, if any: an
// ATX heading ("# Title") or a setext one ("Title" over "====" or "----").
func FirstHeading(body []byte) string {
	inFence := false
	lines := strings.Split(string(body), "\n")
	prev := ""
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			prev = ""
			continue
		}
		if inFence {
			continue
		}
		if prev != "" && len(t) >= 2 && (strings.Trim(t, "=") == "" || strings.Trim(t, "-") == "") &&
			!strings.HasPrefix(prev, "#") && !strings.HasPrefix(prev, "<") && !strings.HasPrefix(prev, "-") && !strings.HasPrefix(prev, "!") {
			return StripInline(prev)
		}
		prev = t
		if !strings.HasPrefix(t, "#") {
			continue
		}
		h := strings.TrimLeft(t, "#")
		if h != "" && h[0] != ' ' {
			continue
		}
		h = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(h), "#"))
		return StripInline(h)
	}
	return ""
}

// StripInline removes the most common inline markdown from a short text.
func StripInline(s string) string {
	r := strings.NewReplacer("**", "", "__", "", "`", "", "\\", "")
	s = r.Replace(s)
	// [label](url) -> label
	for {
		i := strings.Index(s, "](")
		if i < 0 {
			break
		}
		j := strings.LastIndex(s[:i], "[")
		k := strings.Index(s[i:], ")")
		if j < 0 || k < 0 {
			break
		}
		s = s[:j] + s[j+1:i] + s[i+k+1:]
	}
	return strings.TrimSpace(s)
}

// PageInfo is what the index knows about a page.
type PageInfo struct {
	ID       string
	Title    string
	Meta     map[string]any
	Created  time.Time
	Modified time.Time
	Author   string // last author
	Links    []string
	Image    string   // first image src, for picture listings
	Media    []string // media files used, as paths under _media/
	Size     int
}

// Index is an in-memory view of every page.
type Index struct {
	Pages     map[string]*PageInfo
	Backlinks map[string][]string
	MediaUse  map[string][]string // media path -> pages using it
	Text      map[string]string   // lowercased body, for search
}

// Index returns the current index.
func (s *Store) Index() *Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index
}

// Exists reports whether a page exists.
func (s *Store) Exists(id string) bool {
	_, ok := s.Index().Pages[id]
	return ok
}

// Title returns a page's title, or "" if it does not exist.
func (s *Store) Title(id string) string {
	if p, ok := s.Index().Pages[id]; ok {
		return p.Title
	}
	return ""
}

// Reindex rebuilds the whole index from disk and git history.
func (s *Store) Reindex() error {
	idx := &Index{Pages: map[string]*PageInfo{}, Backlinks: map[string][]string{}, MediaUse: map[string][]string{}, Text: map[string]string{}}
	err := filepath.WalkDir(s.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != s.Root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "_") {
			return nil
		}
		rel, _ := filepath.Rel(s.Root, path)
		id := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		if id == "README" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		p := ParsePage(id, raw)
		info := &PageInfo{ID: id, Title: p.Title, Meta: p.Meta, Size: len(raw)}
		info.Links, info.Image = scanLinks(id, p.Body)
		info.Media = scanMedia(p.Body)
		if st, err := d.Info(); err == nil {
			info.Modified = st.ModTime()
			info.Created = st.ModTime()
		}
		idx.Pages[id] = info
		idx.Text[id] = strings.ToLower(string(p.Body))
		return nil
	})
	if err != nil {
		return err
	}
	for id, p := range idx.Pages {
		for _, l := range p.Links {
			idx.Backlinks[l] = append(idx.Backlinks[l], id)
		}
		for _, m := range p.Media {
			idx.MediaUse[m] = append(idx.MediaUse[m], id)
		}
	}
	for _, v := range idx.Backlinks {
		sort.Strings(v)
	}
	if hist, err := s.Git.FileTimes(); err == nil {
		for id, p := range idx.Pages {
			if h, ok := hist[id+".md"]; ok {
				p.Created, p.Modified, p.Author = h.Created, h.Modified, h.Author
			}
		}
	}
	s.mu.Lock()
	s.index = idx
	s.mu.Unlock()
	return nil
}

// UpdateIndex refreshes a single page after a write (cheaper than Reindex).
func (s *Store) UpdateIndex(id, author string, when time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.index
	idx := &Index{Pages: make(map[string]*PageInfo, len(old.Pages)), Backlinks: map[string][]string{}, MediaUse: map[string][]string{}, Text: make(map[string]string, len(old.Text))}
	for k, v := range old.Pages {
		idx.Pages[k] = v
	}
	for k, v := range old.Text {
		idx.Text[k] = v
	}
	raw, err := os.ReadFile(s.PageFile(id))
	if err != nil {
		delete(idx.Pages, id)
		delete(idx.Text, id)
	} else {
		p := ParsePage(id, raw)
		info := &PageInfo{ID: id, Title: p.Title, Meta: p.Meta, Size: len(raw), Modified: when, Author: author, Created: when}
		if prev, ok := old.Pages[id]; ok && !prev.Created.IsZero() {
			info.Created = prev.Created
		}
		info.Links, info.Image = scanLinks(id, p.Body)
		info.Media = scanMedia(p.Body)
		idx.Pages[id] = info
		idx.Text[id] = strings.ToLower(string(p.Body))
	}
	for pid, p := range idx.Pages {
		for _, l := range p.Links {
			idx.Backlinks[l] = append(idx.Backlinks[l], pid)
		}
		for _, m := range p.Media {
			idx.MediaUse[m] = append(idx.MediaUse[m], pid)
		}
	}
	for _, v := range idx.Backlinks {
		sort.Strings(v)
	}
	s.index = idx
}

// Children lists the pages directly in a namespace and its direct
// sub-namespaces.
func (idx *Index) Children(ns string) (pages []*PageInfo, subns []string) {
	seen := map[string]bool{}
	prefix := ns + "/"
	if ns == "" {
		prefix = ""
	}
	for id, p := range idx.Pages {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		rest := id[len(prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			sub := prefix + rest[:i]
			if !seen[sub] {
				seen[sub] = true
				subns = append(subns, sub)
			}
			continue
		}
		pages = append(pages, p)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].ID < pages[j].ID })
	sort.Strings(subns)
	return
}

// HasNamespace reports whether any page lives under a namespace.
func (s *Store) HasNamespace(ns string) bool {
	if ns == "" {
		return true
	}
	prefix := ns + "/"
	for id := range s.Index().Pages {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// Linkable reports whether a link to id leads somewhere: an existing page,
// or the start page of a namespace that has pages (shown as a listing).
func (s *Store) Linkable(id string) bool {
	if s.Exists(id) {
		return true
	}
	return Name(id) == StartPage && s.HasNamespace(Namespace(id))
}
