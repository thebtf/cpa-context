package main

import "testing"

// The skill classifier decides whether a category appears at all, so both of its
// answers are asserted: a skill preamble is one, and ordinary injected context is
// not, because a false skill would invent a category that does not exist.
func TestSkillInjectionClassification(t *testing.T) {
	cases := []struct {
		name    string
		label   string
		text    string
		want    bool
		wantNam string
	}{
		{
			name:  "skill block",
			label: "developer",
			text:  "<skill>\nname: release-checklist\nRun the checks before tagging.\n</skill>",
			want:  true,
		},
		{
			name:  "available skills catalog",
			label: "developer",
			text:  "Available skills:\n- release-checklist\n- postmortem",
			want:  true,
		},
		{
			name:  "slash command load",
			label: "user",
			text:  "/skill postmortem",
			want:  true,
		},
		{
			name:  "ordinary injected note",
			label: "developer",
			text:  "Today's date is 2026-11-26. Answer in Russian.",
			want:  false,
		},
		{
			name:  "a mention deep inside a long message is incidental",
			label: "user",
			text:  longText(5000) + "and then we discussed the skill catalog",
			want:  false,
		},
	}
	for _, tc := range cases {
		name, ok := isSkillInjection(tc.label, tc.text)
		if ok != tc.want {
			t.Errorf("%s: isSkillInjection = %v, want %v", tc.name, ok, tc.want)
		}
		if ok && name == "" {
			t.Errorf("%s: a skill was detected with no name", tc.name)
		}
	}
}

func longText(size int) string {
	out := make([]byte, size)
	for i := range out {
		out[i] = 'x'
	}
	return string(out)
}
