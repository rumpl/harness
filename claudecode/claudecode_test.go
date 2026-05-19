package claudecode

import (
	"encoding/json"
	"slices"
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

	t.Run("omits model flag when empty", func(t *testing.T) {
		p := New("")
		cmd := p.PrintCommand("do something")
		if strings.Contains(cmd, "--model") {
			t.Errorf("PrintCommand should not contain --model: %q", cmd)
		}
	})

	t.Run("strips anthropic provider prefix from model", func(t *testing.T) {
		p := New("anthropic/claude-opus-4-6")
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "--model 'claude-opus-4-6'") {
			t.Errorf("PrintCommand did not strip anthropic prefix: %q", cmd)
		}
		if strings.Contains(cmd, "anthropic/claude-opus-4-6") {
			t.Errorf("PrintCommand should not pass provider/model form to claude: %q", cmd)
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
		if !slices.Contains(args, "claude-sonnet-4-6") || !slices.Contains(args, "--model") {
			t.Errorf("args missing model: %v", args)
		}
	})

	t.Run("strips anthropic provider prefix from model", func(t *testing.T) {
		p := New("anthropic/claude-opus-4-6")
		args := p.InteractiveArgs("")
		if !slices.Contains(args, "claude-opus-4-6") {
			t.Errorf("args missing normalized model: %v", args)
		}
		if slices.Contains(args, "anthropic/claude-opus-4-6") {
			t.Errorf("args should not pass provider/model form to claude: %v", args)
		}
	})

	t.Run("includes effort when set", func(t *testing.T) {
		p := New("claude-opus-4-6", WithEffort(EffortLow))
		args := p.InteractiveArgs("")
		if !slices.Contains(args, "--effort") || !slices.Contains(args, "low") {
			t.Errorf("args missing effort: %v", args)
		}
	})

	t.Run("omits model when empty", func(t *testing.T) {
		p := New("")
		args := p.InteractiveArgs("")
		if slices.Contains(args, "--model") {
			t.Errorf("args should not contain model: %v", args)
		}
	})

	t.Run("omits effort when not set", func(t *testing.T) {
		p := New("claude-opus-4-6")
		args := p.InteractiveArgs("")
		if slices.Contains(args, "--effort") {
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
			{Type: harness.EventToolCall, ToolName: "Bash", ToolArgs: `{"command":"npm test"}`},
		})
	})

	t.Run("extracts text and Write tool_use from assistant message", func(t *testing.T) {
		line := jsonStr(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "I'll write the file."},
					map[string]any{
						"type":  "tool_use",
						"name":  "Write",
						"input": map[string]any{"file_path": "/tmp/poem.md", "content": "roses"},
					},
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventText, Text: "I'll write the file."},
			{Type: harness.EventToolCall, ToolName: "Write", ToolArgs: `{"content":"roses","file_path":"/tmp/poem.md"}`},
		})
	})

	t.Run("extracts unknown tools", func(t *testing.T) {
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
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "UnknownTool", ToolArgs: `{"foo":"bar"}`},
		})
	})

	t.Run("does not replay assistant text after partial text deltas", func(t *testing.T) {
		p := New("claude-opus-4-6")
		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type": "stream_event",
			"event": map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": "Hello"},
			},
		}))
		assertEqual(t, events, []harness.Event{{Type: harness.EventText, Text: "Hello"}})

		events = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "stream_event",
			"event": map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": " world"},
			},
		}))
		assertEqual(t, events, []harness.Event{{Type: harness.EventText, Text: " world"}})

		events = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "Hello world"}},
			},
		}))
		assertEqual(t, events, nil)
	})

	t.Run("streams partial tool use and skips assistant snapshot", func(t *testing.T) {
		p := New("claude-opus-4-6")
		assertEqual(t, p.ParseStreamLine(jsonStr(map[string]any{
			"type": "stream_event",
			"event": map[string]any{
				"type":  "content_block_start",
				"index": 1,
				"content_block": map[string]any{
					"type": "tool_use",
					"id":   "toolu_1",
					"name": "Bash",
				},
			},
		})), []harness.Event{{Type: harness.EventToolCallStart, ToolID: "toolu_1", ToolName: "Bash"}})
		assertEqual(t, p.ParseStreamLine(jsonStr(map[string]any{
			"type": "stream_event",
			"event": map[string]any{
				"type":  "content_block_delta",
				"index": 1,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": "{\"command\":\"uname -a\"}"},
			},
		})), []harness.Event{{Type: harness.EventToolCallDelta, ToolID: "toolu_1", ToolName: "Bash", ToolArgs: "{\"command\":\"uname -a\"}"}})
		assertEqual(t, p.ParseStreamLine(jsonStr(map[string]any{
			"type":  "stream_event",
			"event": map[string]any{"type": "content_block_stop", "index": 1},
		})), []harness.Event{{Type: harness.EventToolCall, ToolID: "toolu_1", ToolName: "Bash"}})

		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{map[string]any{
					"type":  "tool_use",
					"id":    "toolu_1",
					"name":  "Bash",
					"input": map[string]any{"command": "uname -a"},
				}},
			},
		}))
		assertEqual(t, events, nil)
	})

	t.Run("extracts tool result from user message", func(t *testing.T) {
		p := New("claude-opus-4-6")
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{map[string]any{
					"type":  "tool_use",
					"id":    "toolu_2",
					"name":  "Bash",
					"input": map[string]any{"command": "uname -a"},
				}},
			},
		}))

		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type": "user",
			"message": map[string]any{
				"content": []any{map[string]any{
					"type":        "tool_result",
					"tool_use_id": "toolu_2",
					"content":     "Darwin localhost 25.0.0",
				}},
			},
		}))
		assertEqual(t, events, []harness.Event{{
			Type:       harness.EventToolResult,
			ToolID:     "toolu_2",
			ToolName:   "Bash",
			ToolOutput: "Darwin localhost 25.0.0",
		}})
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
