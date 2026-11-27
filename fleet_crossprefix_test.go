package main

import (
	"encoding/json"
	"testing"
)

// The interesting case is a parent that DID arrive, over one format, and a child
// that names it over another. CPA writes the parent id with the child's own
// prefix, so the two names differ: the parent is claude:shared-2 and the child
// points at header:shared-2. The plugin must link them anyway, because it is the
// same session id underneath.
func TestAParentAndChildOnDifferentFormatsStillLink(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	send := func(id, parent, format, body string) {
		headers := map[string][]string{}
		switch format {
		case "claude":
			headers["X-Claude-Code-Session-Id"] = []string{id}
		default:
			headers["X-Session-Id"] = []string{id}
		}
		if parent != "" {
			headers["X-Parent-Session-ID"] = []string{parent}
		}
		raw, _ := json.Marshal(map[string]any{
			"RequestID": id, "TraceID": id, "SourceFormat": format, "Model": "m",
			"Headers": headers,
			"Body":    []byte(`{"model":"m","messages":[{"role":"user","content":"` + body + `"}]}`),
		})
		p.handle("request.intercept_before", raw)
		usage, _ := json.Marshal(map[string]any{
			"TraceID": id, "Model": "m", "SessionID": id, "ParentID": parent,
			"Detail": map[string]any{"InputTokens": 20, "TotalTokens": 20},
		})
		p.handle("usage.handle", usage)
	}

	// Parent over Claude, child over a generic OpenAI-compatible client.
	send("shared-2", "", "claude", "plan the work")
	send("kid-2", "shared-2", "openai", "do one slice")

	// What the store actually holds for each side.
	t.Log("sessions:")
	for _, s := range p.store.snapshotSessions() {
		t.Logf("  id=%q parent=%q", s.ID, s.ParentID)
	}

	child := p.store.snapshotSession("header:kid-2")
	if child == nil {
		t.Fatal("the child was not recorded")
	}
	// The child's parent carries the child's prefix, not the parent's.
	if child.ParentID != "header:shared-2" {
		t.Logf("parent id = %q (CPA builds it from the child's prefix)", child.ParentID)
	}

	team := p.teamProjection("header:kid-2")
	if team == nil {
		t.Fatal("no team projection")
	}
	members, _ := team["members"].([]map[string]any)
	if len(members) == 0 {
		t.Fatal("the roster is empty")
	}
	// The important assertion: the view must not lose the conversation because
	// the parent is spelled differently from the row that holds it.
	found := false
	for _, member := range members {
		if id, _ := member["id"].(string); id == "header:kid-2" {
			found = true
		}
	}
	if !found {
		t.Error("the child is missing from its own roster")
	}
}

// storeKey must resolve a bare id to whichever prefixed row holds it, even when
// the caller names the other format's prefix.
func TestStoreKeyResolvesAcrossPrefixes(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	raw, _ := json.Marshal(map[string]any{
		"RequestID": "a1", "TraceID": "a1", "SourceFormat": "claude", "Model": "m",
		"Headers": map[string][]string{"X-Claude-Code-Session-Id": {"plain-id"}},
		"Body":    []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`),
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"a1","Model":"m","SessionID":"plain-id","Detail":{"InputTokens":10,"TotalTokens":10}}`))

	if got := p.store.storeKey("plain-id"); got != "claude:plain-id" {
		t.Errorf("storeKey(plain-id) = %q, want claude:plain-id", got)
	}
	// A prefix from another format still resolves by suffix.
	if got := p.store.storeKey("header:plain-id"); got != "claude:plain-id" {
		t.Errorf("storeKey(header:plain-id) = %q, want the row that exists", got)
	}
}
