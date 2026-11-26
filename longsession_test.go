package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A conversation that runs long must still produce a bounded answer: the browser
// fetches it every five seconds, so a payload that grows with the number of
// requests makes the dashboard slower the longer the operator watches it.
func TestLongConversationStaysBounded(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"a question ` +
		strings.Repeat("x", 400) + `"}]}`)
	for i := 0; i < 600; i++ {
		trace := fmt.Sprintf("t-%d", i)
		raw, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
			"Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", raw)
		p.handle("usage.handle", []byte(
			`{"TraceID":"`+trace+`","Model":"m","SessionID":"s-long","Detail":{"InputTokens":900,"TotalTokens":900}}`))
	}

	built, ok := p.store.projectionFor("s-long")
	if !ok {
		t.Fatal("no projection for a long conversation")
	}
	raw, err := json.Marshal(built)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	headRaw, _ := json.Marshal(slimHead(built.ContextTimeline))
	t.Logf("600 requests: projection %d KiB, head %d KiB, %d request records",
		len(raw)/1024, len(headRaw)/1024, len(built.ContextTimeline.Requests))

	if len(headRaw) > 64<<10 {
		t.Errorf("the polled head is %d KiB, too large to fetch every few seconds", len(headRaw)/1024)
	}
	if len(built.ContextTimeline.Requests) > 400 {
		t.Errorf("the projection lists %d request records, the timeline cap is 200",
			len(built.ContextTimeline.Requests))
	}
}
