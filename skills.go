package main

import "strings"

// A skill load arrives as injected context: the harness prepends the skill's
// instructions to a message. CPA has no skill registry to ask, but the shape of
// that injection is recognisable, and the dashboard has a category for it. This
// file decides which injected items are skill loads and what to call them.
//
// The test is deliberately conservative. A misclassified injection would show up
// as a skill that never existed, which is worse than leaving it as injected
// context, so only the markers that a skill preamble actually carries count.

// skillMarkers are the phrases a skill preamble introduces itself with.
var skillMarkers = []string{
	"<skill", "skill instructions", "loaded skill", "skills available",
	"available skills", "skill catalog", "/skill", "activate the skill",
}

// isSkillInjection reports whether an injected item is a skill load, and returns
// the skill's name when one can be read.
func isSkillInjection(label, text string) (string, bool) {
	lowerLabel := strings.ToLower(label)
	lowerText := strings.ToLower(text)

	name := ""
	marked := false
	for _, marker := range skillMarkers {
		if strings.Contains(lowerLabel, marker) {
			marked = true
			break
		}
		// Only the head of a long message is checked: a skill preamble comes
		// first, while a mention deep inside a conversation is incidental.
		head := lowerText
		if len(head) > 4096 {
			head = head[:4096]
		}
		if strings.Contains(head, marker) {
			marked = true
			break
		}
	}
	if !marked {
		return "", false
	}

	// A name is what a skill is called: the slug after a skill marker, the first
	// line of a marked block, or the label the provider gave the injected part.
	if name == "" && label != "" && !isGenericLabel(label) {
		name = label
	}
	if name == "" {
		name = skillNameFromText(text)
	}
	if name == "" {
		name = "skill"
	}
	return name, true
}

func isGenericLabel(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "", "injected", "user", "system", "developer", "skill", "<skill>":
		return true
	}
	return false
}

// skillNameFromText reads the first meaningful line of a skill preamble.
func skillNameFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "#")
		trimmed = strings.TrimPrefix(trimmed, "<skill")
		trimmed = strings.TrimPrefix(trimmed, ">")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" {
			continue
		}
		if len([]rune(trimmed)) > 60 {
			trimmed = string([]rune(trimmed)[:60])
		}
		return trimmed
	}
	return ""
}
