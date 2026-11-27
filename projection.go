package main

import (
	"sort"
	"strings"
	"time"
)

// The dashboard in web/ is a port of the dsh-context client. Its components read
// one projection per session and expect the shapes dsh publishes, so this file
// renders those shapes from what the plugin observed on the wire: the parsed
// prompt of every request plus the provider's usage counters.
//
// The vocabulary is dsh's, not CPA's: a "node" is one piece of the prompt as the
// model reads it, a "request" is one model call, and an "event" is a moment the
// window changed for a reason other than the model's own reply.

// Category keys used by SurfaceNode.cat. They are dsh's five message
// categories; system prompt and tool schemas are not nodes because dsh shows
// them as two fixed groups instead.
const (
	dshUser      = "user"
	dshInject    = "inject"
	dshSkill     = "skill"
	dshAssistant = "assistant"
	dshTool      = "tool"
)

// projection is the /cpa-context/session payload: one entry per projection key
// the client asks for.
type projection struct {
	ContextTimeline  *timelineProjection `json:"contextTimeline"`
	ContextPressure  map[string]any      `json:"contextPressure"`
	ContextBreakdown map[string]any      `json:"contextBreakdown"`
	ContextHeaders   map[string]any      `json:"contextHeaders"`
	TokenUsage       map[string]any      `json:"tokenUsage"`
}

type timelineProjection struct {
	OK      bool   `json:"ok"`
	Model   string `json:"model,omitempty"`
	Current struct {
		System              int64 `json:"system"`
		Tools               int64 `json:"tools"`
		User                int64 `json:"user"`
		Inject              int64 `json:"inject"`
		Skill               int64 `json:"skill"`
		Assistant           int64 `json:"assistant"`
		AssistantReasoning  int64 `json:"assistantReasoning"`
		AssistantAnswerTok  int64 `json:"assistantAnswerTokens"`
		AssistantCallTokens int64 `json:"assistantCallTokens"`
		Tool                int64 `json:"tool"`
		Total               int64 `json:"total"`
	} `json:"current"`
	Nodes        []surfaceNode  `json:"nodes"`
	DroppedNodes int            `json:"droppedNodes"`
	Archive      []surfaceNode  `json:"archive"`
	Requests     []requestRec   `json:"requests"`
	Events       []eventRec     `json:"events"`
	FileOps      []fileOpRec    `json:"fileOps"`
	Commands     []commandRec   `json:"commands"`
	CommandStats *commandStats  `json:"commandStats,omitempty"`
	Systems      []systemNode   `json:"systems,omitempty"`
	Timing       *timingTotals  `json:"timing,omitempty"`
	ToolTally    map[string]int `json:"toolTally,omitempty"`
	ProgramTally map[string]int `json:"programTally,omitempty"`
	ToolCalls    int            `json:"toolCalls,omitempty"`
	Images       int            `json:"images,omitempty"`
	HumanInputs  int            `json:"humanInputs,omitempty"`
	Answers      int            `json:"answers,omitempty"`
	LastUser     string         `json:"lastUser,omitempty"`
	Counts       *timelineCounts `json:"counts,omitempty"`
	Last         *timelineLast   `json:"last,omitempty"`
	DetailRev    int             `json:"detailRev,omitempty"`
	MaxOutput    int64           `json:"maxOutputTokens,omitempty"`
	// Cost carries the estimated price of this session. It is absent when the
	// model is not in the price catalog, which the card renders as unpriced.
	Cost     map[string]any `json:"cost,omitempty"`
	CostRead map[string]any `json:"costRead,omitempty"`
}

type timelineCounts struct {
	Turns        int `json:"turns"`
	Steps        int `json:"steps"`
	Injects      int `json:"injects"`
	Compactions  int `json:"compactions"`
	Prunes       int `json:"prunes"`
	Skills       int `json:"skills"`
	Events       int `json:"events"`
	ModelSwitches int `json:"modelSwitches"`
}

type timelineLast struct {
	Seq   int   `json:"seq"`
	Total int64 `json:"total"`
}

// surfaceNode is one addressable piece of the prompt in model read order.
type surfaceNode struct {
	Seq         int      `json:"seq"`
	Time        int64    `json:"time,omitempty"`
	Cat         string   `json:"cat"`
	Tokens      int64    `json:"tokens"`
	Reasoning   int64    `json:"reasoning,omitempty"`
	AnswerTok   int64    `json:"answerTokens,omitempty"`
	CallTokens  int64    `json:"callTokens,omitempty"`
	Imgs        int      `json:"imgs,omitempty"`
	Form        string   `json:"form,omitempty"`
	Name        string   `json:"name,omitempty"`
	Text        string   `json:"text,omitempty"`
	Tool        string   `json:"tool,omitempty"`
	Err         bool     `json:"err,omitempty"`
	Skill       string   `json:"skill,omitempty"`
	Calls       []string `json:"calls,omitempty"`
	Agent       string   `json:"agent,omitempty"`
}

type requestRec struct {
	Turn      int   `json:"turn"`
	Step      int   `json:"step"`
	Time      int64 `json:"time"`
	Seq       int   `json:"seq"`
	Total     int64 `json:"total"`
	Prompt    int64 `json:"prompt"`
	System    int64 `json:"system"`
	Tools     int64 `json:"tools"`
	User      int64 `json:"user"`
	Inject    int64 `json:"inject"`
	Skill     int64 `json:"skill"`
	Assistant int64 `json:"assistant"`
	Tool      int64 `json:"tool"`
	CacheRead int64 `json:"cacheRead,omitempty"`
	Output    int64 `json:"output,omitempty"`
	StepCount int   `json:"stepCount,omitempty"`
	Net       int64 `json:"net,omitempty"`
	// Chart is the per-category split of this request, used to draw its bar.
	Chart map[string]int `json:"-"`
}

type eventRec struct {
	Seq     int    `json:"seq"`
	Time    int64  `json:"time"`
	Kind    string `json:"kind"`
	Form    string `json:"form,omitempty"`
	Tokens  int64  `json:"tokens,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Name    string `json:"name,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Turn    int    `json:"turn,omitempty"`
	Step    int    `json:"step,omitempty"`
}

type fileOpRec struct {
	Seq     int    `json:"seq"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool"`
	Time    int64  `json:"time,omitempty"`
	Err     bool   `json:"err"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Detail  string `json:"detail,omitempty"`
	Hits    int    `json:"hits,omitempty"`
	Pattern bool   `json:"pattern,omitempty"`
}

type commandRec struct {
	Seq     int    `json:"seq"`
	Time    int64  `json:"time"`
	Tool    string `json:"tool"`
	Cmd     string `json:"cmd,omitempty"`
	Start   int64  `json:"start"`
	End     int64  `json:"end,omitempty"`
	Status  string `json:"status"`
	Exit    int    `json:"exit,omitempty"`
	Err     bool   `json:"err"`
	OutChars int   `json:"outChars,omitempty"`
	Turn    int    `json:"turn,omitempty"`
	Step    int    `json:"step,omitempty"`
}

type commandStats struct {
	Runs   int   `json:"runs"`
	Failed int   `json:"failed"`
	Ms     int64 `json:"ms"`
	Bg     int   `json:"bg"`
}

type systemNode struct {
	Seq    int   `json:"seq"`
	Time   int64 `json:"time"`
	Tokens int64 `json:"tokens"`
}

type timingTotals struct {
	WallMs    int64                    `json:"wallMs"`
	TTFTMs    int64                    `json:"ttftMs"`
	GenMs     int64                    `json:"genMs"`
	Calls     int                      `json:"calls"`
	ToolsMs   int64                    `json:"toolsMs"`
	IdleMs    int64                    `json:"idleMs"`
	ToolCalls int                      `json:"toolCalls"`
	Tools     map[string]toolTimingTot `json:"tools"`
}

type toolTimingTot struct {
	Calls int   `json:"calls"`
	Ms    int64 `json:"ms"`
}

// buildProjection renders the projections for one conversation from its
// snapshots, newest last.
//
// Every request body carries the whole conversation so far, so the newest
// snapshot already describes the current window, every node in it, every file
// operation and every command. Only the per-request timeline needs the older
// snapshots: those are the individual model calls.
func buildProjection(sessionID string, snaps []*snapshot, events []event) projection {
	// Only the newest snapshots are worth keeping: the client walks the timeline
	// from the head and each one replays the whole conversation.
	const maxPoints = 200
	if len(snaps) > maxPoints {
		snaps = snaps[len(snaps)-maxPoints:]
	}
	if len(snaps) == 0 {
		return projection{ContextTimeline: &timelineProjection{OK: false}}
	}

	newest := snaps[len(snaps)-1]
	timeline := &timelineProjection{
		OK:        true,
		Model:     newest.Model,
		DetailRev: len(snaps),
		MaxOutput: int64(newest.MaxTokens),
		Nodes:     []surfaceNode{},
		Archive:   []surfaceNode{},
		Requests:  make([]requestRec, 0, len(snaps)),
		Events:    []eventRec{},
		FileOps:   []fileOpRec{},
		Commands:  []commandRec{},
	}

	// The newest prompt defines the current window: what is in it right now,
	// split the way the composition bar shows it.
	current := &timeline.Current
	// A replayed snapshot carries counters but no prompt text: the text of an
	// old request is superseded by the next one, so the journal does not store
	// it. The composition is enough to rebuild the window and a per-category
	// split, which keeps the dashboard useful across a restart instead of empty.
	if len(newest.Items) == 0 && len(newest.Composition) > 0 {
		buildNodesFromComposition(timeline, newest)
	}
	for index, it := range newest.Items {
		tokens := int64(it.Chars)
		switch it.Category {
		case CatSystem:
			current.System += tokens
		case CatTools:
			current.Tools += tokens
		case CatUser:
			current.User += tokens
		case CatInjected:
			current.Inject += tokens
		case CatSkill:
			current.Skill += tokens
		case CatThinking:
			current.Assistant += tokens
			current.AssistantReasoning += tokens
		case CatAssistant:
			current.Assistant += tokens
			current.AssistantAnswerTok += tokens
		case CatToolCall:
			current.Assistant += tokens
			current.AssistantCallTokens += tokens
		case CatToolResult:
			current.Tool += tokens
		case CatImage:
			timeline.Images++
		}

		category := nodeCategory(it.Category)
		if category == "" {
			continue
		}
		node := surfaceNode{
			Seq:    index + 1,
			Time:   millis(newest.StartedAt),
			Tokens: tokens,
			Cat:    category,
			Form:   it.Category,
			Name:   it.Label,
			Text:   it.Text,
		}
		if it.Category == CatToolCall || it.Category == CatToolResult {
			node.Tool = it.Tool
			node.Err = it.Err
		}
		if it.Category == CatThinking {
			node.Reasoning = tokens
		}
		timeline.Nodes = append(timeline.Nodes, node)
	}
	current.Total = newest.InputTokens

	var turns int
	var compactions, prunes, injects, skills int
	toolTally := map[string]int{}
	for index, snap := range snaps {
		step := index + 1
		record := requestRec{
			Seq:       step,
			Step:      step,
			Time:      millis(snap.StartedAt),
			Total:     snap.InputTokens,
			Prompt:    snap.InputTokens,
			CacheRead: snap.CacheReadTokens,
			Output:    snap.OutputTokens,
		}
		// The split comes from the stored counters, not from the items: a
		// snapshot whose prompt text was dropped still carries its composition,
		// and the chart needs every request, not only the newest.
		record.System = int64(snap.Composition[CatSystem])
		record.Tools = int64(snap.Composition[CatTools])
		record.User = int64(snap.Composition[CatUser])
		record.Inject = int64(snap.Composition[CatInjected])
		record.Assistant = int64(snap.Composition[CatAssistant] +
			snap.Composition[CatThinking] + snap.Composition[CatToolCall])
		record.Tool = int64(snap.Composition[CatToolResult])
		if len(snap.Composition) > 0 {
			chart := make(map[string]int, len(snap.Composition))
			for category, value := range snap.Composition {
				if value > 0 {
					chart[category] = value
				}
			}
			record.Chart = chart
		}
		// A step that opens with a user message starts a new turn; the rest
		// continue the turn the agent is already working in.
		if hasUserMessage(snap) {
			turns++
			record.Turn = turns
			record.Net = snap.InputTokens - previousInput(snaps, index)
		} else {
			record.Turn = max(turns, 1)
		}
		if snap.Compaction {
			compactions++
		}
		if snap.LatencyMs > 0 {
			timeline.Timing = ensureTiming(timeline.Timing)
			timeline.Timing.Calls++
			timeline.Timing.GenMs += snap.LatencyMs
			timeline.Timing.WallMs += snap.LatencyMs
		}
		if snap.TTFTMs > 0 {
			timeline.Timing = ensureTiming(timeline.Timing)
			timeline.Timing.TTFTMs += snap.TTFTMs
		}
		timeline.Requests = append(timeline.Requests, record)
	}

	// File operations, commands and tool tallies read from the newest prompt
	// only: it replays every call of the conversation, so accumulating across
	// snapshots would repeat each call once per request.
	timeline.FileOps = append(timeline.FileOps, fileOpsOfSnap(newest)...)
	commands, programs, commandsMs, _ := commandsOfSnap(newest)
	timeline.Commands = append(timeline.Commands, commands...)
	for name, count := range programs {
		timeline.ProgramTally = addTally(timeline.ProgramTally, name, count)
	}
	for _, it := range newest.Items {
		switch it.Category {
		case CatToolCall:
			if it.Tool != "" {
				toolTally[it.Tool]++
			}
		case CatInjected:
			if strings.HasPrefix(it.Label, "skill:") {
				skills++
				injects++
			}
		}
	}
	if len(toolTally) > 0 {
		timeline.ToolTally = toolTally
	}
	for name := range toolTally {
		timeline.ToolCalls += toolTally[name]
	}
	if len(commands) > 0 {
		stats := &commandStats{Runs: len(commands), Ms: commandsMs}
		for _, cmd := range commands {
			if cmd.Err {
				stats.Failed++
			}
		}
		timeline.CommandStats = stats
	}
	if newest.CacheReadTokens == 0 && newest.InputTokens > 0 {
		prunes = 0
	}
	timeline.HumanInputs = turns
	timeline.Answers = countCategory(newest.Items, CatAssistant)
	timeline.Counts = &timelineCounts{
		Turns:       max(turns, 1),
		Steps:       len(snaps),
		Injects:     injects,
		Compactions: compactions,
		Prunes:      prunes,
		Skills:      skills,
		Events:      len(events),
	}
	last := snaps[len(snaps)-1]
	timeline.Last = &timelineLast{Seq: len(snaps), Total: last.InputTokens}
	timeline.LastUser = newest.Title
	timeline.Systems = []systemNode{{
		Seq:    1,
		Time:   millis(snaps[0].StartedAt),
		Tokens: current.System,
	}}

	for _, ev := range events {
		kind := dshEventKind(ev.Kind)
		if kind == "" {
			continue
		}
		timeline.Events = append(timeline.Events, eventRec{
			Seq:    len(timeline.Events) + 1,
			Time:   millis(ev.At),
			Kind:   kind,
			Tokens: ev.Delta,
			Detail: ev.Detail,
		})
	}

	pressureTokens := newest.InputTokens + newest.OutputTokens
	return projection{
		ContextTimeline: timeline,
		ContextPressure: map[string]any{
			"pressureTokens": pressureTokens,
		},
		ContextBreakdown: map[string]any{
			"systemTokens":  current.System,
			"toolsTokens":   current.Tools,
			"messageTokens": current.User + current.Inject + current.Skill + current.Assistant + current.Tool,
		},
		ContextHeaders: map[string]any{
			"headers": headerRecords(snaps),
		},
		TokenUsage: map[string]any{
			"uncachedInputTokens": newest.UncachedTokens,
			"outputTokens":        newest.OutputTokens,
			"cacheReadTokens":     newest.CacheReadTokens,
			"cacheWriteTokens":    newest.CacheWriteTokens,
		},
	}
}

// buildNodesFromComposition fills the window and one node per category when the
// prompt text is no longer held.
func buildNodesFromComposition(timeline *timelineProjection, snap *snapshot) {
	current := &timeline.Current
	seq := 0
	for _, category := range categoryOrder {
		tokens := int64(snap.Composition[category])
		if tokens <= 0 {
			continue
		}
		switch category {
		case CatSystem:
			current.System += tokens
		case CatTools:
			current.Tools += tokens
		case CatUser:
			current.User += tokens
		case CatInjected:
			current.Inject += tokens
		case CatThinking:
			current.Assistant += tokens
			current.AssistantReasoning += tokens
		case CatAssistant:
			current.Assistant += tokens
			current.AssistantAnswerTok += tokens
		case CatToolCall:
			current.Assistant += tokens
			current.AssistantCallTokens += tokens
		case CatToolResult:
			current.Tool += tokens
		}
		nodeCat := nodeCategory(category)
		if nodeCat == "" {
			continue
		}
		seq++
		timeline.Nodes = append(timeline.Nodes, surfaceNode{
			Seq:    seq,
			Time:   millis(snap.StartedAt),
			Tokens: tokens,
			Cat:    nodeCat,
			Form:   category,
			Name:   category,
		})
	}
	current.Total = snap.InputTokens
}

func previousInput(snaps []*snapshot, index int) int64 {
	if index == 0 {
		return 0
	}
	return snaps[index-1].InputTokens
}

func addTally(tally map[string]int, name string, count int) map[string]int {
	if tally == nil {
		tally = map[string]int{}
	}
	tally[name] += count
	return tally
}

func countCategory(items []item, category string) int {
	count := 0
	for _, it := range items {
		if it.Category == category {
			count++
		}
	}
	return count
}

func ensureTiming(t *timingTotals) *timingTotals {
	if t == nil {
		return &timingTotals{Tools: map[string]toolTimingTot{}}
	}
	if t.Tools == nil {
		t.Tools = map[string]toolTimingTot{}
	}
	return t
}

// nodeCategory maps a prompt item onto one of dsh's five node categories.
// System prompt and tool schemas return empty: dsh renders those as two fixed
// groups rather than as nodes.
func nodeCategory(category string) string {
	switch category {
	case CatUser:
		return dshUser
	case CatInjected:
		return dshInject
	case CatSkill:
		return dshSkill
	case CatAssistant, CatThinking, CatToolCall:
		return dshAssistant
	case CatToolResult:
		return dshTool
	default:
		return ""
	}
}

func dshEventKind(kind string) string {
	switch kind {
	case eventCompaction:
		return "compaction"
	case eventModelChange:
		return "model"
	case eventCacheMiss, eventTrim:
		return "prune"
	case eventFirstRequest:
		return "inject"
	}
	return ""
}

func hasUserMessage(snap *snapshot) bool {
	for _, it := range snap.Items {
		if it.Category == CatUser {
			return true
		}
	}
	return false
}

func headerRecords(points []*snapshot) []map[string]any {
	seen := map[int64]bool{}
	out := make([]map[string]any, 0, len(points))
	for _, snap := range points {
		stamp := millis(snap.StartedAt)
		if seen[stamp] {
			continue
		}
		seen[stamp] = true
		tools := make([]map[string]any, 0)
		systemTokens := int64(0)
		for _, it := range snap.Items {
			switch it.Category {
			case CatSystem:
				systemTokens += int64(it.Chars)
			case CatTools:
				tools = append(tools, map[string]any{
					"name":   it.Label,
					"tokens": int64(it.Chars),
				})
			}
		}
		out = append(out, map[string]any{
			"seq":          len(out) + 1,
			"time":         stamp,
			"systemTokens": systemTokens,
			"tools":        tools,
		})
	}
	return out
}

// fileOpsOfSnap lists the file operations the newest prompt replays, with the
// result of each call resolved from the matching tool result.
func fileOpsOfSnap(snap *snapshot) []fileOpRec {
	out := make([]fileOpRec, 0)
	for index, it := range snap.Items {
		if it.Category != CatToolCall {
			continue
		}
		kind := fileOpKind(it.Tool)
		if kind == "" || it.Target == "" {
			continue
		}
		op := fileOpRec{
			Seq:     index + 1,
			Path:    it.Target,
			Kind:    kind,
			Tool:    it.Tool,
			Time:    millis(snap.StartedAt),
			Added:   it.Added,
			Removed: it.Removed,
			Detail:  it.Detail,
		}
		out = append(out, op)
	}
	return out
}

func fileOpKind(tool string) string {
	switch tool {
	case "read", "read_file", "Read", "read_image", "str_replace_editor":
		return "read"
	case "write", "edit", "Write", "Edit", "create_file":
		return "write"
	case "grep", "glob", "search", "Grep", "Glob":
		return "search"
	}
	return ""
}

// commandsOfSnap lists the shell and code executions the newest prompt replays,
// with the wall time of the request that ran them.
func commandsOfSnap(snap *snapshot) ([]commandRec, map[string]int, int64, int) {
	out := make([]commandRec, 0)
	programs := map[string]int{}
	var duration int64
	var failed int
	for index, it := range snap.Items {
		if it.Category != CatToolCall || !isCommandTool(it.Tool) {
			continue
		}
		rec := commandRec{
			Seq:    index + 1,
			Time:   millis(snap.StartedAt),
			Tool:   it.Tool,
			Cmd:    it.Target,
			Start:  millis(snap.StartedAt),
			Status: "completed",
		}
		if snap.CompletedAt.After(snap.StartedAt) {
			rec.End = millis(snap.CompletedAt)
			duration += snap.CompletedAt.Sub(snap.StartedAt).Milliseconds()
		}
		if name := firstWord(it.Target); name != "" {
			programs[name]++
		}
		if it.Err {
			rec.Err = true
			rec.Status = "failed"
			failed++
		}
		out = append(out, rec)
	}
	return out, programs, duration, failed
}

func isCommandTool(tool string) bool {
	switch tool {
	case "bash", "Bash", "shell", "run_code", "run_terminal_cmd", "execute", "terminal":
		return true
	}
	return false
}

func firstWord(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func millis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// sessionRows lists the conversations the dashboard can open.
func sessionRows(sessions []*session) []map[string]any {
	known := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		known[sess.ID] = true
	}
	rows := make([]map[string]any, 0, len(sessions))
	for _, sess := range sessions {
		row := map[string]any{
			"id":                sess.ID,
			"title":             sess.Title,
			"model":             firstModel(sess),
			"cwd":               sess.Project,
			"origin":            originOf(sess),
			"firstSeen":         sess.FirstSeen,
			"lastSeen":          sess.LastSeen,
			"requests":          sess.Requests,
			"failed":            sess.Failed,
			"lastContextTokens": sess.LastContext,
			"cacheHitRequests":  sess.CacheHitRequests,
			"cacheMissRequests": sess.CacheMissRequests,
			"cacheReadTokens":   sess.CacheRead,
			"inputTokens":       sess.InputTokens,
			"outputTokens":      sess.OutputTokens,
		}
		if parent := sess.ParentID; parent != "" {
			// CPA prefixes a parent with the child's transport. The graph needs the
			// actual row key when the same instance also observed the other transport.
			if sess.IsSubagent && !known[parent] {
				prefix, id, _ := strings.Cut(parent, ":")
				if prefix == "claude" && known["header:"+id] && sess.ID != "header:"+id {
					parent = "header:" + id
				} else if prefix == "header" && known["claude:"+id] && sess.ID != "claude:"+id {
					parent = "claude:" + id
				}
			}
			row["parentId"] = parent
		}
		// The fleet reads identity from these fields: it names an agent by its own
		// label and decides what a conversation is from the two flags. A row without
		// them is an anonymous root, so every family looked like one agent called
		// "main" no matter what the traffic said.
		if sess.AgentName != "" {
			row["agentName"] = sess.AgentName
		}
		if sess.ClientType != "" {
			row["clientType"] = sess.ClientType
		}
		if sess.IsSubagent {
			row["isSubagent"] = true
		}
		if sess.IsFork {
			row["isFork"] = true
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := rows[i]["lastSeen"].(time.Time)
		right, _ := rows[j]["lastSeen"].(time.Time)
		return left.After(right)
	})
	return rows
}

func originOf(sess *session) string {
	if sess.IsSubagent {
		return "subagent"
	}
	if sess.IsFork || isDerivedForkID(sess.ID) {
		return "fork"
	}
	return "root"
}

// isDerivedForkID reports the id scheme CPA uses for a session it derived by
// branching a prompt. Such a session has a parent id computed from the point of
// divergence, and that parent never ran as a conversation, so the two must not be
// drawn as a delegation.
func isDerivedForkID(id string) bool {
	return strings.HasPrefix(id, "lcp:")
}

func firstModel(sess *session) string {
	if len(sess.Models) > 0 {
		return sess.Models[0]
	}
	return ""
}

