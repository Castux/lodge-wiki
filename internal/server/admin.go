package server

import (
	"net/http"
	"runtime"
	"strings"
	"time"
)

// started is when the program started, for the Admin page.
var started = time.Now()

// adminPage gathers what admins need: accounts, syncing, backups, and the
// program's version and health.
func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	roles := map[string]int{}
	users := s.auth.List()
	for _, u := range users {
		roles[RoleNames[u.Role]]++
	}
	var counts []string
	for _, role := range []Role{RoleAdmin, RoleTinkerer, RoleEditor, RoleReader} {
		if n := roles[RoleNames[role]]; n > 0 {
			counts = append(counts, plural(n, RoleNames[role]))
		}
	}

	m := s.readManifest()
	var media, trash int64
	for _, e := range m.Files {
		media += e.Size
	}
	for _, e := range m.Trash {
		trash += e.Size
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	commit, date, _ := strings.Cut(s.cfg.Version, " ")
	short := commit[:min(len(commit), 7)]
	if strings.HasSuffix(commit, "-dirty") {
		short += "-dirty" // built with uncommitted changes
	}
	repo := sourceRepo()
	s.show(w, r, &View{ID: "_admin", Title: "Admin", Mode: "admin",
		Content: s.ui_("admin.html", map[string]any{
			"Users": len(users), "Roles": strings.Join(counts, ", "),
			"Syncing": s.syncing, "Sync": s.store.Git.SyncStatus(),
			"Commit": commit, "Short": short, "Date": date,
			"Repo": repo, "Newer": s.newer.Load(), "Checkable": repo != "" && s.commit() != "",
			"Started": started, "Pages": len(s.store.Index().Pages),
			"Media": len(m.Files), "MediaSize": media, "Trash": len(m.Trash), "TrashSize": trash,
			"Memory": int64(mem.Sys), "Go": runtime.Version(),
		})})
}

// syncAlert reports whether admins should look at the Admin page's sync
// section: the last sync failed, or a recent merge kept the wiki's version
// of some files.
func (s *Server) syncAlert() bool {
	if !s.syncing {
		return false
	}
	st := s.store.Git.SyncStatus()
	return st.Err != "" || len(st.Overridden) > 0 && time.Since(st.OverriddenAt) < 7*24*time.Hour
}
