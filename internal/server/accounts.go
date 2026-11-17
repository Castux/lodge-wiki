package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ImportDokuWiki adds the accounts of a DokuWiki users.auth.php file
// (user:hash:Real Name:email:groups). Hashes are kept as they are, so
// people keep their passwords (DokuWiki used bcrypt). Groups map to roles:
// admin → admin, user → editor, anything else → pending. Accounts listed in
// skip (by user name or group), and accounts that already exist here, are
// left alone.
func (a *Auth) ImportDokuWiki(file string, skip map[string]bool, author string) (added []string, err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	a.mu.Lock()
	defer a.mu.Unlock()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Split(line, ":")
		if len(p) < 5 || skip[p[0]] || a.users[p[0]] != nil {
			continue
		}
		skipped := false
		for _, g := range strings.Split(p[4], ",") {
			skipped = skipped || skip[strings.TrimSpace(g)]
		}
		if skipped {
			continue // service accounts (map token, notifier bot)
		}
		if !strings.HasPrefix(p[1], "$2") {
			continue // not bcrypt: cannot be checked here
		}
		role := RolePending
		for _, g := range strings.Split(p[4], ",") {
			switch strings.TrimSpace(g) {
			case "admin":
				role = RoleAdmin
			case "user":
				if role < RoleEditor {
					role = RoleEditor
				}
			}
		}
		a.users[p[0]] = &User{Name: p[0], Hash: p[1], Role: role, RealName: p[2], Email: p[3]}
		added = append(added, p[0])
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Strings(added)
	if len(added) == 0 {
		return nil, nil
	}
	return added, a.saveLocked(author, fmt.Sprintf("accounts: import %d from DokuWiki", len(added)))
}

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

// Update changes an account under the lock and saves.
func (a *Auth) Update(name, author, msg string, fn func(u *User)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.users[name]
	if u == nil {
		return fmt.Errorf("no account %q", name)
	}
	fn(u)
	return a.saveLocked(author, msg)
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

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
		Content: s.ui_("account.html", map[string]any{"User": u, "Role": RoleNames[u.Role], "Saved": r.URL.Query().Get("saved") != ""})})
}

func (s *Server) accountSave(w http.ResponseWriter, r *http.Request) {
	u := s.auth.User(r)
	if u == nil {
		http.Redirect(w, r, "/_/login?next=/_/account", http.StatusFound)
		return
	}
	fail := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		s.show(w, r, &View{ID: "_account", Title: "Your account", Mode: "account",
			Content: s.ui_("account.html", map[string]any{"User": u, "Role": RoleNames[u.Role], "Error": msg})})
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
	s.auth.Start(w, u)
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
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	s.show(w, r, &View{ID: "_users", Title: "Users", Mode: "users",
		Content: s.ui_("users.html", map[string]any{"Rows": rows, "Msg": msg, "Error": errMsg, "Counts": counts,
			"Me": s.auth.User(r).Name, "Roles": []string{"pending", "reader", "editor", "admin"}})})
}

// usersSave handles the users page's actions: add, role, password, delete.
func (s *Server) usersSave(w http.ResponseWriter, r *http.Request) {
	me := s.auth.User(r).Name
	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	role := RolePending
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
			s.auth.Update(name, me, "accounts: real name of "+name, func(u *User) { u.RealName = rn })
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
