package server

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// The running build, for the Admin page, with the number of newer commits
// on the code's GitHub repository (its module path) when it can be asked
// without logging in (a public repository): checked at start and every 6
// hours.

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
// -1 when it cannot be known (a private repository answers 404).
func newerCommits(client *http.Client, repo, commit string) int64 {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/compare/"+commit+"...main", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return -1
	}
	defer res.Body.Close()
	var cmp struct {
		Status  string `json:"status"`
		AheadBy int64  `json:"ahead_by"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&cmp) != nil {
		return -1
	}
	return cmp.AheadBy
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
