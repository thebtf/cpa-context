package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// A request whose StartedAt was never set has a zero time, and a zero time is
// before any cutoff, so a full pending map would drop exactly the newest
// requests and their usage record would arrive with nothing to attach to.
func TestPrunePendingKeepsRequestsWithoutATime(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.store.limits.maxRecent = 5
	p.store.limits.maxPendingAge = time.Hour

	// One request that carries a time and is genuinely old.
	old, _ := json.Marshal(map[string]any{
		"RequestID": "old", "TraceID": "old", "SourceFormat": "openai",
		"Model": "m", "Body": []byte(`{"messages":[{"role":"user","content":"old"}]}`),
	})
	p.handle("request.intercept_before", old)
	p.store.mu.Lock()
	if snap, ok := p.store.pending["old"]; ok {
		snap.StartedAt = time.Now().Add(-2 * time.Hour)
	}
	p.store.mu.Unlock()

	// Enough fresh requests to push the pending map over its cap. None of them
	// sets a body time, which is the ordinary case.
	for i := 0; i < 10; i++ {
		trace := fmt.Sprintf("fresh-%d", i)
		raw, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
			"Model": "m", "Body": []byte(`{"messages":[{"role":"user","content":"fresh"}]}`),
		})
		p.handle("request.intercept_before", raw)
	}

	p.store.mu.Lock()
	_, hasOld := p.store.pending["old"]
	fresh := 0
	for key := range p.store.pending {
		if key != "old" {
			fresh++
		}
	}
	p.store.mu.Unlock()

	if hasOld {
		t.Error("the old request stayed in pending past its age")
	}
	if fresh == 0 {
		t.Error("every fresh request was dropped: a zero time sorts as oldest")
	}
	// The cap must hold even when nothing is old enough to go: a pending map
	// that only shrinks by age grows without bound under steady traffic.
	if fresh > 6 {
		t.Errorf("pending holds %d fresh requests with a cap of 5", fresh)
	}
}

// The usage record of a request whose pending entry was pruned must still land
// in its conversation rather than vanishing.
func TestUsageSurvivesAPrunedPendingEntry(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	// No interceptor ran: the store has no pending entry at all.
	p.handle("usage.handle", []byte(
		`{"TraceID":"nobody","Model":"m","SessionID":"s-pruned","Detail":{"InputTokens":5,"TotalTokens":5}}`))

	sess := p.store.snapshotSession("s-pruned")
	if sess == nil || sess.Requests != 1 {
		t.Fatalf("a usage record without an interceptor was lost: %+v", sess)
	}
}
