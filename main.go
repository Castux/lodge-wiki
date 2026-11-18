// Command lodge-wiki serves the Chasers' Lodge wiki from a content repo.
//
//	go run . --root ../lodge-data
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Castux/lodge-wiki/internal/server"
)

// version is set when building for a server (deploy/setup.sh):
// "<commit> <date>". Otherwise Go's build info gives it, if it can.
var version string

func main() {
	root := flag.String("root", "../lodge-data", "content repository")
	state := flag.String("state", "", "disposable runtime state folder (session key, scaled-image cache); default: <root>/../lodge-state")
	addr := flag.String("addr", "localhost:8080", "listen address")
	title := flag.String("title", "The Chasers' Lodge", "site title")
	public := flag.Bool("public", false, "allow reading without logging in")
	devAs := flag.String("as", "", "DEVELOPMENT ONLY: treat every request as this logged-in account")
	notify := flag.Bool("notify", false, "send the notifications in _config/notifications.json (otherwise they are only logged)")
	syncRepo := flag.Bool("sync", false, "sync the content repo with its upstream (e.g. GitHub): merge changes pushed there, push the wiki's own")
	syncEvery := flag.Duration("sync-every", 5*time.Minute, "with --sync: how often to check the upstream for changes")
	setPassword := flag.String("set-password", "", "create or update this admin account (password read from stdin) and exit")
	flag.Parse()

	if *state == "" {
		abs, _ := filepath.Abs(*root)
		*state = filepath.Join(filepath.Dir(abs), "lodge-state")
	}
	auth, err := server.NewAuthFor(*state, *root)
	if err != nil {
		log.Fatal(err)
	}
	if *setPassword != "" {
		fmt.Fprint(os.Stderr, "Password: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pw := strings.TrimSpace(line)
		if pw == "" {
			log.Fatal("empty password")
		}
		if err := auth.SetPassword(*setPassword, pw, server.RoleAdmin, "lodge-wiki"); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(os.Stderr, "Saved account", *setPassword)
		return
	}
	if !auth.HasUsers() {
		pw := rand.Text()
		if err := auth.SetPassword("admin", pw, server.RoleAdmin, "lodge-wiki"); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nCreated account  admin / %s\n(change it with: go run . --root %s --set-password admin)\n\n", pw, *root)
	}

	start := time.Now()
	srv, err := server.New(server.Config{Root: *root, State: *state, Title: *title, Public: *public, DevUser: *devAs, Notify: *notify, Sync: *syncRepo, SyncEvery: *syncEvery, Version: version})
	if err != nil {
		log.Fatal(err)
	}
	go srv.WatchUpdates()
	log.Printf("indexed %d pages in %s", len(srv.Store().Index().Pages), time.Since(start).Round(time.Millisecond))
	go srv.SyncMediaManifest()
	if *devAs != "" {
		log.Printf("WARNING: --as %s: every visitor is logged in as %s. Never use this on a server.", *devAs, *devAs)
	}
	// Stop cleanly on Ctrl+C or a service stop: requests in progress (and
	// the git commit they may be making) finish first, so no stale git
	// lock is left behind.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hs := &http.Server{Addr: *addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		log.Printf("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hs.Shutdown(sctx)
	}()
	log.Printf("serving %s on http://%s", srv.Store().Root, *addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
