package main

import (
	"encoding/json"
	"testing"
)

// A streaming request reports usage the same way a buffered one does, but the
// interceptor sees Stream set. Both must land in the same conversation and keep
// the stream flag, so the dashboard can tell them apart.
func TestStreamingRequestIsRecorded(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	body := []byte(`{"model":"m","stream":true,"messages":[{"role":"user","content":"stream please"}]}`)
	intercept, _ := json.Marshal(map[string]any{
		"RequestID": "rs", "TraceID": "ts", "SourceFormat": "openai",
		"Model": "m", "Stream": true, "Body": body,
	})
	p.handle("request.intercept_before", intercept)
	usageRaw, _ := json.Marshal(map[string]any{
		"TraceID": "ts", "Model": "m", "SessionID": "s-stream", "Stream": true,
		"TTFT": int64(120 * 1000 * 1000),
		"Detail": map[string]any{"InputTokens": 200, "OutputTokens": 50, "TotalTokens": 250},
	})
	p.handle("usage.handle", usageRaw)

	snaps := p.store.snapshotsFor("s-stream")
	if len(snaps) != 1 {
		t.Fatalf("stored snapshots = %d, want 1", len(snaps))
	}
	if !snaps[0].Stream {
		t.Error("the stream flag was lost between the interceptor and the usage record")
	}
	if snaps[0].TTFTMs != 120 {
		t.Errorf("ttft = %d ms, want 120", snaps[0].TTFTMs)
	}
	if snaps[0].InputTokens != 200 {
		t.Errorf("prompt = %d, want 200", snaps[0].InputTokens)
	}
}

// An empty store must answer every dashboard route without a panic: the first
// thing an operator sees after installing the plugin is this state.
func TestEmptyStoreAnswersEveryRoute(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	for _, route := range []string{"sessions", "overview", "events"} {
		payload, ok := p.handleClientRoute("/cpa-context/"+route, "GET", nil)
		if ok && payload == nil {
			t.Errorf("%s answered nothing", route)
		}
	}
	for _, probe := range []struct{ path, body string }{
		{routeDetail, `{"sessionId":"none"}`},
		{routeFacts, `{"sessionId":"none","parts":["head"]}`},
		{routeHistory, `{"sessionId":"none","part":"requests"}`},
		{routeDelta, `{"sessionId":"none"}`},
		{routeFleet, `{"sessionId":"none"}`},
		{routeOverview, `{"part":"summary"}`},
		{routeSkills, `{}`},
		{routeBalance, `{}`},
		{routeVersion, `{}`},
	} {
		payload, ok := p.handleClientRoute(probe.path, "POST", []byte(probe.body))
		if !ok {
			t.Errorf("%s was not matched", probe.path)
			continue
		}
		if payload == nil {
			t.Errorf("%s answered nothing on an empty store", probe.path)
		}
	}

	// The resource payload must build too, which is what the page renders.
	if overview := p.overview(); overview.Plugin != pluginID {
		t.Errorf("overview plugin = %q", overview.Plugin)
	}
	if len(p.store.snapshotSessions()) != 0 {
		t.Error("a fresh store reported conversations")
	}
}
