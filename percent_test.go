package main

import "testing"

// The cache figure is the number an operator watches, so its formatting is
// pinned at the boundaries where a hand-rolled version went wrong.
func TestFormatPercentBoundaries(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{100, "100.00"},
		{99.96, "99.96"},
		{99.999, "100.00"},
		{75, "75.00"},
		{0, "0.00"},
		{0.004, "0.00"},
		{-5, "0.00"},
		{140, "100.00"},
	}
	for _, tc := range cases {
		if got := formatPercent(tc.value); got != tc.want {
			t.Errorf("formatPercent(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

// A session with some requests reading cached tokens and some not must report a
// rate between the two, not a rounded-away extreme.
func TestCacheHitRateReflectsBothSides(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	for i := 0; i < 4; i++ {
		cacheRead := int64(0)
		if i < 3 {
			cacheRead = 900
		}
		raw := []byte(`{"TraceID":"t` + string(rune('0'+i)) + `","Model":"m","SessionID":"s-rate","Detail":{"InputTokens":1000,"TotalTokens":1000,"CacheReadTokens":` +
			itoaForTest(cacheRead) + `}}`)
		p.handle("usage.handle", raw)
	}
	sess := p.store.snapshotSession("s-rate")
	if sess == nil {
		t.Fatal("session missing")
	}
	if sess.CacheHitRequests != 3 || sess.CacheMissRequests != 1 {
		t.Fatalf("hits/misses = %d/%d, want 3/1", sess.CacheHitRequests, sess.CacheMissRequests)
	}
	if got := cacheHitOf(3, 1); got != "75.00" {
		t.Errorf("hit rate = %v, want 75.00", got)
	}
}

func itoaForTest(value int64) string {
	if value == 0 {
		return "0"
	}
	out := ""
	for value > 0 {
		out = string(rune('0'+value%10)) + out
		value /= 10
	}
	return out
}
