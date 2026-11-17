package server

import "testing"

func TestNotifyRules(t *testing.T) {
	h := notifyHook{Webhook: "x", On: []string{"create"}, Namespaces: []string{"foolslanding/log"}}
	if !h.matches(pageEvent{Type: "create", ID: "foolslanding/log/a"}) {
		t.Error("should match a new log")
	}
	if h.matches(pageEvent{Type: "edit", ID: "foolslanding/log/a"}) {
		t.Error("should not match an edit")
	}
	if h.matches(pageEvent{Type: "create", ID: "foolslanding/log/sub/a"}) {
		t.Error("namespaces match exactly, not sub-folders (as before)")
	}
	all := notifyHook{Webhook: "x", ColorCreate: "0f917c", Color: "ffffff"}
	if !all.matches(pageEvent{Type: "delete", ID: "x"}) || all.color("create") != "0f917c" || all.color("edit") != "ffffff" {
		t.Error("rule without filters, or colours")
	}
}

func TestFillTemplate(t *testing.T) {
	tpl := map[string]any{
		"title":  "Page %id% %type-past%",
		"url":    "%url%",
		"fields": []any{map[string]any{"name": "Summary", "value": "%summary%"}},
	}
	got := fillTemplate(tpl, pageEvent{Type: "edit", ID: "rules/combat"}, "https://example.org/")
	if got["title"] != "Page rules/combat edited" || got["url"] != "https://example.org/rules/combat" {
		t.Errorf("got %v", got)
	}
	if v := got["fields"].([]any)[0].(map[string]any)["value"]; v != "(none)" {
		t.Errorf("empty value should read (none), got %v", v)
	}
	if tpl["title"] != "Page %id% %type-past%" {
		t.Error("template was modified")
	}
}
