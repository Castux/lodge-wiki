package server

import (
	"log"
	"net/http"
	"slices"
)

// merged refreshes what depends on files that a merge from the upstream
// changed: the page index and, if they changed, the accounts.
func (s *Server) merged(files []string) {
	log.Printf("merged %d changed file(s) from %s", len(files), s.store.Git.SyncStatus().Remote)
	if err := s.store.Reindex(); err != nil {
		log.Printf("reindex after merge: %v", err)
	}
	if slices.Contains(files, UsersFile) {
		if err := s.auth.Reload(); err != nil {
			log.Printf("reloading accounts: %v", err)
		}
	}
}

// syncNow is the admins' "Sync now" button (on Recent changes).
func (s *Server) syncNow(w http.ResponseWriter, r *http.Request) {
	if !s.syncing {
		http.Error(w, "syncing is off (start the wiki with --sync, on a branch with an upstream)", http.StatusBadRequest)
		return
	}
	if changed, _ := s.store.Git.SyncLogged(log.Printf); len(changed) > 0 {
		s.merged(changed)
	}
	http.Redirect(w, r, "/_/recent", http.StatusSeeOther)
}
