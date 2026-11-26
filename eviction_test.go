package main

import (
	"fmt"
	"testing"
	"time"
)

// Evicting a conversation must release everything that hangs off it. A store
// that keeps its index or its cached projection grows without bound while the
// session count looks capped.
func TestEvictionReleasesSessionState(t *testing.T) {
	limits := defaultLimits()
	limits.maxSessions = 3
	s := newStore(limits)

	for i := 0; i < 6; i++ {
		sessionID := fmt.Sprintf("s-%d", i)
		// A real request carries a start time, which is what orders eviction.
		at := timeNow().Add(time.Duration(i) * time.Second)
		snap := &snapshot{
			TraceID:     sessionID,
			SessionID:   sessionID,
			StartedAt:   at,
			CompletedAt: at,
			InputTokens: 100,
		}
		s.mu.Lock()
		s.applyToSessionLocked(snap)
		s.rememberLocked(snap)
		s.mu.Unlock()
	}

	s.mu.RLock()
	t.Logf("limit in store = %d", s.limits.maxSessions)
	sessions := len(s.sessions)
	indexed := len(s.bySession)
	cached := len(s.projections)
	ids := make([]string, 0, len(s.bySession))
	for k := range s.bySession {
		ids = append(ids, k)
	}
	s.mu.RUnlock()
	if len(ids) == 0 {
		t.Fatal("no sessions indexed at all")
	}

	if sessions > 3 {
		t.Errorf("sessions = %d, want the cap of 3", sessions)
	}
	if indexed > sessions {
		t.Errorf("index holds %d conversations, more than the %d sessions kept", indexed, sessions)
	}
	if cached > sessions {
		t.Errorf("projection cache holds %d conversations, more than the %d kept", cached, sessions)
	}
}
