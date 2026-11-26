package main

import (
	"encoding/json"
	"strings"
)

// The ported client lazily asks for more detail than the head projection
// carries, and it does so through its own routes. These serve the same shapes
// from data the plugin already holds, so a card shows its content instead of
// offering a retry.
// The suffix matches whatever prefix the client's page URL gives it; the full
// path is what the host is told to dispatch, under the management namespace.
const (
	routeDetail  = "/api/dsh-context/detail"
	routeFacts   = "/api/dsh-context/facts"
	routeDelta   = "/api/dsh-context/delta"
	routeHistory = "/api/dsh-context/history"
	routeRecom   = "/api/dsh-context/recom"
	routeVersion = "/api/dsh-context/version"
	routeFleet    = "/api/dsh-context/fleet"
	routeOverview = "/api/dsh-context/overview"
	routeBalance  = "/api/dsh-context/balance"
	routeSkills   = "/api/dsh-context/skills"

	clientDetailPath  = "/cpa-context" + routeDetail
	clientFactsPath   = "/cpa-context" + routeFacts
	clientDeltaPath   = "/cpa-context" + routeDelta
	clientHistoryPath = "/cpa-context" + routeHistory
	clientRecomPath   = "/cpa-context" + routeRecom
	clientVersionPath = "/cpa-context" + routeVersion
	clientFleetPath    = "/cpa-context" + routeFleet
	clientOverviewPath = "/cpa-context" + routeOverview
	clientBalancePath  = "/cpa-context" + routeBalance
	clientSkillsPath   = "/cpa-context" + routeSkills
)

// clientEnvelope is the shape the ported client unwraps: ok plus a value.
func clientEnvelope(value any) map[string]any {
	return map[string]any{"ok": true, "value": value, "unchanged": false}
}

func clientError(message string) map[string]any {
	return map[string]any{"ok": false, "error": message}
}

// detailBody serves the whole detail record for one session.
func (p *plugin) detailBody(sessionID string) any {
	built, ok := p.store.projectionFor(sessionID)
	if !ok {
		return clientError("session not found")
	}
	timeline := built.ContextTimeline
	body := map[string]any{
		"rev":      timeline.DetailRev,
		"head":     timeline,
		"requests": seriesWithChart(timeline.Requests),
		"events":   timeline.Events,
		"fileOps":  timeline.FileOps,
		"commands": timeline.Commands,
		"nodes":    timeline.Nodes,
		"archive":  timeline.Archive,
	}
	if timeline.CommandStats != nil {
		body["commandStats"] = timeline.CommandStats
	}
	return clientEnvelope(body)
}

// factsBody serves one or more named parts. The client asks for "head" and
// "series" for the chart, then for a single part when a card mounts.
func (p *plugin) factsBody(raw []byte) any {
	var request struct {
		SessionID string   `json:"sessionId"`
		Parts     []string `json:"parts"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return clientError("invalid request")
	}
	built, ok := p.store.projectionFor(request.SessionID)
	if !ok {
		return clientError("session not found")
	}
	timeline := built.ContextTimeline

	parts := map[string]any{}
	for _, part := range request.Parts {
		switch part {
		case "", "head":
			parts["head"] = timeline
		case "series":
			parts["series"] = seriesWithChart(timeline.Requests)
		case "requests":
			parts["requests"] = timeline.Requests
		case "nodes":
			parts["nodes"] = map[string]any{"nodes": timeline.Nodes, "droppedNodes": 0}
		case "events":
			parts["events"] = timeline.Events
		case "spans":
			parts["spans"] = []any{}
		case "files":
			parts["files"] = map[string]any{"fileOps": timeline.FileOps, "fileOpsFloor": 0}
		case "commands":
			entry := map[string]any{"commands": timeline.Commands, "commandsFloor": 0}
			if timeline.CommandStats != nil {
				entry["commandStats"] = timeline.CommandStats
			}
			parts["commands"] = entry
		}
	}
	return clientEnvelope(map[string]any{
		"rev":   timeline.DetailRev,
		"asOfSeq": len(timeline.Requests),
		"live":  true,
		"parts": parts,
	})
}

// historyBody serves a page of one part. The plugin keeps every request of a
// session in memory, so a page is a slice of the head projection.
func (p *plugin) historyBody(raw []byte) any {
	var request struct {
		SessionID string `json:"sessionId"`
		Part      string `json:"part"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return clientError("invalid request")
	}
	built, ok := p.store.projectionFor(request.SessionID)
	if !ok {
		return clientError("session not found")
	}
	timeline := built.ContextTimeline

	var rows []any
	switch request.Part {
	case "requests":
		rows = anySlice(seriesWithChart(timeline.Requests))
	case "events":
		rows = anySlice(timeline.Events)
	case "nodes":
		rows = anySlice(timeline.Nodes)
	case "files":
		rows = anySlice(timeline.FileOps)
	case "commands":
		rows = anySlice(timeline.Commands)
	default:
		rows = []any{}
	}
	limit := request.Limit
	if limit <= 0 || limit > len(rows) {
		limit = len(rows)
	}
	return clientEnvelope(map[string]any{
		"rev":     timeline.DetailRev,
		"part":    request.Part,
		"rows":    rows[len(rows)-limit:],
		"first":   len(rows) - limit,
		"last":    len(rows) - 1,
		"total":   len(rows),
		"atStart": limit == len(rows),
		"atEnd":   true,
	})
}

// deltaBody answers a step comparison. The ported view only needs the nodes it
// would show for one step, which the head projection already carries.
func (p *plugin) deltaBody(raw []byte) any {
	var request struct {
		SessionID string `json:"sessionId"`
		Seq       int    `json:"seq"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return clientError("invalid request")
	}
	built, ok := p.store.projectionFor(request.SessionID)
	if !ok {
		return clientError("session not found")
	}
	timeline := built.ContextTimeline
	return clientEnvelope(map[string]any{
		"added":      timeline.Nodes,
		"viewNodes":  timeline.Nodes,
		"counts":     map[string]int{},
		"tokens":     map[string]int{},
		"removedCounts": map[string]int{},
		"tokenUp":    map[string]int{},
	})
}

// recomBody replaces the upstream sponsor feed with an empty list: the ported
// card renders nothing rather than fetching from someone else's service.
func recomBody() any {
	return clientEnvelope([]any{})
}

// versionBody reports the plugin version the page runs.
func versionBody() any {
	return clientEnvelope(map[string]any{"version": pluginVersion})
}

func anySlice[T any](values []T) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// handleClientRoute serves one of the ported client's own routes. It answers
// nil when the path is not one of them, so the caller can keep matching.
func (p *plugin) handleClientRoute(path string, method string, raw []byte) (any, bool) {
	switch {
	case strings.HasSuffix(path, routeDetail):
		return p.detailBody(sessionFieldOf(raw, "sessionId")), true
	case strings.HasSuffix(path, routeFacts):
		return p.factsBody(raw), true
	case strings.HasSuffix(path, routeHistory):
		return p.historyBody(raw), true
	case strings.HasSuffix(path, routeDelta):
		return p.deltaBody(raw), true
	case strings.HasSuffix(path, routeRecom):
		return recomBody(), true
	case strings.HasSuffix(path, routeVersion):
		return versionBody(), true
	case strings.HasSuffix(path, routeFleet):
		return p.fleetAnswer(raw), true
	case strings.HasSuffix(path, routeOverview):
		return p.overviewAnswer(raw), true
	case strings.HasSuffix(path, routeBalance):
		// CPA keeps no account balance for a plugin to read. Answering with both
		// arms absent is the shape the card understands as "not available",
		// which is better than a 404 the card logs as a failure.
		return clientEnvelope(map[string]any{"account": nil, "api": nil}), true
	case strings.HasSuffix(path, routeSkills):
		return clientEnvelope(map[string]any{"skills": p.skillList()}), true
	}
	return nil, false
}

// sessionFieldOf reads the session id out of a JSON body, for the routes that
// only take one field.
func sessionFieldOf(raw []byte, field string) string {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	value, _ := body[field].(string)
	return value
}


// seriesWithChart turns the per-request records into the shape the chart reads.
// The split travels as a separate map so the record keeps its plain fields and
// the chart still gets one bar per request.
func seriesWithChart(requests []requestRec) []map[string]any {
	out := make([]map[string]any, 0, len(requests))
	for _, record := range requests {
		entry := map[string]any{
			"seq":       record.Seq,
			"step":      record.Step,
			"turn":      record.Turn,
			"time":      record.Time,
			"total":     record.Total,
			"prompt":    record.Prompt,
			"system":    record.System,
			"tools":     record.Tools,
			"user":      record.User,
			"inject":    record.Inject,
			"skill":     record.Skill,
			"assistant": record.Assistant,
			"tool":      record.Tool,
		}
		if record.CacheRead > 0 {
			entry["cacheRead"] = record.CacheRead
		}
		if record.Output > 0 {
			entry["output"] = record.Output
		}
		if record.Net != 0 {
			entry["net"] = record.Net
		}
		if len(record.Chart) > 0 {
			entry["composition"] = record.Chart
		}
		out = append(out, entry)
	}
	return out
}
