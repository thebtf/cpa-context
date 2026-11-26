package main

import "testing"

// A client puts machine instruction into the user turn. The conversation must be
// named after what the person asked, not after the wrapper the harness added.
func TestTitleIgnoresHarnessWrappers(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "system reminder first",
			text: "<system-reminder>\nThe user has not used this tool yet.\n</system-reminder>\n\nInvestigate why the chart bars are flat.",
			want: "Investigate why the chart bars are flat.",
		},
		{
			name: "unclosed reminder swallows nothing real after it",
			text: "<system-reminder>\ninternal note only\n",
			want: "",
		},
		{
			name: "bare closing tag is not a title",
			text: "</user>",
			want: "",
		},
		{
			name: "topic tag is skipped for the sentence after it",
			text: "Topic\n\nSummarize the LLM-wiki format for project docs.",
			want: "Summarize the LLM-wiki format for project docs.",
		},
		{
			name: "tool result block is not the conversation",
			text: "<tool_result>\nmain.go go.mod\n</tool_result>\n\nWhat does the user prefer in coding style?",
			want: "What does the user prefer in coding style?",
		},
		{
			name: "plain first line is unchanged",
			text: "Add cache-hit percentage to the header",
			want: "Add cache-hit percentage to the header",
		},
		{
			name: "inline tag is stripped and its text kept",
			text: "<b>Fix</b> the parser",
			want: "Fix the parser",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleFromText(tc.text); got != tc.want {
				t.Errorf("titleFromText(%q)\n got %q\nwant %q", tc.text, got, tc.want)
			}
		})
	}
}
