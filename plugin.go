package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	coresession "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/session"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginID      = "cpa-context"
	pluginVersion = "0.16.0"
	pluginRepo    = "https://github.com/thebtf/cpa-context"
)

// main is required by the c-shared build mode and never runs.
func main() {}

type rpcError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type rpcEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

// hostCall runs one host callback, so the rest of the plugin never touches cgo.
type hostCall func(method string, payload any, into any) *rpcError

// config is the plugin-owned configuration under plugins.configs.cpa-context.
type config struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`
	MaxSessions  int  `yaml:"max_sessions" json:"max_sessions"`
	RecordBodies bool `yaml:"record_bodies" json:"record_bodies"`
	// DataDir is where the SQLite journal lives. The host does not tell a plugin
	// its own path, so this is configured: in a container it is the mounted
	// plugins directory, which is the only one that survives a restart.
	DataDir string `yaml:"data_dir" json:"data_dir"`
}

func defaultConfig() config {
	return config{MaxSessions: 400, RecordBodies: true}
}

type plugin struct {
	mu     sync.RWMutex
	cfg    config
	store  *store
	call   hostCall
	schema uint32
}

var timeNow = time.Now

func newPlugin(call hostCall) *plugin {
	p := &plugin{
		cfg:   defaultConfig(),
		store: newStore(defaultLimits()),
		call:  call,
	}
	return p
}

// attachJournal opens the persistent store and replays it. Replaying happens
// once, at startup: the dashboard then reads memory, and every new observation
// is written through.
func (p *plugin) attachJournal(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// The same file can be spelled with either separator, and a configure that
	// only changed a limit must not close and reopen the journal: the reopen
	// replays the file and would duplicate what is already in memory.
	path = filepath.Clean(path)
	if existing := p.store.journalOf(); existing != nil {
		if filepath.Clean(existing.path) == path {
			return
		}
		existing.close()
		p.store.setJournal(nil)
	}
	opened := openJournal(path)
	p.store.setJournal(opened)
	// Drop what is past retention before loading, so an old file does not fill
	// memory on the way in.
	opened.pruneOld(timeNow())
	opened.pruneEvery(10*time.Minute, opened.stop)
	snaps, events := opened.replay()
	if len(snaps) == 0 && len(events) == 0 {
		return
	}
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	for _, snap := range snaps {
		p.store.applyToSessionLocked(snap)
		p.store.recent = append(p.store.recent, snap)
		if snap.SessionID != "" {
			p.store.bySession[snap.SessionID] = append(p.store.bySession[snap.SessionID], snap)
		}
	}
	p.store.events = append(p.store.events, events...)
	if len(p.store.recent) > p.store.limits.maxRecent {
		p.store.recent = p.store.recent[len(p.store.recent)-p.store.limits.maxRecent:]
	}
	if len(p.store.events) > p.store.limits.maxEvents {
		p.store.events = p.store.events[len(p.store.events)-p.store.limits.maxEvents:]
	}
}

// journalStatus reports where observations are stored, for the dashboard to
// show instead of pretending the history is permanent.
func (p *plugin) journalStatus() map[string]any {
	p.mu.RLock()
	opened := p.store.journal
	p.mu.RUnlock()
	if opened == nil {
		return map[string]any{"durable": false, "reason": "the journal was not opened"}
	}
	if opened.broken != "" {
		return map[string]any{"durable": false, "reason": opened.broken}
	}
	return map[string]any{"durable": true, "path": opened.path}
}

func (p *plugin) shutdown() {
	p.mu.Lock()
	opened := p.store.journal
	p.mu.Unlock()
	// Closing flushes the write-ahead log; the observations stay on disk so the
	// next start reads them back.
	opened.close()
	p.store.reset(true)
}

func (p *plugin) handle(method string, raw []byte) (any, *rpcError) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if failure := p.configure(raw); failure != nil {
			return nil, failure
		}
		return pluginRegistration(), nil
	case pluginabi.MethodPluginShutdown:
		p.shutdown()
		return struct{}{}, nil
	case pluginabi.MethodRequestInterceptBefore:
		return p.interceptBefore(raw), nil
	case pluginabi.MethodRequestInterceptAfter:
		return pluginapi.RequestInterceptResponse{}, nil
	case pluginabi.MethodRequestComplete:
		return p.complete(raw), nil
	case pluginabi.MethodUsageHandle:
		return p.usage(raw), nil
	case pluginabi.MethodManagementRegister:
		return p.registerManagement(), nil
	case pluginabi.MethodManagementHandle:
		return p.handleManagement(raw), nil
	}
	return nil, &rpcError{Code: "unsupported_method", Message: "unsupported plugin method", HTTPStatus: http.StatusNotImplemented}
}

type registerRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

func (p *plugin) configure(raw []byte) *rpcError {
	var req registerRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return &rpcError{Code: "invalid_config", Message: "plugin configuration is not valid", HTTPStatus: http.StatusBadRequest}
		}
	}
	next := defaultConfig()
	if len(req.ConfigYAML) > 0 {
		if err := yaml.Unmarshal(req.ConfigYAML, &next); err != nil {
			return &rpcError{Code: "invalid_config", Message: "plugin configuration is not valid YAML", HTTPStatus: http.StatusBadRequest}
		}
	}
	if next.MaxSessions <= 0 {
		next.MaxSessions = defaultConfig().MaxSessions
	}

	p.mu.Lock()
	p.cfg = next
	p.schema = req.SchemaVersion
	limits := defaultLimits()
	limits.maxSessions = next.MaxSessions
	limits.keepItemsForNewest = next.RecordBodies
	p.store.setLimits(limits)
	p.mu.Unlock()
	if next.DataDir != "" {
		p.attachJournal(filepath.Join(next.DataDir, stateFileName))
		if p.store.prices == nil {
			catalog := newPriceCatalog(next.DataDir)
			p.store.prices = catalog
			catalog.start()
		}
	}
	return nil
}

func (p *plugin) enabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg.Enabled
}

func (p *plugin) schemaVersion() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.schema
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	RequestInterceptor     bool `json:"request_interceptor"`
	RequestLifecyclePlugin bool `json:"request_lifecycle_plugin"`
	UsagePlugin            bool `json:"usage_plugin"`
	ManagementAPI          bool `json:"management_api"`
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginID,
			Version:          pluginVersion,
			Author:           "thebtf",
			GitHubRepository: pluginRepo,
			ConfigFields: []pluginapi.ConfigField{
				{
					Name:        "max_sessions",
					Type:        pluginapi.ConfigFieldTypeInteger,
					Default:     400,
					Description: "Conversations kept in memory; the least recently active are evicted first.",
				},
				{
					Name:        "record_bodies",
					Type:        pluginapi.ConfigFieldTypeBoolean,
					Default:     true,
					Description: "Keep the parsed prompt text of the newest request per conversation so the dashboard can expand it.",
				},
			},
		},
		Capabilities: registrationCapability{
			RequestInterceptor:     true,
			RequestLifecyclePlugin: true,
			UsagePlugin:            true,
			ManagementAPI:          true,
		},
	}
}

// interceptRequest names the subset of pluginapi.RequestInterceptRequest this
// plugin reads. The host sends the full struct; unknown fields are ignored.
type interceptRequest struct {
	RequestID      string         `json:"RequestID"`
	TraceID        string         `json:"TraceID"`
	SourceFormat   string         `json:"SourceFormat"`
	ToFormat       string         `json:"ToFormat"`
	Model          string         `json:"Model"`
	RequestedModel string         `json:"RequestedModel"`
	Stream         bool           `json:"Stream"`
	Headers        http.Header    `json:"Headers"`
	Body           []byte         `json:"Body"`
	CallerScope    string         `json:"CallerScope"`
	Metadata       map[string]any `json:"Metadata"`
}

// interceptBefore observes a request and never modifies it: the response is
// always empty, which the host reads as "no changes".
func (p *plugin) interceptBefore(raw []byte) any {
	if !p.enabled() {
		return pluginapi.RequestInterceptResponse{}
	}
	var req interceptRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return pluginapi.RequestInterceptResponse{}
	}
	p.snapshotFrom(req)
	return pluginapi.RequestInterceptResponse{}
}

func (p *plugin) snapshotFrom(req interceptRequest) *snapshot {
	format := normalizeFormat(req.SourceFormat)
	if format == "" {
		format = normalizeFormat(req.ToFormat)
	}
	parsed := parseRequestBody(format, req.Body)
	snap := &snapshot{
		RequestID:      req.RequestID,
		TraceID:        req.TraceID,
		SourceFormat:   format,
		Model:          firstNonEmpty(req.Model, req.RequestedModel),
		RequestedModel: req.RequestedModel,
		Stream:         req.Stream,
		StartedAt:      timeNow(),
		MessageCount:   parsed.MessageCount,
		ToolCount:      parsed.ToolCount,
		MaxTokens:      parsed.MaxTokens,
		SystemChars:    len(parsed.SystemText),
		Project:        projectFromMetadata(req.Metadata),
		Title:          parsed.Title,
	}

	// CPA already resolves session identity from headers, body and metadata. Reuse
	// that instead of reimplementing the client-specific header rules here.
	if info, ok := coresession.ExtractSessionInfo(req.Headers, req.Body, req.Metadata); ok {
		snap.SessionID = info.SessionID
		snap.ParentSession = info.ParentSessionID
		snap.ClientType = info.ClientType
		snap.AgentName = info.AgentName
		snap.IsSubagent = info.IsSubagent
		snap.IsFork = info.IsFork
		snap.Compaction = info.IsCompaction
	}

	counts := make(map[string]int, len(categoryOrder))
	for _, it := range parsed.Items {
		counts[it.Category] += tokens.countItem(snap.Model, it)
	}
	snap.Estimated = counts
	snap.Composition = counts
	snap.CompositionKnown = len(parsed.Items) > 0
	snap.Items = parsed.Items

	p.mu.RLock()
	keepBodies := p.cfg.RecordBodies
	p.mu.RUnlock()
	if !keepBodies {
		snap.Items = nil
	}

	p.store.recordRequest(snap)
	return snap
}

// projectFromMetadata picks a human-readable workspace name. CPA's caller scope
// is a stable hash of the credential, which identifies the tenant but is not a
// project name, so it is deliberately not used here.
func projectFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	for _, key := range []string{"project", "workspace", "cwd", "repo", "repository"} {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type completionRequest struct {
	RequestID      string `json:"RequestID"`
	TraceID        string `json:"TraceID"`
	SourceFormat   string `json:"SourceFormat"`
	Outcome        string `json:"Outcome"`
	StatusCode     int    `json:"StatusCode"`
	Error          string `json:"Error"`
	Model          string `json:"Model"`
	RequestedModel string `json:"RequestedModel"`
	Stream         bool   `json:"Stream"`
}

func (p *plugin) complete(raw []byte) any {
	var req completionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return struct{}{}
	}
	// A completion with no matching interception means this plugin was not in the
	// chain for that request, so there is nothing to correct. CPA rejects an
	// unroutable model before it creates the lifecycle this event belongs to, and
	// such a request never reaches an interceptor either.
	p.store.recordCompletion(req.TraceID, req.RequestID, req.Outcome, req.Error)
	return struct{}{}
}

// usageRequest names the subset of pluginapi.UsageRecord this plugin reads.
// Durations arrive as nanoseconds because that is how the host marshals them.
type usageRequest struct {
	RequestID       string `json:"RequestID"`
	TraceID         string `json:"TraceID"`
	Provider        string `json:"Provider"`
	ExecutorType    string `json:"ExecutorType"`
	Model           string `json:"Model"`
	Alias           string `json:"Alias"`
	SessionID       string `json:"SessionID"`
	ParentSessionID string `json:"ParentSessionID"`
	Stream          bool   `json:"Stream"`
	Latency         int64  `json:"Latency"`
	TTFT            int64  `json:"TTFT"`
	Failed          bool   `json:"Failed"`
	// RequestedAt is when the proxy received the request. Usage arrives after the
	// upstream answers, so a record that only used the arrival time placed every
	// request at its own completion.
	RequestedAt string `json:"RequestedAt"`
	Detail      struct {
		InputTokens         int64 `json:"InputTokens"`
		OutputTokens        int64 `json:"OutputTokens"`
		ReasoningTokens     int64 `json:"ReasoningTokens"`
		CachedTokens        int64 `json:"CachedTokens"`
		CacheReadTokens     int64 `json:"CacheReadTokens"`
		CacheCreationTokens int64 `json:"CacheCreationTokens"`
		TotalTokens         int64 `json:"TotalTokens"`
	} `json:"Detail"`
}

func (p *plugin) usage(raw []byte) any {
	if !p.enabled() {
		return struct{}{}
	}
	var req usageRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return struct{}{}
	}

	cacheRead := firstNonZero(req.Detail.CacheReadTokens, req.Detail.CachedTokens)
	cacheWrite := req.Detail.CacheCreationTokens
	prompt := promptTotal(coreusage.Detail{
		InputTokens:         req.Detail.InputTokens,
		OutputTokens:        req.Detail.OutputTokens,
		ReasoningTokens:     req.Detail.ReasoningTokens,
		CachedTokens:        req.Detail.CachedTokens,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheWrite,
		TotalTokens:         req.Detail.TotalTokens,
	}, req.Provider, req.ExecutorType)

	// The request's own arrival time is authoritative for the timeline: a usage
	// record only exists once the upstream has answered.
	requestedAt := time.Time{}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, req.RequestedAt); err == nil {
			requestedAt = parsed
			break
		}
	}

	p.store.recordUsage(usage{
		RequestedAt:  requestedAt,
		TraceID:      req.TraceID,
		RequestID:    req.RequestID,
		SessionID:    req.SessionID,
		ParentID:     req.ParentSessionID,
		Model:        firstNonEmpty(req.Model, req.Alias),
		Alias:        req.Alias,
		Provider:     req.Provider,
		InputTokens:  prompt,
		OutputTokens: req.Detail.OutputTokens,
		Reasoning:    req.Detail.ReasoningTokens,
		Cached:       req.Detail.CachedTokens,
		CacheRead:    cacheRead,
		CacheWrite:   cacheWrite,
		Total:        req.Detail.TotalTokens,
		LatencyMs:    req.Latency / int64(time.Millisecond),
		TTFTMs:       req.TTFT / int64(time.Millisecond),
		Failed:       req.Failed,
	})
	return struct{}{}
}

// promptTotal returns the full prompt size, cache included. Providers disagree
// on whether cache counters sit inside the input total, so this defers to the
// same accounting CPA applies to its own usage records.
//
// When CPA can classify the counters the answer is exact. When it cannot (the
// counters contradict each other, or the provider is unknown) this returns the
// smallest prompt size consistent with the reported total.
func promptTotal(detail coreusage.Detail, provider, executorType string) int64 {
	normalized := coreusage.EnsureTokenBreakdownForProvider(detail, provider, executorType)
	breakdown := normalized.TokenBreakdown
	if breakdown.Valid() && breakdown.Input.TotalTokens > 0 {
		return breakdown.Input.TotalTokens
	}
	candidate := detail.InputTokens + detail.CacheReadTokens + detail.CacheCreationTokens
	if candidate < 0 {
		return 0
	}
	if total := breakdown.TotalTokens; total > 0 && candidate > total-detail.OutputTokens {
		if room := total - detail.OutputTokens; room > 0 {
			return room
		}
		return total
	}
	return candidate
}
