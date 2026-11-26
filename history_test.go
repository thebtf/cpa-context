package main

import (
	"encoding/json"
	"testing"
)

// The ported browser reads a step's header and system prompt from events of type
// "request/header" and "system/message". Without them the panel reports that the
// environment cannot load a header, which is what the operator saw.
func TestHistoryServesHeaderAndSystemEvents(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	body := []byte(`{"model":"m","messages":[{"role":"system","content":"You are a careful engineer."},` +
		`{"role":"user","content":"explain the parser"}],` +
		`"tools":[{"type":"function","function":{"name":"bash","description":"Run a command",` +
		`"parameters":{"type":"object","properties":{"command":{"type":"string"}}}}}]}`)
	intercept, _ := json.Marshal(map[string]any{
		"RequestID": "r1", "TraceID": "t1", "SourceFormat": "openai", "Model": "m", "Body": body,
	})
	p.handle("request.intercept_before", intercept)
	p.handle("usage.handle", []byte(
		`{"TraceID":"t1","Model":"m","SessionID":"s-hist","Detail":{"InputTokens":300,"TotalTokens":300}}`))

	raw, _ := json.Marshal(map[string]any{"sessionId": "s-hist", "throughSeq": 1, "beforeSeq": 2})
	answer := p.historyPage(raw)

	payload, ok := answer.(map[string]any)
	if !ok {
		t.Fatalf("answer is %T, want a map", answer)
	}
	value, _ := payload["value"].(map[string]any)
	rows, _ := value["rows"].([]historyRow)
	if len(rows) == 0 {
		t.Fatal("no rows: the client would report that a header cannot be loaded")
	}

	var sawHeader, sawSystem bool
	for _, row := range rows {
		switch row.Type {
		case "request/header":
			sawHeader = true
			header, _ := row.Data["header"].(map[string]any)
			if header == nil {
				t.Fatal("the header event carries no header")
			}
			tools, _ := header["tools"].([]map[string]any)
			if len(tools) == 0 {
				t.Error("the header event lists no tools, so the schemas panel stays empty")
			}
			if name, _ := tools[0]["name"].(string); name != "bash" {
				t.Errorf("tool name = %q, want bash", name)
			}
			if tokens, _ := header["systemTokens"].(int64); tokens == 0 {
				t.Error("the header event reports no system prompt size")
			}
		case "system/message":
			sawSystem = true
			message, _ := row.Data["message"].(map[string]any)
			blocks, _ := message["content"].([]map[string]any)
			if len(blocks) == 0 {
				t.Fatal("the system message carries no content blocks")
			}
			if text, _ := blocks[0]["text"].(string); text == "" {
				t.Error("the system message text is empty")
			}
		}
	}
	if !sawHeader {
		t.Error("no request/header event was served")
	}
	if !sawSystem {
		t.Error("no system/message event was served")
	}
}

// A step whose prompt text was dropped still answers, so the browser can show
// counters instead of failing to load anything.
func TestHistoryAnswersAfterTheTextIsDropped(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	for i := 0; i < 3; i++ {
		body := []byte(`{"model":"m","messages":[{"role":"system","content":"be brief"},` +
			`{"role":"user","content":"turn ` + string(rune('0'+i)) + `"}]}`)
		trace := "t" + string(rune('0'+i))
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai", "Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", intercept)
		p.handle("usage.handle", []byte(
			`{"TraceID":"`+trace+`","Model":"m","SessionID":"s-old","Detail":{"InputTokens":120,"TotalTokens":120}}`))
	}

	// The superseded steps lost their prompt text; the newest still has it.
	raw, _ := json.Marshal(map[string]any{"sessionId": "s-old", "throughSeq": 3})
	answer := p.historyPage(raw)
	payload, _ := answer.(map[string]any)
	value, _ := payload["value"].(map[string]any)
	rows, _ := value["rows"].([]historyRow)
	if len(rows) < 3 {
		t.Fatalf("%d rows for three steps, the page is short", len(rows))
	}
	for _, row := range rows {
		if row.Type != "request/header" {
			continue
		}
		header, _ := row.Data["header"].(map[string]any)
		if header == nil {
			t.Errorf("step %d has no header record", row.Seq)
		}
	}
}
