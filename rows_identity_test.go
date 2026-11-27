package main

import (
	"encoding/json"
	"testing"
)

// The fleet names an agent by its own label and decides what a conversation is
// from the two flags. Without them every row is an anonymous root, which is what
// made the view say "one agent" on traffic full of delegated work.
func TestSessionRowsCarryTheAgentIdentity(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	raw, _ := json.Marshal(map[string]any{
		"RequestID": "c1", "TraceID": "c1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{
			"X-Session-Id":        {"worker-1"},
			"X-Parent-Session-ID": {"lead-1"},
			"X-Agent-Id":          {"researcher"},
		},
		"Body": []byte(`{"model":"m","messages":[{"role":"user","content":"look into the parser"}]}`),
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"c1","Model":"m","SessionID":"worker-1","ParentID":"lead-1","Detail":{"InputTokens":10,"TotalTokens":10}}`))

	rows := sessionRows(p.store.snapshotSessions())
	if len(rows) != 1 {
		t.Fatalf("%d rows, want one", len(rows))
	}
	row := rows[0]

	if name, _ := row["agentName"].(string); name != "researcher" {
		t.Errorf("agentName = %q, want researcher: the fleet names an agent by it", row["agentName"])
	}
	if client, _ := row["clientType"].(string); client == "" {
		t.Error("clientType is missing from the row")
	}
	if sub, _ := row["isSubagent"].(bool); !sub {
		t.Error("isSubagent is missing: a child reads as a root without it")
	}
	if parent, _ := row["parentId"].(string); parent == "" {
		t.Error("parentId is missing from the row")
	}
}

// A root conversation carries no subagent flag and is reported as the root it is.
func TestSessionRowsMarkARootAsRoot(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	raw, _ := json.Marshal(map[string]any{
		"RequestID": "r1", "TraceID": "r1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{"X-Session-Id": {"root-1"}},
		"Body":    []byte(`{"model":"m","messages":[{"role":"user","content":"plan"}]}`),
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"r1","Model":"m","SessionID":"root-1","Detail":{"InputTokens":10,"TotalTokens":10}}`))

	row := sessionRows(p.store.snapshotSessions())[0]
	if origin, _ := row["origin"].(string); origin != "root" {
		t.Errorf("origin = %q, want root", origin)
	}
	if _, present := row["isSubagent"]; present {
		t.Error("a root carries the subagent flag")
	}
}
