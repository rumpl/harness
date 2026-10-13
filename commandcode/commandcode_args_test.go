package commandcode

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

// TestArgsSingleSource pins the exact argv produced by the single flag builder
// for the three shapes: no optional flags, model+effort, and resume.
func TestArgsSingleSource(t *testing.T) {
	tests := []struct {
		name      string
		provider  *provider
		sessionID string
		prompt    string
		want      []string
	}{
		{
			name:     "no flags",
			provider: New("").(*provider),
			prompt:   "hello world",
			want:     []string{"cmd", "-p", "--output-format", "json", "--yolo", "hello world"},
		},
		{
			name:     "model and effort",
			provider: New("m1", WithEffort("low")).(*provider),
			prompt:   "hi",
			want:     []string{"cmd", "-p", "--output-format", "json", "--yolo", "--model", "m1", "--effort", "low", "hi"},
		},
		{
			name:      "resume",
			provider:  New("", WithEffort("high")).(*provider),
			sessionID: "sess-123",
			prompt:    "again",
			want:      []string{"cmd", "-p", "--output-format", "json", "--yolo", "--effort", "high", "-r", "sess-123", "again"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.provider.args(tt.sessionID, tt.prompt)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("args(%q, %q) = %q, want %q", tt.sessionID, tt.prompt, got, tt.want)
			}
		})
	}
}

// TestPrintCommandArgsParity asserts the rendered shell string carries the same
// tokens, in the same order, as the argv from the single flag builder.
func TestPrintCommandArgsParity(t *testing.T) {
	tests := []struct {
		name      string
		provider  *provider
		sessionID string
		prompt    string
	}{
		{name: "no flags", provider: New("").(*provider), prompt: "plain prompt"},
		{name: "model and effort", provider: New("m1", WithEffort("low")).(*provider), prompt: "say 'hi'"},
		{name: "resume", provider: New("m1", WithEffort("high")).(*provider), sessionID: "sess-123", prompt: "again"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := tt.provider.args(tt.sessionID, tt.prompt)
			rendered := tt.provider.printCommand(tt.sessionID, tt.prompt)

			cursor := 0
			for i, tok := range argv {
				// A token appears in the rendered command either verbatim (the
				// executable, flags, fixed values) or shell-escaped (the
				// caller-supplied values). Find whichever form, in order.
				idx := strings.Index(rendered[cursor:], tok)
				if idx < 0 {
					idx = strings.Index(rendered[cursor:], harness.ShellEscape(tok))
				}
				if idx < 0 {
					t.Fatalf("token %d (%q) not found in order in rendered command %q", i, tok, rendered)
				}
				cursor += idx + len(tok)
			}
		})
	}
}
