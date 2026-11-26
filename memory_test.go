// The memory behaviour under a long conversation is what decides whether a
// running proxy survives, so it is asserted rather than assumed.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestOnlyNewestPromptIsHeld checks that a session's older snapshots drop their
// prompt text. Each body replays the whole conversation, so keeping them all
// would multiply the same text once per request.
func TestOnlyNewestPromptIsHeld(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	filler := strings.Repeat("context line for the parser ", 400)
	for i := 0; i < 8; i++ {
		messages := make([]map[string]any, 0, i+3)
		messages = append(messages, map[string]any{"role": "user", "content": "start"})
		for j := 0; j < i+1; j++ {
			messages = append(messages, map[string]any{"role": "user", "content": fmt.Sprintf("%s %d", filler, j)})
		}
		body, _ := json.Marshal(map[string]any{"model": "m", "messages": messages})
		trace := fmt.Sprintf("t%d", i)
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
			"Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", intercept)
		usageRaw, _ := json.Marshal(map[string]any{
			"TraceID": trace, "Model": "m", "SessionID": "s-mem",
			"Detail": map[string]any{"InputTokens": 5000, "TotalTokens": 5000},
		})
		p.handle("usage.handle", usageRaw)
	}

	snaps := p.store.snapshotsFor("s-mem")
	if len(snaps) != 8 {
		t.Fatalf("stored snapshots = %d, want 8", len(snaps))
	}
	withItems := 0
	for _, snap := range snaps {
		if len(snap.Items) > 0 {
			withItems++
		}
	}
	if withItems != 1 {
		t.Errorf("%d snapshots still hold prompt text, want only the newest", withItems)
	}
	if len(snaps[len(snaps)-1].Items) == 0 {
		t.Error("the newest snapshot lost its items: the browser would be empty")
	}
	// The composition must survive even where the text was dropped, otherwise the
	// timeline loses its history.
	for _, snap := range snaps {
		if len(snap.Composition) == 0 && len(snap.Estimated) == 0 {
			t.Error("a snapshot lost its composition along with its text")
		}
	}
}

// TestItemBudgetBoundsHeldText checks the global cap: many sessions each keeping
// a newest prompt must still fit the budget.
func TestItemBudgetBoundsHeldText(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.store.limits.maxItemBytes = 64 * 1024

	filler := strings.Repeat("x", 8*1024)
	for i := 0; i < 40; i++ {
		body, _ := json.Marshal(map[string]any{
			"model": "m",
			"messages": []map[string]any{
				{"role": "user", "content": filler},
			},
		})
		trace := fmt.Sprintf("t%d", i)
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
			"Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", intercept)
		usageRaw, _ := json.Marshal(map[string]any{
			"TraceID": trace, "Model": "m", "SessionID": fmt.Sprintf("s%d", i),
			"Detail": map[string]any{"InputTokens": 2000, "TotalTokens": 2000},
		})
		p.handle("usage.handle", usageRaw)
	}

	held := p.store.itemBudgetLocked()
	if held > 2*64*1024 {
		t.Errorf("held prompt text = %d bytes, budget was %d", held, 64*1024)
	}
	if len(p.store.snapshotSessions()) != 40 {
		t.Error("dropping text also dropped sessions, which it must not")
	}
}

// Dropping the prompt text must not drop the chart. Every request keeps its own
// split even when only the newest snapshot still holds its text.
func TestTimelineKeepsSplitAfterTextIsDropped(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	filler := strings.Repeat("a long line of context ", 200)
	for i := 0; i < 5; i++ {
		messages := []map[string]any{
			{"role": "system", "content": "be careful"},
			{"role": "user", "content": filler + string(rune('a'+i))},
		}
		body, _ := json.Marshal(map[string]any{"model": "m", "messages": messages})
		trace := fmt.Sprintf("t%d", i)
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
			"Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", intercept)
		usageRaw, _ := json.Marshal(map[string]any{
			"TraceID": trace, "Model": "m", "SessionID": "s-chart",
			"Detail": map[string]any{"InputTokens": 3000 + i*100, "TotalTokens": 3000 + i*100},
		})
		p.handle("usage.handle", usageRaw)
	}

	built, ok := p.store.projectionFor("s-chart")
	if !ok {
		t.Fatal("projection is missing")
	}
	requests := built.ContextTimeline.Requests
	if len(requests) != 5 {
		t.Fatalf("requests = %d, want one per model call", len(requests))
	}
	for _, record := range requests {
		if len(record.Chart) == 0 {
			t.Errorf("request %d has no split: its bar would be empty", record.Seq)
		}
		if record.User == 0 {
			t.Errorf("request %d reports no user tokens", record.Seq)
		}
	}
}
