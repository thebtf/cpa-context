package main

import "testing"

// A parent cycle must not hide a family. Every member has an existing parent, so
// a naive root scan reports nothing and the conversations disappear from the
// dashboard entirely.
func TestFamilyRootsSurviveAParentCycle(t *testing.T) {
	s := newStore(defaultLimits())
	for _, entry := range []struct{ id, parent string }{
		{"a", "b"},
		{"b", "a"},
		{"c", "a"},
	} {
		s.sessions[entry.id] = &session{ID: entry.id, ParentID: entry.parent, Requests: 1}
	}

	roots := s.familyRoots()
	if len(roots) == 0 {
		t.Fatal("a cyclic family produced no roots, so it would vanish from the list")
	}
	seen := map[string]bool{}
	var count func(node *agentNode)
	count = func(node *agentNode) {
		if node == nil || seen[node.ID] {
			return
		}
		seen[node.ID] = true
		for i := range node.Children {
			count(&node.Children[i])
		}
	}
	for _, root := range roots {
		count(root)
	}
	for _, id := range []string{"a", "b", "c"} {
		if !seen[id] {
			t.Errorf("conversation %q is missing from every family", id)
		}
	}
}

// A gap in the parent chain must keep the orphan visible as its own root.
func TestFamilyRootsKeepOrphans(t *testing.T) {
	s := newStore(defaultLimits())
	s.sessions["child"] = &session{ID: "child", ParentID: "gone", Requests: 1}
	roots := s.familyRoots()
	if len(roots) != 1 || roots[0].ID != "child" {
		t.Fatalf("orphan roots = %v, want the orphan itself", roots)
	}
}
