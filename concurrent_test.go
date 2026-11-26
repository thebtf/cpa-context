package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// Traffic arrives from many goroutines at once. Nothing may be lost, no counter
// may end up short, and every projection must stay valid JSON while it happens.
func TestConcurrentTrafficLosesNothing(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	const writers = 16
	const perWriter = 40
	const sessions = 4

	var sent atomic.Int64
	var failures atomic.Int64

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				session := fmt.Sprintf("s-%d", (worker+i)%sessions)
				trace := fmt.Sprintf("w%d-i%d", worker, i)
				body := []byte(`{"model":"m","messages":[{"role":"user","content":"concurrent request ` +
					trace + `"}]}`)
				intercept, err := json.Marshal(map[string]any{
					"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
					"Model": "m", "Body": body,
				})
				if err != nil {
					failures.Add(1)
					continue
				}
				p.handle("request.intercept_before", intercept)
				usageRaw, err := json.Marshal(map[string]any{
					"TraceID": trace, "Model": "m", "SessionID": session,
					"Detail": map[string]any{"InputTokens": 100, "TotalTokens": 100},
				})
				if err != nil {
					failures.Add(1)
					continue
				}
				p.handle("usage.handle", usageRaw)
				sent.Add(1)
			}
		}(w)
	}

	// Readers run alongside, asking for the same projections the dashboard does.
	var reads atomic.Int64
	var badJSON atomic.Int64
	stop := make(chan struct{})
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for s := 0; s < sessions; s++ {
					id := fmt.Sprintf("s-%d", s)
					if built, ok := p.store.projectionFor(id); ok {
						if _, err := json.Marshal(built); err != nil {
							badJSON.Add(1)
						}
					}
					_ = sessionRows(p.store.snapshotSessions())
					_ = p.overviewAnswer([]byte(`{"part":"summary"}`))
					reads.Add(1)
				}
			}
		}()
	}

	// Let the writers finish, then stop the readers.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		if sent.Load() >= int64(writers*perWriter) {
			break
		}
		select {
		case <-done:
		default:
		}
	}
	close(stop)
	<-done

	if failures.Load() != 0 {
		t.Errorf("%d operations failed to marshal", failures.Load())
	}
	if badJSON.Load() != 0 {
		t.Errorf("%d projections produced invalid JSON under concurrency", badJSON.Load())
	}

	// Every request that was sent must be accounted for exactly once.
	var accounted int
	for _, sess := range p.store.snapshotSessions() {
		accounted += sess.Requests
	}
	if want := int(sent.Load()); accounted != want {
		t.Errorf("accounted for %d requests, %d were sent", accounted, want)
	}
	if reads.Load() == 0 {
		t.Error("no reader ran, the test proved nothing about concurrent reads")
	}
	t.Logf("sent=%d accounted=%d reads=%d", sent.Load(), accounted, reads.Load())
}
