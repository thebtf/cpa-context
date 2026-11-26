package main

import (
	"encoding/json"
	"strings"
)

// The prompt replays every tool call's arguments, so the file a call touched,
// the command it ran and the number of lines it added are all observable on the
// wire. This file turns those arguments into the facts the dashboard needs:
// File Activity rows and the command log.

// toolTarget returns what a call acted on, plus the line counts a write changed.
// An unknown tool yields an empty target and no line deltas.
func toolTarget(tool, args string) (string, int, int) {
	parsed := parseArgs(args)
	if parsed == nil {
		return "", 0, 0
	}
	switch tool {
	case "grep", "Grep", "glob", "Glob", "search":
		pattern, _ := parsed["pattern"].(string)
		if dir, ok := parsed["path"].(string); ok && dir != "" {
			if pattern != "" {
				return dir + " · " + pattern, 0, 0
			}
			return dir, 0, 0
		}
		return pattern, 0, 0
	case "bash", "Bash", "shell", "run_terminal_cmd", "execute", "terminal":
		command, _ := parsed["command"].(string)
		return collapseSpace(command), 0, 0
	case "edit", "Edit":
		path := pathOfArgs(parsed)
		removed := lineCount(stringField(parsed, "old_string"))
		added := lineCount(stringField(parsed, "new_string"))
		return path, added, removed
	case "write", "Write", "create_file":
		return pathOfArgs(parsed), lineCount(stringField(parsed, "content")), 0
	case "str_replace_editor":
		return pathOfArgs(parsed), lineCount(stringField(parsed, "new_str")), lineCount(stringField(parsed, "old_str"))
	default:
		return pathOfArgs(parsed), 0, 0
	}
}

func parseArgs(raw string) map[string]any {
	if raw == "" || raw == "{}" {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	return parsed
}

// pathOfArgs reads the file a call names. Providers disagree on the spelling,
// so the common keys are tried in order.
func pathOfArgs(args map[string]any) string {
	for _, key := range []string{"file_path", "filePath", "path", "notebook_path"} {
		if value, ok := args[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func stringField(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

// lineCount matches the dsh file activity measure: the number of lines a block
// of text occupies, counting a trailing newline as ending the last line rather
// than starting an empty one.
func lineCount(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if strings.HasSuffix(text, "\n") {
		return count
	}
	return count + 1
}

func collapseSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
