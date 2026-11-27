package main

import "testing"

// The operator saw a column of hashes: the roster named a derived session by its
// own id, and that id is a sha256. A hash is not a name.
func TestMemberNameIsNeverAHash(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true

	cases := []struct {
		name string
		node *agentNode
	}{
		{"derived fork with no label", &agentNode{ID: "lcp:v1:581ba2d7442cf1ba1d5900f53ccc6c335a24581f98ec915a08425fe7a9b89cd7"}},
		{"plain hash", &agentNode{ID: "335ec9d7b080b7a7196d32d085655a3515d91a18e36225ca062f6a5b6ff9e47d"}},
	}
	for _, tc := range cases {
		got := p.memberName(tc.node)
		if got == "" {
			t.Errorf("%s: no name", tc.name)
			continue
		}
		if got == tc.node.ID {
			t.Errorf("%s: the name is the technical id %q", tc.name, got)
		}
		if looksLikeHash(got) {
			t.Errorf("%s: the name %q is a hash", tc.name, got)
		}
	}

	// A label wins over everything.
	if got := p.memberName(&agentNode{ID: "lcp:v1:abc123", AgentName: "researcher"}); got != "researcher" {
		t.Errorf("name = %q, want the agent's own label", got)
	}
	// A derived session with no label is called what it is.
	if got := p.memberName(&agentNode{ID: "lcp:v1:581ba2d7442cf1ba1d5900f53ccc6c33"}); got != "branch" {
		t.Errorf("name = %q, want branch for a derived session with no label", got)
	}
}

func TestLooksLikeHash(t *testing.T) {
	if !looksLikeHash("lcp:v1:581ba2d7442cf1ba1d5900f53ccc6c335a24581f98ec915") {
		t.Error("a scheme prefix plus hex was not reported as a hash")
	}
	if looksLikeHash("researcher") {
		t.Error("a word was reported as a hash")
	}
	if looksLikeHash("header:session-56c8855a-4912-4d3e") {
		t.Error("a readable session id was reported as a hash")
	}
}
