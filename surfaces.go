package main

import (
	"encoding/json"
	"sort"
	"time"
)

// The ported client has two more surfaces beyond the per-session view: the
// agent family and the cross-session overview. Both are computed from the same
// observations, in the shapes dsh publishes.

// fleetAnswer serves every fleet request: one member, one summary, or a batch.
func (p *plugin) fleetAnswer(raw []byte) any {
	var request struct {
		SessionID string `json:"sessionId"`
		Summary   bool   `json:"summary"`
		Full      bool   `json:"full"`
		History   bool   `json:"history"`
		Batch     *struct {
			IDs    []string `json:"ids"`
			Mode   string   `json:"mode"`
			RootID string   `json:"rootId"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return clientError("invalid request")
	}

	if request.Batch != nil {
		rows := map[string]any{}
		missing := make([]string, 0)
		for _, id := range request.Batch.IDs {
			detail, ok := p.fleetMember(id, request.Batch.Mode == "history")
			if !ok {
				missing = append(missing, id)
				continue
			}
			rows[id] = detail
		}
		return clientEnvelope(map[string]any{
			"rows":    rows,
			"missing": missing,
			"total":   len(request.Batch.IDs),
		})
	}

	if request.SessionID == "" {
		return clientError("sessionId is required")
	}
	if !request.Summary {
		if detail, ok := p.fleetMember(request.SessionID, request.History); ok {
			return clientEnvelope(detail)
		}
		return clientError("session not found")
	}

	sessions := p.store.snapshotSessions()
	root := request.SessionID
	members := make([]map[string]any, 0)
	counts := map[string]any{}
	createdAt := int64(0)
	for _, sess := range sessions {
		if sess.ParentID != root && sess.ID != root {
			continue
		}
		if sess.ID != root {
			members = append(members, map[string]any{
				"id":          sess.ID,
				"createdAt":   millis(sess.FirstSeen),
				"mode":        "unknown",
				"label":       sess.Title,
				"name":        sess.AgentName,
				"description": sess.Title,
				"phase":       "active",
			})
		} else {
			createdAt = millis(sess.FirstSeen)
		}
		counts[sess.ID] = map[string]any{
			"readAt":           millis(time.Now()),
			"messagesTotal":    0,
			"delegationsCount": 0,
		}
	}
	detail, ok := p.fleetMember(root, false)
	if !ok {
		return clientError("session not found")
	}
	summary := map[string]any{
		"rootId":     root,
		"detail":     detail,
		"members":    members,
		"familySize": len(members) + 1,
		"counts":     counts,
	}
	if createdAt > 0 {
		summary["rootCreatedAt"] = createdAt
	}
	return clientEnvelope(summary)
}

// fleetMember renders one agent's own record: who it is, what it was asked, and
// the delegations its prompt shows.
func (p *plugin) fleetMember(sessionID string, history bool) (map[string]any, bool) {
	snaps := p.store.snapshotsFor(sessionID)
	if len(snaps) == 0 {
		return nil, false
	}
	newest := snaps[len(snaps)-1]
	delegations := make([]map[string]any, 0)
	seen := map[string]bool{}
	seq := 0
	for _, it := range newest.Items {
		if it.Category != CatToolCall || !isAgentTool(it.Tool) {
			continue
		}
		target := firstNonEmpty(it.Target, it.Label)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		seq++
		delegations = append(delegations, map[string]any{
			"seq":  seq,
			"time": millis(newest.StartedAt),
			"to":   target,
			"mode": "unknown",
		})
	}

	out := map[string]any{
		"messages":    []any{},
		"delegations": delegations,
		"truncated":   false,
	}
	if newest.Title != "" {
		out["initialPrompt"] = newest.Title
	}
	if newest.Model != "" {
		out["descriptor"] = map[string]any{
			"mode":       "continuable",
			"agentModel": newest.Model,
			"provider":   newest.Provider,
		}
	}
	if len(snaps) > 1 {
		previous := snaps[len(snaps)-2]
		if previous.Title != "" {
			out["lastReply"] = map[string]any{
				"time": millis(previous.CompletedAt),
				"text": previous.Title,
			}
		}
	}
	_ = history
	return out, true
}

func isAgentTool(tool string) bool {
	switch tool {
	case "task", "Task", "delegate", "spawn_agent", "agent", "Agent", "fleet":
		return true
	}
	return false
}

// overviewAnswer serves the cross-session page: summary, daily series, the
// session list, tool and command tallies, and per-project totals.
func (p *plugin) overviewAnswer(raw []byte) any {
	var request struct {
		Part  string `json:"part"`
		Sort  string `json:"sort"`
		Page  int    `json:"page"`
		Tally string `json:"tally"`
		Scope *struct {
			Range any    `json:"range"`
			Day   string `json:"day"`
			Query string `json:"query"`
		} `json:"scope"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return clientError("invalid request")
	}

	sessions := p.store.snapshotSessions()
	revision := revisionOf(sessions)
	days := 30
	switch rangeOf(request) {
	case "today":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	// Scoping by range keeps the whole page consistent: a day filter that only
	// applied to one card would make the header disagree with the list.
	scoped := make([]*session, 0, len(sessions))
	for _, sess := range sessions {
		if sess.LastSeen.After(cutoff) {
			scoped = append(scoped, sess)
		}
	}

	switch request.Part {
	case "", "summary":
		return clientEnvelope(map[string]any{
			"revision":     revision,
			"pricingReady": false,
			"summary":      p.overviewSummary(scoped, days),
		})
	case "days":
		return clientEnvelope(map[string]any{
			"revision":     revision,
			"pricingReady": false,
			"days":         p.overviewDays(scoped, days),
		})
	case "page":
		return clientEnvelope(map[string]any{
			"revision":     revision,
			"pricingReady": false,
			"page":         p.overviewPage(scoped, request.Sort),
		})
	case "tally":
		return clientEnvelope(map[string]any{
			"revision":     revision,
			"pricingReady": false,
			"tally":        p.overviewTally(request.Tally),
		})
	case "projects":
		return clientEnvelope(map[string]any{
			"revision":     revision,
			"pricingReady": false,
			"projects":     p.overviewProjects(scoped),
		})
	}
	return clientError("unknown overview part")
}

func rangeOf(request struct {
	Part  string `json:"part"`
	Sort  string `json:"sort"`
	Page  int    `json:"page"`
	Tally string `json:"tally"`
	Scope *struct {
		Range any    `json:"range"`
		Day   string `json:"day"`
		Query string `json:"query"`
	} `json:"scope"`
},
) string {
	if request.Scope == nil {
		return "30d"
	}
	if value, ok := request.Scope.Range.(string); ok && value != "" {
		return value
	}
	if request.Scope.Day != "" {
		return "today"
	}
	return "30d"
}

func revisionOf(sessions []*session) string {
	if len(sessions) == 0 {
		return "0"
	}
	latest := sessions[0].LastSeen
	for _, sess := range sessions {
		if sess.LastSeen.After(latest) {
			latest = sess.LastSeen
		}
	}
	return latest.UTC().Format(time.RFC3339Nano)
}

func (p *plugin) overviewSummary(sessions []*session, days int) map[string]any {
	var tokens int64
	var turns, toolCalls int
	var hits, misses int
	for _, sess := range sessions {
		toolCalls += sess.ToolCalls
		tokens += sess.InputTokens + sess.OutputTokens
		turns += sess.Requests
		hits += sess.CacheHitRequests
		misses += sess.CacheMissRequests
	}
	parts := []map[string]any{
		{"key": "system", "value": sumField(sessions, func(s *session) int64 { return int64(s.Composition[CatSystem]) })},
		{"key": "tools", "value": sumField(sessions, func(s *session) int64 { return int64(s.Composition[CatTools]) })},
		{"key": "user", "value": sumField(sessions, func(s *session) int64 { return int64(s.Composition[CatUser]) })},
		{"key": "inject", "value": sumField(sessions, func(s *session) int64 { return int64(s.Composition[CatInjected]) })},
		{"key": "tool", "value": sumField(sessions, func(s *session) int64 { return int64(s.Composition[CatToolResult]) })},
		{"key": "assistant", "value": sumField(sessions, func(s *session) int64 {
			return int64(s.Composition[CatAssistant] + s.Composition[CatThinking] + s.Composition[CatToolCall])
		})},
	}
	var wallMs int64
	for _, sess := range sessions {
		if sess.LastSeen.After(sess.FirstSeen) {
			wallMs += sess.LastSeen.Sub(sess.FirstSeen).Milliseconds()
		}
	}
	summary := map[string]any{
		"sessions":         len(sessions),
		"sessionsTotal":    len(sessions),
		"sessionsArchived": 0,
		"archivedScoped":   0,
		"tokens":           tokens,
		"turns":            turns,
		"costUsd":          nil,
		"costSessions":     0,
		"usageSessions":    len(sessions),
		"cacheHit":         cacheHitOf(hits, misses),
		"tokenParts":       parts,
		"timing":           nil,
		"windowTiming": map[string]any{
			"calls":     turns,
			"toolCalls": toolCalls,
			"toolsMs":   0,
			"wallMs":    wallMs,
		},
		"coverage": map[string]any{
			"complete":    true,
			"remaining":   0,
			"failed":      0,
			"running":     false,
			"total":       len(sessions),
			"covered":     len(sessions),
			"unsupported": 0,
			"unreadable":  0,
		},
	}
	_ = days
	return summary
}

func sumField(sessions []*session, read func(*session) int64) int64 {
	var total int64
	for _, sess := range sessions {
		total += read(sess)
	}
	return total
}

func cacheHitOf(hits, misses int) any {
	if hits+misses == 0 {
		return nil
	}
	// The card appends the sign itself, so the value is the bare number.
	return formatPercent(100 * float64(hits) / float64(hits+misses))
}

func (p *plugin) overviewDays(sessions []*session, days int) []map[string]any {
	byDay := map[string]map[string]any{}
	order := make([]string, 0, days)
	now := time.Now()
	for offset := days - 1; offset >= 0; offset-- {
		key := now.AddDate(0, 0, -offset).Format("2006-01-02")
		order = append(order, key)
		byDay[key] = map[string]any{"key": key, "tokens": int64(0), "sessions": 0, "costUsd": nil}
	}
	for _, sess := range sessions {
		key := sess.LastSeen.Format("2006-01-02")
		day, ok := byDay[key]
		if !ok {
			continue
		}
		day["tokens"] = day["tokens"].(int64) + sess.InputTokens + sess.OutputTokens
		day["sessions"] = day["sessions"].(int) + 1
	}
	out := make([]map[string]any, 0, len(order))
	for _, key := range order {
		out = append(out, byDay[key])
	}
	return out
}

func (p *plugin) overviewPage(sessions []*session, sortBy string) map[string]any {
	rows := make([]map[string]any, 0, len(sessions))
	for _, sess := range sessions {
		parts := make([]map[string]any, 0, 4)
		for _, key := range []string{CatSystem, CatTools, CatUser, CatToolResult} {
			if value := sess.Composition[key]; value > 0 {
				parts = append(parts, map[string]any{"key": categoryKeyFor(key), "value": value})
			}
		}
		row := map[string]any{
			"id":        sess.ID,
			"title":     firstNonEmpty(sess.Title, sess.ID),
			"updatedAt": millis(sess.LastSeen),
			"running":   false,
			"archived":  false,
			"tokens":    sess.InputTokens + sess.OutputTokens,
			"turns":     sess.Requests,
			"steps":     sess.Requests,
			"parts":     parts,
			"total":     sess.LastContext,
			"costUsd":   nil,
			"model":     firstModel(sess),
		}
		if sess.Project != "" {
			row["cwd"] = sess.Project
			row["group"] = sess.Project
		}
		rows = append(rows, row)
	}
	switch sortBy {
	case "tokens":
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i]["tokens"].(int64) > rows[j]["tokens"].(int64)
		})
	case "context":
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i]["total"].(int64) > rows[j]["total"].(int64)
		})
	}
	return map[string]any{
		"items":      rows,
		"index":      0,
		"count":      len(rows),
		"total":      len(rows),
		"scopeTotal": len(rows),
		"groups":     []any{},
	}
}

func categoryKeyFor(category string) string {
	switch category {
	case CatSystem:
		return "system"
	case CatTools:
		return "tools"
	case CatUser:
		return "user"
	case CatToolResult:
		return "tool"
	case CatInjected:
		return "inject"
	case CatThinking:
		return "thinking"
	case CatAssistant:
		return "answers"
	}
	return category
}

func (p *plugin) overviewTally(field string) map[string]any {
	tally := map[string]int{}
	lastSeen := map[string]time.Time{}
	for _, sess := range p.store.snapshotSessions() {
		snaps := p.store.snapshotsFor(sess.ID)
		if len(snaps) == 0 {
			continue
		}
		newest := snaps[len(snaps)-1]
		for _, it := range newest.Items {
			var name string
			switch field {
			case "cmds":
				if it.Category == CatToolCall && isCommandTool(it.Tool) {
					name = firstWord(it.Target)
				}
			case "skills":
				if it.Category == CatInjected && it.Label != "" {
					name = it.Label
				}
			default:
				if it.Category == CatToolCall {
					name = it.Tool
				}
			}
			if name == "" {
				continue
			}
			tally[name]++
			if newest.StartedAt.After(lastSeen[name]) {
				lastSeen[name] = newest.StartedAt
			}
		}
	}
	stats := make([]map[string]any, 0, len(tally))
	for name, count := range tally {
		stats = append(stats, map[string]any{
			"name":     name,
			"n":        count,
			"sessions": 1,
			"last":     millis(lastSeen[name]),
		})
	}
	sort.SliceStable(stats, func(i, j int) bool {
		return stats[i]["n"].(int) > stats[j]["n"].(int)
	})
	return map[string]any{"field": field, "stats": stats}
}

func (p *plugin) overviewProjects(sessions []*session) []map[string]any {
	type entry struct {
		tokens   int64
		sessions int
	}
	byProject := map[string]*entry{}
	order := make([]string, 0)
	for _, sess := range sessions {
		key := firstNonEmpty(sess.Project, sess.Title, sess.ID)
		found, ok := byProject[key]
		if !ok {
			found = &entry{}
			byProject[key] = found
			order = append(order, key)
		}
		found.tokens += sess.InputTokens + sess.OutputTokens
		found.sessions++
	}
	out := make([]map[string]any, 0, len(order))
	for _, key := range order {
		entry := byProject[key]
		out = append(out, map[string]any{
			"key":      key,
			"tokens":   entry.tokens,
			"sessions": entry.sessions,
		})
	}
	return out
}

func formatPercent(value float64) string {
	whole := int(value)
	fraction := int((value - float64(whole)) * 100)
	return itoa(whole) + "." + pad2(fraction)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func pad2(value int) string {
	if value < 10 {
		return "0" + itoa(value)
	}
	return itoa(value)
}

// skillList reports the skill names the traffic mentions.
//
// CPA does not have a skill registry the plugin can read; what it can observe is
// a prompt that names a skill, which the composition already counts as an
// injected item. Reporting those names is honest: they are skills that were
// loaded in observed traffic, not the full catalog of the harness.
func (p *plugin) skillList() []map[string]any {
	seen := map[string]bool{}
	out := make([]map[string]any, 0)
	for _, sess := range p.store.snapshotSessions() {
		snaps := p.store.snapshotsFor(sess.ID)
		if len(snaps) == 0 {
			continue
		}
		for _, it := range snaps[len(snaps)-1].Items {
			if it.Category != CatInjected || it.Label == "" || seen[it.Label] {
				continue
			}
			seen[it.Label] = true
			out = append(out, map[string]any{
				"name":        it.Label,
				"description": "observed in traffic",
				"source":      "cpa-context",
			})
		}
	}
	return out
}
