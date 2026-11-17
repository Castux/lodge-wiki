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

	"golang.org/x/crypto/bcrypt"
)

// Role is a user's permission level.
type Role int

const (
	RoleReader Role = iota + 1
	RoleEditor
	RoleAdmin
)

// User is an account.
type User struct {
	Name string `json:"-"`
	Hash string `json:"hash"`
	Role Role   `json:"role"`
}

// Auth stores accounts in <state>/users.json and signs session cookies
// with a key in <state>/session.key. Neither lives in the content repo.
type Auth struct {
	file  string
	key   []byte
	mu    sync.RWMutex
	users map[string]*User

	devUser string // development only, see Config.DevUser
}

const cookieName = "lodge_session"
const sessionLife = 90 * 24 * time.Hour

// NewAuth loads (or creates) the account store.
func NewAuth(state string) (*Auth, error) {
	a := &Auth{file: filepath.Join(state, "users.json"), users: map[string]*User{}}
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
	if b, err := os.ReadFile(a.file); err == nil {
		if err := json.Unmarshal(b, &a.users); err != nil {
			return nil, fmt.Errorf("%s: %w", a.file, err)
		}
	}
	for n, u := range a.users {
		u.Name = n
	}
	return a, nil
}

// HasUsers reports whether any account exists.
func (a *Auth) HasUsers() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.users) > 0
}

// SetPassword creates or updates an account.
func (a *Auth) SetPassword(name, password string, role Role) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.users[name] = &User{Name: name, Hash: string(h), Role: role}
	b, _ := json.MarshalIndent(a.users, "", "  ")
	return os.WriteFile(a.file, b, 0o600)
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

// Start sets a session cookie.
func (a *Auth) Start(w http.ResponseWriter, u *User) {
	exp := time.Now().Add(sessionLife).Unix()
	val := url.QueryEscape(u.Name) + "|" + strconv.FormatInt(exp, 10)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: val + "|" + a.sign(val), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: time.Unix(exp, 0)})
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
	return a.users[name]
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
	u := s.auth.Check(r.FormValue("user"), r.FormValue("password"))
	if u == nil {
		w.WriteHeader(http.StatusUnauthorized)
		s.show(w, r, &View{ID: "_login", Title: "Log in", Mode: "login",
			Content: s.ui_("login.html", map[string]any{"Next": next, "Error": "Wrong user name or password.", "User": r.FormValue("user")})})
		return
	}
	s.auth.Start(w, u)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.End(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// NewAuthFor opens the account store in a state folder, creating it.
func NewAuthFor(state string) (*Auth, error) {
	if err := os.MkdirAll(state, 0o755); err != nil {
		return nil, err
	}
	return NewAuth(state)
}
