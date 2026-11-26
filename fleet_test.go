package main

import (
	"encoding/json"
	"testing"
)

// The fleet view draws one member per agent and links a child to its parent. CPA
// resolves that link from the client's own headers, so a subagent that names its
// parent must appear in the roster; without the roster the view says there are
// none, whatever the traffic actually shows.
func TestFleetShowsASubagentUnderItsParent(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	// A parent request, then a child that names it.
	parent, _ := json.Marshal(map[string]any{
		"RequestID": "p1", "TraceID": "p1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{"X-Session-Id": {"lead-session"}},
		"Body":    []byte(`{"messages":[{"role":"user","content":"plan the work"}]}`),
	})
	p.handle("request.intercept_before", parent)
	p.handle("usage.handle", []byte(
		`{"TraceID":"p1","Model":"m","SessionID":"lead-session","Detail":{"InputTokens":100,"TotalTokens":100}}`))

	child, _ := json.Marshal(map[string]any{
		"RequestID": "c1", "TraceID": "c1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{
			"X-Session-Id":        {"child-session"},
			"X-Parent-Session-Id": {"lead-session"},
		},
		"Body": []byte(`{"messages":[{"role":"user","content":"do one slice"}]}`),
	})
	p.handle("request.intercept_before", child)
	p.handle("usage.handle", []byte(
		`{"TraceID":"c1","Model":"m","SessionID":"child-session","ParentID":"lead-session","Detail":{"InputTokens":80,"TotalTokens":80}}`))

	team := p.teamProjection("lead-session")
	if team == nil {
		t.Fatal("no team projection for the lead")
	}
	members, _ := team["members"].([]map[string]any)
	if len(members) < 2 {
		t.Fatalf("%d members, the subagent is missing from the roster", len(members))
	}
	var lead, teammate bool
	for _, member := range members {
		switch member["role"] {
		case "lead":
			lead = true
		case "teammate":
			teammate = true
		}
	}
	if !lead {
		t.Error("the roster has no lead")
	}
	if !teammate {
		t.Error("the roster has no teammate, so the child is drawn as another root")
	}
}

// A delegation a prompt shows is named before the child calls back.
func TestFleetCatalogListsADelegation(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"spawn a worker"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"t1","type":"function",` +
		`"function":{"name":"task","arguments":"{\"description\":\"summarize the docs\"}"}}]}],` +
		`"tools":[{"type":"function","function":{"name":"task","parameters":{"type":"object"}}}]}`)
	raw, _ := json.Marshal(map[string]any{
		"RequestID": "a1", "TraceID": "a1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{"X-Session-Id": {"lead-2"}},
		"Body":    body,
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"a1","Model":"m","SessionID":"lead-2","Detail":{"InputTokens":90,"TotalTokens":90}}`))

	catalog := p.subagentCatalog("lead-2")
	if len(catalog) == 0 {
		t.Fatal("the delegation the prompt shows is not in the catalog")
	}
	if name, _ := catalog[0]["name"].(string); name == "" {
		t.Error("the catalog entry has no name")
	}
}
