package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The media manifest (_config/media.json, in the content repo) lists every
// media file with its size, SHA-256 and modification time, so that media,
// which is not in git, still has a history and can be backed up and
// checked by a separate tool. Files in the bin (_media/.trash) are listed
// in their own section.

// MediaManifestFile is the manifest's path in the content repo.
const MediaManifestFile = "_config/media.json"

// MediaEntry describes one media file.
type MediaEntry struct {
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Modified string `json:"modified"` // RFC 3339, UTC
}

// MediaManifest is the content of _config/media.json.
type MediaManifest struct {
	Files map[string]MediaEntry `json:"files"` // path under _media/
	Trash map[string]MediaEntry `json:"trash"` // path under _media/.trash/
}

var manifestMu sync.Mutex // one scan or write at a time

func (s *Server) readManifest() *MediaManifest {
	m := &MediaManifest{Files: map[string]MediaEntry{}, Trash: map[string]MediaEntry{}}
	if b, err := os.ReadFile(s.store.File(MediaManifestFile)); err == nil {
		json.Unmarshal(b, m)
	}
	if m.Files == nil {
		m.Files = map[string]MediaEntry{}
	}
	if m.Trash == nil {
		m.Trash = map[string]MediaEntry{}
	}
	return m
}

// scanMedia walks _media and fills a manifest, reusing the hashes of prev
// for files whose size and modification time did not change.
func (s *Server) scanMedia(prev *MediaManifest) (*MediaManifest, error) {
	root := s.store.File("_media")
	out := &MediaManifest{Files: map[string]MediaEntry{}, Trash: map[string]MediaEntry{}}
	err := filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(root, fp)
		rel = filepath.ToSlash(rel)
		inTrash := rel == ".trash" || strings.HasPrefix(rel, ".trash/")
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") && !inTrash {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") { // hidden files, including uploads in progress
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		target, old := out.Files, prev.Files
		key := rel
		if inTrash {
			target, old = out.Trash, prev.Trash
			key = strings.TrimPrefix(rel, ".trash/")
		}
		e := MediaEntry{Size: info.Size(), Modified: info.ModTime().UTC().Truncate(time.Second).Format(time.RFC3339)}
		if p, ok := old[key]; ok && p.Size == e.Size && p.Modified == e.Modified && p.SHA256 != "" {
			e.SHA256 = p.SHA256
		} else if e.SHA256, err = fileSHA256(fp); err != nil {
			// Unreadable for now (locked, being written): keep what the
			// manifest knew rather than record a deletion.
			log.Printf("media manifest: %s: %v", rel, err)
			if p, ok := old[key]; ok {
				target[key] = p
			}
			return nil
		}
		target[key] = e
		return nil
	})
	return out, err
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// encode writes the manifest with one file per line, sorted, so its git
// history shows exactly which files changed.
func (m *MediaManifest) encode() []byte {
	var b bytes.Buffer
	section := func(name string, entries map[string]MediaEntry, last bool) {
		fmt.Fprintf(&b, "  %q: {", name)
		keys := make([]string, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for i, k := range keys {
			kj, _ := json.Marshal(k)
			ej, _ := json.Marshal(entries[k])
			sep := ","
			if i == len(keys)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "\n    %s: %s%s", kj, ej, sep)
		}
		if len(keys) > 0 {
			b.WriteString("\n  ")
		}
		b.WriteString("}")
		if !last {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("{\n")
	section("files", m.Files, false)
	section("trash", m.Trash, true)
	b.WriteString("}\n")
	return b.Bytes()
}

// manifestUpdate rescans the media and returns the new manifest content,
// or nil when nothing changed. The caller holds manifestMu.
func (s *Server) manifestUpdate() ([]byte, error) {
	m, err := s.scanMedia(s.readManifest())
	if err != nil {
		return nil, err
	}
	nu := m.encode()
	if old, err := os.ReadFile(s.store.File(MediaManifestFile)); err == nil && bytes.Equal(old, nu) {
		return nil, nil
	}
	return nu, nil
}

// mediaChanged records a media change in the manifest, as its own commit.
func (s *Server) mediaChanged(author, msg string) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	nu, err := s.manifestUpdate()
	if err != nil {
		log.Printf("media manifest: %v", err)
		return
	}
	if nu == nil {
		return
	}
	if _, err := s.store.Git.Write(MediaManifestFile, nu, "-", author, msg); err != nil {
		log.Printf("media manifest: %v", err)
	}
}

// SyncMediaManifest brings the manifest up to date with the files on disk
// (media added or changed outside the wiki). Called once at start-up.
func (s *Server) SyncMediaManifest() {
	start := time.Now()
	before := s.readManifest()
	manifestMu.Lock()
	defer manifestMu.Unlock()
	nu, err := s.manifestUpdate()
	if err != nil || nu == nil {
		if err != nil {
			log.Printf("media manifest: %v", err)
		}
		return
	}
	var after MediaManifest
	json.Unmarshal(nu, &after)
	added, changed, removed := 0, 0, 0
	for k, e := range after.Files {
		if p, ok := before.Files[k]; !ok {
			added++
		} else if p.SHA256 != e.SHA256 {
			changed++
		}
	}
	for k := range before.Files {
		if _, ok := after.Files[k]; !ok {
			removed++
		}
	}
	// A missing or half-copied media folder (fresh server, unmounted
	// disk) must not be recorded as mass deletion: a backup tool trusting
	// the manifest could then drop its copies.
	if removed > 20 && removed*2 > len(before.Files) {
		log.Printf("media manifest NOT updated: %d of %d listed files are missing from %s. Is the media folder in place? Restore it, or delete the files through the wiki.",
			removed, len(before.Files), s.store.File("_media"))
		return
	}
	msg := fmt.Sprintf("media: update the manifest (%d added, %d changed, %d removed)", added, changed, removed)
	if _, err := s.store.Git.Write(MediaManifestFile, nu, "-", "lodge-wiki", msg); err != nil {
		log.Printf("media manifest: %v", err)
		return
	}
	log.Printf("%s in %s", msg, time.Since(start).Round(time.Millisecond))
}
