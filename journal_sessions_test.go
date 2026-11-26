package main

import (
	"encoding/json"
	"testing"
)

// A request with no session identity must stay its own conversation across a
// restart. The in-memory path keys such a request by its trace id; the journal
// must store that same key, or every unidentified request rejoins into one
// bucket after a restart and the dashboard shows a conversation that never
// existed.
func TestUnsourcedRequestsSurviveRestartSeparately(t *testing.T) {
	dir := t.TempDir()

	first := newPlugin(nil)
	first.cfg.Enabled = true
	first.attachJournal(testStatePath(dir))
	for _, trace := range []string{"u1", "u2", "u3"} {
		usageRaw, _ := json.Marshal(map[string]any{
			"TraceID": trace, "Model": "m",
			"Detail": map[string]any{"InputTokens": 500, "TotalTokens": 500},
		})
		first.handle("usage.handle", usageRaw)
	}
	before := len(first.store.snapshotSessions())
	if before != 3 {
		t.Fatalf("in memory: %d conversations, want one per unidentified request", before)
	}
	first.shutdown()

	second := newPlugin(nil)
	second.attachJournal(testStatePath(dir))
	defer second.shutdown()
	after := second.store.snapshotSessions()
	if len(after) != before {
		t.Errorf("after restart: %d conversations, want %d", len(after), before)
	}
	for _, sess := range after {
		if sess.Requests != 1 {
			t.Errorf("conversation %q has %d requests after restart, want its own one", sess.ID, sess.Requests)
		}
		if !sess.Unsourced {
			t.Errorf("conversation %q lost its unsourced mark", sess.ID)
		}
	}
}

// testStatePath keeps the journal file in one place for the tests in this file.
func testStatePath(dir string) string {
	return dir + string(pathSeparator()) + stateFileName
}

func pathSeparator() rune {
	return '/'
}
