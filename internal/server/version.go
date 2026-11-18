package server

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// The running build, for admins at the bottom of the sidebar, with the
// number of newer commits on the code's GitHub repository (its module path)
// when it can be asked without logging in (a public repository): checked
// at start and every 6 hours; nothing is shown when it cannot be known.

const updateEvery = 6 * time.Hour

// sourceRepo is "owner/name" for a module on GitHub, or "".
func sourceRepo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	p, ok := strings.CutPrefix(info.Main.Path, "github.com/")
	if !ok || strings.Count(p, "/") != 1 {
		return ""
	}
	return p
}

// commit is the full hash in Config.Version ("<hash> <date>"), or "".
func (s *Server) commit() string {
	h, _, _ := strings.Cut(s.cfg.Version, " ")
	if len(h) != 40 {
		return ""
	}
	return h
}

// WatchUpdates keeps the count of newer commits up to date, for as long as
// the program runs (go srv.WatchUpdates()).
func (s *Server) WatchUpdates() {
	repo, commit := sourceRepo(), s.commit()
	if repo == "" || commit == "" {
		return
	}
	client := &http.Client{Timeout: 20 * time.Second}
	for {
		s.newer.Store(newerCommits(client, repo, commit))
		time.Sleep(updateEvery)
	}
}

// newerCommits is how many commits main has that the running build lacks;
// 0 when it cannot be known (a private repository answers 404).
func newerCommits(client *http.Client, repo, commit string) int64 {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/compare/"+commit+"...main", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer res.Body.Close()
	var cmp struct {
		Status  string `json:"status"`
		AheadBy int64  `json:"ahead_by"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&cmp) != nil {
		return 0
	}
	return cmp.AheadBy
}

// versionTool is the sidebar's line for admins: the running commit and
// its date, linking to it, or to the newer commits when there are some.
func (s *Server) versionTool() (Tool, bool) {
	h, date, _ := strings.Cut(s.cfg.Version, " ")
	if h == "" {
		return Tool{}, false
	}
	label := "lodge-wiki " + h[:min(7, len(h))]
	if strings.HasSuffix(h, "-dirty") {
		label += "-dirty" // built with uncommitted changes
	}
	if date != "" {
		label += ", " + date
	}
	t := Tool{Name: "version", Label: label}
	if repo := sourceRepo(); repo != "" && s.commit() != "" {
		t.URL = "https://github.com/" + repo + "/commit/" + h
		if n := s.newer.Load(); n > 0 {
			t.URL = "https://github.com/" + repo + "/compare/" + h + "...main"
			t.Badge = strconv.FormatInt(n, 10) + " new"
		}
	}
	return t, true
}

// buildVersion is the version Go records when building from a git checkout
// (go build, not go run): "<hash> <date>", "<hash>-dirty <date>" with
// uncommitted changes, or "".
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, at, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			at = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev == "" {
		return ""
	}
	return rev + dirty + " " + strings.SplitN(at, "T", 2)[0]
}
