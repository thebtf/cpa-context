package main

import "testing"

// The identifiers the operator's list showed: a bare id, an id in an acp wrapper
// carrying the token count, and a hash. None may become a title, and none may
// survive a replay.
func TestReadableTitleRejectsWhatTheListShowed(t *testing.T) {
	notNames := []string{
		"m45881",
		"m05657",
		"m00001",
		"m63115",
		"m05517",
		"335ec9d7b080b7a7196d32d085655a3515d91a18",
		"<acp tokens=\"9.3K\" type=\"text\">m44881</acp>",
	}
	for _, candidate := range notNames {
		if readableTitle(stripTags(candidate)) {
			t.Errorf("%q was accepted as a name", candidate)
		}
	}

	names := []string{
		"Complete assignment thoroughly:",
		"Prior model work/tool state available.",
		"Invoke skill session `load`.",
		"Подтяни спутник. В AIM-030 новый способ з...",
	}
	for _, candidate := range names {
		if !readableTitle(candidate) {
			t.Errorf("%q was rejected as a name", candidate)
		}
	}
}
