package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// Fleet, overview and history read the raw snapshots of every conversation
// while requests keep arriving and superseded prompt text is dropped. Reading
// the fields that survive that drop must not race with it.
func TestReadsRaceWithTraffic(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	const sessions = 6
	for i := 0; i < sessions; i++ {
		id := fmt.Sprintf("s-%d", i)
		body := []byte(`{"model":"m","messages":[{"role":"user","content":"seed"}]}`)
		intercept, _ := json.Marshal(map[string]any{
			"RequestID": "seed-" + id, "TraceID": "seed-" + id, "SourceFormat": "openai",
			"Model": "m", "Body": body,
		})
		p.handle("request.intercept_before", intercept)
		p.handle("usage.handle", []byte(
			`{"TraceID":"seed-`+id+`","Model":"m","SessionID":"`+id+`","Detail":{"InputTokens":100,"TotalTokens":100}}`))
	}

	stop := make(chan struct{})
	var writers sync.WaitGroup
	var readers sync.WaitGroup
	var bad atomic.Int64
	var read int64

	// Writers: each new request drops the prompt text of its predecessor.
	for w := 0; w < 8; w++ {
		writers.Add(1)
		go func(worker int) {
			defer writers.Done()
			for i := 0; i < 30; i++ {
				id := fmt.Sprintf("s-%d", (worker+i)%sessions)
				trace := fmt.Sprintf("w%d-%d", worker, i)
				body := []byte(`{"model":"m","messages":[{"role":"user","content":"later"}]}`)
				intercept, _ := json.Marshal(map[string]any{
					"RequestID": trace, "TraceID": trace, "SourceFormat": "openai",
					"Model": "m", "Body": body,
				})
				p.handle("request.intercept_before", intercept)
				p.handle("usage.handle", []byte(
					`{"TraceID":"`+trace+`","Model":"m","SessionID":"`+id+`","Detail":{"InputTokens":10,"TotalTokens":10}}`))
			}
		}(w)
	}

	// Readers: everything the dashboard calls while those writes happen.
	for r := 0; r < 6; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for i := 0; i < sessions; i++ {
					id := fmt.Sprintf("s-%d", i)
					for _, snap := range p.store.snapshotsFor(id) {
						if snap.InputTokens < 0 {
							bad.Add(1)
						}
						_ = snap.TotalTokens
						_ = snap.CacheReadTokens
						_ = snap.StartedAt
					}
					_ = p.store.snapshotSession(id)
					_, _ = json.Marshal(p.overviewAnswer([]byte(`{"part":"summary"}`)))
					_, _ = json.Marshal(p.fleetAnswer([]byte(`{"part":"member","session":"` + id + `","history":true}`)))
					_, _ = p.store.projectionFor(id)
					// The route the browser calls, which walks raw prompt items.
					_, _, _ = p.store.renderSession(id)
					p.store.withSnapshots(id, func(snaps []*snapshot) {
						for _, snap := range snaps {
							for _, it := range snap.Items {
								if it.Chars < 0 {
									bad.Add(1)
								}
							}
						}
					})
					atomic.AddInt64(&read, 1)
				}
			}
		}()
	}

	writers.Wait()
	close(stop)
	readers.Wait()

	if bad.Load() != 0 {
		t.Errorf("%d impossible values read", bad.Load())
	}
	if read == 0 {
		t.Error("no reader ran, the test proved nothing")
	}
}
