package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// bigPrompt builds a request body shaped like a long agent conversation: many
// messages, each carrying a tool call and a result.
func bigPrompt(messages int) []byte {
	parts := make([]map[string]any, 0, messages)
	parts = append(parts, map[string]any{"role": "system", "content": strings.Repeat("You are a careful engineer. ", 60)})
	for i := 0; i < messages; i++ {
		text := strings.Repeat("The reasoning continues at length because the parser is subtle. ", 40)
		parts = append(parts, map[string]any{"role": "user", "content": fmt.Sprintf("%s %d", text, i)})
		parts = append(parts, map[string]any{
			"role": "assistant", "content": nil,
			"tool_calls": []map[string]any{{
				"id": "c1", "type": "function",
				"function": map[string]any{"name": "read", "arguments": `{"file_path":"internal/api/server.go"}`},
			}},
		})
		parts = append(parts, map[string]any{"role": "tool", "tool_call_id": "c1", "name": "read", "content": text})
	}
	body, _ := json.Marshal(map[string]any{"model": "claude-sonnet-4-5", "messages": parts})
	return body
}

// BenchmarkInterceptorOneRequest measures what the plugin does on the request
// path, which is the latency a client pays.
func BenchmarkInterceptorOneRequest(b *testing.B) {
	body := bigPrompt(40)
	raw, _ := json.Marshal(map[string]any{
		"RequestID": "r", "TraceID": "t", "SourceFormat": "openai",
		"Model": "claude-sonnet-4-5", "Body": body,
	})
	p := newPlugin(nil)
	p.cfg.Enabled = true
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		p.interceptBefore(raw)
	}
}

// BenchmarkProjection measures building the dashboard projection for a session
// with many requests, which happens on every page load and every card mount.
func BenchmarkProjection(b *testing.B) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	for i := 0; i < 40; i++ {
		body := bigPrompt(5 + i)
		trace := fmt.Sprintf("t%d", i)
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
			"Model": "claude-sonnet-4-5", "Body": body,
		})
		p.interceptBefore(intercept)
		usageRaw, _ := json.Marshal(map[string]any{
			"TraceID": trace, "Model": "claude-sonnet-4-5", "SessionID": "s",
			"Detail": map[string]any{"InputTokens": 9000, "OutputTokens": 200, "TotalTokens": 9200},
		})
		p.handle("usage.handle", usageRaw)
	}
	snaps := p.store.snapshotsFor("s")
	for b.Loop() {
		buildProjection("s", snaps, nil)
	}
}

// TestMemoryPerSnapshot reports how much a single stored snapshot costs when it
// keeps the full prompt text. This is the number that decides whether a long
// running proxy survives.
func TestMemoryPerSnapshot(t *testing.T) {
	body := bigPrompt(60)
	parsed := parseRequestBody("openai", body)
	total := 0
	for _, it := range parsed.Items {
		total += len(it.Text) + len(it.Label) + len(it.Target) + len(it.Detail) + 200
	}
	text := 0
	for _, it := range parsed.Items {
		text += len(it.Text)
	}
	t.Logf("prompt body: %d bytes, items: %d, text kept: %d bytes, per snapshot: %d bytes",
		len(body), len(parsed.Items), text, total)
	t.Logf("2000 stored snapshots would hold %d MiB", total*2000/1024/1024)

	start := time.Now()
	for i := 0; i < 20; i++ {
		parseRequestBody("openai", body)
	}
	t.Logf("parse cost per body: %s", time.Since(start)/20)
}
