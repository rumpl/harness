package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rumpl/harness"
)

func useCLI(t *testing.T, output string, exit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "output")
	argsPath := filepath.Join(dir, "args")
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %s\ncat %s\nexit %d\n", harness.ShellEscape(argsPath), harness.ShellEscape(outputPath), exit)
	binary := filepath.Join(dir, "codex")
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsPath
}

func TestTurnOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name, output, want string
		exit               int
	}{
		{name: "completed", output: `{"type":"turn.completed"}`},
		{name: "reconnected", output: "{\"type\":\"error\",\"message\":\"Reconnecting...\"}\n" + `{"type":"turn.completed"}`},
		{name: "unknown", output: "{\"type\":\"future.event\",\"error\":[1]}\n" + `{"type":"turn.completed"}`},
		{name: "empty", want: "without turn.completed"},
		{name: "message is not completion", output: `{"type":"item.completed","item":{"type":"agent_message","text":"answer"}}`, want: "without turn.completed"},
		{name: "failed", output: `{"type":"turn.failed","error":{"message":"secret"}}`, want: "codex turn failed"},
		{name: "invalid JSON", output: "{broken\n", want: "invalid Codex JSON event"},
		{name: "array", output: "[]\n", want: "invalid Codex JSON event"},
		{name: "null", output: "null\n", want: "missing event type"},
		{name: "missing type", output: "{}\n", want: "missing event type"},
		{name: "bad type", output: "{\"type\":4}\n", want: "invalid Codex JSON event"},
		{name: "nonzero exit", output: `{"type":"turn.completed"}`, exit: 2, want: "exit status 2"},
		{name: "new incomplete turn", output: "{\"type\":\"turn.completed\"}\n{\"type\":\"turn.started\"}\n", want: "without turn.completed"},
		{name: "whitespace", output: "\n \t\n{\"type\":\"turn.completed\"}\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			useCLI(t, tt.output, tt.exit)
			p := New("")
			for _, resume := range []bool{false, true} {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				var err error
				if resume {
					err = harness.Resume(ctx, p, "thread", "prompt", func(harness.Event) {})
				} else {
					err = harness.Run(ctx, p, "prompt", func(harness.Event) {})
				}
				if ctx.Err() != nil {
					t.Fatalf("watchdog expired: %v", err)
				}
				cancel()
				if tt.want == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error = %v, want %q", err, tt.want)
				}
				if err != nil && strings.Contains(err.Error(), "secret") {
					t.Fatal("error leaked stream contents")
				}
			}
		})
	}
}

func TestLargeEvents(t *testing.T) {
	text := strings.Repeat("界", 100000)
	tool := strings.Repeat("output", 20000)
	useCLI(t, fmt.Sprintf("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":%q}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"command_execution\",\"aggregated_output\":%q}}\n{\"type\":\"turn.completed\"}\n", text, tool), 0)
	var gotText, gotTool string
	err := harness.Run(t.Context(), New(""), "prompt", func(ev harness.Event) {
		switch ev.Type {
		case harness.EventText:
			gotText = ev.Text
		case harness.EventToolResult:
			gotTool = ev.ToolOutput
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotText != text || gotTool != tool {
		t.Fatal("large event lost or truncated")
	}
}

func TestResumeArguments(t *testing.T) {
	argsPath := useCLI(t, `{"type":"turn.completed"}`, 0)
	err := harness.Resume(t.Context(), New("model"), "thread'id", "don't interpolate $(echo unsafe)", func(harness.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "exec\nresume\nthread'id\n--json\n--dangerously-bypass-approvals-and-sandbox\n-m\nmodel\ndon't interpolate $(echo unsafe)\n"
	if string(args) != want {
		t.Fatalf("args = %q", args)
	}
}

func TestSessionIDSurvivesFailure(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		argsPath := useCLI(t, "{\"type\":\"thread.started\",\"thread_id\":\"retry-thread\"}\n{\"type\":\"turn.failed\"}\n", 0)
		ctx, cancel := context.WithCancel(t.Context())
		var sessionID string
		p := New("")
		err := harness.Run(ctx, p, "prompt", func(ev harness.Event) {
			if ev.Type == harness.EventSessionID {
				sessionID = ev.SessionID
				if canceled {
					cancel()
				}
			}
		})
		cancel()
		if err == nil || sessionID != "retry-thread" {
			t.Fatalf("error = %v, session = %q", err, sessionID)
		}
		if canceled && !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
		// The same provider must start with fresh validation state on resume.
		outputPath := filepath.Join(filepath.Dir(argsPath), "output")
		if err := os.WriteFile(outputPath, []byte(`{"type":"turn.completed"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := harness.Resume(t.Context(), p, sessionID, "retry", func(harness.Event) {}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStreamErrorsDoNotWaitForCLI(t *testing.T) {
	for _, tt := range []struct{ name, script, want string }{
		{"malformed", "echo '{broken'\nexec sleep 60", "invalid Codex JSON event"},
		{"failed", "echo '{\"type\":\"turn.failed\"}'\nexec sleep 60", "codex turn failed"},
		{"early EOF", "exec 1>&-\nexec sleep 60", "without turn.completed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			argsPath := useCLI(t, "", 0)
			binary := filepath.Join(filepath.Dir(argsPath), "codex")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+tt.script+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			err := harness.Run(ctx, New(""), "prompt", func(harness.Event) {})
			if err == nil || !strings.Contains(err.Error(), tt.want) || ctx.Err() != nil {
				t.Fatalf("error = %v, context = %v", err, ctx.Err())
			}
		})
	}
}
