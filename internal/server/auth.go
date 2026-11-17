package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Castux/lodge-wiki/internal/wiki"
	"golang.org/x/crypto/bcrypt"
)

// Role is a user's permission level.
type Role int

const (
	RolePending Role = iota // registered, not approved yet: no access
	RoleReader
	RoleEditor
	RoleAdmin
)

// RoleNames are the roles shown in the users page.
var RoleNames = map[Role]string{RolePending: "pending", RoleReader: "reader", RoleEditor: "editor", RoleAdmin: "admin"}

// User is an account.
type User struct {
	Name     string `json:"-"`
	Hash     string `json:"hash"` // bcrypt
	Role     Role   `json:"role"`
	RealName string `json:"realname,omitempty"`
	Email    string `json:"email,omitempty"`
	// Sessions started before this time are refused (set when the
	// password changes, to log out other devices).
	Since int64 `json:"since,omitempty"`
}

// UsersFile is where accounts live in the content repo. Folders named
// _config are private: only admins can see them.
const UsersFile = "_config/users.json"

// Auth keeps the accounts in the content repo (_config/users.json, so they
// are versioned and backed up with everything else; every change is a
// commit) and signs session cookies with a key in <state>/session.key.
// The key is the only secret kept outside the repo: whoever holds it can
// forge a login, and losing it merely logs everyone out.
type Auth struct {
	git   *wiki.Git
	file  string // absolute path of UsersFile
	key   []byte
	mu    sync.RWMutex
	users map[string]*User

	devUser string // development only, see Config.DevUser
}

const cookieName = "lodge_session"
const sessionLife = 90 * 24 * time.Hour

// NewAuth loads the accounts from the content repo and the session key
// from the state folder (creating it). Accounts from an older layout
// (<state>/users.json) are moved into the repo.
func NewAuth(state string, git *wiki.Git) (*Auth, error) {
	a := &Auth{git: git, file: filepath.Join(git.Dir, filepath.FromSlash(UsersFile)), users: map[string]*User{}}
	keyFile := filepath.Join(state, "session.key")
	key, err := os.ReadFile(keyFile)
	if err != nil {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(key)), 0o600); err != nil {
			return nil, err
		}
	} else if k, err := hex.DecodeString(strings.TrimSpace(string(key))); err == nil {
		key = k
	}
	a.key = key

	old := filepath.Join(state, "users.json")
	src := a.file
	if _, err := os.Stat(a.file); err != nil {
		if _, err := os.Stat(old); err == nil {
			src = old
		}
	}
	if b, err := os.ReadFile(src); err == nil {
		if err := json.Unmarshal(b, &a.users); err != nil {
			return nil, fmt.Errorf("%s: %w", src, err)
		}
	}
	for n, u := range a.users {
		u.Name = n
	}
	if src == old {
		a.mu.Lock()
		err := a.saveLocked("lodge-wiki", "accounts: move into the content repo")
		a.mu.Unlock()
		if err != nil {
			return nil, err
		}
		os.Rename(old, old+".moved-to-repo")
	}
	return a, nil
}

// Reload reads the accounts file again (after it was edited by hand).
func (a *Auth) Reload() error {
	b, err := os.ReadFile(a.file)
	if err != nil {
		return err
	}
	users := map[string]*User{}
	if err := json.Unmarshal(b, &users); err != nil {
		return err
	}
	for n, u := range users {
		u.Name = n
	}
	a.mu.Lock()
	a.users = users
	a.mu.Unlock()
	return nil
}

// HasUsers reports whether any account exists.
func (a *Auth) HasUsers() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.users) > 0
}

// SetPassword creates or updates an account.
func (a *Auth) SetPassword(name, password string, role Role, author string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	msg := "accounts: add " + name
	if u, ok := a.users[name]; ok {
		u.Hash, u.Role, u.Since = string(h), role, time.Now().Unix()
		msg = "accounts: new password for " + name
	} else {
		a.users[name] = &User{Name: name, Hash: string(h), Role: role}
	}
	return a.saveLocked(author, msg)
}

// saveLocked commits users.json; the caller holds a.mu. Messages stay
// neutral (they show in recent changes): never passwords or emails.
func (a *Auth) saveLocked(author, msg string) error {
	b, _ := json.MarshalIndent(a.users, "", "  ")
	_, err := a.git.Write(UsersFile, append(b, '\n'), "-", author, msg)
	return err
}

// Check verifies a password.
func (a *Auth) Check(name, password string) *User {
	a.mu.RLock()
	u := a.users[name]
	a.mu.RUnlock()
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.Hash), []byte(password)) != nil {
		return nil
	}
	return u
}

func (a *Auth) sign(s string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Start sets a session cookie (HTTPS-only when the site is served over
// HTTPS).
func (a *Auth) Start(w http.ResponseWriter, r *http.Request, u *User) {
	exp := time.Now().Add(sessionLife).Unix()
	val := url.QueryEscape(u.Name) + "|" + strconv.FormatInt(exp, 10)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: val + "|" + a.sign(val), Path: "/",
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, Expires: time.Unix(exp, 0)})
}

// End clears the session cookie.
func (a *Auth) End(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
}

// User returns the logged-in user, or nil.
func (a *Auth) User(r *http.Request) *User {
	if a.devUser != "" {
		a.mu.RLock()
		u := a.users[a.devUser]
		a.mu.RUnlock()
		if u != nil {
			return u
		}
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	i := strings.LastIndexByte(c.Value, '|')
	if i < 0 {
		return nil
	}
	val, sig := c.Value[:i], c.Value[i+1:]
	if !hmac.Equal([]byte(sig), []byte(a.sign(val))) {
		return nil
	}
	name, expS, _ := strings.Cut(val, "|")
	exp, _ := strconv.ParseInt(expS, 10, 64)
	if time.Now().Unix() > exp {
		return nil
	}
	name, _ = url.QueryUnescape(name)
	a.mu.RLock()
	defer a.mu.RUnlock()
	u := a.users[name]
	if u != nil && u.Since > exp-int64(sessionLife/time.Second) {
		return nil // started before the last password change
	}
	return u
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	s.show(w, r, &View{ID: "_login", Title: "Log in", Mode: "login",
		Content: s.ui_("login.html", map[string]any{"Next": r.URL.Query().Get("next")})})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	next := r.FormValue("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	ip := clientIP(r)
	if !s.loginLimit.allow(ip) {
		w.WriteHeader(http.StatusTooManyRequests)
		s.show(w, r, &View{ID: "_login", Title: "Log in", Mode: "login",
			Content: s.ui_("login.html", map[string]any{"Next": next, "Error": "Too many failed attempts. Wait a minute and try again.", "User": r.FormValue("user")})})
		return
	}
	u := s.auth.Check(strings.ToLower(strings.TrimSpace(r.FormValue("user"))), r.FormValue("password"))
	if u == nil {
		w.WriteHeader(http.StatusUnauthorized)
		s.show(w, r, &View{ID: "_login", Title: "Log in", Mode: "login",
			Content: s.ui_("login.html", map[string]any{"Next": next, "Error": "Wrong user name or password.", "User": r.FormValue("user")})})
		return
	}
	s.loginLimit.reset(ip)
	s.auth.Start(w, r, u)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.End(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// NewAuthFor opens the account store in a state folder, creating it.
func NewAuthFor(state, root string) (*Auth, error) {
	if err := os.MkdirAll(state, 0o755); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return NewAuth(state, &wiki.Git{Dir: abs})
}
