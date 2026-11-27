package main

import "testing"

// CPA derives an lcp session by branching a prompt at the point where it
// diverges, and gives it a parent id computed from that point. The parent never
// ran as a conversation. Calling such a session a teammate would claim a
// delegation that never happened.
func TestAForkIsReportedAsAFork(t *testing.T) {
	if !isForkSession(&agentNode{ID: "lcp:v1:cda44f7c50b6b25"}) {
		t.Error("an lcp conversation is not reported as a fork")
	}
	if !isForkSession(&agentNode{ID: "header:abc", IsFork: true}) {
		t.Error("a node flagged as a fork is not reported as one")
	}
	if isForkSession(&agentNode{ID: "header:abc"}) {
		t.Error("a plain conversation is reported as a fork")
	}
	if isForkSession(nil) {
		t.Error("a nil node is reported as a fork")
	}
}

// A delegated conversation keeps the teammate role: it is not a fork, however
// unusual its parent looks.
func TestADelegatedConversationIsNotAFork(t *testing.T) {
	if isForkSession(&agentNode{ID: "header:kid-a", ParentID: "header:lead", IsSubagent: true}) {
		t.Error("a subagent was reported as a fork")
	}
}
