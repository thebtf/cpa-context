package main

import (
	"sort"
	"sync"
	"time"
)

// A snapshot is one observed model request, as the plugin saw it: the parsed
// composition on the way in, and the provider-reported counters on the way out.
// The two are joined by trace id, which is the inbound HTTP request id that CPA
// stamps on both the interceptor call and the usage record.
type snapshot struct {
	RequestID      string    `json:"requestId"`
	TraceID        string    `json:"traceId"`
	SessionID      string    `json:"sessionId"`
	ParentSession  string    `json:"parentSessionId,omitempty"`
	Model          string    `json:"model"`
	RequestedModel string    `json:"requestedModel,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	SourceFormat   string    `json:"sourceFormat"`
	Stream         bool      `json:"stream"`
	StartedAt      time.Time `json:"startedAt"`
	CompletedAt    time.Time `json:"completedAt,omitempty"`
	Outcome        string    `json:"outcome,omitempty"`
	StatusCode     int       `json:"statusCode,omitempty"`
	Error          string    `json:"error,omitempty"`
	MessageCount   int       `json:"messageCount"`
	ToolCount      int       `json:"toolCount"`
	MaxTokens      int       `json:"maxTokens,omitempty"`
	// Estimated holds the pre-calibration composition: what the tokenizer said
	// about each category before scale.
	Estimated map[string]int `json:"estimated,omitempty"`
	// Composition is the calibrated split. It sums to InputTokens.
	Composition map[string]int `json:"composition,omitempty"`
	// InputTokens is the authoritative prompt total from the provider.
	InputTokens int64 `json:"inputTokens"`
	// UncachedTokens, CacheReadTokens and CacheWriteTokens are the provider's
	// own partition of InputTokens, when the protocol reports it.
	UncachedTokens   int64 `json:"uncachedTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	ReasoningTokens  int64 `json:"reasoningTokens"`
	TotalTokens      int64 `json:"totalTokens"`
	LatencyMs        int64 `json:"latencyMs,omitempty"`
	TTFTMs           int64 `json:"ttftMs,omitempty"`
	Failed           bool  `json:"failed,omitempty"`
	// CompositionKnown reports whether the request body was parsed at all. When
	// it is false the dashboard shows the provider counters without a split.
	CompositionKnown bool `json:"compositionKnown"`
	// Calibrated reports whether the split was scaled to a provider total.
	Calibrated bool `json:"calibrated"`
	// Items is kept only for the newest snapshot of a session, because holding
	// full request text for every request would dwarf the traffic it describes.
	Items []item `json:"items,omitempty"`
	// SystemChars lets the timeline show instruction growth without the text.
	SystemChars int `json:"systemChars"`
	// ClientType and AgentName describe the caller, when CPA could infer them.
	ClientType string `json:"clientType,omitempty"`
	AgentName  string `json:"agentName,omitempty"`
	IsSubagent bool   `json:"isSubagent,omitempty"`
	IsFork     bool   `json:"isFork,omitempty"`
	// Compaction marks a request CPA identified as a compaction or summary turn.
	Compaction bool `json:"compaction,omitempty"`
	// Project is filled from the caller scope so cards group by workspace.
	Project string `json:"project,omitempty"`
	// Title is the first user message of the request, trimmed. It is the only
	// readable name the traffic carries: CPA resolves ids, not titles.
	Title string `json:"title,omitempty"`
}

// event is a moment the context window changed for a reason other than the
// model's own reply: compaction, a model switch, a cache boundary.
type event struct {
	At        time.Time `json:"at"`
	SessionID string    `json:"sessionId"`
	Kind      string    `json:"kind"`
	Detail    string    `json:"detail,omitempty"`
	// Delta is the signed change in prompt tokens this event caused.
	Delta int64 `json:"delta,omitempty"`
}

const (
	eventCompaction   = "compaction"
	eventModelChange  = "model"
	eventCacheMiss    = "cache"
	eventFirstRequest = "first"
	eventTrim         = "trim"
)

// session is the per-conversation aggregate the dashboard lists.
type session struct {
	ID           string    `json:"id"`
	ParentID     string    `json:"parentSessionId,omitempty"`
	Project      string    `json:"project,omitempty"`
	ClientType   string    `json:"clientType,omitempty"`
	AgentName    string    `json:"agentName,omitempty"`
	IsSubagent   bool      `json:"isSubagent,omitempty"`
	IsFork       bool      `json:"isFork,omitempty"`
	FirstSeen    time.Time `json:"firstSeen"`
	LastSeen     time.Time `json:"lastSeen"`
	Requests     int       `json:"requests"`
	Failed       int       `json:"failed"`
	Models       []string  `json:"models"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheRead    int64     `json:"cacheReadTokens"`
	CacheWrite   int64     `json:"cacheWriteTokens"`
	Reasoning    int64     `json:"reasoningTokens"`
	TotalTokens  int64     `json:"totalTokens"`
	// CacheHitRequests and CacheMissRequests count requests that read any cached
	// prompt tokens and requests that read none. The hit rate is what tells an
	// operator whether a client is reusing its prefix.
	CacheHitRequests  int `json:"cacheHitRequests"`
	CacheMissRequests int `json:"cacheMissRequests"`
	// ToolCalls counts the tool invocations the newest prompt replays, which is
	// the same number the context view shows for this conversation.
	ToolCalls int `json:"toolCalls,omitempty"`
	// Title is the conversation's own first user message, trimmed. CPA exposes no
	// session title, so this is the closest thing to a name the traffic carries.
	Title string `json:"title,omitempty"`
	// LastContext is the prompt size of the newest request: the current window.
	LastContext int64 `json:"lastContextTokens"`
	// PeakContext is the largest prompt this session ever sent.
	PeakContext int64 `json:"peakContextTokens"`
	// MaxOutput is the output cap the client requested on the newest request.
	// It is not the context window: CPA exposes no model-window information to
	// plugins, so the dashboard must not pretend to know it.
	MaxOutput   int64 `json:"maxOutputTokens,omitempty"`
	Compactions int   `json:"compactions"`
	// Unsourced marks a conversation CPA could not identify: the client sent no
	// session header, so this card is one request rather than one conversation.
	Unsourced bool `json:"unsourced,omitempty"`
	// Composition is the calibrated split of the newest request.
	Composition map[string]int `json:"composition,omitempty"`
	// Estimated is the pre-calibration split of the newest request, kept so the
	// dashboard can show how far the tokenizer was from the provider.
	Estimated  map[string]int `json:"estimated,omitempty"`
	Calibrated bool           `json:"calibrated"`
	// Timeline holds the newest requests, oldest first, capped by the store.
	Timeline []timelinePoint `json:"timeline,omitempty"`
	// LastItems is the expandable composition of the newest request.
	LastItems  []item `json:"lastItems,omitempty"`
	LastFormat string `json:"lastFormat,omitempty"`
}

// timelinePoint is one bar in the session chart. It is deliberately small: the
// chart draws thousands of them.
type timelinePoint struct {
	At          time.Time      `json:"at"`
	RequestID   string         `json:"requestId,omitempty"`
	Model       string         `json:"model,omitempty"`
	Input       int64          `json:"input"`
	Output      int64          `json:"output"`
	CacheRead   int64          `json:"cacheRead,omitempty"`
	Composition map[string]int `json:"composition,omitempty"`
	Compaction  bool           `json:"compaction,omitempty"`
	Failed      bool           `json:"failed,omitempty"`
}

// store is the in-memory picture of recent traffic. CPA has no history API for
// plugins, so the plugin keeps its own bounded window and says so in the UI.
type store struct {
	mu sync.RWMutex
	// sessions maps session id to its aggregate.
	sessions map[string]*session
	// recent keeps the newest snapshots across all sessions, newest last.
	recent []*snapshot
	// events keeps the newest context events, newest last.
	events []event
	// pending maps trace id to the snapshot awaiting its usage record.
	pending map[string]*snapshot
	limits  limits
	// bySession indexes the same snapshots by conversation, so a lookup does not
	// walk every request the proxy has seen. It holds the same pointers as
	// recent, which is what keeps the two views consistent.
	bySession map[string][]*snapshot
	// journal is the persistent store. It may be nil, in which case the plugin
	// works from memory only and says so.
	journal *journal
	// projections caches the rendered projection per session. The client asks
	// for it through several routes per page load, and each build walks every
	// stored request, so rebuilding per call is wasted work. A session's version
	// is its request count, which only grows.
	projections   map[string]*cachedProjection
	projectionCap int
	// prices is the catalog used to estimate cost. It may be nil, in which case
	// a session reports no price.
	prices *priceCatalog
}

type cachedProjection struct {
	version int
	value   projection
}

type limits struct {
	maxSessions        int
	maxTimeline        int
	maxRecent          int
	maxEvents          int
	maxPendingAge      time.Duration
	keepItemsForNewest bool
	// maxItemBytes caps the prompt text held across all snapshots. Rendering
	// needs one session's newest prompt; holding many is what makes the process
	// huge, and a huge heap costs CPU in the collector.
	maxItemBytes int
}

func defaultLimits() limits {
	return limits{
		maxSessions:        400,
		maxTimeline:        200,
		maxRecent:          2000,
		maxEvents:          2000,
		maxPendingAge:      10 * time.Minute,
		keepItemsForNewest: true,
		maxItemBytes:       16 << 20,
	}
}

func newStore(limits limits) *store {
	return &store{
		sessions:      make(map[string]*session),
		pending:       make(map[string]*snapshot),
		bySession:     make(map[string][]*snapshot),
		projections:   make(map[string]*cachedProjection),
		projectionCap: 64,
		limits:        limits,
	}
}

// recordRequest stores the parsed composition of an inbound request, keyed by
// trace id so the later usage record can complete it.
func (s *store) recordRequest(snap *snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if snap.TraceID == "" {
		// Without a join key the request is still useful on its own; it lands in
		// the session directly and never waits for usage.
		s.applyToSessionLocked(snap)
		return
	}
	if existing, ok := s.pending[snap.TraceID]; ok {
		snap.StartedAt = existing.StartedAt
	}
	s.pending[snap.TraceID] = snap
	s.prunePendingLocked()
}

// recordCompletion closes the lifecycle of a request, which is the only signal
// for a request that never produced a usage record.
func (s *store) recordCompletion(traceID, requestID, outcome, message string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.pending[traceID]
	if !ok {
		return false
	}
	if snap.RequestID == "" {
		snap.RequestID = requestID
	}
	snap.Outcome = outcome
	snap.Error = message
	if snap.CompletedAt.IsZero() {
		snap.CompletedAt = time.Now()
	}

	// A rejected or canceled request never reaches a provider, so no usage record
	// will ever arrive to finish it. Keep what the interceptor measured and file
	// it now, otherwise a request the user never saw fail disappears from the
	// dashboard entirely. A completed or failed one still expects its usage
	// record, which carries the authoritative counters.
	if outcome != "rejected" && outcome != "canceled" {
		return true
	}
	snap.Failed = true
	delete(s.pending, traceID)
	s.applyToSessionLocked(snap)
	s.rememberLocked(snap)
	s.journal.appendRequest(snap)
	return true
}

// usage is the provider-reported accounting for one request.
type usage struct {
	TraceID      string
	RequestID    string
	SessionID    string
	ParentID     string
	Model        string
	Alias        string
	Provider     string
	InputTokens  int64
	OutputTokens int64
	Reasoning    int64
	Cached       int64
	CacheRead    int64
	CacheWrite   int64
	Total        int64
	LatencyMs    int64
	TTFTMs       int64
	Failed       bool
	// RequestedAt is when the proxy received the request, zero when unknown. The
	// usage record only exists once the upstream has answered, so using its
	// arrival time would place every request at its own completion.
	RequestedAt time.Time
	ClientType  string
	AgentName   string
	IsSubagent  bool
	IsFork      bool
	Compaction  bool
}

// recordUsage attaches provider counters to the request it belongs to and folds
// the finished snapshot into its session.
func (s *store) recordUsage(u usage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.pending[u.TraceID]
	if ok {
		delete(s.pending, u.TraceID)
	} else {
		// A usage record without a parsed request still carries a context total,
		// which is what the timeline needs. Build a counters-only snapshot.
		// A usage record with no interception still needs a position on the
		// timeline. The request's own arrival time is used when the host reported
		// one, so the chart places it where the request was made.
		startedAt := u.RequestedAt
		if startedAt.IsZero() {
			startedAt = time.Now()
		}
		snap = &snapshot{
			TraceID:   u.TraceID,
			RequestID: u.RequestID,
			SessionID: u.SessionID,
			StartedAt: startedAt,
		}
	}

	if snap.RequestID == "" {
		snap.RequestID = u.RequestID
	}
	if snap.SessionID == "" {
		snap.SessionID = u.SessionID
	}
	if snap.ParentSession == "" {
		snap.ParentSession = u.ParentID
	}
	if snap.Model == "" {
		snap.Model = firstNonEmpty(u.Model, u.Alias)
	}
	if snap.Provider == "" {
		snap.Provider = u.Provider
	}
	snap.InputTokens = u.InputTokens
	snap.OutputTokens = u.OutputTokens
	snap.ReasoningTokens = u.Reasoning
	snap.CacheReadTokens = firstNonZero(u.CacheRead, u.Cached)
	snap.CacheWriteTokens = u.CacheWrite
	snap.TotalTokens = u.Total
	snap.LatencyMs = u.LatencyMs
	snap.TTFTMs = u.TTFTMs
	snap.Failed = snap.Failed || u.Failed
	snap.ClientType = firstNonEmpty(snap.ClientType, u.ClientType)
	snap.AgentName = firstNonEmpty(snap.AgentName, u.AgentName)
	snap.IsSubagent = snap.IsSubagent || u.IsSubagent
	snap.IsFork = snap.IsFork || u.IsFork
	snap.Compaction = snap.Compaction || u.Compaction
	if snap.CompletedAt.IsZero() {
		snap.CompletedAt = time.Now()
	}
	if snap.StartedAt.IsZero() {
		snap.StartedAt = snap.CompletedAt
	}

	// The provider's own partition is authoritative where present. Providers
	// that report a subset (cache included in input) still expose the split
	// because cache read and cache creation are separate counters.
	uncached := u.InputTokens
	if u.CacheRead > 0 || u.CacheWrite > 0 {
		if remainder := u.InputTokens - u.CacheRead - u.CacheWrite; remainder >= 0 {
			uncached = remainder
		}
	}
	snap.UncachedTokens = uncached
	// Scale the per-item estimate to the billed prompt size so the stacked bar
	// sums to the number the provider actually charged for.
	if len(snap.Estimated) > 0 && u.InputTokens > 0 {
		snap.Composition = scaledComposition(snap.Estimated, int(u.InputTokens))
		snap.Calibrated = true
	} else if len(snap.Composition) > 0 {
		snap.Calibrated = false
	}

	s.applyToSessionLocked(snap)
	s.rememberLocked(snap)
	s.journal.appendRequest(snap)
}

// CPA's Claude decoder sets the subagent role and parent but not the flag.
// Normalize here so live usage and journal replay feed the same family graph.
func isDelegation(snap *snapshot) bool {
	if snap.IsSubagent {
		return true
	}
	return snap.ClientType == "claude" && snap.AgentName == "subagent" &&
		snap.SessionID != "" && snap.ParentSession != "" &&
		lastSessionSegment(snap.SessionID) != lastSessionSegment(snap.ParentSession) &&
		!snap.IsFork && !snap.Compaction && !isDerivedForkID(snap.SessionID)
}

// applyToSessionLocked folds one finished snapshot into its session aggregate.
func (s *store) applyToSessionLocked(snap *snapshot) {
	// A request with no session identity must not be folded into one shared
	// bucket: unrelated calls would look like a single growing conversation.
	// Key it by its own request so each stands alone and says why.
	id := snap.SessionID
	unsourced := id == ""
	if unsourced {
		id = firstNonEmpty(snap.TraceID, snap.RequestID)
		if id == "" {
			id = "unidentified"
		}
	}
	sess, ok := s.sessions[id]
	if !ok {
		sess = &session{ID: id, FirstSeen: snap.StartedAt, Unsourced: unsourced}
		s.sessions[id] = sess
	}
	if sess.FirstSeen.IsZero() || (!snap.StartedAt.IsZero() && snap.StartedAt.Before(sess.FirstSeen)) {
		sess.FirstSeen = snap.StartedAt
	}
	if snap.CompletedAt.After(sess.LastSeen) {
		sess.LastSeen = snap.CompletedAt
	}
	if snap.StartedAt.After(sess.LastSeen) {
		sess.LastSeen = snap.StartedAt
	}
	sess.Requests++
	if snap.Failed {
		sess.Failed++
	}
	sess.InputTokens += snap.InputTokens
	sess.OutputTokens += snap.OutputTokens
	sess.CacheRead += snap.CacheReadTokens
	sess.CacheWrite += snap.CacheWriteTokens
	sess.Reasoning += snap.ReasoningTokens
	sess.TotalTokens += snap.TotalTokens

	// A request counts as a cache hit when the provider read any cached prompt
	// tokens for it. The hit rate is per request, not per token: a single large
	// miss should not be hidden by many small hits.
	if snap.InputTokens > 0 {
		if snap.CacheReadTokens > 0 {
			sess.CacheHitRequests++
		} else {
			sess.CacheMissRequests++
		}
	}
	// A stored title is kept only when it still reads as a name. The journal
	// outlives the code that wrote it: a title derived before the rules learned to
	// skip harness wrappers and identifiers is "m45881" or "<acp ...>", and
	// replaying it would show that forever, because the prompt text needed to
	// derive a better one is not stored. Dropping it leaves the conversation
	// unnamed until its next request, which derives a real title.
	if sess.Title == "" && snap.Title != "" && readableTitle(snap.Title) {
		sess.Title = snap.Title
	}

	if snap.Model != "" && !contains(sess.Models, snap.Model) {
		sess.Models = append(sess.Models, snap.Model)
	}
	if sess.ParentID == "" {
		sess.ParentID = snap.ParentSession
	}
	if sess.Project == "" {
		sess.Project = snap.Project
	}
	if sess.ClientType == "" {
		sess.ClientType = snap.ClientType
	}
	if sess.AgentName == "" {
		sess.AgentName = snap.AgentName
	}
	sess.IsSubagent = sess.IsSubagent || isDelegation(snap)
	sess.IsFork = sess.IsFork || snap.IsFork
	if snap.Compaction {
		sess.Compactions++
	}

	if snap.InputTokens > sess.PeakContext {
		sess.PeakContext = snap.InputTokens
	}

	point := timelinePoint{
		At:          snap.StartedAt,
		RequestID:   snap.RequestID,
		Model:       snap.Model,
		Input:       snap.InputTokens,
		Output:      snap.OutputTokens,
		CacheRead:   snap.CacheReadTokens,
		Composition: snap.Composition,
		Compaction:  snap.Compaction,
		Failed:      snap.Failed,
	}
	sess.Timeline = append(sess.Timeline, point)
	if len(sess.Timeline) > s.limits.maxTimeline {
		sess.Timeline = sess.Timeline[len(sess.Timeline)-s.limits.maxTimeline:]
	}

	// The request observed last defines the current window. Arrival order, not the
	// timestamp: a retry or a clock that stepped can carry an earlier stamp, and
	// comparing stamps then left the window describing the request before it.
	newest := true
	if newest {
		sess.LastContext = snap.InputTokens
		sess.Composition = snap.Composition
		sess.Estimated = snap.Estimated
		sess.Calibrated = snap.Calibrated
		sess.LastFormat = snap.SourceFormat
		if s.limits.keepItemsForNewest {
			sess.LastItems = snap.Items
		}
		calls := 0
		for _, it := range snap.Items {
			if it.Category == CatToolCall {
				calls++
			}
		}
		sess.ToolCalls = calls
		sess.MaxOutput = int64(snap.MaxTokens)
	}

	s.recordEventsLocked(sess, snap)

	// The cap is enforced here, once this request has set LastSeen. Evicting any
	// earlier compared a brand-new session, whose LastSeen is still zero, against
	// the others and dropped the newest conversation instead of the oldest.
	s.evictSessionsLocked()
}

// recordEventsLocked derives window-change events from the snapshot sequence.
func (s *store) recordEventsLocked(sess *session, snap *snapshot) {
	previous := int64(0)
	previousModel := ""
	if len(sess.Timeline) > 1 {
		previousPoint := sess.Timeline[len(sess.Timeline)-2]
		previous = previousPoint.Input
		previousModel = previousPoint.Model
	} else {
		s.pushEventLocked(event{At: snap.StartedAt, SessionID: sess.ID, Kind: eventFirstRequest, Delta: snap.InputTokens})
	}

	if snap.Compaction {
		s.pushEventLocked(event{
			At:        snap.StartedAt,
			SessionID: sess.ID,
			Kind:      eventCompaction,
			Detail:    snap.Model,
			Delta:     snap.InputTokens - previous,
		})
	}
	if previousModel != "" && snap.Model != "" && previousModel != snap.Model {
		s.pushEventLocked(event{
			At:        snap.StartedAt,
			SessionID: sess.ID,
			Kind:      eventModelChange,
			Detail:    previousModel + " → " + snap.Model,
		})
	}
	// A prompt that shrank without CPA flagging a compaction means the client cut
	// its own history: truncation, a dropped attachment, a manual reset. Small
	// dips are ordinary turn boundaries and stay quiet.
	if previous > 0 && !snap.Compaction && snap.InputTokens < previous {
		if drop := previous - snap.InputTokens; drop*100 >= previous*20 {
			s.pushEventLocked(event{
				At:        snap.StartedAt,
				SessionID: sess.ID,
				Kind:      eventTrim,
				Detail:    snap.Model,
				Delta:     -drop,
			})
		}
	}

	// A shrinking prompt is a trim, not a cache miss: the provider read nothing
	// new because there was nothing new to read. Reporting both would count one
	// event twice.
	trimmed := previous > 0 && !snap.Compaction && snap.InputTokens < previous &&
		(previous-snap.InputTokens)*100 >= previous*20
	if previous > 0 && !trimmed && snap.CacheReadTokens == 0 && snap.InputTokens > 0 {
		s.pushEventLocked(event{
			At:        snap.StartedAt,
			SessionID: sess.ID,
			Kind:      eventCacheMiss,
			Detail:    snap.Model,
			Delta:     snap.InputTokens - previous,
		})
	}
}

func (s *store) pushEventLocked(ev event) {
	// The journal can be swapped while a running proxy reconfigures, so it is
	// read under the same lock that replaces it. The methods are nil-safe.
	s.journal.appendEvent(ev)
	s.events = append(s.events, ev)
	if len(s.events) > s.limits.maxEvents {
		s.events = s.events[len(s.events)-s.limits.maxEvents:]
	}
}

// rememberLocked keeps a finished snapshot and bounds what the process holds.
//
// A request body carries the whole conversation, so a snapshot's Items duplicate
// every earlier request's text. Only the newest snapshot of a session is ever
// rendered, so the older ones drop their text and keep their counters. That is
// what keeps a long-running proxy from spending all its CPU in the collector.
func (s *store) rememberLocked(snap *snapshot) {
	s.recent = append(s.recent, snap)
	if len(s.recent) > s.limits.maxRecent {
		overflow := s.recent[:len(s.recent)-s.limits.maxRecent]
		for _, dropped := range overflow {
			list := s.bySession[dropped.SessionID]
			for i, entry := range list {
				if entry == dropped {
					list = append(list[:i], list[i+1:]...)
					break
				}
			}
			if len(list) == 0 {
				delete(s.bySession, dropped.SessionID)
			} else {
				s.bySession[dropped.SessionID] = list
			}
		}
		s.recent = s.recent[len(s.recent)-s.limits.maxRecent:]
	}
	if snap.SessionID != "" {
		s.bySession[snap.SessionID] = append(s.bySession[snap.SessionID], snap)
		if len(s.bySession[snap.SessionID]) > s.limits.maxTimeline*4 {
			s.bySession[snap.SessionID] = s.bySession[snap.SessionID][len(s.bySession[snap.SessionID])-s.limits.maxTimeline*4:]
		}
	}
	s.dropSupersededItemsLocked(snap)
	s.enforceItemBudgetLocked()
}

// enforceItemBudgetLocked drops prompt text, oldest first, until the held bytes
// fit the budget. Counters, composition and timeline are untouched, so only the
// expandable prompt text is lost, and the dashboard already says when a prompt
// is no longer available.
func (s *store) enforceItemBudgetLocked() {
	held := s.itemBudgetLocked()
	if held <= s.limits.maxItemBytes {
		return
	}
	for i := 0; i < len(s.recent) && held > s.limits.maxItemBytes; i++ {
		snap := s.recent[i]
		if len(snap.Items) == 0 {
			continue
		}
		for _, it := range snap.Items {
			held -= len(it.Text) + len(it.Label) + len(it.Target) + len(it.Detail)
		}
		snap.Items = nil
	}
}

// dropSupersededItemsLocked clears the prompt text of every snapshot of this
// session except the one just stored. The composition survives, so the timeline
// and the totals stay exact; only the expandable text goes.
func (s *store) dropSupersededItemsLocked(newest *snapshot) {
	if newest.SessionID == "" {
		return
	}
	for i := len(s.recent) - 2; i >= 0; i-- {
		earlier := s.recent[i]
		if earlier == newest {
			continue
		}
		if earlier.SessionID != newest.SessionID {
			continue
		}
		if len(earlier.Items) > 0 {
			earlier.Items = nil
		}
	}
}

// itemBudget bounds the total prompt text held in memory across all sessions.
// Without it, many sessions each keeping their newest prompt would still add up.
func (s *store) itemBudgetLocked() int {
	held := 0
	for _, snap := range s.recent {
		for _, it := range snap.Items {
			held += len(it.Text) + len(it.Label) + len(it.Target) + len(it.Detail)
		}
	}
	return held
}

// prunePendingLocked keeps the pending map bounded. Its entries hold parsed
// prompt text, which is the largest thing the store can hold, so an unbounded
// map would grow with traffic whenever requests finish slower than they arrive.
//
// Age alone is not enough: under steady load every entry is young, nothing is
// old enough to drop, and the map grows anyway. When the cap is still exceeded
// after the age sweep, the oldest entries go regardless of age. Their usage
// record still lands, because recordUsage builds a counters-only snapshot when
// it finds no pending entry.
func (s *store) prunePendingLocked() {
	if len(s.pending) <= s.limits.maxRecent {
		return
	}
	cutoff := time.Now().Add(-s.limits.maxPendingAge)
	for traceID, snap := range s.pending {
		// A request that never set a start time is not old: a zero time sorts
		// before every cutoff and would drop exactly the newest requests.
		if !snap.StartedAt.IsZero() && snap.StartedAt.Before(cutoff) {
			delete(s.pending, traceID)
		}
	}
	if len(s.pending) <= s.limits.maxRecent {
		return
	}
	type entry struct {
		trace string
		at    time.Time
	}
	entries := make([]entry, 0, len(s.pending))
	for traceID, snap := range s.pending {
		entries = append(entries, entry{trace: traceID, at: snap.StartedAt})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
	for i := 0; i < len(entries)-s.limits.maxRecent; i++ {
		delete(s.pending, entries[i].trace)
	}
}

// evictSessionsLocked drops the least recently active sessions when the cap is
// reached, so a long-running proxy does not grow without bound.
func (s *store) evictSessionsLocked() {
	if len(s.sessions) <= s.limits.maxSessions {
		return
	}
	type entry struct {
		id   string
		seen time.Time
	}
	entries := make([]entry, 0, len(s.sessions))
	for id, sess := range s.sessions {
		entries = append(entries, entry{id: id, seen: sess.LastSeen})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].seen.Before(entries[j].seen) })
	remove := len(entries) - s.limits.maxSessions
	for i := range remove {
		id := entries[i].id
		delete(s.sessions, id)
		// Everything that hangs off the conversation goes with it: its snapshot
		// index and its cached projection. Leaving them behind leaks a session's
		// worth of memory for every conversation the proxy has ever dropped.
		delete(s.bySession, id)
		delete(s.projections, id)
	}
}

// projectionFor returns the rendered projection of one conversation, reusing
// the previous render while the conversation has not gained a request.
//
// The render happens under the lock: the snapshot pointers are shared, and a
// concurrent request drops the prompt text of superseded snapshots while the
// projection is reading them. Rendering outside the lock raced the eviction.
func (s *store) projectionFor(sessionID string) (projection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snaps := s.bySession[sessionID]
	if len(snaps) == 0 {
		return projection{}, false
	}
	if cached, ok := s.projections[sessionID]; ok && cached.version == len(snaps) {
		return cached.value, true
	}

	events := make([]event, 0, 32)
	for i := len(s.events) - 1; i >= 0 && len(events) < 500; i-- {
		if s.events[i].SessionID == sessionID {
			events = append(events, s.events[i])
		}
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}

	value := buildProjection(sessionID, snaps, events)
	if s.prices != nil {
		newest := snaps[len(snaps)-1]
		applyCost(&value, s.prices, newest)
	}
	if len(s.projections) >= s.projectionCap {
		for key := range s.projections {
			delete(s.projections, key)
			break
		}
	}
	s.projections[sessionID] = &cachedProjection{version: len(snaps), value: value}
	return value, true
}

// snapshotsFor returns the finished snapshots of one conversation, oldest
// first. The dashboard renders one projection per conversation, and a
// projection needs the parsed prompt of every request, not only the newest.
//
// The elements are shared pointers, and a request that arrives drops the prompt
// text of the snapshots it supersedes. A caller that walks Items therefore runs
// inside withSnapshots, not on the slice this returns.
func (s *store) snapshotsFor(sessionID string) []*snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	indexed := s.bySession[sessionID]
	if len(indexed) == 0 {
		return nil
	}
	return append([]*snapshot(nil), indexed...)
}

// withSnapshots runs read over the snapshots of one conversation while the
// store lock is held, so nothing rewrites the prompt text underneath it. It
// reports false when the conversation has no snapshots.
func (s *store) withSnapshots(sessionID string, read func([]*snapshot)) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	indexed := s.bySession[sessionID]
	if len(indexed) == 0 {
		return false
	}
	read(indexed)
	return true
}

// renderSession builds the projection of one conversation under the lock. It is
// the read side of /session: the render walks the raw items, and a concurrent
// usage record clears those items of every superseded snapshot.
func (s *store) renderSession(sessionID string) (projection, []event, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	indexed := s.bySession[sessionID]
	if len(indexed) == 0 {
		return projection{}, nil, false
	}
	events := make([]event, 0, 32)
	for i := len(s.events) - 1; i >= 0 && len(events) < 500; i-- {
		if s.events[i].SessionID == sessionID {
			events = append(events, s.events[i])
		}
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return buildProjection(sessionID, indexed, events), events, true
}

// snapshotSessions copies the aggregate of every conversation for a caller that
// only reads it. The per-request timeline is not copied: no caller of this
// function reads it, and copying it made every list render cost grow with the
// number of requests the proxy had ever seen. A caller that needs the timeline
// reads it through the projection, which is cached.
func (s *store) snapshotSessions() []*session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		clone := *sess
		clone.Models = append([]string(nil), sess.Models...)
		out = append(out, &clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// agentNode is one member of an agent family: this conversation, its parent,
// and its children, as CPA identified them from session headers.
type agentNode struct {
	ID         string `json:"id"`
	Label      string `json:"label,omitempty"`
	ClientType string `json:"clientType,omitempty"`
	AgentName  string `json:"agentName,omitempty"`
	IsSubagent bool   `json:"isSubagent,omitempty"`
	IsFork     bool   `json:"isFork,omitempty"`
	Requests   int    `json:"requests"`
	// Context is the prompt size of this member's newest request.
	Context int64 `json:"contextTokens"`
	// Output is what this member has generated in total.
	Output      int64 `json:"outputTokens"`
	Compactions int   `json:"compactions"`
	// Children are the conversations that named this one as their parent.
	Children []agentNode `json:"children,omitempty"`
	// ParentID is the conversation this one named as its parent, empty for a
	// root. The fleet view needs it to tell a lead from a teammate.
	ParentID string `json:"parentId,omitempty"`
	// Role is "lead" for the top of a family and "teammate" for a member. A
	// family whose lead issued no request has no session of its own, and this is
	// how the roster still tells the reader which agent is in charge.
	Role string `json:"role,omitempty"`
	// Virtual names the parent id that has no session, when this node stands in
	// for it. It is informational: the client does not need it.
	Virtual string `json:"virtualParent,omitempty"`
}

// agentFamily assembles the tree rooted at id, walking children transitively.
// A conversation whose parent is not in the store is treated as a root, so the
// view never shows an orphan with a dangling edge.
func (s *store) agentFamily(id string) *agentNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	root, ok := s.sessions[id]
	if !ok {
		return nil
	}
	// A member of a family resolves to the family's top, not to itself: the fleet
	// is opened from the agent the operator is looking at, and a teammate that
	// showed only itself would hide the work it belongs to. The walk stops at the
	// first conversation whose parent is absent, which is either the true root or
	// a lead that issued no request.
	root = s.topOfLocked(root)
	seen := map[string]bool{}
	// A conversation that names a parent the store has no requests for is the top
	// of its family as far as this proxy can see. That is the ordinary shape of a
	// subagent: the lead issues no request of its own, so no session is created for
	// it, and only its children's ParentID says it exists. Without this the family
	// is a root with no members and the fleet draws one lonely agent.
	if root.ParentID != "" {
		if _, hasParent := s.sessions[root.ParentID]; !hasParent {
			// The children of this family are keyed by the absent parent id, not by
			// this conversation, so walking from root.ID would find none of the
			// siblings. The tree is assembled around the parent it names, with this
			// conversation as its first child.
			node := s.buildNodeLocked(root, seen)
			if node == nil {
				return nil
			}
			// The node is the top of the tree the view can draw, but it is not the
			// lead: it named a parent that never passed through this proxy, so what
			// it is, is a subagent. Only the top-of-tree position is virtual.
			node.Virtual = root.ParentID
			for id, sibling := range s.sessions {
				if id == root.ID || sibling.ParentID != root.ParentID {
					continue
				}
				if built := s.buildNodeLocked(sibling, seen); built != nil {
					node.Children = append(node.Children, *built)
				}
			}
			sort.Slice(node.Children, func(i, j int) bool {
				return node.Children[i].Context > node.Children[j].Context
			})
			return node
		}
	}
	return s.buildNodeLocked(root, seen)
}

// topOfLocked walks parent links upward to the top of a family. A cycle cannot
// run forever here: every visited id is remembered and the walk stops when it
// repeats one.
func (s *store) topOfLocked(start *session) *session {
	if start == nil {
		return nil
	}
	visited := map[string]bool{start.ID: true}
	current := start
	for current.ParentID != "" {
		parent, ok := s.sessions[current.ParentID]
		if !ok {
			// The parent is stored under the prefix of the request that created it,
			// which can differ from the one the child wrote. The id underneath is
			// what identifies it.
			parent, ok = s.byUnprefixedLocked(current.ParentID)
		}
		if !ok || visited[parent.ID] {
			return current
		}
		visited[parent.ID] = true
		current = parent
	}
	return current
}

// byUnprefixedLocked finds a conversation by the id CPA derived it from, ignoring
// the prefix that records which wire format it arrived over.
func (s *store) byUnprefixedLocked(id string) (*session, bool) {
	want := lastSessionSegment(id)
	for key, sess := range s.sessions {
		if lastSessionSegment(key) == want {
			return sess, true
		}
	}
	return nil, false
}

func (s *store) buildNodeLocked(sess *session, seen map[string]bool) *agentNode {
	if sess == nil || seen[sess.ID] {
		return nil
	}
	seen[sess.ID] = true
	node := &agentNode{
		ID:          sess.ID,
		Label:       sess.Project,
		ClientType:  sess.ClientType,
		AgentName:   sess.AgentName,
		IsSubagent:  sess.IsSubagent,
		IsFork:      sess.IsFork,
		Requests:    sess.Requests,
		Context:     sess.LastContext,
		Output:      sess.OutputTokens,
		Compactions: sess.Compactions,
		ParentID:    sess.ParentID,
	}
	for _, child := range s.sessions {
		if child.ID == sess.ID || child.ParentID == "" {
			continue
		}
		// The parent id is written with the child's own prefix, so a child that
		// names claude:<id> from a generic request carries header:<id>. Comparing
		// the strings verbatim loses that family; comparing the id underneath finds
		// it whatever the two requests' formats were.
		if lastSessionSegment(child.ParentID) != lastSessionSegment(sess.ID) {
			continue
		}
		if built := s.buildNodeLocked(child, seen); built != nil {
			node.Children = append(node.Children, *built)
		}
	}
	sort.Slice(node.Children, func(i, j int) bool {
		return node.Children[i].Context > node.Children[j].Context
	})
	return node
}

// familyRoots returns the top most conversation of every family that has
// members, so the dashboard can show one entry per family.
func (s *store) familyRoots() []*agentNode {
	s.mu.RLock()
	ids := make([]string, 0, len(s.sessions))
	for id, sess := range s.sessions {
		if sess.ParentID == "" {
			ids = append(ids, id)
			continue
		}
		if _, hasParent := s.sessions[sess.ParentID]; !hasParent {
			ids = append(ids, id)
		}
	}
	// A cycle leaves every member with an existing parent, so no root would be
	// reported and the whole family would vanish from the list. Every member that
	// no root reaches becomes a root itself; the cycle guard in buildNodeLocked
	// then keeps the tree finite.
	reached := make(map[string]bool, len(s.sessions))
	var walk func(id string)
	walk = func(id string) {
		if reached[id] {
			return
		}
		reached[id] = true
		for childID, child := range s.sessions {
			if child.ParentID == id {
				walk(childID)
			}
		}
	}
	for _, id := range ids {
		walk(id)
	}
	extra := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		if !reached[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	for _, id := range extra {
		ids = append(ids, id)
		reached[id] = true
	}
	s.mu.RUnlock()
	sort.Strings(ids)

	out := make([]*agentNode, 0, len(ids))
	for _, id := range ids {
		if node := s.agentFamily(id); node != nil {
			out = append(out, node)
		}
	}
	return out
}

func (s *store) snapshotSession(id string) *session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil
	}
	clone := *sess
	clone.Models = append([]string(nil), sess.Models...)
	clone.Timeline = append([]timelinePoint(nil), sess.Timeline...)
	clone.LastItems = append([]item(nil), sess.LastItems...)
	return &clone
}

func (s *store) snapshotEvents(sessionID string, limit int) []event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]event, 0, limit)
	for i := len(s.events) - 1; i >= 0 && len(out) < limit; i-- {
		if sessionID != "" && s.events[i].SessionID != sessionID {
			continue
		}
		out = append(out, s.events[i])
	}
	// Newest first is what the list shows.
	return out
}

func (s *store) snapshotRecent(limit int) []*snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*snapshot, 0, limit)
	for i := len(s.recent) - 1; i >= 0 && len(out) < limit; i-- {
		clone := *s.recent[i]
		clone.Items = nil
		out = append(out, &clone)
	}
	return out
}

// setLimits applies a new configuration without dropping collected data.
func (s *store) setLimits(next limits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = next
	s.evictSessionsLocked()
	if !next.keepItemsForNewest {
		for _, sess := range s.sessions {
			sess.LastItems = nil
		}
	}
}

// reset forgets every conversation. With keepDisk set the journal keeps what it
// holds, which is what a shutdown wants: the process stops answering with the
// previous run's picture, and the next start reads the same file back. Clearing
// the disk is what the operator's Reset button asks for.
func (s *store) reset(keepDisk bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The handle is released either way: after this the store holds no journal,
	// so a descriptor left open would be both a leak and a locked file.
	if s.journal != nil {
		if !keepDisk {
			s.journal.reset()
		}
		s.journal.close()
	}
	s.projections = make(map[string]*cachedProjection)
	s.sessions = make(map[string]*session)
	s.bySession = make(map[string][]*snapshot)
	s.pending = make(map[string]*snapshot)
	s.recent = nil
	s.events = nil
	// The handle is released either way, so nothing can write to a closed file
	// after the reset and the next configure opens a fresh one.
	s.journal = nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

// sortedKeys returns map keys in a stable order, so a scan does not depend on
// map iteration.

// reachedFromAny reports whether any id has a parent chain that terminates.

// setJournal replaces the persistent store under the store lock, so a request
// path that is already running cannot observe a half-swapped value.
func (s *store) setJournal(next *journal) *journal {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.journal
	s.journal = next
	return previous
}

// journalOf returns the current journal under the store lock.
func (s *store) journalOf() *journal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.journal
}
