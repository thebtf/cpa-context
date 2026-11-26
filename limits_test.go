package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
)

// base64YAML encodes a configuration the way the host frame carries it.
func base64YAML(text string) string {
	return base64.StdEncoding.EncodeToString([]byte(text))
}

// Lowering max_sessions must release the conversations that no longer fit. A cap
// that only applies to new conversations leaves the process holding everything it
// had before the operator cut the limit.
func TestLoweringTheSessionCapEvicts(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	for i := 0; i < 20; i++ {
		p.handle("usage.handle", []byte(`{"TraceID":"t`+fmt.Sprint(i)+`","Model":"m","SessionID":"s-`+fmt.Sprint(i)+`","Detail":{"InputTokens":100,"TotalTokens":100}}`))
	}
	if got := len(p.store.snapshotSessions()); got != 20 {
		t.Fatalf("before the change: %d conversations, want 20", got)
	}

	raw := []byte(`{"config_yaml":"` + base64YAML("enabled: true\nmax_sessions: 5\n") + `"}`)
	p.handle("plugin.reconfigure", raw)

	sessions := len(p.store.snapshotSessions())
	p.store.mu.RLock()
	indexed := len(p.store.bySession)
	p.store.mu.RUnlock()

	if sessions > 5 {
		t.Errorf("after lowering the cap: %d conversations, want at most 5", sessions)
	}
	if indexed > sessions {
		t.Errorf("the index holds %d conversations, more than the %d kept", indexed, sessions)
	}
}

// Turning record_bodies off must drop the text that is already held, not only
// stop collecting new text: the operator changes it to reduce what the process
// keeps.
func TestDisablingBodiesDropsHeldText(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"some prompt text here"}]}`)
	intercept, _ := json.Marshal(map[string]any{
		"RequestID": "r", "TraceID": "t", "SourceFormat": "openai", "Model": "m", "Body": body,
	})
	p.handle("request.intercept_before", intercept)
	p.handle("usage.handle", []byte(`{"TraceID":"t","Model":"m","SessionID":"s-text","Detail":{"InputTokens":50,"TotalTokens":50}}`))

	sess := p.store.snapshotSession("s-text")
	if sess == nil || len(sess.LastItems) == 0 {
		t.Fatal("the text was not stored while record_bodies was on")
	}

	raw := []byte(`{"config_yaml":"` + base64YAML("enabled: true\nrecord_bodies: false\n") + `"}`)
	p.handle("plugin.reconfigure", raw)

	sess = p.store.snapshotSession("s-text")
	if sess == nil {
		t.Fatal("the conversation disappeared, it should only lose its text")
	}
	if len(sess.LastItems) != 0 {
		t.Errorf("%d items are still held after record_bodies was turned off", len(sess.LastItems))
	}
	if sess.InputTokens != 50 {
		t.Errorf("prompt = %d, the counters must survive", sess.InputTokens)
	}
}

// Raising the cap must not disturb what is already held: the operator may tune it
// up while traffic is flowing.
func TestRaisingTheCapKeepsEverything(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	p.cfg.MaxSessions = 5

	for i := 0; i < 5; i++ {
		p.handle("usage.handle", []byte(`{"TraceID":"t`+fmt.Sprint(i)+`","Model":"m","SessionID":"s-`+fmt.Sprint(i)+`","Detail":{"InputTokens":100,"TotalTokens":100}}`))
	}
	before := len(p.store.snapshotSessions())

	raw := []byte(`{"config_yaml":"` + base64YAML("enabled: true\nmax_sessions: 50\n") + `"}`)
	p.handle("plugin.reconfigure", raw)

	if after := len(p.store.snapshotSessions()); after != before {
		t.Errorf("raising the cap changed the conversation count from %d to %d", before, after)
	}
}
