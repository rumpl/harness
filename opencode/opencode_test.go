package opencode

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

func TestName(t *testing.T) {
	p := New("anthropic/claude-3-5-sonnet")
	if p.Name() != "opencode" {
		t.Errorf("Name() = %q, want %q", p.Name(), "opencode")
	}
}

func TestPrintCommand(t *testing.T) {
	t.Run("includes model and json flags", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		cmd := p.PrintCommand("do something")
		for _, want := range []string{
			"opencode run",
			"--format json",
			"--dangerously-skip-permissions",
			"--model 'anthropic/claude-3-5-sonnet'",
		} {
			if !strings.Contains(cmd, want) {
				t.Errorf("PrintCommand missing %q in %q", want, cmd)
			}
		}
	})

	t.Run("shell-escapes prompt", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		cmd := p.PrintCommand("it's a test")
		if !strings.Contains(cmd, "'it'\\''s a test'") {
			t.Errorf("PrintCommand did not escape prompt: %q", cmd)
		}
	})

	t.Run("omits model flag when empty", func(t *testing.T) {
		p := New("")
		cmd := p.PrintCommand("do something")
		if strings.Contains(cmd, "--model") {
			t.Errorf("PrintCommand should not contain --model: %q", cmd)
		}
	})

	t.Run("includes agent when set", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet", WithAgent("plan"))
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "--agent 'plan'") {
			t.Errorf("PrintCommand missing --agent 'plan': %q", cmd)
		}
	})

	t.Run("omits agent when not set", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		cmd := p.PrintCommand("do something")
		if strings.Contains(cmd, "--agent") {
			t.Errorf("PrintCommand should not contain --agent: %q", cmd)
		}
	})

	t.Run("includes thinking when set", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet", WithThinking())
		cmd := p.PrintCommand("do something")
		if !strings.Contains(cmd, "--thinking") {
			t.Errorf("PrintCommand missing --thinking: %q", cmd)
		}
	})

	t.Run("omits thinking when not set", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		cmd := p.PrintCommand("do something")
		if strings.Contains(cmd, "--thinking") {
			t.Errorf("PrintCommand should not contain --thinking: %q", cmd)
		}
	})
}

func TestInteractiveArgs(t *testing.T) {
	t.Run("includes binary and model", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		args := p.InteractiveArgs("")
		if args[0] != "opencode" {
			t.Errorf("args[0] = %q, want opencode", args[0])
		}
		if !slices.Contains(args, "anthropic/claude-3-5-sonnet") || !slices.Contains(args, "--model") {
			t.Errorf("args missing model: %v", args)
		}
	})

	t.Run("includes agent when set", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet", WithAgent("plan"))
		args := p.InteractiveArgs("")
		if !slices.Contains(args, "--agent") || !slices.Contains(args, "plan") {
			t.Errorf("args missing agent: %v", args)
		}
	})

	t.Run("omits model when empty", func(t *testing.T) {
		p := New("")
		args := p.InteractiveArgs("")
		if slices.Contains(args, "--model") {
			t.Errorf("args should not contain model: %v", args)
		}
	})

	t.Run("omits agent when not set", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		args := p.InteractiveArgs("")
		if slices.Contains(args, "--agent") {
			t.Errorf("args should not contain --agent: %v", args)
		}
	})
}

func TestParseStreamLine(t *testing.T) {
	t.Run("emits text only when part has time.end", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")

		// In-progress text: no time.end → no event.
		inProgress := jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "text",
					"text": "partial",
				},
			},
		})
		if events := p.ParseStreamLine(inProgress); len(events) != 0 {
			t.Errorf("expected no events for in-progress text, got %+v", events)
		}

		// Final text: time.end is set → EventText.
		final := jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "text",
					"text": "Hello world",
					"time": map[string]any{"end": float64(1234567890)},
				},
			},
		})
		events := p.ParseStreamLine(final)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventText, Text: "Hello world"},
		})
	})

	t.Run("emits reasoning event from reasoning part", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		line := jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "reasoning",
					"text": "let me think about this",
					"time": map[string]any{"end": float64(1)},
				},
			},
		})
		events := p.ParseStreamLine(line)
		if len(events) != 1 || events[0].Type != harness.EventReasoning ||
			events[0].Reasoning != "let me think about this" {
			t.Errorf("unexpected events: %+v", events)
		}
	})

	t.Run("emits tool call when state is completed", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		line := jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "tool",
					"tool": "bash",
					"state": map[string]any{
						"status": "completed",
						"input":  map[string]any{"command": "npm test"},
					},
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "bash", ToolArgs: `{"command":"npm test"}`},
			{Type: harness.EventToolResult, ToolName: "bash"},
		})
	})

	t.Run("emits text from current top-level text event", func(t *testing.T) {
		p := New("anthropic/claude-sonnet-4-6")
		line := jsonStr(map[string]any{
			"type": "text",
			"part": map[string]any{
				"type": "text",
				"text": "Hello from opencode 1.15",
				"time": map[string]any{"end": float64(1)},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventText, Text: "Hello from opencode 1.15"},
		})
	})

	t.Run("streams message.part.delta text and suppresses final duplicate", func(t *testing.T) {
		p := New("anthropic/claude-sonnet-4-6")
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"time": float64(1000),
				"part": map[string]any{"id": "step-1", "type": "step-start"},
			},
		}))
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"id":   "text-1",
					"type": "text",
					"text": "",
				},
			},
		}))

		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.delta",
			"properties": map[string]any{
				"partID": "text-1",
				"field":  "text",
				"delta":  "Hello",
			},
		}))
		assertEqual(t, events, []harness.Event{{Type: harness.EventText, Text: "Hello"}})

		events = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.delta",
			"properties": map[string]any{
				"partID": "text-1",
				"field":  "text",
				"delta":  " world",
			},
		}))
		assertEqual(t, events, []harness.Event{{Type: harness.EventText, Text: " world"}})

		// The completed part contains the full text; it should not be emitted
		// again because the deltas already streamed it.
		events = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"id":   "text-1",
					"type": "text",
					"text": "Hello world",
					"time": map[string]any{"end": float64(1100)},
				},
			},
		}))
		if len(events) != 0 {
			t.Fatalf("expected final text update to be suppressed, got %+v", events)
		}

		events = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"time": float64(1200),
				"part": map[string]any{
					"id":     "finish-1",
					"type":   "step-finish",
					"reason": "stop",
					"tokens": map[string]any{
						"input":  float64(2),
						"output": float64(3),
					},
					"cost": float64(0.01),
				},
			},
		}))
		if len(events) != 1 || events[0].Type != harness.EventResult || events[0].Result != "Hello world" {
			t.Fatalf("unexpected events: %+v", events)
		}
		assertUsage(t, events[0].Usage, harness.Usage{
			InputTokens:  2,
			OutputTokens: 3,
			TotalCostUSD: 0.01,
			NumTurns:     1,
			DurationMS:   200,
		})
	})

	t.Run("streams message.part.delta reasoning", func(t *testing.T) {
		p := New("anthropic/claude-sonnet-4-6")
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"id":   "reason-1",
					"type": "reasoning",
					"text": "",
				},
			},
		}))
		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.delta",
			"properties": map[string]any{
				"partID": "reason-1",
				"field":  "text",
				"delta":  "thinking...",
			},
		}))
		if len(events) != 1 || events[0].Type != harness.EventReasoning || events[0].Reasoning != "thinking..." {
			t.Fatalf("unexpected events: %+v", events)
		}
	})

	t.Run("emits tool call from current top-level tool_use event", func(t *testing.T) {
		p := New("anthropic/claude-sonnet-4-6")
		line := jsonStr(map[string]any{
			"type": "tool_use",
			"part": map[string]any{
				"type": "tool",
				"tool": "bash",
				"state": map[string]any{
					"status": "completed",
					"input": map[string]any{
						"command":     "pwd",
						"description": "Print current working directory",
					},
				},
			},
		})
		events := p.ParseStreamLine(line)
		assertEqual(t, events, []harness.Event{
			{Type: harness.EventToolCall, ToolName: "bash", ToolArgs: `{"command":"pwd","description":"Print current working directory"}`},
			{Type: harness.EventToolResult, ToolName: "bash"},
		})
	})

	t.Run("step_finish stop emits result with usage", func(t *testing.T) {
		p := New("anthropic/claude-sonnet-4-6")
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type":      "step_start",
			"timestamp": float64(1000),
			"part":      map[string]any{"type": "step-start"},
		}))
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "text",
			"part": map[string]any{
				"type": "text",
				"text": "Final answer",
				"time": map[string]any{"end": float64(1001)},
			},
		}))

		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type":      "step_finish",
			"timestamp": float64(1100),
			"part": map[string]any{
				"type":   "step-finish",
				"reason": "stop",
				"tokens": map[string]any{
					"input":  float64(3),
					"output": float64(12),
					"cache": map[string]any{
						"write": float64(2),
						"read":  float64(5),
					},
				},
				"cost": float64(0.5),
			},
		}))
		if len(events) != 1 || events[0].Type != harness.EventResult || events[0].Result != "Final answer" {
			t.Fatalf("unexpected events: %+v", events)
		}
		assertUsage(t, events[0].Usage, harness.Usage{
			InputTokens:              3,
			OutputTokens:             12,
			CacheReadInputTokens:     5,
			CacheCreationInputTokens: 2,
			TotalCostUSD:             0.5,
			NumTurns:                 1,
			DurationMS:               100,
		})
	})

	t.Run("aggregates usage across tool-call steps", func(t *testing.T) {
		p := New("anthropic/claude-sonnet-4-6")
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type":      "step_start",
			"timestamp": float64(1000),
			"part":      map[string]any{"type": "step-start"},
		}))
		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type":      "step_finish",
			"timestamp": float64(1100),
			"part": map[string]any{
				"type":   "step-finish",
				"reason": "tool-calls",
				"tokens": map[string]any{
					"input":  float64(1),
					"output": float64(2),
					"cache": map[string]any{
						"write": float64(3),
						"read":  float64(4),
					},
				},
				"cost": float64(0.25),
			},
		}))
		if len(events) != 0 {
			t.Fatalf("expected no result for tool-calls step, got %+v", events)
		}

		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type":      "step_start",
			"timestamp": float64(1200),
			"part":      map[string]any{"type": "step-start"},
		}))
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "text",
			"part": map[string]any{
				"type": "text",
				"text": "Done",
				"time": map[string]any{"end": float64(1300)},
			},
		}))
		events = p.ParseStreamLine(jsonStr(map[string]any{
			"type":      "step_finish",
			"timestamp": float64(1400),
			"part": map[string]any{
				"type":   "step-finish",
				"reason": "stop",
				"tokens": map[string]any{
					"input":  float64(5),
					"output": float64(6),
					"cache": map[string]any{
						"write": float64(7),
						"read":  float64(8),
					},
				},
				"cost": float64(0.5),
			},
		}))
		if len(events) != 1 || events[0].Type != harness.EventResult || events[0].Result != "Done" {
			t.Fatalf("unexpected events: %+v", events)
		}
		assertUsage(t, events[0].Usage, harness.Usage{
			InputTokens:              6,
			OutputTokens:             8,
			CacheReadInputTokens:     12,
			CacheCreationInputTokens: 10,
			TotalCostUSD:             0.75,
			NumTurns:                 2,
			DurationMS:               400,
		})
	})

	t.Run("standalone usage event emits result when text is available", func(t *testing.T) {
		p := New("anthropic/claude-sonnet-4-6")
		_ = p.ParseStreamLine(jsonStr(map[string]any{
			"type": "text",
			"part": map[string]any{
				"type": "text",
				"text": "Answer with separate usage",
				"time": map[string]any{"end": float64(1)},
			},
		}))

		events := p.ParseStreamLine(jsonStr(map[string]any{
			"type": "usage",
			"usage": map[string]any{
				"input_tokens":                float64(10),
				"output_tokens":               float64(4),
				"cache_read_input_tokens":     float64(2),
				"cache_creation_input_tokens": float64(1),
			},
			"total_cost_usd": float64(0.5),
		}))
		if len(events) != 1 || events[0].Type != harness.EventResult || events[0].Result != "Answer with separate usage" {
			t.Fatalf("unexpected events: %+v", events)
		}
		assertUsage(t, events[0].Usage, harness.Usage{
			InputTokens:              10,
			OutputTokens:             4,
			CacheReadInputTokens:     2,
			CacheCreationInputTokens: 1,
			TotalCostUSD:             0.5,
			NumTurns:                 1,
		})
	})

	t.Run("skips tool call when state is in progress", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		line := jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "tool",
					"tool": "bash",
					"state": map[string]any{
						"status": "running",
						"input":  map[string]any{"command": "npm test"},
					},
				},
			},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected no events for running tool, got %+v", events)
		}
	})

	t.Run("emits unknown tools", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		line := jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "tool",
					"tool": "some_unknown_tool",
					"state": map[string]any{
						"status": "completed",
						"input":  map[string]any{"foo": "bar"},
					},
				},
			},
		})
		assertEqual(t, p.ParseStreamLine(line), []harness.Event{
			{Type: harness.EventToolCall, ToolName: "some_unknown_tool", ToolArgs: `{"foo":"bar"}`},
			{Type: harness.EventToolResult, ToolName: "some_unknown_tool"},
		})
	})

	t.Run("session.status idle emits EventResult with last text", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")

		// Feed a completed text part first.
		textLine := jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "text",
					"text": "Final answer",
					"time": map[string]any{"end": float64(1)},
				},
			},
		})
		_ = p.ParseStreamLine(textLine)

		idle := jsonStr(map[string]any{
			"type": "session.status",
			"properties": map[string]any{
				"status": "idle",
			},
		})
		events := p.ParseStreamLine(idle)
		if len(events) != 1 || events[0].Type != harness.EventResult ||
			events[0].Result != "Final answer" {
			t.Errorf("unexpected events: %+v", events)
		}
		if events[0].Usage != nil {
			t.Errorf("expected nil usage (opencode does not stream usage), got %+v", events[0].Usage)
		}
	})

	t.Run("session.status idle without prior text emits nothing", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		line := jsonStr(map[string]any{
			"type": "session.status",
			"properties": map[string]any{
				"status": "idle",
			},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("session.status non-idle is ignored", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		line := jsonStr(map[string]any{
			"type": "session.status",
			"properties": map[string]any{
				"status": "running",
			},
		})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("step-start and step-finish parts are ignored", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		for _, partType := range []string{"step-start", "step-finish"} {
			line := jsonStr(map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{
					"part": map[string]any{"type": partType},
				},
			})
			if events := p.ParseStreamLine(line); len(events) != 0 {
				t.Errorf("expected empty for part type %q, got %+v", partType, events)
			}
		}
	})

	t.Run("returns empty for non-JSON", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		if events := p.ParseStreamLine("not json"); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
		if events := p.ParseStreamLine(""); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("returns empty for malformed JSON", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		if events := p.ParseStreamLine("{bad json"); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("returns empty for unrecognized event types", func(t *testing.T) {
		p := New("anthropic/claude-3-5-sonnet")
		line := jsonStr(map[string]any{"type": "unknown_event"})
		if events := p.ParseStreamLine(line); len(events) != 0 {
			t.Errorf("expected empty, got %+v", events)
		}
	})

	t.Run("independent provider instances do not share parser state", func(t *testing.T) {
		p1 := New("anthropic/claude-3-5-sonnet")
		p2 := New("anthropic/claude-3-5-sonnet")

		// p1 sees a final text, then idle.
		_ = p1.ParseStreamLine(jsonStr(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"type": "text",
					"text": "from p1",
					"time": map[string]any{"end": float64(1)},
				},
			},
		}))

		// p2 only sees idle — should emit nothing because it has no text.
		idle := jsonStr(map[string]any{
			"type": "session.status",
			"properties": map[string]any{
				"status": "idle",
			},
		})
		if events := p2.ParseStreamLine(idle); len(events) != 0 {
			t.Errorf("p2 should not see p1's text, got %+v", events)
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

func assertUsage(t *testing.T, got *harness.Usage, want harness.Usage) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected usage %+v, got nil", want)
	}
	if *got != want {
		t.Fatalf("usage = %+v, want %+v", *got, want)
	}
}
