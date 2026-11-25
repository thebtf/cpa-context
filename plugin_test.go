package main

import (
	"encoding/json"
	"testing"

	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const claudeBody = `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 8192,
  "system": [{"type":"text","text":"You are a coding agent."}],
  "tools": [{"name":"read","description":"Read a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}],
  "messages": [
    {"role":"user","content":[{"type":"text","text":"hello"}]},
    {"role":"assistant","content":[{"type":"thinking","thinking":"let me think"},{"type":"tool_use","id":"t1","name":"read","input":{"path":"a.go"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"package main"}]}
  ]
}`

func TestParseClaudeComposition(t *testing.T) {
	c := parseRequestBody("claude", []byte(claudeBody))
	if c == nil {
		t.Fatal("parseRequestBody returned nil")
	}
	if c.Model != "claude-sonnet-4-5" {
		t.Fatalf("model = %q", c.Model)
	}
	if c.MessageCount != 3 {
		t.Fatalf("MessageCount = %d, want 3", c.MessageCount)
	}
	if c.ToolCount != 1 {
		t.Fatalf("ToolCount = %d, want 1", c.ToolCount)
	}
	if c.SystemText != "You are a coding agent." {
		t.Fatalf("SystemText = %q", c.SystemText)
	}
	seen := map[string]bool{}
	for _, it := range c.Items {
		seen[it.Category] = true
	}
	for _, want := range []string{CatSystem, CatTools, CatUser, CatThinking, CatToolCall, CatToolResult} {
		if !seen[want] {
			t.Errorf("missing category %q in %v", want, seen)
		}
	}
}

const openAIBody = `{
  "model": "gpt-5.2",
  "max_completion_tokens": 4096,
  "messages": [
    {"role":"system","content":"be brief"},
    {"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},
    {"role":"assistant","content":null,"tool_calls":[{"function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]},
    {"role":"tool","tool_call_id":"c1","name":"bash","content":"a.go b.go"}
  ],
  "tools": [{"function":{"name":"bash","description":"run","parameters":{"type":"object"}}}]
}`

func TestParseOpenAIChatComposition(t *testing.T) {
	c := parseRequestBody("openai", []byte(openAIBody))
	if c == nil {
		t.Fatal("nil composition")
	}
	if c.MaxTokens != 4096 {
		t.Fatalf("MaxTokens = %d", c.MaxTokens)
	}
	counts := map[string]int{}
	for _, it := range c.Items {
		counts[it.Category]++
	}
	if counts[CatSystem] != 1 || counts[CatImage] != 1 || counts[CatToolCall] != 1 || counts[CatToolResult] != 1 || counts[CatTools] != 1 {
		t.Fatalf("unexpected category counts: %v", counts)
	}
}

const responsesBody = `{
  "model": "gpt-5.2",
  "instructions": "system here",
  "input": [
    {"type":"message","role":"user","content":[{"type":"input_text","text":"go"}]},
    {"type":"function_call","name":"read","arguments":"{}","call_id":"c9"},
    {"type":"function_call_output","call_id":"c9","output":"data"}
  ],
  "tools": [{"type":"function","name":"read","description":"d","parameters":{"type":"object"}}]
}`

func TestParseResponsesComposition(t *testing.T) {
	c := parseRequestBody("openai-response", []byte(responsesBody))
	if c == nil {
		t.Fatal("nil composition")
	}
	counts := map[string]int{}
	for _, it := range c.Items {
		counts[it.Category]++
	}
	if counts[CatSystem] != 1 || counts[CatToolCall] != 1 || counts[CatToolResult] != 1 || counts[CatTools] != 1 {
		t.Fatalf("unexpected counts: %v", counts)
	}
}

const geminiBody = `{
  "systemInstruction": {"parts":[{"text":"be terse"}]},
  "contents": [
    {"role":"user","parts":[{"text":"hi"}]},
    {"role":"model","parts":[{"text":"thinking","thought":true},{"functionCall":{"name":"f","args":{}}}]}
  ],
  "tools": [{"functionDeclarations":[{"name":"f","description":"d","parameters":{"type":"object"}}]}],
  "generationConfig": {"maxOutputTokens": 2048}
}`

func TestParseGeminiComposition(t *testing.T) {
	c := parseRequestBody("gemini", []byte(geminiBody))
	if c == nil {
		t.Fatal("nil composition")
	}
	if c.MaxTokens != 2048 {
		t.Fatalf("MaxTokens = %d", c.MaxTokens)
	}
	counts := map[string]int{}
	for _, it := range c.Items {
		counts[it.Category]++
	}
	if counts[CatSystem] != 1 || counts[CatThinking] != 1 || counts[CatToolCall] != 1 {
		t.Fatalf("unexpected counts: %v", counts)
	}
}

func TestUnknownFormatIsHonest(t *testing.T) {
	c := parseRequestBody("some-format", []byte("not json at all"))
	if len(c.Items) != 1 || c.Items[0].Category != CatOther {
		t.Fatalf("expected one opaque item, got %+v", c.Items)
	}
}

func TestPromptTotalHandlesBothShapes(t *testing.T) {
	cases := []struct {
		name                            string
		provider                        string
		input, read, write, output, total, want int64
	}{
		// Anthropic reports uncached input and the cache beside it.
		{"anthropic separate cache", "claude", 200, 800, 0, 0, 1000, 1000},
		// OpenAI reports input with cache inside it.
		{"openai subset cache", "openai", 1000, 800, 0, 0, 1000, 1000},
		{"no cache", "openai", 700, 0, 0, 0, 700, 700},
		// Contradictory counters: the prompt cannot exceed the reported total.
		{"contradictory counters", "claude", 500, 0, 300, 0, 900, 800},
	}
	for _, tc := range cases {
		detail := coreusage.Detail{
			InputTokens: tc.input, CacheReadTokens: tc.read,
			CacheCreationTokens: tc.write, OutputTokens: tc.output, TotalTokens: tc.total,
		}
		if got := promptTotal(detail, tc.provider, ""); got != tc.want {
			t.Errorf("%s: promptTotal = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestScaledCompositionSumsToTarget(t *testing.T) {
	base := map[string]int{CatSystem: 100, CatTools: 300, CatUser: 50, CatToolResult: 7}
	got := scaledComposition(base, 5000)
	sum := 0
	for _, value := range got {
		sum += value
	}
	if sum != 5000 {
		t.Fatalf("sum = %d, want 5000 (%v)", sum, got)
	}
	if got[CatTools] <= got[CatSystem] {
		t.Fatalf("proportions lost: %v", got)
	}
}

func TestUsageJoinsInterceptByTraceID(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	body := []byte("{\"model\":\"m\",\"messages\":[{\"role\":\"user\",\"content\":\"hello world\"}]}")
	intercept, _ := json.Marshal(map[string]any{
		"RequestID": "req-1", "TraceID": "trace-1", "SourceFormat": "openai",
		"Model": "m", "Body": body,
	})
	p.handle("request.intercept_before", intercept)

	usageRaw, _ := json.Marshal(map[string]any{
		"RequestID": "req-1", "TraceID": "trace-1", "Model": "m", "Provider": "p",
		"SessionID": "sess-1",
		"Detail":    map[string]any{"InputTokens": 4000, "OutputTokens": 100, "TotalTokens": 4100},
	})
	p.handle("usage.handle", usageRaw)

	sess := p.store.snapshotSession("sess-1")
	if sess == nil {
		t.Fatal("session not recorded")
	}
	if sess.LastContext != 4000 {
		t.Fatalf("LastContext = %d, want 4000", sess.LastContext)
	}
	sum := 0
	for _, value := range sess.Composition {
		sum += value
	}
	if sum != 4000 {
		t.Fatalf("composition sums to %d, want 4000 (%v)", sum, sess.Composition)
	}
	if !sess.Calibrated {
		t.Fatal("expected the composition to be marked calibrated")
	}
	if len(sess.Timeline) != 1 {
		t.Fatalf("timeline = %d points", len(sess.Timeline))
	}
}

func TestUsageWithoutInterceptStillCounts(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	usageRaw, _ := json.Marshal(map[string]any{
		"TraceID": "t2", "Model": "m", "SessionID": "s2",
		"Detail": map[string]any{"InputTokens": 900, "OutputTokens": 10, "TotalTokens": 910},
	})
	p.handle("usage.handle", usageRaw)
	sess := p.store.snapshotSession("s2")
	if sess == nil || sess.LastContext != 900 {
		t.Fatalf("counters-only snapshot missing: %+v", sess)
	}
	if sess.Calibrated {
		t.Fatal("a counters-only request must not claim a calibrated composition")
	}
}

func TestInterceptNeverModifiesRequest(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	raw, _ := json.Marshal(map[string]any{
		"RequestID": "r", "TraceID": "t", "SourceFormat": "openai",
		"Body": []byte("{\"messages\":[]}"),
	})
	result, failure := p.handle("request.intercept_before", raw)
	if failure != nil {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	resp, ok := result.(pluginapi.RequestInterceptResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", result)
	}
	if resp.Terminate || len(resp.Body) != 0 || len(resp.Headers) != 0 || len(resp.ClearHeaders) != 0 {
		t.Fatalf("interceptor must not modify the request: %+v", resp)
	}
}

func TestDisabledPluginCollectsNothing(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = false
	raw, _ := json.Marshal(map[string]any{"TraceID": "t", "SessionID": "s", "Body": []byte("{}")})
	p.handle("request.intercept_before", raw)
	usageRaw, _ := json.Marshal(map[string]any{"TraceID": "t", "SessionID": "s"})
	p.handle("usage.handle", usageRaw)
	if got := p.store.snapshotSessions(); len(got) != 0 {
		t.Fatalf("disabled plugin recorded %d sessions", len(got))
	}
}
