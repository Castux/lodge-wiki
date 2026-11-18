package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Castux/lodge-wiki/internal/wiki"
)

// Notifications: after a page is created, edited, deleted or reverted,
// the rules in _config/notifications.json may post a message to a webhook
// (Discord). Nothing is sent unless the server runs with --notify;
// otherwise the messages are only logged.

// NotificationsFile is the rules file in the content repo.
const NotificationsFile = "_config/notifications.json"

type notifyConfig struct {
	BaseURL string       `json:"base_url"` // public address of the wiki, for links
	Hooks   []notifyHook `json:"hooks"`
}

type notifyHook struct {
	Webhook     string         `json:"webhook"`
	On          []string       `json:"on"`         // create, edit, delete, revert (all if absent)
	Namespaces  []string       `json:"namespaces"` // folders whose pages trigger it (all if absent)
	Template    map[string]any `json:"template"`   // a Discord embed, with %placeholders%
	AttachImage bool           `json:"attach-image"`
	Color       string         `json:"color"`
	ColorCreate string         `json:"color-create"`
	ColorEdit   string         `json:"color-edit"`
	ColorDelete string         `json:"color-delete"`
	ColorRevert string         `json:"color-revert"`
}

// pageEvent describes a change, captured when it happens.
type pageEvent struct {
	Type     string // create, edit, delete, revert
	ID       string
	Title    string
	Abstract string
	Image    string // media path of the page's first image, or ""
	User     string // display name
	Summary  string
}

var pastTense = map[string]string{"create": "created", "edit": "edited", "delete": "deleted", "revert": "reverted"}

// pageChanged is called after a page change is committed. It captures
// what the message needs now and sends in the background.
func (s *Server) pageChanged(r *http.Request, typ, id, summary string, before *wiki.PageInfo) {
	ev := pageEvent{Type: typ, ID: id, Summary: summary, User: s.user(r)}
	if u := s.auth.User(r); u != nil && u.RealName != "" {
		ev.User = u.RealName // DokuWiki showed the user's full name
	}
	if pg, err := s.store.ReadPage(id); err == nil {
		ev.Title = pg.Title
		ev.Abstract = abstract(pg)
		if p, ok := s.store.Index().Pages[id]; ok && p.Image != "" {
			ev.Image = wiki.MediaPath(p.Image)
		}
	} else if before != nil { // deleted: use what the index knew
		ev.Title = before.Title
	}
	go s.notify(ev)
}

// abstract is the start of the page's text, without its title, like
// DokuWiki's page abstract.
func abstract(pg *wiki.Page) string {
	body := string(pg.Body)
	if h := strings.Index(body, "\n"); h >= 0 && strings.HasPrefix(strings.TrimSpace(body), "#") {
		body = body[h+1:]
	}
	text := strings.Join(strings.Fields(plainText(body)), " ")
	if len([]rune(text)) > 300 {
		text = string([]rune(text)[:300]) + "…"
	}
	return text
}

func (s *Server) loadNotifyConfig() (*notifyConfig, error) {
	b, err := os.ReadFile(s.store.File(NotificationsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c notifyConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", NotificationsFile, err)
	}
	return &c, nil
}

func (s *Server) notify(ev pageEvent) {
	cfg, err := s.loadNotifyConfig()
	if err != nil {
		log.Printf("notifications: %v", err)
		return
	}
	if cfg == nil {
		return
	}
	for i, h := range cfg.Hooks {
		if !h.matches(ev) {
			continue
		}
		embed := fillTemplate(h.Template, ev, cfg.BaseURL)
		if c := h.color(ev.Type); c != "" {
			if n, err := strconv.ParseInt(strings.TrimPrefix(c, "#"), 16, 64); err == nil {
				embed["color"] = n
			}
		}
		var image string
		if h.AttachImage && ev.Image != "" {
			image = s.store.File(path.Join("_media", ev.Image))
			embed["image"] = map[string]any{"url": "attachment://embed" + strings.ToLower(path.Ext(ev.Image))}
		}
		if !s.cfg.Notify {
			title, _ := embed["title"].(string)
			log.Printf("notification %d not sent (start with --notify to send): %s %s %q", i+1, ev.Type, ev.ID, title)
			continue
		}
		if err := sendWebhook(h.Webhook, map[string]any{"embeds": []any{embed}}, image); err != nil {
			log.Printf("notification %d for %s: %v", i+1, ev.ID, err)
		}
	}
}

func (h *notifyHook) matches(ev pageEvent) bool {
	if len(h.On) > 0 && !slices.Contains(h.On, ev.Type) {
		return false
	}
	if len(h.Namespaces) > 0 && !slices.Contains(h.Namespaces, wiki.Namespace(ev.ID)) {
		return false
	}
	return h.Webhook != ""
}

func (h *notifyHook) color(typ string) string {
	c := map[string]string{"create": h.ColorCreate, "edit": h.ColorEdit, "delete": h.ColorDelete, "revert": h.ColorRevert}[typ]
	if c == "" {
		c = h.Color
	}
	return c
}

// fillTemplate copies the template, replacing %placeholders% in every
// string. Empty values become "(none)", as before.
func fillTemplate(tpl map[string]any, ev pageEvent, base string) map[string]any {
	val := func(v string) string {
		if v == "" {
			return "(none)"
		}
		return v
	}
	pageURL := strings.TrimRight(base, "/") + wiki.PageURL(ev.ID)
	repl := strings.NewReplacer(
		"%id%", val(ev.ID), "%url%", val(pageURL), "%title%", val(ev.Title),
		"%abstract%", val(ev.Abstract), "%user%", val(ev.User), "%summary%", val(ev.Summary),
		"%type%", val(ev.Type), "%type-past%", val(pastTense[ev.Type]),
	)
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return repl.Replace(x)
		case map[string]any:
			out := make(map[string]any, len(x))
			for k, e := range x {
				out[k] = walk(e)
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, e := range x {
				out[i] = walk(e)
			}
			return out
		}
		return v
	}
	return walk(tpl).(map[string]any)
}

// sendWebhook posts a Discord webhook message, with the image file attached
// when there is one (multipart, as the old plugin did).
func sendWebhook(webhook string, payload map[string]any, image string) error {
	u, err := url.Parse(webhook)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("bad webhook address")
	}
	js, _ := json.Marshal(payload)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("payload_json", string(js))
	if image != "" {
		if f, err := os.Open(image); err == nil {
			part, _ := mw.CreateFormFile("file", "embed"+strings.ToLower(path.Ext(image)))
			io.Copy(part, f)
			f.Close()
		}
	}
	mw.Close()
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(webhook, mw.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("webhook answered %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
