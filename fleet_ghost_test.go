package main

import (
	"encoding/json"
	"testing"
)

// The ordinary shape of a delegated family: the lead issues no request of its
// own, so the store never creates a session for it, and only its children's
// ParentID says it exists. The fleet must still draw the family, with the lead
// as lead and the children as teammates.
func TestFamilyWithoutALeadRequestKeepsItsMembers(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	child := func(id, parent, content string) {
		body := []byte(`{"model":"m","messages":[{"role":"user","content":"` + content + `"}]}`)
		raw, _ := json.Marshal(map[string]any{
			"RequestID": id, "TraceID": id, "SourceFormat": "openai", "Model": "m",
			"Headers": map[string][]string{
				"X-Session-Id":        {id},
				"X-Parent-Session-ID": {parent},
			},
			"Body": body,
		})
		p.handle("request.intercept_before", raw)
		p.handle("usage.handle", []byte(
			`{"TraceID":"`+id+`","Model":"m","SessionID":"`+id+`","ParentID":"`+parent+`","Detail":{"InputTokens":40,"TotalTokens":40}}`))
	}
	child("kid-a", "lead-ghost", "slice one")
	child("kid-b", "lead-ghost", "slice two")

	team := p.teamProjection("header:kid-a")
	if team == nil {
		t.Fatal("no team projection: the family was lost with its absent lead")
	}
	members, _ := team["members"].([]map[string]any)
	if len(members) != 2 {
		t.Fatalf("%d members, want the lead and its two children", len(members))
	}

	// Every member here named the same absent parent, so every member is a
	// teammate. The family is still drawn together: the siblings are the two rows,
	// and the tree has a top, but that top is not a lead it never was.
	leads, teammates := 0, 0
	for _, member := range members {
		switch member["role"] {
		case "lead":
			leads++
		case "teammate":
			teammates++
		}
	}
	if leads != 0 {
		t.Errorf("%d leads, want none: every member named a parent", leads)
	}
	if teammates != 2 {
		t.Errorf("%d teammates, want both siblings", teammates)
	}
}

// A family whose lead does have requests keeps working the way it did.
func TestFamilyWithALiveLeadIsUnchanged(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	send := func(id, parent, content string) {
		headers := map[string][]string{"X-Session-Id": {id}}
		if parent != "" {
			headers["X-Parent-Session-ID"] = []string{parent}
		}
		raw, _ := json.Marshal(map[string]any{
			"RequestID": id, "TraceID": id, "SourceFormat": "openai", "Model": "m",
			"Headers": headers,
			"Body":    []byte(`{"model":"m","messages":[{"role":"user","content":"` + content + `"}]}`),
		})
		p.handle("request.intercept_before", raw)
		p.handle("usage.handle", []byte(
			`{"TraceID":"`+id+`","Model":"m","SessionID":"`+id+`","ParentID":"`+parent+`","Detail":{"InputTokens":40,"TotalTokens":40}}`))
	}
	send("lead-live", "", "plan the work")
	send("kid-c", "lead-live", "do one slice")

	team := p.teamProjection("header:kid-c")
	members, _ := team["members"].([]map[string]any)
	if len(members) != 2 {
		t.Fatalf("%d members, want the live lead and its child", len(members))
	}
	roles := map[string]string{}
	for _, member := range members {
		roles[member["id"].(string)] = member["role"].(string)
	}
	if roles["header:lead-live"] != "lead" {
		t.Errorf("the live lead has role %q, want lead", roles["header:lead-live"])
	}
}
