package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// A proxy runs for weeks. Every structure the plugin keeps must stop growing,
// or the process grows until the operator restarts it. This drives far more
// requests than every cap allows and checks that memory settles instead of
// climbing with the request count.
func TestSustainedTrafficSettles(t *testing.T) {
	if testing.Short() {
		t.Skip("sustained load is skipped in short mode")
	}

	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.store.limits.maxSessions = 20
	p.store.limits.maxRecent = 200

	body := []byte(`{"model":"m","messages":[{"role":"system","content":"be careful"},` +
		`{"role":"user","content":"a long enough question to allocate some text in the parser"}]}`)

	drive := func(round int) {
		for i := 0; i < 60; i++ {
			session := fmt.Sprintf("s-%d", i%25)
			trace := fmt.Sprintf("r%d-%d", round, i)
			intercept, _ := json.Marshal(map[string]any{
				"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
				"Model": "m", "Body": body,
			})
			p.handle("request.intercept_before", intercept)
			p.handle("usage.handle", []byte(
				`{"TraceID":"`+trace+`","Model":"m","SessionID":"`+session+`","Detail":{"InputTokens":500,"TotalTokens":520}}`))
			p.handle("request.intercept_after", []byte(`{"TraceID":"`+trace+`"}`))
			// The dashboard polls while all this happens.
			_ = p.overviewAnswer([]byte(`{"part":"summary"}`))
			_ = p.skillList()
			p.store.projectionFor(session)
		}
	}

	// Warm up so the first round's allocation is not counted as growth.
	for round := 0; round < 30; round++ {
		drive(round)
	}

	// Two equal windows measuring what the plugin actually retains: the prompt
	// text it holds and the number of records it keeps. Heap size is not used
	// here because the race detector and the coverage instrumentation both
	// perturb it, and a growing-but-bounded structure is exactly what this
	// checks. A structure that appends per request grows in the second window
	// the same way it grew in the first.
	rounds := 60
	retained := func() (bytes int, records int) {
		p.store.mu.RLock()
		defer p.store.mu.RUnlock()
		return p.store.itemBudgetLocked(), len(p.store.recent)
	}

	started := time.Now()
	beforeBytes, beforeRecords := retained()
	for round := 30; round < 30+rounds; round++ {
		drive(round)
	}
	middleBytes, middleRecords := retained()
	for round := 30 + rounds; round < 30+2*rounds; round++ {
		drive(round)
	}
	afterBytes, afterRecords := retained()

	requests := rounds * 2 * 60
	t.Logf("%d requests in %s: held text %d -> %d -> %d bytes, records %d -> %d -> %d, sessions=%d pending=%d projections=%d",
		requests, time.Since(started).Round(time.Millisecond),
		beforeBytes, middleBytes, afterBytes,
		beforeRecords, middleRecords, afterRecords,
		len(p.store.sessions), len(p.store.pending), len(p.store.projections))

	// The first window reaches the cap; the second must stay at it.
	if afterBytes > middleBytes {
		t.Errorf("held prompt text grew from %d to %d bytes after the cap was reached",
			middleBytes, afterBytes)
	}
	if afterRecords != middleRecords {
		t.Errorf("records went from %d to %d: the recent window is not bounded", middleRecords, afterRecords)
	}
	if afterBytes > 16<<20 {
		t.Errorf("held prompt text is %d bytes, above the item budget", afterBytes)
	}
	if len(p.store.sessions) > 20 {
		t.Errorf("sessions = %d, the cap is 20", len(p.store.sessions))
	}
	if len(p.store.recent) > 200 {
		t.Errorf("recent = %d, the cap is 200", len(p.store.recent))
	}
	if len(p.store.pending) > 0 {
		t.Errorf("pending = %d, every request was closed", len(p.store.pending))
	}
}
