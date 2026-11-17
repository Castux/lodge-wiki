// Command lodge-wiki serves the Chaser's Lodge wiki from a content repo.
//
//	go run . --root ../lodge-data
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Castux/lodge-wiki/internal/server"
)

func main() {
	root := flag.String("root", "../lodge-data", "content repository")
	state := flag.String("state", "", "runtime state folder (accounts, session key, image cache); default: <root>/../lodge-state")
	addr := flag.String("addr", "localhost:8080", "listen address")
	title := flag.String("title", "The Chasers' Lodge", "site title")
	public := flag.Bool("public", false, "allow reading without logging in")
	devAs := flag.String("as", "", "DEVELOPMENT ONLY: treat every request as this logged-in account")
	setPassword := flag.String("set-password", "", "create or update this admin account (password read from stdin) and exit")
	flag.Parse()

	if *state == "" {
		abs, _ := filepath.Abs(*root)
		*state = filepath.Join(filepath.Dir(abs), "lodge-state")
	}
	auth, err := server.NewAuthFor(*state)
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
		if err := auth.SetPassword(*setPassword, pw, server.RoleAdmin); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(os.Stderr, "Saved account", *setPassword)
		return
	}
	if !auth.HasUsers() {
		b := make([]byte, 12)
		rand.Read(b)
		pw := base64.RawURLEncoding.EncodeToString(b)
		if err := auth.SetPassword("admin", pw, server.RoleAdmin); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nCreated account  admin / %s\n(change it with: go run . --root %s --set-password admin)\n\n", pw, *root)
	}

	start := time.Now()
	srv, err := server.New(server.Config{Root: *root, State: *state, Title: *title, Public: *public, DevUser: *devAs})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("indexed %d pages in %s", len(srv.Store().Index().Pages), time.Since(start).Round(time.Millisecond))
	if *devAs != "" {
		log.Printf("WARNING: --as %s: every visitor is logged in as %s. Never use this on a server.", *devAs, *devAs)
	}
	log.Printf("serving %s on http://%s", srv.Store().Root, *addr)
	log.Fatal(http.ListenAndServe(*addr, srv))
}
