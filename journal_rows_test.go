package main

import (
	"fmt"
	"testing"
	"time"
)

// The journal must stay bounded while the proxy runs, not only at startup. A
// file that keeps every row a busy proxy wrote is loaded back on the next start
// and will not fit in what memory is allowed to hold.
func TestJournalIsTrimmedByRowCount(t *testing.T) {
	dir := t.TempDir()
	j := openJournal(dir + "/" + stateFileName)
	if j.db == nil {
		t.Fatalf("journal did not open: %s", j.broken)
	}
	defer j.close()

	for i := 0; i < journalRows+500; i++ {
		at := time.Now().Add(time.Duration(i) * time.Millisecond)
		j.appendRequest(&snapshot{
			TraceID:     fmt.Sprintf("t-%d", i),
			SessionID:   "s",
			StartedAt:   at,
			CompletedAt: at,
			InputTokens: 10,
		})
	}

	var rows int
	if err := j.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&rows); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if rows <= journalRows {
		t.Fatalf("before pruning there are %d rows, the test needs more than %d", rows, journalRows)
	}

	removed := j.pruneOld(time.Now())
	if removed == 0 {
		t.Fatal("pruning removed nothing")
	}
	if err := j.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&rows); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if rows > journalRows {
		t.Errorf("after pruning there are %d rows, want at most %d", rows, journalRows)
	}

	// The newest rows must be the ones kept, or a restart would replay an old
	// picture and drop the current one.
	snaps, _ := j.replay()
	if len(snaps) == 0 {
		t.Fatal("replay returned nothing")
	}
	if last := snaps[len(snaps)-1]; last.TraceID != fmt.Sprintf("t-%d", journalRows+499) {
		t.Errorf("newest replayed row is %q, want the newest written", last.TraceID)
	}
}
