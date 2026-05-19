package harness

import "strings"

// ShellEscape returns s wrapped in single quotes with any embedded single
// quotes escaped for POSIX shells: ' → '\”
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
