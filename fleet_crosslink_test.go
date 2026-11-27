package main

import (
	"encoding/json"
	"testing"
)

// The end state that matters: a parent that arrived over Claude and a child that
// names it over a generic format must appear as one family, not as two unrelated
// conversations. This is the cross-provider case, and it is the one that silently
// broke before: the two rows exist under different prefixes for the same session.
func TestAFamilyLinksAcrossFormats(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	send := func(id, parent, format, body string) {
		headers := map[string][]string{}
		if format == "claude" {
			headers["X-Claude-Code-Session-Id"] = []string{id}
		} else {
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

	send("family-root", "", "claude", "plan the work")
	send("family-kid", "family-root", "openai", "do one slice")

	// The child points at header:family-root, while the parent row is
	// claude:family-root. Asking for the family from the parent must find the
	// child, and asking from the child must reach the parent.
	fromParent := p.teamProjection("header:family-root")
	if fromParent == nil {
		t.Fatal("no family from the parent side")
	}
	parentMembers, _ := fromParent["members"].([]map[string]any)
	if len(parentMembers) < 2 {
		t.Errorf("%d members from the parent side, want the parent and its child", len(parentMembers))
	}

	fromChild := p.teamProjection("header:family-kid")
	if fromChild == nil {
		t.Fatal("no family from the child side")
	}
	childMembers, _ := fromChild["members"].([]map[string]any)
	if len(childMembers) < 2 {
		t.Errorf("%d members from the child side, want both", len(childMembers))
	}

	// Both sides must describe the same family, whatever order they are in.
	if len(parentMembers) != len(childMembers) {
		t.Errorf("the two sides disagree: %d vs %d members", len(parentMembers), len(childMembers))
	}
}
