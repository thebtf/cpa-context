package main

import (
	"encoding/json"
	"testing"
)

// A subagent can land on a different provider than the one its parent used, and
// the two then arrive over different wire formats. CPA builds a parent id from the
// child's own prefix, so a parent that came in over Claude is not "claude:..." when
// a generic-header child names it: the child writes "header:<parent>".
//
// The plugin must survive that: no crash, a family drawn from whatever the child
// named, and a parent link that does not pretend to resolve.
func TestAFamilyAcrossTwoWireFormats(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	// The parent arrives as an Anthropic message: CPA names it claude:<id>.
	parentBody := []byte(`{"model":"m","messages":[{"role":"user","content":"plan the work"}]}`)
	parentRaw, _ := json.Marshal(map[string]any{
		"RequestID": "p1", "TraceID": "p1", "SourceFormat": "claude", "Model": "m",
		"Headers": map[string][]string{"X-Claude-Code-Session-Id": {"shared-1"}},
		"Body":    parentBody,
	})
	p.handle("request.intercept_before", parentRaw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"p1","Model":"m","SessionID":"claude:shared-1","Detail":{"InputTokens":50,"TotalTokens":50}}`))

	// The child lands on an OpenAI-compatible provider and names the same parent.
	childRaw, _ := json.Marshal(map[string]any{
		"RequestID": "c1", "TraceID": "c1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{
			"X-Session-Id":        {"child-1"},
			"X-Parent-Session-ID": {"shared-1"},
		},
		"Body": []byte(`{"model":"m","messages":[{"role":"user","content":"do one slice"}]}`),
	})
	p.handle("request.intercept_before", childRaw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"c1","Model":"m","SessionID":"child-1","ParentID":"shared-1","Detail":{"InputTokens":30,"TotalTokens":30}}`))

	// Both conversations exist; the child names a parent under its own prefix.
	if sess := p.store.snapshotSession("claude:shared-1"); sess == nil {
		t.Error("the parent conversation was lost")
	}
	child := p.store.snapshotSession("header:child-1")
	if child == nil {
		t.Fatal("the child conversation was lost")
	}
	if child.ParentID == "" {
		t.Error("the child lost its parent link")
	}

	// The family is served without a panic, and the child is in it.
	team := p.teamProjection("header:child-1")
	if team == nil {
		t.Fatal("no team projection for a cross-format child")
	}
	members, _ := team["members"].([]map[string]any)
	if len(members) == 0 {
		t.Fatal("the roster is empty")
	}
	for _, member := range members {
		if name, _ := member["name"].(string); name == "" {
			t.Error("a member has no name")
		}
	}
}

// A parent id written under a prefix that no conversation uses must not break the
// view: the family is the child alone, and it says so.
func TestAParentUnderAnotherPrefixIsNotInvented(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	raw, _ := json.Marshal(map[string]any{
		"RequestID": "x1", "TraceID": "x1", "SourceFormat": "openai", "Model": "m",
		"Headers": map[string][]string{
			"X-Session-Id":        {"orphan"},
			"X-Parent-Session-ID": {"claude:never-arrived"},
		},
		"Body": []byte(`{"model":"m","messages":[{"role":"user","content":"work"}]}`),
	})
	p.handle("request.intercept_before", raw)
	p.handle("usage.handle", []byte(
		`{"TraceID":"x1","Model":"m","SessionID":"orphan","ParentID":"claude:never-arrived","Detail":{"InputTokens":10,"TotalTokens":10}}`))

	team := p.teamProjection("header:orphan")
	if team == nil {
		t.Fatal("no team projection for an orphan")
	}
	members, _ := team["members"].([]map[string]any)
	if len(members) != 1 {
		t.Fatalf("%d members, want the one conversation", len(members))
	}
	// The roster must not claim the absent parent as a member: it never ran.
	for _, member := range members {
		if id, _ := member["id"].(string); id == "claude:never-arrived" {
			t.Error("the roster invented a parent that never ran")
		}
	}
}
