package harness

import "strings"

// ShellEscape returns s wrapped in single quotes with any embedded single
// quotes escaped for POSIX shells: ' → '\''
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// ToolArgFields maps allowlisted tool names to the input field that contains
// the human-readable argument for display purposes.
var ToolArgFields = map[string]string{
	"Bash":      "command",
	"WebSearch": "query",
	"WebFetch":  "url",
	"Agent":     "description",
}
