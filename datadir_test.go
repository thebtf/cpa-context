package main

import (
	"strings"
	"testing"
)

// Changing data_dir must not lose what was already observed: the operator moves
// the directory, and the running process keeps its picture while writing the new
// file.
func TestChangingDataDirKeepsTheObservation(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.attachJournal(first + "/" + stateFileName)
	defer p.shutdown()

	p.handle("usage.handle", []byte(
		`{"TraceID":"t1","Model":"m","SessionID":"s-dir","Detail":{"InputTokens":10,"TotalTokens":10}}`))
	if sess := p.store.snapshotSession("s-dir"); sess == nil {
		t.Fatal("the request was not recorded")
	}

	raw := []byte(`{"config_yaml":"` + base64YAML("enabled: true\ndata_dir: "+second+"\n") + `"}`)
	p.handle("plugin.reconfigure", raw)

	if sess := p.store.snapshotSession("s-dir"); sess == nil {
		t.Error("moving the directory dropped the conversation from memory")
	}
	opened := p.store.journalOf()
	if opened == nil || opened.path == "" {
		t.Fatal("the journal was not reopened on the new directory")
	}
	if !strings.HasPrefix(opened.path, second) {
		t.Errorf("the journal points at %q, want the new directory", opened.path)
	}
}
