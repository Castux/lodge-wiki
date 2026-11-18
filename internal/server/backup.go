package server

import (
	"archive/zip"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// backup sends, to admins, one zip with everything needed to rebuild the
// wiki elsewhere: every file of the content repo (pages, data, layouts,
// settings, accounts) with the uploaded media and the bin, the whole
// history as a git bundle, and instructions. It is written as it is sent.
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	bundle, err := s.store.Git.Bundle()
	if err != nil {
		http.Error(w, "could not read the history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	name := "wiki-backup-" + time.Now().Format("2006-01-02-1504") // the server's local time
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()

	add := func(path string, modified time.Time, compress bool) (io.Writer, error) {
		method := zip.Store // pictures, sound and pdf are compressed already
		if compress {
			method = zip.Deflate
		}
		return zw.CreateHeader(&zip.FileHeader{Name: name + "/" + path, Method: method, Modified: modified})
	}
	if f, err := add("README.txt", time.Now(), true); err == nil {
		io.WriteString(f, restoreNote)
	}
	if f, err := add("lodge-data.git.bundle", time.Now(), false); err == nil {
		f.Write(bundle)
	}
	root := s.store.Root
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir // in the bundle
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		src, err := os.Open(p)
		if err != nil {
			return nil // vanished meanwhile (a move): it is in the history
		}
		defer src.Close()
		dst, err := add("lodge-data/"+rel, info.ModTime(), textExts[strings.ToLower(filepath.Ext(rel))] != "")
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		return err
	})
	if err != nil {
		log.Printf("backup: %v", err) // the download is cut short; the browser shows it failed
	}
}

const restoreNote = `This is a complete backup of the wiki.

lodge-data/            every file of the wiki, as plain files: the pages
                       (Markdown, the path is the page's address), data
                       files, the look (_layout), widgets (_widgets),
                       settings and accounts (_config: password hashes,
                       emails, Discord webhooks: keep this private), and
                       the uploaded files (_media, with the bin in
                       _media/.trash). Readable as is.
lodge-data.git.bundle  the whole history of the wiki (every edit, by whom
                       and when), except uploaded files, which git does
                       not keep.

To run the wiki again, with its history:

  1. git clone lodge-data.git.bundle lodge-data-restored
  2. copy lodge-data/_media into lodge-data-restored/_media
  3. get the wiki program: https://github.com/Castux/lodge-wiki
     (Go 1.27 or newer), then from its folder:
       go run . --root ../lodge-data-restored
     and open http://localhost:8080. Accounts and passwords are kept;
     to set an admin password: go run . --root ../lodge-data-restored --set-password <name>
  4. to put it on a server (Debian or Ubuntu): push lodge-data-restored
     to a new private git repository, then follow deploy/README.md in
     lodge-wiki (it asks for the repository's address), and copy the
     media to the server with rsync.
`
