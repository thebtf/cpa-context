package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// The roster is what the fleet renders, and it names each member itself. A fix
// that only reached rosterOf left the name in the roster untouched, which is how
// the column of hashes survived one release.
func TestTeamProjectionNamesAMember(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, stateFileName)

	const forkID = "lcp:v1:581ba2d7442cf1ba1d5900f53ccc6c335a24581f98ec915a08425fe7a9b89cd7"
	j := openJournal(path)
	j.appendRequest(&snapshot{
		TraceID: "f1", SessionID: forkID, ParentSession: "lcp:v1:aae707a3b0f3830c5a24f61ecbb0978",
		StartedAt: timeNow(), Model: "m", InputTokens: 10,
		Title: "Investigate the parser window",
	})
	j.close()

	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.attachJournal(path)
	defer p.shutdown()

	team := p.teamProjection(forkID)
	if team == nil {
		t.Fatal("no team projection")
	}
	members, _ := team["members"].([]map[string]any)
	if len(members) != 1 {
		t.Fatalf("%d members, want one", len(members))
	}
	name, _ := members[0]["name"].(string)
	if name == "" {
		t.Fatal("the member has no name")
	}
	if name == forkID || looksLikeHash(name) {
		t.Errorf("name = %q: a hash is not a name", name)
	}
	if strings.HasPrefix(name, "lcp:") {
		t.Errorf("name = %q: the technical id leaked into the name", name)
	}
}

// A session with a title is named by that title, which is the same line its card
// shows in the list.
func TestTeamProjectionUsesTheTitle(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	raw, _ := json.Marshal(map[string]any{
		"RequestID": "c1", "TraceID": "c1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{"X-Session-Id": {"titled"}},
		"Body":    []byte(`{"model":"m","messages":[{"role":"user","content":"Review the rate limiter for gaps"}]}`),
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"c1","Model":"m","SessionID":"titled","Detail":{"InputTokens":10,"TotalTokens":10}}`))

	members, _ := p.teamProjection("header:titled")["members"].([]map[string]any)
	name, _ := members[0]["name"].(string)
	if name != "Review the rate limiter for gaps" {
		t.Errorf("name = %q, want the conversation title", name)
	}
}
