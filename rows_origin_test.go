package main

import (
	"path/filepath"
	"testing"
)

// A conversation CPA derived by branching a prompt has an lcp id and a parent
// computed from the divergence point. The list has to say so, or the fleet draws
// the pair as a delegation that never happened.
func TestRowOriginRecognisesADerivedFork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, stateFileName)

	j := openJournal(path)
	j.appendRequest(&snapshot{
		TraceID: "f1", SessionID: "lcp:v1:129063e1b23", ParentSession: "lcp:v1:79010f1c988",
		StartedAt: timeNow(), Model: "m", InputTokens: 10,
	})
	j.appendRequest(&snapshot{
		TraceID: "r1", SessionID: "header:root", StartedAt: timeNow(), Model: "m", InputTokens: 10,
	})
	j.close()

	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.attachJournal(path)
	defer p.shutdown()

	byID := map[string]map[string]any{}
	for _, row := range sessionRows(p.store.snapshotSessions()) {
		id, _ := row["id"].(string)
		byID[id] = row
	}

	fork, ok := byID["lcp:v1:129063e1b23"]
	if !ok {
		t.Fatal("the fork row was not listed")
	}
	if origin, _ := fork["origin"].(string); origin != "fork" {
		t.Errorf("origin = %q, want fork: the id is CPA's derived-fork scheme", origin)
	}

	root, ok := byID["header:root"]
	if !ok {
		t.Fatal("the root row was not listed")
	}
	if origin, _ := root["origin"].(string); origin != "root" {
		t.Errorf("origin = %q, want root", origin)
	}
}

func TestClaudeDelegationOnJournalReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateFileName)
	j := openJournal(path)
	j.appendRequest(&snapshot{
		TraceID: "child-request", SessionID: "claude:child", ParentSession: "claude:parent",
		ClientType: "claude", AgentName: "subagent", StartedAt: timeNow(), Model: "m",
	})
	j.close()
	p := newPlugin(nil)
	p.attachJournal(path)
	defer p.shutdown()
	rows := sessionRows(p.store.snapshotSessions())
	if len(rows) != 1 || rows[0]["origin"] != "subagent" || rows[0]["isSubagent"] != true || rows[0]["parentId"] != "claude:parent" {
		t.Fatalf("replayed child: %v", rows)
	}
}

func TestClaudeDelegationRequiresAnExplicitSignal(t *testing.T) {
	for _, tc := range []struct {
		name string
		change func(*snapshot)
		want string
	}{
		{"delegation", func(*snapshot) {}, "subagent"},
		{"missing parent", func(s *snapshot) { s.ParentSession = "" }, "root"},
		{"self parent", func(s *snapshot) { s.ParentSession = s.SessionID }, "root"},
		{"compaction", func(s *snapshot) { s.Compaction = true }, "root"},
		{"explicit fork", func(s *snapshot) { s.IsFork = true }, "fork"},
		{"derived fork", func(s *snapshot) { s.SessionID = "lcp:v1:abcdef" }, "fork"},
		{"other client", func(s *snapshot) { s.ClientType = "generic" }, "root"},
		{"no agent role", func(s *snapshot) { s.AgentName = "main" }, "root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := &snapshot{SessionID: "claude:kid", ParentSession: "claude:lead", ClientType: "claude", AgentName: "subagent"}
			tc.change(snap)
			s := newStore(defaultLimits())
			s.applyToSessionLocked(snap)
			row := sessionRows(s.snapshotSessions())[0]
			if row["origin"] != tc.want || (row["isSubagent"] == true) != (tc.want == "subagent") {
				t.Errorf("inconsistent origin/flag: %v", row)
			}
		})
	}
}
