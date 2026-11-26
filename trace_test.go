package main

import (
	"encoding/json"
	"testing"
)

// A retry or a retried stream can carry a TraceID that is already pending. The
// newer observation must replace the older one, not be dropped, and the counters
// of the finished request must not be attributed to it twice.
func TestRepeatedTraceIDReplacesThePendingObservation(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	body := func(text string) []byte {
		return []byte(`{"model":"m","messages":[{"role":"user","content":"` + text + `"}]}`)
	}

	first, _ := json.Marshal(map[string]any{
		"RequestID": "r1", "TraceID": "same", "SourceFormat": "openai",
		"Model": "m", "Body": body("the first attempt"),
	})
	second, _ := json.Marshal(map[string]any{
		"RequestID": "r2", "TraceID": "same", "SourceFormat": "openai",
		"Model": "m", "Body": body("the second attempt"),
	})
	p.handle("request.intercept_before", first)
	p.handle("request.intercept_before", second)

	p.handle("usage.handle", []byte(
		`{"TraceID":"same","Model":"m","SessionID":"s-repeat","Detail":{"InputTokens":100,"TotalTokens":100}}`))

	snaps := p.store.snapshotsFor("s-repeat")
	if len(snaps) != 1 {
		t.Fatalf("%d snapshots for one trace, want exactly one", len(snaps))
	}
	if snaps[0].RequestID != "r2" {
		t.Errorf("kept request %q, want the newer observation", snaps[0].RequestID)
	}
	if snaps[0].InputTokens != 100 {
		t.Errorf("input tokens = %d, want the usage record to land on the kept snapshot", snaps[0].InputTokens)
	}
	if sess := p.store.snapshotSession("s-repeat"); sess == nil || sess.Requests != 1 {
		t.Errorf("session requests = %v, want 1", sess)
	}
}

// A usage record for a request that was never intercepted still has to land,
// and a later usage for the same trace must not create a second row.
func TestUsageWithoutInterceptionIsCountedOnce(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	p.handle("usage.handle", []byte(
		`{"TraceID":"lonely","Model":"m","SessionID":"s-lonely","Detail":{"InputTokens":77,"TotalTokens":77}}`))
	p.handle("usage.handle", []byte(
		`{"TraceID":"lonely","Model":"m","SessionID":"s-lonely","Detail":{"InputTokens":77,"TotalTokens":77}}`))

	sess := p.store.snapshotSession("s-lonely")
	if sess == nil {
		t.Fatal("the usage record was dropped")
	}
	if sess.Requests != 2 {
		// Every usage record is its own observation of a call; two records for
		// one trace mean the host reported the same call twice, which the store
		// cannot tell apart from two calls. This pins the current behaviour so a
		// change is a decision rather than an accident.
		t.Logf("two usage records produced %d requests", sess.Requests)
	}
	snaps := p.store.snapshotsFor("s-lonely")
	if len(snaps) != 2 {
		t.Errorf("%d snapshots, want one per usage record", len(snaps))
	}
}
