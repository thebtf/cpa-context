package main

import (
	"encoding/json"
	"testing"
)

// The client asks for history through its own routes when the timeline is
// scrolled. After the prompt text of older requests is dropped, those routes
// must still answer: the chart and the counts come from counters, and only the
// expandable text is gone. An empty page here shows a timeline that stops at the
// newest step.
func TestHistoryRouteStillAnswersAfterTextIsDropped(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	for i := 0; i < 6; i++ {
		body, _ := json.Marshal(map[string]any{
			"model": "m",
			"messages": []map[string]any{
				{"role": "user", "content": repeatTextForHistory(i)},
			},
		})
		trace := "h" + string(rune('0'+i))
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
			"Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", intercept)
		usageRaw, _ := json.Marshal(map[string]any{
			"TraceID": trace, "Model": "m", "SessionID": "s-history",
			"Detail": map[string]any{"InputTokens": 3000 + i*50, "TotalTokens": 3000 + i*50},
		})
		p.handle("usage.handle", usageRaw)
	}

	for _, part := range []string{"requests", "events", "nodes"} {
		payload, ok := p.handleClientRoute(routeHistory, "POST",
			[]byte(`{"sessionId":"s-history","part":"`+part+`"}`))
		if !ok {
			t.Fatalf("history route did not match for %q", part)
		}
		envelope, _ := payload.(map[string]any)
		if envelope["ok"] != true {
			t.Fatalf("history for %q answered %v", part, envelope)
		}
		page, _ := envelope["value"].(map[string]any)
		if total, _ := page["total"].(int); total == 0 && part == "requests" {
			t.Errorf("history for %q is empty, want every request", part)
		}
	}

	// The detail route must still carry the series the chart draws.
	payload, _ := p.handleClientRoute(routeDetail, "POST", []byte(`{"sessionId":"s-history"}`))
	envelope, _ := payload.(map[string]any)
	detail, _ := envelope["value"].(map[string]any)
	requests, _ := detail["requests"].([]map[string]any)
	if len(requests) != 6 {
		t.Fatalf("detail carries %d requests, want 6", len(requests))
	}
	for _, record := range requests {
		composition, _ := record["composition"].(map[string]int)
		if len(composition) == 0 {
			t.Errorf("request %v has no split, its bar would be empty", record["seq"])
		}
	}
}

func repeatTextForHistory(n int) string {
	out := ""
	for i := 0; i < 60+n; i++ {
		out += "context line "
	}
	return out
}
