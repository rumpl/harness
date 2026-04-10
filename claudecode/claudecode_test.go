package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

func TestName(t *testing.T) {
	p := New("claude-opus-4-6")
	if p.Name() != "claude-code" {
		t.Errorf("Name() = %q, want %q", p.Name(), "claude-code")
	}
}

func TestPrintCommand(t *testing.T) {
	t.Run("includes model and flags", func(t *testing.T) {
		p := New("claude-sonnet-4-6")
		cmd := p.PrintCommand("do something")
		for _, want := range []string{"claude-sonnet-4-6", "--output-format stream-json", "--print"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("PrintCommand missing %q in %q", want, cmd)
			}
		}
	})

	t.Run("shell-escapes prompt", func(t *testing.T) {
		p := New("claude-opus-4-6")
		cmd := p.PrintCommand("it's a test")
		if !strings.Contains(cmd, "'it'\\''s a test'") {
			t.Errorf("PrintCommand did not escape prompt: %q", cmd)
		}
	})

	t.Run("shell-escapes model", func(t *testing.T) {
		p := New("claude-opus-4-6")
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "--model 'claude-opus-4-6'") {
			t.Errorf("PrintCommand did not escape model: %q", cmd)
		}
	})

	t.Run("includes effort when set", func(t *testing.T) {
		p := New("claude-opus-4-6", WithEffort(EffortHigh))
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "--effort high") {
			t.Errorf("PrintCommand missing --effort high: %q", cmd)
		}
	})

	t.Run("omits effort when not set", func(t *testing.T) {
		p := New("claude-opus-4-6")
		cmd := p.PrintCommand("do something")
		if strings.Contains(cmd, "--effort") {
			t.Errorf("PrintCommand should not contain --effort: %q", cmd)
		}
	})

	t.Run("supports all effort levels", func(t *testing.T) {
		for _, e := range []Effort{EffortLow, EffortMedium, EffortHigh, EffortMax} {
			p := New("claude-opus-4-6", WithEffort(e))
			cmd := p.PrintCommand("test")
			if !strings.Contains(cmd, "--effort "+string(e)) {
				t.Errorf("PrintCommand missing --effort %s: %q", e, cmd)
			}
		}
	})
}

func TestInteractiveArgs(t *testing.T) {
	t.Run("includes binary and model", func(t *testing.T) {
		p := New("claude-sonnet-4-6")
		args := p.InteractiveArgs("")
		if args[0] != "claude" {
			t.Errorf("args[0] = %q, want claude", args[0])
		}
		if !contains(args, "claude-sonnet-4-6") || !contains(args, "--model") {
			t.Errorf("args missing model: %v", args)
		}
	})

	t.Run("includes effort when set", func(t *testing.T) {
		p := New("claude-opus-4-6", WithEffort(EffortLow))
		args := p.InteractiveArgs("")
		if !contains(args, "--effort") || !contains(args, "low") {
			t.Errorf("args missing effort: %v", args)
		}
	})

	t.Run("omits effort when not set", func(t *testing.T) {
		p := New("claude-opus-4-6")
		args := p.InteractiveArgs("")
		if contains(args, "--effort") {
			t.Errorf("args should not contain --effort: %v", args)
		}
	})
}

func TestParseStreamLine(t *testing.T) {
	p := New("claude-opus-4-6")

	t.Run("extracts text from assistant message", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "Hello world"},
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventText, Text: "Hello world"},
		})
	})

	t.Run("extracts result", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":   "result",
			"result": "Final answer",
		})
		events := p.ParseStreamLine(line)
		if len(events) != 1 || events[0].Type != harness.EventResult || events[0].Result != "Final answer" {
			t.Errorf("unexpected events: %+v", events)
		}
		if events[0].Usage != nil {
			t.Errorf("expected nil usage, got %+v", events[0].Usage)
		}
	})

	t.Run("extracts tool_use Bash", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{
						"type":  "tool_use",
						"name":  "Bash",
						"input": map[string]any{"command": "npm test"},
					},
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "Bash", ToolArgs: "npm test"},
		})
	})

	t.Run("skips non-allowlisted tools", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{
						"type":  "tool_use",
						"name":  "UnknownTool",
						"input": map[string]any{"foo": "bar"},
					},
				},
			},
		})
		events := p.ParseStreamLine(line)
		if len(events) != 0 {
			t.Errorf("expected empty events, got %+v", events)
		}
	})

	t.Run("returns empty for non-JSON", func(t *testing.T) {
		if events := p.ParseStreamLine("not json"); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
		if events := p.ParseStreamLine(""); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("returns empty for malformed JSON", func(t *testing.T) {
		if events := p.ParseStreamLine("{bad json"); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("independent model instances", func(t *testing.T) {
		p1 := New("model-a")
		p2 := New("model-b")
		cmd1 := p1.PrintCommand("test")
		cmd2 := p2.PrintCommand("test")
		if !strings.Contains(cmd1, "model-a") || strings.Contains(cmd1, "model-b") {
			t.Errorf("p1 command contains wrong model: %q", cmd1)
		}
		if !strings.Contains(cmd2, "model-b") || strings.Contains(cmd2, "model-a") {
			t.Errorf("p2 command contains wrong model: %q", cmd2)
		}
	})
}

// --- helpers ---

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func assertEqual(t *testing.T, got, want []harness.Event) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(events) = %d, want %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i].Type != want[i].Type || got[i].Text != want[i].Text ||
			got[i].Result != want[i].Result || got[i].ToolName != want[i].ToolName ||
			got[i].ToolArgs != want[i].ToolArgs {
			t.Errorf("event[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
