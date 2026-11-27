package main

import "testing"

// A client sends the user turn as several blocks: an id wrapper first, then the
// question. The title must come from the block that says something, not from the
// first one and not from the session id.
func TestTitleComesFromALaterBlock(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"<acp tokens=\"9.3K\" type=\"text\">m44881</acp>"},` +
		`{"type":"text","text":"Why does the fleet view crash on a missing roster?"}]}]}`)

	c := parseOpenAIChat(body)
	if c == nil {
		t.Fatal("the body did not parse")
	}
	if c.Title == "" {
		t.Fatal("no title: the card would show the session id")
	}
	if c.Title != "Why does the fleet view crash on a missing roster?" {
		t.Errorf("title = %q, want the question from the second block", c.Title)
	}
}

// Claude sends the same shape with its own content blocks.
func TestClaudeTitleComesFromALaterBlock(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"<system-reminder>internal only</system-reminder>"},` +
		`{"type":"text","text":"Review the rate limiter for off-by-one gaps"}]}]}`)

	c := parseClaude(body)
	if c == nil {
		t.Fatal("the body did not parse")
	}
	if c.Title != "Review the rate limiter for off-by-one gaps" {
		t.Errorf("title = %q, want the second block", c.Title)
	}
}

// When nothing in the message names the work, the title stays empty and the
// caller falls back to the session id rather than inventing one.
func TestTitleStaysEmptyForPureIdentifiers(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"m00001"},` +
		`{"type":"text","text":"335ec9d7b080b7a7196d32d085655a3515d91a18"}]}]}`)

	c := parseOpenAIChat(body)
	if c == nil {
		t.Fatal("the body did not parse")
	}
	if c.Title != "" {
		t.Errorf("title = %q, want empty for two identifiers", c.Title)
	}
}
