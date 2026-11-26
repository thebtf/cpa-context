// The request path must cost the same whether the proxy has seen ten requests or
// ten thousand. These benchmarks drive that: they fill a store with N snapshots
// and measure one more request at each size.
package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func fillStore(p *plugin, sessions, perSession int) {
	for s := 0; s < sessions; s++ {
		for i := 0; i < perSession; i++ {
			trace := fmt.Sprintf("t-%d-%d", s, i)
			body := []byte(`{"model":"m","messages":[{"role":"user","content":"` +
				repeatText(i) + `"}]}`)
			intercept, _ := json.Marshal(map[string]any{
				"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
				"Model": "m", "Body": body,
			})
			p.interceptBefore(intercept)
			usageRaw, _ := json.Marshal(map[string]any{
				"TraceID": trace, "Model": "m", "SessionID": fmt.Sprintf("s-%d", s),
				"Detail": map[string]any{"InputTokens": 4000, "TotalTokens": 4000},
			})
			p.handle("usage.handle", usageRaw)
		}
	}
}

func repeatText(n int) string {
	out := ""
	for i := 0; i < 40+n%20; i++ {
		out += "line of context "
	}
	return out
}

// BenchmarkRequestCostGrowth measures one request against a store that already
// holds many snapshots.
func BenchmarkRequestCostGrowth(b *testing.B) {
	for _, sessions := range []int{1, 20, 100} {
		b.Run(fmt.Sprintf("sessions=%d", sessions), func(b *testing.B) {
			p := newPlugin(nil)
			p.cfg.Enabled = true
			fillStore(p, sessions, 20)
			raw, _ := json.Marshal(map[string]any{
				"RequestID": "x", "TraceID": "x", "SourceFormat": "openai",
				"Model": "m", "Body": []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`),
			})
			for b.Loop() {
				p.interceptBefore(raw)
			}
		})
	}
}

// BenchmarkProjectionCostGrowth measures the projection build, which the client
// asks for on every page load.
func BenchmarkProjectionCostGrowth(b *testing.B) {
	for _, sessions := range []int{1, 20, 100} {
		b.Run(fmt.Sprintf("sessions=%d", sessions), func(b *testing.B) {
			p := newPlugin(nil)
			p.cfg.Enabled = true
			fillStore(p, sessions, 20)
			for b.Loop() {
				buildProjection("s-0", p.store.snapshotsFor("s-0"), nil)
			}
		})
	}
}

// BenchmarkFleetCost measures one fleet request across many sessions.
func BenchmarkFleetCost(b *testing.B) {
	for _, sessions := range []int{1, 20, 100} {
		b.Run(fmt.Sprintf("sessions=%d", sessions), func(b *testing.B) {
			p := newPlugin(nil)
			p.cfg.Enabled = true
			fillStore(p, sessions, 20)
			raw, _ := json.Marshal(map[string]any{"sessionId": "s-0", "summary": true})
			for b.Loop() {
				p.fleetAnswer(raw)
			}
		})
	}
}
