package main

import (
	"encoding/json"
	"sort"
)

// The ported browser asks for a step's own content by sequence number, and reads
// it as events: "request/header" carries the tool schemas and the system-prompt
// size, "system/message" carries the system prompt text. CPA never hands a
// plugin a session transcript, so these events are built from the prompt the
// plugin parsed itself. A step whose text is no longer held answers with its
// counters, which the client renders as a meta-only row rather than a blank
// panel.

// historyPageRequest is what the client's session page face sends.
type historyPageRequest struct {
	SessionID string `json:"sessionId"`
	Part      string `json:"part"`
	Limit     int    `json:"limit"`
	Through   int    `json:"throughSeq"`
}

// historyRow is one event record. The client reads seq and type at the top level
// and the payload under data.
type historyRow struct {
	Seq  int            `json:"seq"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// historyPage answers the client's step-numbered read.
func (p *plugin) historyPage(raw []byte) any {
	var request historyPageRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return clientError("invalid request")
	}
	built, ok := p.store.projectionFor(request.SessionID)
	if !ok {
		return clientError("session not found")
	}
	rows := p.historyRows(request.SessionID, request.Part, &built)

	// A step-numbered read wants everything up to that step: the client scans the
	// page for the event whose seq it asked about.
	if request.Through > 0 {
		kept := make([]historyRow, 0, len(rows))
		for _, row := range rows {
			if row.Seq <= request.Through {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	limit := request.Limit
	if limit <= 0 || limit > len(rows) {
		limit = len(rows)
	}
	return clientEnvelope(map[string]any{
		"rev":     built.ContextTimeline.DetailRev,
		"part":    request.Part,
		"rows":    rows[len(rows)-limit:],
		"first":   len(rows) - limit,
		"last":    len(rows) - 1,
		"total":   len(rows),
		"atStart": limit == len(rows),
		"atEnd":   true,
	})
}

// historyRows renders one part as the event records the client reads.
func (p *plugin) historyRows(sessionID, part string, built *projection) []historyRow {
	timeline := built.ContextTimeline
	switch part {
	case "events":
		return genericRows(anySlice(timeline.Events))
	case "nodes":
		return genericRows(anySlice(timeline.Nodes))
	case "files":
		return genericRows(anySlice(timeline.FileOps))
	case "commands":
		return genericRows(anySlice(timeline.Commands))
	}

	// The default and the "requests", "header" and "content" parts all answer
	// with the per-step records: a header of tool schemas and prompt size, then
	// the system prompt text when it is still held.
	rows := make([]historyRow, 0, len(timeline.Requests)*2)
	p.store.withSnapshots(sessionID, func(snaps []*snapshot) {
		for index, snap := range snaps {
			seq := index + 1
			tools := make([]map[string]any, 0, 8)
			systemTokens := 0
			systemText := ""
			for _, it := range snap.Items {
				switch it.Category {
				case CatTools:
					schema := map[string]any{"name": it.Label}
					if it.Detail != "" {
						schema["description"] = it.Detail
					}
					if it.Text != "" {
						schema["parameters"] = it.Text
					}
					tools = append(tools, map[string]any{
						"name":        it.Label,
						"tokens":      int64(it.Chars),
						"schema":      schema,
						"description": it.Detail,
					})
				case CatSystem:
					systemTokens += it.Chars
					if systemText == "" && it.Text != "" {
						systemText = it.Text
					}
				}
			}
			if systemTokens == 0 {
				systemTokens = snap.Composition[CatSystem]
			}
			rows = append(rows, historyRow{
				Seq:  seq,
				Type: "request/header",
				Data: map[string]any{
					"header": map[string]any{
						"tools":        tools,
						"systemTokens": int64(systemTokens),
					},
				},
			})
			if systemText != "" {
				rows = append(rows, historyRow{
					Seq:  seq,
					Type: "system/message",
					Data: map[string]any{
						"message": map[string]any{
							"content": []map[string]any{{"type": "text", "text": systemText}},
						},
					},
				})
			}
		}
	})
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	return rows
}

// genericRows wraps a list of records in the event envelope, numbering them in
// order. The client accepts the record as its own event when the wrapper keys
// are absent, and this keeps a single shape for every part.
func genericRows(values []any) []historyRow {
	out := make([]historyRow, 0, len(values))
	for index, value := range values {
		data, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, historyRow{Seq: index + 1, Type: "node", Data: data})
	}
	return out
}
