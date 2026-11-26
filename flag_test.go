package main

import (
	"encoding/json"
	"testing"
)

// record_bodies: false keeps the counters and the split but drops the text, which
// is what the README promises.
func TestRecordBodiesOffKeepsCounters(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.cfg.RecordBodies = false

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hello there"}]}`)
	intercept, _ := json.Marshal(map[string]any{
		"RequestID": "r", "TraceID": "t", "SourceFormat": "openai", "Model": "m", "Body": body,
	})
	p.handle("request.intercept_before", intercept)
	usageRaw, _ := json.Marshal(map[string]any{
		"TraceID": "t", "Model": "m", "SessionID": "s-flag",
		"Detail": map[string]any{"InputTokens": 50, "TotalTokens": 50},
	})
	p.handle("usage.handle", usageRaw)

	snaps := p.store.snapshotsFor("s-flag")
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(snaps))
	}
	if len(snaps[0].Items) != 0 {
		t.Errorf("items = %d, record_bodies is off", len(snaps[0].Items))
	}
	if snaps[0].InputTokens != 50 {
		t.Errorf("prompt = %d, want the counter kept", snaps[0].InputTokens)
	}
	if len(snaps[0].Composition) == 0 {
		t.Error("the split was dropped along with the text")
	}
}
