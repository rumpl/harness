package dockeragent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

func TestName(t *testing.T) {
	p := New("coder")
	if p.Name() != "docker-agent" {
		t.Errorf("Name() = %q, want %q", p.Name(), "docker-agent")
	}
}

func TestPrintCommand(t *testing.T) {
	t.Run("includes image and --json flag", func(t *testing.T) {
		p := New("coder")
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "coder") {
			t.Errorf("PrintCommand missing image: %q", cmd)
		}
		if !strings.Contains(cmd, "--json") {
			t.Errorf("PrintCommand missing --json: %q", cmd)
		}
		if !strings.Contains(cmd, "--yolo") {
			t.Errorf("PrintCommand missing --yolo: %q", cmd)
		}
	})

	t.Run("shell-escapes prompt", func(t *testing.T) {
		p := New("coder")
		cmd := p.PrintCommand("it's a test")
		if !strings.Contains(cmd, "'it'\\''s a test'") {
			t.Errorf("PrintCommand did not escape prompt: %q", cmd)
		}
	})

	t.Run("shell-escapes image", func(t *testing.T) {
		p := New("my-image")
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "'my-image'") {
			t.Errorf("PrintCommand did not escape image: %q", cmd)
		}
	})
}

func TestInteractiveArgs(t *testing.T) {
	p := New("coder")
	args := p.InteractiveArgs("")
	if args[0] != "docker-agent" {
		t.Errorf("args[0] = %q, want docker-agent", args[0])
	}
	if !contains(args, "coder") || !contains(args, "--yolo") {
		t.Errorf("args missing image or --yolo: %v", args)
	}
}

func TestParseStreamLine(t *testing.T) {
	p := New("coder")

	t.Run("extracts text from agent_choice", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":    "agent_choice",
			"content": "Hello world",
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventText, Text: "Hello world"},
		})
	})

	t.Run("extracts tool call from tool_call", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "tool_call",
			"tool_call": map[string]any{
				"id":   "toolu_123",
				"type": "function",
				"function": map[string]any{
					"name":      "shell",
					"arguments": `{"cmd": "ls -l"}`,
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "shell", ToolArgs: "ls -l"},
		})
	})

	t.Run("extracts usage from token_usage", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "token_usage",
			"usage": map[string]any{
				"input_tokens":  float64(3872),
				"output_tokens": float64(54),
				"cost":          0.02071,
				"last_message": map[string]any{
					"input_tokens":        float64(3872),
					"output_tokens":       float64(54),
					"cached_input_tokens": float64(100),
				},
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
		if u.InputTokens != 3872 {
			t.Errorf("InputTokens = %d, want 3872", u.InputTokens)
		}
		if u.OutputTokens != 54 {
			t.Errorf("OutputTokens = %d, want 54", u.OutputTokens)
		}
		if u.CacheReadInputTokens != 100 {
			t.Errorf("CacheReadInputTokens = %d, want 100", u.CacheReadInputTokens)
		}
	})

	t.Run("returns empty for stream_stopped", func(t *testing.T) {
		line := jsonStr(map[string]any{"type": "stream_stopped"})
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

	t.Run("handles agent_choice with empty content", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":    "agent_choice",
			"content": "",
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles tool_call with missing function", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":      "tool_call",
			"tool_call": map[string]any{"id": "toolu_123"},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles tool_call with empty name", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "tool_call",
			"tool_call": map[string]any{
				"function": map[string]any{
					"name":      "",
					"arguments": `{"cmd": "ls"}`,
				},
			},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("tool_call falls back to raw JSON for unknown tools", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "tool_call",
			"tool_call": map[string]any{
				"function": map[string]any{
					"name":      "unknown_tool",
					"arguments": `{"foo": "bar"}`,
				},
			},
		})
		events := p.ParseStreamLine(line)
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].ToolArgs != `{"foo": "bar"}` {
			t.Errorf("ToolArgs = %q, want raw JSON", events[0].ToolArgs)
		}
	})

	t.Run("handles read_file tool", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "tool_call",
			"tool_call": map[string]any{
				"function": map[string]any{
					"name":      "read_file",
					"arguments": `{"path": "/tmp/test.txt"}`,
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "read_file", ToolArgs: "/tmp/test.txt"},
		})
	})

	t.Run("handles search tool", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "tool_call",
			"tool_call": map[string]any{
				"function": map[string]any{
					"name":      "search",
					"arguments": `{"pattern": "TODO", "path": "."}`,
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "search", ToolArgs: "TODO"},
		})
	})

	t.Run("independent image instances", func(t *testing.T) {
		p1 := New("image-a")
		p2 := New("image-b")
		cmd1 := p1.PrintCommand("test")
		cmd2 := p2.PrintCommand("test")
		if !strings.Contains(cmd1, "image-a") || strings.Contains(cmd1, "image-b") {
			t.Errorf("p1 command contains wrong image: %q", cmd1)
		}
		if !strings.Contains(cmd2, "image-b") || strings.Contains(cmd2, "image-a") {
			t.Errorf("p2 command contains wrong image: %q", cmd2)
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
