package main

import (
	"testing"
)

// The host shuts a plugin down and may start it again in the same process. After
// a shutdown the plugin must not answer with the state of the previous run, and
// a second shutdown must not panic on an already-closed journal.
func TestShutdownIsIdempotentAndClearsState(t *testing.T) {
	dir := t.TempDir()
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.attachJournal(dir + "/" + stateFileName)

	p.handle("usage.handle", []byte(
		`{"TraceID":"t1","Model":"m","SessionID":"s-shut","Detail":{"InputTokens":10,"TotalTokens":10}}`))
	if p.store.snapshotSession("s-shut") == nil {
		t.Fatal("the request was not recorded")
	}

	p.handle("plugin.shutdown", nil)

	if p.store.snapshotSession("s-shut") != nil {
		t.Error("shutdown left the previous run's conversation in memory")
	}
	if p.store.journalOf() != nil {
		t.Error("shutdown left the journal attached")
	}

	// A second shutdown arrives when the host tears the process down twice.
	p.handle("plugin.shutdown", nil)

	// And a configure after shutdown opens a journal again rather than writing
	// to the closed handle.
	p.cfg.Enabled = true
	if err := p.configure([]byte(`{"config_yaml":"` + base64YAML("enabled: true\ndata_dir: "+dir+"\n") + `"}`)); err != nil {
		t.Fatalf("configure after shutdown: %v", err)
	}
	if p.store.journalOf() == nil {
		t.Error("configure after shutdown did not reopen the journal")
	}
	p.shutdown()
}

// A configure that only changes the limits must not close and reopen the journal,
// because the reopen replays the file and would duplicate what is in memory.
func TestReconfigureWithTheSameDirectoryKeepsOneJournal(t *testing.T) {
	dir := t.TempDir()
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.attachJournal(dir + "/" + stateFileName)
	defer p.shutdown()

	p.handle("usage.handle", []byte(
		`{"TraceID":"t1","Model":"m","SessionID":"s-keep","Detail":{"InputTokens":10,"TotalTokens":10}}`))
	before := p.store.journalOf()

	if err := p.configure([]byte(`{"config_yaml":"` + base64YAML("enabled: true\nmax_sessions: 7\ndata_dir: "+dir+"\n") + `"}`)); err != nil {
		t.Fatalf("configure: %v", err)
	}

	if after := p.store.journalOf(); after != before {
		t.Error("a limit change replaced the journal handle")
	}
	if p.store.snapshotSession("s-keep") == nil {
		t.Error("a limit change dropped the conversation")
	}
	if got := p.store.limits.maxSessions; got != 7 {
		t.Errorf("maxSessions = %d, want 7", got)
	}
}
