package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// The current window is whatever the newest request carried. When the tool call
// counter is read from the prompt text alone, dropping that text for memory
// reasons silently zeroes the number the dashboard shows.
func TestToolCallCountSurvivesTextEviction(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.store.limits.maxItemBytes = 1

	for i := 0; i < 3; i++ {
		body, _ := json.Marshal(map[string]any{
			"model": "m",
			"messages": []map[string]any{
				{"role": "user", "content": "go"},
				{"role": "assistant", "content": nil, "tool_calls": []map[string]any{
					{"id": "c1", "type": "function", "function": map[string]any{"name": "read", "arguments": `{"file_path":"a.go"}`}},
					{"id": "c2", "type": "function", "function": map[string]any{"name": "grep", "arguments": `{"pattern":"x"}`}},
				}},
			},
		})
		trace := fmt.Sprintf("t%d", i)
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai", "Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", intercept)
		usageRaw, _ := json.Marshal(map[string]any{
			"TraceID": trace, "Model": "m", "SessionID": "s-calls",
			"Detail": map[string]any{"InputTokens": 500, "TotalTokens": 500},
		})
		p.handle("usage.handle", usageRaw)
	}

	sess := p.store.snapshotSession("s-calls")
	if sess == nil {
		t.Fatal("session missing")
	}
	if sess.ToolCalls != 2 {
		t.Errorf("tool calls = %d, want the two the prompt carried", sess.ToolCalls)
	}
	if sess.LastContext != 500 {
		t.Errorf("current window = %d, want the newest prompt size", sess.LastContext)
	}
}

// A request with an earlier timestamp than the previous one — a retry, or a
// clock that stepped — must not leave the window describing the older request.
// The most recently observed request is the current one regardless of the stamp.
func TestCurrentWindowFollowsArrivalNotTimestamp(t *testing.T) {
	s := newStore(defaultLimits())
	later := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	earlier := later.Add(-time.Minute)

	s.mu.Lock()
	s.applyToSessionLocked(&snapshot{
		TraceID: "a", SessionID: "s", StartedAt: later, CompletedAt: later,
		InputTokens: 1000, Composition: map[string]int{CatUser: 1000},
	})
	s.applyToSessionLocked(&snapshot{
		TraceID: "b", SessionID: "s", StartedAt: earlier, CompletedAt: earlier,
		InputTokens: 2000, Composition: map[string]int{CatUser: 2000},
	})
	window := s.sessions["s"].LastContext
	s.mu.Unlock()

	if window != 2000 {
		t.Errorf("current window = %d, want the request observed last (2000)", window)
	}
}
