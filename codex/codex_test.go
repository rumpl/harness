package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

func TestName(t *testing.T) {
	p := New("gpt-5.4-mini")
	if p.Name() != "codex" {
		t.Errorf("Name() = %q, want %q", p.Name(), "codex")
	}
}

func TestPrintCommand(t *testing.T) {
	t.Run("includes model and --json flag", func(t *testing.T) {
		p := New("gpt-5.4-mini")
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "gpt-5.4-mini") {
			t.Errorf("PrintCommand missing model: %q", cmd)
		}
		if !strings.Contains(cmd, "--json") {
			t.Errorf("PrintCommand missing --json: %q", cmd)
		}
	})

	t.Run("shell-escapes prompt", func(t *testing.T) {
		p := New("gpt-5.4-mini")
		cmd := p.PrintCommand("it's a test")
		if !strings.Contains(cmd, "'it'\\''s a test'") {
			t.Errorf("PrintCommand did not escape prompt: %q", cmd)
		}
	})

	t.Run("shell-escapes model", func(t *testing.T) {
		p := New("gpt-5.4-mini")
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "-m 'gpt-5.4-mini'") {
			t.Errorf("PrintCommand did not escape model: %q", cmd)
		}
	})

	t.Run("omits model flag when empty", func(t *testing.T) {
		p := New("")
		cmd := p.PrintCommand("do something")
		if strings.Contains(cmd, " -m ") {
			t.Errorf("PrintCommand should not contain -m: %q", cmd)
		}
	})
}

func TestInteractiveArgs(t *testing.T) {
	t.Run("includes model", func(t *testing.T) {
		p := New("gpt-5.4-mini")
		args := p.InteractiveArgs("")
		if args[0] != "codex" {
			t.Errorf("args[0] = %q, want codex", args[0])
		}
		if !contains(args, "gpt-5.4-mini") || !contains(args, "--model") {
			t.Errorf("args missing model: %v", args)
		}
	})

	t.Run("omits model when empty", func(t *testing.T) {
		p := New("")
		args := p.InteractiveArgs("")
		if contains(args, "--model") {
			t.Errorf("args should not contain model: %v", args)
		}
	})
}

func TestParseStreamLine(t *testing.T) {
	p := New("gpt-5.4-mini")

	t.Run("extracts text and result from item.completed agent_message", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"type": "agent_message", "text": "Hello world"},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventText, Text: "Hello world"},
			{Type: harness.EventResult, Result: "Hello world"},
		})
	})

	t.Run("extracts tool call from item.started command_execution", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "item.started",
			"item": map[string]any{"type": "command_execution", "command": "npm test"},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "Bash", ToolArgs: "npm test"},
		})
	})

	t.Run("extracts usage from turn.completed", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "turn.completed",
			"usage": map[string]any{
				"input_tokens":        float64(8975),
				"cached_input_tokens": float64(0),
				"output_tokens":       float64(14),
			},
		})
		events := p.ParseStreamLine(line)
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].Type != harness.EventResult {
			t.Errorf("Type = %q, want %q", events[0].Type, harness.EventResult)
		}
		u := events[0].Usage
		if u == nil {
			t.Fatal("expected usage, got nil")
		}
		if u.InputTokens != 8975 {
			t.Errorf("InputTokens = %d, want 8975", u.InputTokens)
		}
		if u.OutputTokens != 14 {
			t.Errorf("OutputTokens = %d, want 14", u.OutputTokens)
		}
	})

	t.Run("returns empty for turn.completed without usage", func(t *testing.T) {
		line := jsonStr(map[string]any{"type": "turn.completed"})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
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

	t.Run("returns empty for unrecognized event types", func(t *testing.T) {
		line := jsonStr(map[string]any{"type": "unknown_event"})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("returns empty for malformed JSON", func(t *testing.T) {
		if events := p.ParseStreamLine("{bad json"); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles item.completed with missing text", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"type": "agent_message"},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles item.started with missing command", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "item.started",
			"item": map[string]any{"type": "command_execution"},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles item.completed with non-agent_message type", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"type": "other_type", "text": "foo"},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles item.started with non-command_execution type", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "item.started",
			"item": map[string]any{"type": "other_type", "command": "foo"},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
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
