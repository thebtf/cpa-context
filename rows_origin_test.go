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
