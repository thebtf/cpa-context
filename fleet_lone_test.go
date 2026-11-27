package main

import (
	"encoding/json"
	"testing"
)

// The shape measured on live traffic: 48 conversations named a parent, every one
// a different parent, and none of those parents ever passed through this proxy.
// Each is still a subagent, and calling it the lead would relabel every delegated
// run as the main conversation.
func TestALoneChildIsStillASubagent(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	raw, _ := json.Marshal(map[string]any{
		"RequestID": "only", "TraceID": "only", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{
			"X-Session-Id":        {"only-child"},
			"X-Parent-Session-ID": {"parent-never-seen"},
			"X-Agent-Id":          {"worker"},
		},
		"Body": []byte(`{"model":"m","messages":[{"role":"user","content":"do one slice"}]}`),
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"only","Model":"m","SessionID":"only-child","ParentID":"parent-never-seen","Detail":{"InputTokens":40,"TotalTokens":40}}`))

	team := p.teamProjection("header:only-child")
	if team == nil {
		t.Fatal("no team projection")
	}
	members, _ := team["members"].([]map[string]any)
	if len(members) != 1 {
		t.Fatalf("%d members, want the one conversation", len(members))
	}
	if role, _ := members[0]["role"].(string); role != "teammate" {
		t.Errorf("role = %q, want teammate: the conversation named a parent", role)
	}
	if parent, _ := members[0]["parentId"].(string); parent == "" {
		t.Error("the member carries no parentId, so the client cannot group it")
	}
}

// A root conversation is the lead.
func TestARootIsTheLead(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	raw, _ := json.Marshal(map[string]any{
		"RequestID": "root", "TraceID": "root", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{"X-Session-Id": {"root-one"}},
		"Body":    []byte(`{"model":"m","messages":[{"role":"user","content":"plan"}]}`),
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"root","Model":"m","SessionID":"root-one","Detail":{"InputTokens":40,"TotalTokens":40}}`))

	team := p.teamProjection("header:root-one")
	members, _ := team["members"].([]map[string]any)
	if len(members) != 1 {
		t.Fatalf("%d members, want one", len(members))
	}
	if role, _ := members[0]["role"].(string); role != "lead" {
		t.Errorf("role = %q, want lead for a conversation with no parent", role)
	}
}
