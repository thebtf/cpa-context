package main

import (
	"encoding/json"
	"sync"
	"testing"
)

// Reconfiguration arrives from the host while traffic keeps flowing. A field the
// request path reads must not be written without the same lock, or a running
// proxy races itself.
func TestReconfigureDuringTrafficIsSafe(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Traffic keeps arriving.
	wg.Add(1)
	go func() {
		defer wg.Done()
		body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
		for {
			select {
			case <-stop:
				return
			default:
			}
			raw, _ := json.Marshal(map[string]any{
				"RequestID": "r", "TraceID": "t", "SourceFormat": "openai",
				"Model": "m", "Body": body,
			})
			p.handle("request.intercept_before", raw)
			usageRaw, _ := json.Marshal(map[string]any{
				"TraceID": "t", "Model": "m", "SessionID": "s",
				"Detail": map[string]any{"InputTokens": 10, "TotalTokens": 10},
			})
			p.handle("usage.handle", usageRaw)
		}
	}()

	// The host reconfigures repeatedly.
	wg.Add(1)
	// Directories are created here, on the test goroutine: t.TempDir registers a
	// cleanup that runs at the end of the test, and calling it from a goroutine
	// that is still writing leaves the journal closing after the removal starts.
	dirs := make([]string, 20)
	for i := range dirs {
		dirs[i] = t.TempDir()
	}
	go func() {
		defer wg.Done()
		// Several data directories, so the journal and the price catalog are
		// written while the request path reads them. Each previous handle must be
		// released by the plugin itself, which is what this exercises.
		for i := 0; i < len(dirs); i++ {
			yaml := "enabled: true\ndata_dir: " + dirs[i] + "\n"
			raw, _ := json.Marshal(map[string]any{"config_yaml": []byte(yaml)})
			p.handle("plugin.reconfigure", raw)
		}
		close(stop)
	}()

	wg.Wait()
	// The plugin owns its journal; a test must shut it down the way the host does.
	p.shutdown()
}

// A configuration that is not YAML must be reported rather than silently leaving
// the plugin on defaults.
func TestInvalidConfigIsReported(t *testing.T) {
	p := newPlugin(nil)
	raw, _ := json.Marshal(map[string]any{
		"config_yaml": "enabled: [this is: not: yaml",
	})
	_, failure := p.handle("plugin.register", raw)
	if failure == nil {
		t.Fatal("a malformed configuration was accepted")
	}
	if failure.Code != "invalid_config" {
		t.Errorf("failure code = %q, want invalid_config", failure.Code)
	}
}
