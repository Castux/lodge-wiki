package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// List returns the accounts sorted by name.
func (a *Auth) List() []User {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]User, 0, len(a.users))
	for _, u := range a.users {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Update changes a copy of an account under the lock and saves it.
// Accounts are never changed in place: a *User handed to a request stays
// as it was (copy on write), so readers need no lock.
func (a *Auth) Update(name, author, msg string, fn func(u *User)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.users[name]
	if old == nil {
		return fmt.Errorf("no account %q", name)
	}
	u := *old
	fn(&u)
	a.users[name] = &u
	if err := a.saveLocked(author, msg); err != nil {
		a.users[name] = old
		return err
	}
	return nil
}

// Delete removes an account.
func (a *Auth) Delete(name, author string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.users, name)
	return a.saveLocked(author, "accounts: delete "+name)
}

func hashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

const minPassword = 8

// clientIP is the visitor's address. Behind a reverse proxy on the same
// machine (Caddy), it is the last address the proxy added to
// X-Forwarded-For; that header is ignored from anywhere else, since
// visitors could forge it.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return host
}

// isHTTPS reports whether the visitor reached the wiki over HTTPS
// (directly, or through a local reverse proxy that says so).
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https"
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// accountPage lets a logged-in user change their password.
func (s *Server) accountPage(w http.ResponseWriter, r *http.Request) {
	u := s.auth.User(r)
	if u == nil {
		http.Redirect(w, r, "/_/login?next=/_/account", http.StatusFound)
		return
	}
	s.show(w, r, &View{ID: "_account", Title: "Your account", Mode: "account",
		Content: s.ui_("account.html", map[string]any{"User": u, "Role": RoleNames[u.Role], "Saved": r.URL.Query().Get("saved")})})
}

func (s *Server) accountSave(w http.ResponseWriter, r *http.Request) {
	u := s.auth.User(r)
	if u == nil {
		http.Redirect(w, r, "/_/login?next=/_/account", http.StatusFound)
		return
	}
	fail := func(msg string) {
		s.show(w, r, &View{ID: "_account", Title: "Your account", Mode: "account", Status: http.StatusBadRequest,
			Content: s.ui_("account.html", map[string]any{"User": u, "Role": RoleNames[u.Role], "Error": msg})})
	}
	if r.FormValue("action") == "profile" {
		name := strings.TrimSpace(r.FormValue("realname"))
		email := strings.TrimSpace(r.FormValue("email"))
		if email != "" && (!strings.Contains(email, "@") || strings.ContainsAny(email, " :")) {
			fail("That email address does not look right.")
			return
		}
		if err := s.auth.Update(u.Name, u.Name, "accounts: profile of "+u.Name, func(x *User) { x.RealName, x.Email = name, email }); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/_/account?saved=profile", http.StatusSeeOther)
		return
	}
	if s.auth.Check(u.Name, r.FormValue("old")) == nil {
		fail("Your current password is not right.")
		return
	}
	pw := r.FormValue("new")
	if len(pw) < minPassword {
		fail(fmt.Sprintf("The new password must have at least %d characters.", minPassword))
		return
	}
	if pw != r.FormValue("again") {
		fail("The two new passwords are different.")
		return
	}
	h, err := hashPassword(pw)
	if err == nil {
		err = s.auth.Update(u.Name, u.Name, "accounts: new password for "+u.Name, func(x *User) { x.Hash, x.Since = h, time.Now().Unix() })
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Other devices are logged out; this one gets a fresh session.
	s.auth.Start(w, r, u)
	http.Redirect(w, r, "/_/account?saved=1", http.StatusSeeOther)
}

// usersPage lists accounts for admins.
func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	s.showUsers(w, r, r.URL.Query().Get("msg"), "", http.StatusOK)
}

func (s *Server) showUsers(w http.ResponseWriter, r *http.Request, msg, errMsg string, status int) {
	type row struct {
		User
		RoleName string
	}
	var rows []row
	counts := map[string]int{}
	for _, u := range s.auth.List() {
		rows = append(rows, row{u, RoleNames[u.Role]})
		counts[RoleNames[u.Role]]++
	}
	s.show(w, r, &View{ID: "_users", Title: "Users", Mode: "users", Status: status,
		Content: s.ui_("users.html", map[string]any{"Rows": rows, "Msg": msg, "Error": errMsg, "Counts": counts,
			"Me": s.auth.User(r).Name, "Roles": []string{"reader", "editor", "tinkerer", "admin"}})})
}

// usersSave handles the users page's actions: add, role, password, delete.
func (s *Server) usersSave(w http.ResponseWriter, r *http.Request) {
	me := s.auth.User(r).Name
	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	role := RoleReader
	for k, v := range RoleNames {
		if v == r.FormValue("role") {
			role = k
		}
	}
	fail := func(msg string) { s.showUsers(w, r, "", msg, http.StatusBadRequest) }
	done := func(msg string) { http.Redirect(w, r, "/_/users?msg="+url.QueryEscape(msg), http.StatusSeeOther) }

	switch r.FormValue("action") {
	case "add":
		if name == "" || strings.ContainsAny(name, ": /\\") {
			fail("Choose a user name without spaces, colons or slashes.")
			return
		}
		if s.auth.Exists(name) {
			fail("There is already an account named " + name + ".")
			return
		}
		pw := r.FormValue("password")
		if len(pw) < minPassword {
			fail(fmt.Sprintf("The password must have at least %d characters.", minPassword))
			return
		}
		if err := s.auth.SetPassword(name, pw, role, me); err != nil {
			fail(err.Error())
			return
		}
		if rn := strings.TrimSpace(r.FormValue("realname")); rn != "" {
			if err := s.auth.Update(name, me, "accounts: real name of "+name, func(u *User) { u.RealName = rn }); err != nil {
				fail("Account " + name + " created, but its real name could not be saved: " + err.Error())
				return
			}
		}
		done("Account " + name + " created.")
	case "role":
		if name == me && role != RoleAdmin {
			fail("You cannot remove your own admin role.")
			return
		}
		if err := s.auth.Update(name, me, "accounts: "+name+" is now "+RoleNames[role], func(u *User) { u.Role = role }); err != nil {
			fail(err.Error())
			return
		}
		done(name + " is now " + RoleNames[role] + ".")
	case "password":
		pw := r.FormValue("password")
		if len(pw) < minPassword {
			fail(fmt.Sprintf("The password must have at least %d characters.", minPassword))
			return
		}
		h, err := hashPassword(pw)
		if err == nil {
			err = s.auth.Update(name, me, "accounts: new password for "+name, func(u *User) { u.Hash, u.Since = h, time.Now().Unix() })
		}
		if err != nil {
			fail(err.Error())
			return
		}
		done("New password set for " + name + "; their other sessions are logged out.")
	case "delete":
		if name == me {
			fail("You cannot delete your own account.")
			return
		}
		if err := s.auth.Delete(name, me); err != nil {
			fail(err.Error())
			return
		}
		done("Account " + name + " deleted. Their edits stay in the history under their name.")
	default:
		fail("Unknown action.")
	}
}

// Exists reports whether an account exists.
func (a *Auth) Exists(name string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.users[name] != nil
}
