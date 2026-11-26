package main

import (
	"testing"
	"time"
)

// A range is a calendar window, not a rolling one. These assertions pin the
// boundaries the client draws its own presets with.
func TestScopeWindowIsCalendarBased(t *testing.T) {
	now := time.Date(2026, 11, 26, 15, 30, 0, 0, time.Local)

	start, end := scopeWindow(overviewRequest{Scope: &overviewScope{Range: "today"}}, 1, now)
	wantStart := time.Date(2026, 11, 26, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) {
		t.Errorf("today starts at %s, want midnight %s", start, wantStart)
	}
	if !end.IsZero() {
		t.Errorf("today has an end bound %s, want open", end)
	}

	// Seven days includes today, so it reaches back six.
	start7, _ := scopeWindow(overviewRequest{Scope: &overviewScope{Range: "7d"}}, 7, now)
	want7 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.Local)
	if !start7.Equal(want7) {
		t.Errorf("7d starts at %s, want %s", start7, want7)
	}

	// A day picked on the heatmap narrows to that calendar day.
	dayStart, dayEnd := scopeWindow(overviewRequest{Scope: &overviewScope{Day: "2026-11-24"}}, 30, now)
	wantDay := time.Date(2026, 11, 24, 0, 0, 0, 0, time.Local)
	if !dayStart.Equal(wantDay) {
		t.Errorf("day starts at %s, want %s", dayStart, wantDay)
	}
	if !dayEnd.Equal(wantDay.AddDate(0, 0, 1)) {
		t.Errorf("day ends at %s, want the next midnight", dayEnd)
	}

	// A calendar scope spans its first day to the end of its last.
	calStart, calEnd := scopeWindow(overviewRequest{Scope: &overviewScope{
		Calendar: &overviewSpan{From: "2026-11-20", To: "2026-11-22"},
	}}, 30, now)
	if !calStart.Equal(time.Date(2026, 11, 20, 0, 0, 0, 0, time.Local)) {
		t.Errorf("calendar starts at %s", calStart)
	}
	if !calEnd.Equal(time.Date(2026, 11, 23, 0, 0, 0, 0, time.Local)) {
		t.Errorf("calendar ends at %s, want the day after its last", calEnd)
	}

	// An unparseable day must not silently become the whole window: it answers
	// an empty window rather than everything.
	badStart, badEnd := scopeWindow(overviewRequest{Scope: &overviewScope{Day: "not-a-day"}}, 30, now)
	if !badStart.IsZero() || !badEnd.IsZero() {
		t.Errorf("an invalid day produced %s..%s, want an empty window", badStart, badEnd)
	}
}

// A session from yesterday must not appear under "today".
func TestTodayExcludesYesterday(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	now := time.Now()

	add := func(id string, at time.Time) {
		p.store.mu.Lock()
		p.store.sessions[id] = &session{
			ID:        id,
			FirstSeen: at,
			LastSeen:  at,
			Requests:  1,
		}
		p.store.mu.Unlock()
	}
	add("today", now)
	add("yesterday", now.AddDate(0, 0, -1).Add(-time.Hour))

	raw := []byte(`{"part":"summary","scope":{"range":"today"}}`)
	body, _ := p.overviewAnswer(raw).(map[string]any)["value"].(map[string]any)
	summary, _ := body["summary"].(map[string]any)
	sessions, _ := summary["sessions"].(int)
	if sessions != 1 {
		t.Errorf("today counts %d conversations, want only the one from today", sessions)
	}
}
