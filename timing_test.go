package main

import (
	"encoding/json"
	"testing"
	"time"
)

// A usage record arrives after the upstream answered. Without the request's own
// timestamp, every point on the timeline sits at its completion, and a chart of
// a long conversation collapses into the moment it finished.
func TestUsageUsesTheRequestTime(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	requested := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal(map[string]any{
		"TraceID":     "t-time",
		"SessionID":   "s-time",
		"Model":       "m",
		"RequestedAt": requested.Format(time.RFC3339Nano),
		"Latency":     int64(2 * time.Second),
		"Detail":      map[string]any{"InputTokens": 100, "TotalTokens": 100},
	})
	p.handle("usage.handle", raw)

	snaps := p.store.snapshotsFor("s-time")
	if len(snaps) != 1 {
		t.Fatalf("stored snapshots = %d, want 1", len(snaps))
	}
	if !snaps[0].StartedAt.Equal(requested) {
		t.Errorf("started at %s, want the request time %s", snaps[0].StartedAt, requested)
	}
}

// An unusable timestamp must fall back to now rather than leaving the snapshot
// at the zero time, which would sort it before the beginning of the conversation.
func TestUsageFallsBackToNowWithoutATimestamp(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	before := time.Now().Add(-time.Second)

	raw, _ := json.Marshal(map[string]any{
		"TraceID":     "t-none",
		"SessionID":   "s-none",
		"Model":       "m",
		"RequestedAt": "not a timestamp",
		"Detail":      map[string]any{"InputTokens": 100, "TotalTokens": 100},
	})
	p.handle("usage.handle", raw)

	snaps := p.store.snapshotsFor("s-none")
	if len(snaps) != 1 {
		t.Fatalf("stored snapshots = %d, want 1", len(snaps))
	}
	if snaps[0].StartedAt.Before(before) {
		t.Errorf("started at %s, want the current time", snaps[0].StartedAt)
	}
}
