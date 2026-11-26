package main

import (
	"fmt"
	"testing"
	"time"
)

// TestFleetHotSpot reports where the fleet request spends its time, so the fix
// targets the real limiter rather than a guess.
func TestFleetHotSpot(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	fillStore(p, 100, 20)

	rows := p.store.snapshotSessions()
	t.Logf("sessions: %d", len(rows))

	cloneStart := time.Now()
	for i := 0; i < 20; i++ {
		p.store.snapshotSessions()
	}
	t.Logf("snapshotSessions: %s per call", time.Since(cloneStart)/20)

	snapStart := time.Now()
	for i := 0; i < 20; i++ {
		p.store.snapshotsFor("s-0")
	}
	t.Logf("snapshotsFor: %s per call", time.Since(snapStart)/20)

	raw := []byte(fmt.Sprintf(`{"sessionId":%q,"summary":true}`, "s-0"))
	fleetStart := time.Now()
	for i := 0; i < 20; i++ {
		p.fleetAnswer(raw)
	}
	t.Logf("fleetAnswer: %s per call", time.Since(fleetStart)/20)

	memberStart := time.Now()
	for i := 0; i < 20; i++ {
		p.fleetMember("s-0", false)
	}
	t.Logf("fleetMember: %s per call", time.Since(memberStart)/20)
}
