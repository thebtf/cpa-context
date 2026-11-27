package main

import (
	"path/filepath"
	"testing"
)

// A journal outlives the code that wrote it. A title stored before the rules
// learned to skip wrappers and identifiers is "m45881", and replaying it would
// show that forever, because the prompt text needed to derive a better one is not
// stored.
func TestReplayDropsTitlesThatAreNotNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, stateFileName)

	j := openJournal(path)
	j.appendRequest(&snapshot{
		TraceID: "t1", SessionID: "s-bad", StartedAt: timeNow(),
		Model: "m", Title: "m45881", InputTokens: 10,
	})
	j.appendRequest(&snapshot{
		TraceID: "t2", SessionID: "s-good", StartedAt: timeNow(),
		Model: "m", Title: "Investigate why the chart bars are flat.",
		InputTokens: 10,
	})
	j.close()

	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.attachJournal(path)
	defer p.shutdown()

	bad := p.store.snapshotSession("s-bad")
	if bad == nil {
		t.Fatal("the conversation with an identifier title was not replayed at all")
	}
	if bad.Title != "" {
		t.Errorf("replayed title = %q, want it dropped so a real one can replace it", bad.Title)
	}

	good := p.store.snapshotSession("s-good")
	if good == nil {
		t.Fatal("the conversation with a real title was not replayed")
	}
	if good.Title != "Investigate why the chart bars are flat." {
		t.Errorf("replayed title = %q, want the stored name", good.Title)
	}
}
