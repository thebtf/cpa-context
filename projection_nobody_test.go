package main

import (
	"encoding/json"
	"testing"
)

// With record_bodies off no prompt text is kept, so the projection has to rebuild
// the window and the chart from the composition. An empty chart here means the
// operator who turned bodies off sees a dashboard that does nothing.
func TestProjectionWorksWithoutPromptText(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.cfg.RecordBodies = false

	body := `{"model":"m","messages":[{"role":"system","content":"be careful"},{"role":"user","content":"a question about the parser"}]}`
	for i := 0; i < 3; i++ {
		trace := "t" + string(rune('0'+i))
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": "r" + string(rune('0'+i)), "TraceID": trace,
			"SourceFormat": "openai", "Model": "m", "Body": []byte(body),
		})
		p.handle("request.intercept_before", intercept)
		p.handle("usage.handle", []byte(
			`{"TraceID":"`+trace+`","Model":"m","SessionID":"s-nobody","Detail":{"InputTokens":400,"TotalTokens":400}}`))
	}

	built, ok := p.store.projectionFor("s-nobody")
	if !ok {
		t.Fatal("projection is missing")
	}
	timeline := built.ContextTimeline
	if len(timeline.Nodes) == 0 {
		t.Error("no nodes: the browser would be empty with bodies off")
	}
	if timeline.Current.System == 0 {
		t.Error("the system prompt vanished from the window")
	}
	if timeline.Current.User == 0 {
		t.Error("the user message vanished from the window")
	}
	if len(timeline.Requests) != 3 {
		t.Errorf("requests = %d, want one per call", len(timeline.Requests))
	}
	for _, record := range timeline.Requests {
		if len(record.Chart) == 0 {
			t.Errorf("request %d has no split, its bar would be empty", record.Seq)
		}
	}
}
