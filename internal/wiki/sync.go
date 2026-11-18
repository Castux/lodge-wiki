package wiki

import (
	"strconv"
	"strings"
	"time"
)

// Syncing with the upstream remote (GitHub): fetch, merge, push. Admins
// may push edits made outside the wiki; the wiki merges them in. When a
// file was changed both by the wiki and upstream in ways git cannot
// combine, the wiki's version of that whole file wins: a player's edit
// is kept, and the admin, who can fetch and redo theirs, is told which
// files (the merge commit message and SyncStatus list them; the upstream
// version stays in the history as the merge's second parent).

// SyncStatus is what the last syncs did.
type SyncStatus struct {
	Remote       string    // the upstream branch, e.g. origin/main
	Time         time.Time // last attempt
	Err          string    // error of the last attempt, if it failed
	Merged       time.Time // last time commits came in from upstream
	Overridden   []string  // files where the wiki's version won, at the last merge that had any
	OverriddenAt time.Time
}

// SyncStatus returns the outcome of the recent syncs.
func (g *Git) SyncStatus() SyncStatus {
	g.statusMu.Lock()
	defer g.statusMu.Unlock()
	return g.status
}

// Upstream returns the upstream branch of the checked-out branch, or "".
func (g *Git) Upstream() string {
	out, err := g.run(nil, nil, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var mergeEnv = []string{
	"GIT_AUTHOR_NAME=lodge-wiki", "GIT_AUTHOR_EMAIL=wiki@lodge.invalid",
	"GIT_COMMITTER_NAME=lodge-wiki", "GIT_COMMITTER_EMAIL=wiki@lodge.invalid",
}

// Sync fetches the upstream, merges it and pushes what the upstream
// lacks. It returns the files the merge changed (to reindex).
func (g *Git) Sync() (changed []string, err error) {
	g.syncMu.Lock()
	defer g.syncMu.Unlock()
	defer func() {
		g.statusMu.Lock()
		g.status.Time = time.Now()
		g.status.Err = ""
		if err != nil {
			g.status.Err = err.Error()
		}
		g.statusMu.Unlock()
	}()

	// Fetching only updates remote refs: no need to hold the write lock.
	if _, err := g.run(nil, nil, "fetch", "--quiet"); err != nil {
		return nil, err
	}

	g.mu.Lock()
	changed, overridden, err := g.mergeUpstream()
	g.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if len(changed) > 0 || len(overridden) > 0 {
		g.statusMu.Lock()
		g.status.Merged = time.Now()
		if len(overridden) > 0 {
			g.status.Overridden, g.status.OverriddenAt = overridden, time.Now()
		}
		g.statusMu.Unlock()
	}

	if g.count("@{u}..HEAD") > 0 {
		if _, err := g.run(nil, nil, "push", "--quiet"); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// count counts the commits of a revision range.
func (g *Git) count(rng string) int {
	out, err := g.run(nil, nil, "rev-list", "--count", rng)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// mergeUpstream merges the fetched upstream into HEAD (called with the
// write lock held). Conflicting files keep the wiki's version.
func (g *Git) mergeUpstream() (changed, overridden []string, err error) {
	if g.count("HEAD..@{u}") == 0 {
		return nil, nil, nil
	}
	out, err := g.run(nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, nil, err
	}
	before := strings.TrimSpace(string(out))

	if _, mergeErr := g.run(nil, mergeEnv, "merge", "--no-edit", "--quiet", "@{u}"); mergeErr != nil {
		out, _ := g.run(nil, nil, "diff", "--name-only", "--diff-filter=U")
		conflicted := lines(out)
		if len(conflicted) == 0 { // failed for another reason
			g.run(nil, nil, "merge", "--abort")
			return nil, nil, mergeErr
		}
		for _, f := range conflicted {
			if _, err := g.run(nil, nil, "cat-file", "-e", "HEAD:"+f); err == nil {
				_, err = g.run(nil, nil, "checkout", "--ours", "--", f)
				if err == nil {
					_, err = g.run(nil, nil, "add", "--", f)
				}
				if err != nil {
					g.run(nil, nil, "merge", "--abort")
					return nil, nil, err
				}
			} else if _, err := g.run(nil, nil, "rm", "-q", "--", f); err != nil { // deleted in the wiki
				g.run(nil, nil, "merge", "--abort")
				return nil, nil, err
			}
		}
		msg := "Merge " + g.Upstream() + "; kept the wiki's version of: " + strings.Join(conflicted, ", ")
		if _, err := g.run(nil, mergeEnv, "commit", "--quiet", "-m", msg); err != nil {
			g.run(nil, nil, "merge", "--abort")
			return nil, nil, err
		}
		overridden = conflicted
	}

	out, err = g.run(nil, nil, "diff", "--name-only", before, "HEAD")
	if err != nil {
		return nil, overridden, err
	}
	return lines(out), overridden, nil
}

func lines(b []byte) []string {
	var out []string
	for l := range strings.SplitSeq(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// StartSyncing syncs with the upstream in the background: a few seconds
// after a burst of commits, and every `every` otherwise (to bring in
// what was pushed from elsewhere). onChange gets the files each merge
// changed. It returns false when the branch has no upstream.
func (g *Git) StartSyncing(every time.Duration, logf func(string, ...any), onChange func([]string)) bool {
	up := g.Upstream()
	if up == "" {
		return false
	}
	g.statusMu.Lock()
	g.status.Remote = up
	g.statusMu.Unlock()
	g.syncs = make(chan struct{}, 1)
	go func() {
		pending := true // sync once at start-up
		for {
			wait := every
			if pending {
				wait = 5 * time.Second
			}
			select {
			case <-g.syncs:
				pending = true
				continue // restart the delay: wait for the burst to end
			case <-time.After(wait):
			}
			pending = false
			changed, _ := g.SyncLogged(logf)
			if len(changed) > 0 {
				onChange(changed)
			}
		}
	}()
	return true
}

// SyncLogged is Sync, logging failures and files where the wiki's
// version won a conflict.
func (g *Git) SyncLogged(logf func(string, ...any)) ([]string, error) {
	start := time.Now()
	changed, err := g.Sync()
	st := g.SyncStatus()
	if err != nil {
		logf("sync with %s failed (will retry): %v", st.Remote, err)
	}
	if st.OverriddenAt.After(start) {
		logf("sync with %s: conflicting changes, kept the wiki's version of %v", st.Remote, st.Overridden)
	}
	return changed, err
}

func (g *Git) syncSoon() {
	if g.syncs == nil {
		return
	}
	select {
	case g.syncs <- struct{}{}:
	default:
	}
}
