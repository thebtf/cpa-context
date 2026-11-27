package main

import "testing"

// The operator's session list was a column of identifiers and wrappers:
// "<acp tokens=\"9.3K\" type=\"text\">m44881</acp>", "<system-reminder>", and a
// bare "m00001". None of them say what the conversation was about.
func TestTitleSkipsIdentifiersAndWrapperIds(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "acp wrapper around an id",
			text: "<acp tokens=\"9.3K\" type=\"text\">m44881</acp>",
			want: "",
		},
		{
			name: "acp wrapper, then the real question",
			text: "<acp tokens=\"9.3K\" type=\"text\">m44881</acp>\n\nWhy does the fleet view crash?",
			want: "Why does the fleet view crash?",
		},
		{
			name: "bare short id",
			text: "m00001",
			want: "",
		},
		{
			name: "hex hash",
			text: "335ec9d7b080b7a7196d32d085655a3515d91a18",
			want: "",
		},
		{
			name: "reminder with only an id inside",
			text: "<system-reminder>\n<acp tokens=\"11.2K\">m44526</acp>\n</system-reminder>",
			want: "",
		},
		{
			name: "header line then the task",
			text: "Tool command inventory changed.\n\nUpdate the panel to show programs.",
			want: "Tool command inventory changed.",
		},
		{
			name: "opens with a pasted block, names itself later",
			text: "```\nfunc main() {}\n```\n\nExplain how the compressor chooses a window size.",
			want: "Explain how the compressor chooses a window size.",
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
