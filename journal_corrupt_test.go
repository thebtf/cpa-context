package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A journal written by an older build must not stop the plugin. The file is a
// cache of observation, so anything unreadable means "start empty", never a
// failure to serve traffic.
func TestCorruptJournalFallsBackToMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, stateFileName)
	if err := os.WriteFile(path, []byte("this is not a database"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.attachJournal(path)
	defer p.shutdown()

	status := p.journalStatus()
	if status["durable"] == true {
		t.Errorf("a corrupt file was reported as durable: %#v", status)
	}

	// The plugin must still record and serve.
	p.handle("usage.handle", []byte(`{"TraceID":"t1","Model":"m","SessionID":"s1","Detail":{"InputTokens":10,"TotalTokens":10}}`))
	if sess := p.store.snapshotSession("s1"); sess == nil || sess.Requests != 1 {
		t.Error("a corrupt journal prevented recording")
	}
}

// A table under the plugin's name but with a different shape, left by an older
// build or a downgrade, must be rebuilt rather than silently failing every
// insert. The check is done on the table itself, so the assertion is that a
// write lands afterwards.
func TestJournalRebuildsAForeignLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, stateFileName)

	first := openJournal(path)
	if first.db == nil {
		t.Skipf("journal unavailable: %s", first.broken)
	}
	for _, statement := range []string{
		`DROP TABLE IF EXISTS requests`,
		`DROP TABLE IF EXISTS events`,
		`CREATE TABLE requests (something_else TEXT)`,
		`CREATE TABLE events (something_else TEXT)`,
	} {
		if _, err := first.db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	first.close()

	second := openJournal(path)
	if second.broken != "" {
		t.Fatalf("opening a foreign layout failed: %s", second.broken)
	}
	second.appendRequest(&snapshot{TraceID: "probe", SessionID: "probe", StartedAt: timeNow(), InputTokens: 1})
	snaps, _ := second.replay()
	second.close()

	if len(snaps) != 1 {
		t.Fatalf("after rebuilding, the journal recorded %d requests, want 1", len(snaps))
	}
	if snaps[0].TraceID != "probe" {
		t.Errorf("recorded %q, want the probe", snaps[0].TraceID)
	}
}
