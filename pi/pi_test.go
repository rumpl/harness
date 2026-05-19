package pi

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

func TestName(t *testing.T) {
	p := New("claude-sonnet-4-6")
	if p.Name() != "pi" {
		t.Errorf("Name() = %q, want %q", p.Name(), "pi")
	}
}

func TestPrintCommand(t *testing.T) {
	t.Run("includes model and flags", func(t *testing.T) {
		p := New("claude-sonnet-4-6")
		cmd := p.PrintCommand("do something")
		for _, want := range []string{"claude-sonnet-4-6", "--mode json", "--no-session", "-p"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("PrintCommand missing %q in %q", want, cmd)
			}
		}
	})

	t.Run("shell-escapes prompt", func(t *testing.T) {
		p := New("claude-sonnet-4-6")
		cmd := p.PrintCommand("it's a test")
		if !strings.Contains(cmd, "'it'\\''s a test'") {
			t.Errorf("PrintCommand did not escape prompt: %q", cmd)
		}
	})

	t.Run("shell-escapes model", func(t *testing.T) {
		p := New("claude-sonnet-4-6")
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "--model 'claude-sonnet-4-6'") {
			t.Errorf("PrintCommand did not escape model: %q", cmd)
		}
	})

	t.Run("omits model flag when empty", func(t *testing.T) {
		p := New("")
		cmd := p.PrintCommand("do something")
		if strings.Contains(cmd, "--model") {
			t.Errorf("PrintCommand should not contain --model: %q", cmd)
		}
	})
}

func TestInteractiveArgs(t *testing.T) {
	t.Run("includes model", func(t *testing.T) {
		p := New("claude-sonnet-4-6")
		args := p.InteractiveArgs("")
		if args[0] != "pi" {
			t.Errorf("args[0] = %q, want pi", args[0])
		}
		if !slices.Contains(args, "claude-sonnet-4-6") || !slices.Contains(args, "--model") {
			t.Errorf("args missing model: %v", args)
		}
	})

	t.Run("omits model when empty", func(t *testing.T) {
		p := New("")
		args := p.InteractiveArgs("")
		if slices.Contains(args, "--model") {
			t.Errorf("args should not contain model: %v", args)
		}
	})
}

func TestParseStreamLine(t *testing.T) {
	p := New("claude-sonnet-4-6")

	t.Run("extracts text from message_update text_delta", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "message_update",
			"assistantMessageEvent": map[string]any{
				"type":         "text_delta",
				"contentIndex": float64(0),
				"delta":        "Hello world",
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventText, Text: "Hello world"},
		})
	})

	t.Run("skips message_update with non-text_delta event", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "message_update",
			"assistantMessageEvent": map[string]any{
				"type": "text_start",
			},
		})
		events := p.ParseStreamLine(line)
		if len(events) != 0 {
			t.Errorf("expected empty events, got %+v", events)
		}
	})

	t.Run("skips message_update without assistantMessageEvent", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "message_update",
		})
		events := p.ParseStreamLine(line)
		if len(events) != 0 {
			t.Errorf("expected empty events, got %+v", events)
		}
	})

	t.Run("extracts tool call from tool_execution_start", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":              "tool_execution_start",
			"tool_execution_id": "tool-1",
			"tool_name":         "Bash",
			"input":             map[string]any{"command": "npm test"},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolID: "tool-1", ToolName: "Bash", ToolArgs: `{"command":"npm test"}`},
		})
	})

	t.Run("extracts tool result from tool_execution_result", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":              "tool_execution_result",
			"tool_execution_id": "tool-1",
			"tool_name":         "Bash",
			"output":            "ok\n",
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolResult, ToolID: "tool-1", ToolName: "Bash", ToolOutput: "ok\n"},
		})
	})

	t.Run("extracts unknown tools", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":      "tool_execution_start",
			"tool_name": "UnknownTool",
			"input":     map[string]any{"foo": "bar"},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "UnknownTool", ToolArgs: `{"foo":"bar"}`},
		})
	})

	t.Run("extracts result from agent_end", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "agent_end",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "text", "text": "Hello"},
					},
				},
				map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "text", "text": "Final answer"},
					},
				},
			},
		})
		events := p.ParseStreamLine(line)
		if len(events) != 1 || events[0].Type != harness.EventResult || events[0].Result != "Final answer" {
			t.Errorf("unexpected events: %+v", events)
		}
		if events[0].Usage != nil {
			t.Errorf("expected nil usage, got %+v", events[0].Usage)
		}
	})

	t.Run("extracts usage from agent_end", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "agent_end",
			"messages": []any{
				map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "text", "text": "Done"},
					},
					"usage": map[string]any{
						"input":       float64(100),
						"output":      float64(50),
						"cacheRead":   float64(10),
						"cacheWrite":  float64(5),
						"totalTokens": float64(150),
						"cost": map[string]any{
							"input":      float64(0.003),
							"output":     float64(0.006),
							"cacheRead":  float64(0),
							"cacheWrite": float64(0),
							"total":      float64(0.01),
						},
					},
				},
			},
		})
		events := p.ParseStreamLine(line)
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		u := events[0].Usage
		if u == nil {
			t.Fatal("expected usage, got nil")
		}
		if u.InputTokens != 100 || u.OutputTokens != 50 {
			t.Errorf("unexpected tokens: %+v", u)
		}
		if u.CacheReadInputTokens != 10 {
			t.Errorf("CacheReadInputTokens = %d, want 10", u.CacheReadInputTokens)
		}
		if u.TotalCostUSD != 0.01 {
			t.Errorf("TotalCostUSD = %f, want 0.01", u.TotalCostUSD)
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

	t.Run("handles agent_end with no assistant messages", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "agent_end",
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": []any{map[string]any{"type": "text", "text": "Hello"}},
				},
			},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles agent_end with missing messages", func(t *testing.T) {
		line := jsonStr(map[string]any{"type": "agent_end"})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("handles tool_execution_start with missing input", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type":      "tool_execution_start",
			"tool_name": "Bash",
		})
		assertEqual(t, p.ParseStreamLine(line), []harness.Event{{Type: harness.EventToolCall, ToolName: "Bash"}})
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

func assertEqual(t *testing.T, got, want []harness.Event) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(events) = %d, want %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i].Type != want[i].Type || got[i].Text != want[i].Text ||
			got[i].Result != want[i].Result || got[i].ToolID != want[i].ToolID ||
			got[i].ToolName != want[i].ToolName || got[i].ToolArgs != want[i].ToolArgs ||
			got[i].ToolOutput != want[i].ToolOutput || got[i].ToolError != want[i].ToolError {
			t.Errorf("event[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
