package main

import (
	"os"
	"testing"
)

// Shutting down must not erase what the journal holds. The whole point of the
// file is that the next start reads it back.
func TestShutdownKeepsTheJournalContents(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/" + stateFileName

	first := newPlugin(nil)
	first.cfg.Enabled = true
	first.attachJournal(path)
	first.handle("usage.handle", []byte(
		`{"TraceID":"t1","Model":"m","SessionID":"s-durable","Detail":{"InputTokens":42,"TotalTokens":42}}`))

	// The journal is written through the store's lock, so the row must be on
	// disk before the shutdown rather than only in memory.
	j := first.store.journalOf()
	if j == nil {
		t.Fatal("no journal was opened")
	}
	snaps, _ := j.replay()
	if len(snaps) != 1 {
		t.Fatalf("the journal holds %d rows before shutdown, want 1", len(snaps))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the journal file is missing: %v", err)
	}

	first.handle("plugin.shutdown", nil)

	// The call has to have taken effect, or the rest of the test proves nothing
	// about what a shutdown leaves behind.
	if first.store.journalOf() != nil {
		t.Fatal("shutdown did not detach the journal, the handle was never called")
	}

	second := newPlugin(nil)
	second.cfg.Enabled = true
	second.attachJournal(path)
	defer second.shutdown()

	sess := second.store.snapshotSession("s-durable")
	if sess == nil {
		t.Fatal("a shutdown erased the journal: the next start found nothing")
	}
	if sess.Requests != 1 {
		t.Errorf("requests = %d, want 1", sess.Requests)
	}
}
