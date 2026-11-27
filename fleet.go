package main

import "strings"

// The fleet view draws a tree of agents and one member card per agent. The
// ported client reads that from two projection keys: agentTeam, which lists the
// members of the family this conversation belongs to, and subagentCatalog, which
// lists the delegations the newest prompt shows. CPA resolves session identity
// from the client's own headers, so a child that names its parent appears here
// without the plugin guessing at anything.

// storeKey resolves the key a conversation is stored under. CPA names a session
// from where it found it ("header:<id>", "derived:..."), and the dashboard hands
// the plugin that full name, while a test or a caller may pass the bare id. The
// lookup tries the exact key first and then the suffix, so both forms work.
func (s *store) storeKey(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.sessions[id]; ok {
		return id
	}
	for key := range s.sessions {
		if strings.HasSuffix(key, ":"+id) {
			return key
		}
	}
	return id
}

// teamProjection builds the member roster of one conversation's family.
func (p *plugin) teamProjection(sessionID string) map[string]any {
	family := p.store.agentFamily(p.store.storeKey(sessionID))
	if family == nil {
		return nil
	}
	members := make([]map[string]any, 0, 8)
	collectMembers(family, p.store.storeKey(sessionID), &members)
	return map[string]any{
		"members": members,
		"tasks":   []any{},
	}
}

// collectMembers walks the family and renders one row per agent. The lead keeps
// its own role so the client can anchor the tree on it; a child is a teammate.
func collectMembers(node *agentNode, self string, out *[]map[string]any) {
	if node == nil {
		return
	}
	// The role says what the agent is, not where the tree starts. A conversation
	// that names a parent, when that parent never passed through this proxy, is
	// still a subagent: calling it the lead would relabel every delegated run as
	// the main one, and on the traffic this was measured against there were 48
	// such children and not one shared parent, so nothing would ever read as a
	// subagent at all.
	role := "teammate"
	if node.Role == "lead" || (!node.IsSubagent && node.ParentID == "") {
		role = "lead"
	}
	// A fork is not a teammate. CPA labels an lcp session as a fork of the point
	// where the prompt diverged, and that parent is a computed id that never
	// existed as a conversation. Listing that under the fleet's roster would claim
	// a delegation that never happened, so a fork is reported as the fork it is.
	fork := isForkSession(node)
	name := firstNonEmpty(node.AgentName, node.Label, node.ID)
	phase := "active"
	if node.Requests == 0 {
		phase = "provisioning"
	}
	row := map[string]any{
		"id":    node.ID,
		"name":  name,
		"role":  role,
		"phase": phase,
	}
	if fork {
		row["role"] = "fork"
	}
	if node.ParentID != "" {
		// The client groups by parent, so it needs the link even when the parent has
		// no row of its own.
		row["parentId"] = node.ParentID
	}
	*out = append(*out, row)
	for _, child := range node.Children {
		collectMembers(&child, self, out)
	}
}

// isForkSession reports a conversation CPA derived by branching a prompt. The
// lcp prefix is the id scheme CPA uses for that derivation, and a fork's parent is
// the divergence point, not an agent that ran.
func isForkSession(node *agentNode) bool {
	if node == nil {
		return false
	}
	if node.IsFork {
		return true
	}
	return strings.HasPrefix(node.ID, "lcp:")
}

// subagentCatalog lists the delegations the newest prompt of a conversation
// shows, so an agent that spawned work names it even before the child sends a
// request of its own.
func (p *plugin) subagentCatalog(sessionID string) []map[string]any {
	out := make([]map[string]any, 0, 4)
	seen := map[string]bool{}
	key := p.store.storeKey(sessionID)
	p.store.withSnapshots(key, func(snaps []*snapshot) {
		newest := snaps[len(snaps)-1]
		for _, it := range newest.Items {
			if it.Category != CatToolCall || !isAgentTool(it.Tool) {
				continue
			}
			target := firstNonEmpty(it.Target, it.Label)
			if target == "" || seen[target] {
				continue
			}
			seen[target] = true
			out = append(out, map[string]any{
				"id":   target,
				"name": target,
				"mode": "continuable",
			})
		}
	})
	return out
}
