package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestEncode(t *testing.T) {
	m := &MediaManifest{
		Files: map[string]MediaEntry{"b/x.png": {1, "aa", "t"}, "a/y.jpg": {2, "bb", "t"}},
		Trash: map[string]MediaEntry{},
	}
	out := string(m.encode())
	if !json.Valid([]byte(out)) {
		t.Fatalf("not JSON:\n%s", out)
	}
	if strings.Index(out, "a/y.jpg") > strings.Index(out, "b/x.png") {
		t.Errorf("not sorted:\n%s", out)
	}
	if strings.Count(out, "\n") != 7 { // one line per file
		t.Errorf("layout:\n%s", out)
	}
	var back MediaManifest
	json.Unmarshal([]byte(out), &back)
	if back.Files["b/x.png"].SHA256 != "aa" {
		t.Errorf("round trip: %+v", back)
	}
}
