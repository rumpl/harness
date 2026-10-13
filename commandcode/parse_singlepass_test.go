package commandcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

// wantUsage is the subset of harness.Usage asserted for result lines.
type wantUsage struct {
	input, output, cacheRead, cacheWrite, numTurns, durationMs int
}

// wantEvent is the exact expected shape of one emitted harness.Event. Only the
// fields relevant to the event type are populated.
type wantEvent struct {
	typ        harness.EventType
	sessionID  string
	text       string
	reasoning  string
	toolID     string
	toolName   string
	toolArgs   string
	toolOutput string
	toolError  bool
	result     string
	usage      *wantUsage
}

func checkEvent(t *testing.T, label string, got harness.Event, want wantEvent) {
	t.Helper()

	if got.Type != want.typ {
		t.Fatalf("%s: type = %q, want %q", label, got.Type, want.typ)
	}
	if got.SessionID != want.sessionID {
		t.Errorf("%s: SessionID = %q, want %q", label, got.SessionID, want.sessionID)
	}
	if got.Text != want.text {
		t.Errorf("%s: Text = %q, want %q", label, got.Text, want.text)
	}
	if got.Reasoning != want.reasoning {
		t.Errorf("%s: Reasoning = %q, want %q", label, got.Reasoning, want.reasoning)
	}
	if got.ToolID != want.toolID {
		t.Errorf("%s: ToolID = %q, want %q", label, got.ToolID, want.toolID)
	}
	if got.ToolName != want.toolName {
		t.Errorf("%s: ToolName = %q, want %q", label, got.ToolName, want.toolName)
	}
	if got.ToolArgs != want.toolArgs {
		t.Errorf("%s: ToolArgs = %q, want %q", label, got.ToolArgs, want.toolArgs)
	}
	if got.ToolOutput != want.toolOutput {
		t.Errorf("%s: ToolOutput = %q, want %q", label, got.ToolOutput, want.toolOutput)
	}
	if got.ToolError != want.toolError {
		t.Errorf("%s: ToolError = %v, want %v", label, got.ToolError, want.toolError)
	}
	if got.Result != want.result {
		t.Errorf("%s: Result = %q, want %q", label, got.Result, want.result)
	}

	if want.usage == nil {
		if got.Usage != nil {
			t.Errorf("%s: Usage = %#v, want nil", label, got.Usage)
		}
		return
	}
	if got.Usage == nil {
		t.Fatalf("%s: Usage = nil, want %#v", label, *want.usage)
	}
	if got.Usage.InputTokens != want.usage.input {
		t.Errorf("%s: Usage.InputTokens = %d, want %d", label, got.Usage.InputTokens, want.usage.input)
	}
	if got.Usage.OutputTokens != want.usage.output {
		t.Errorf("%s: Usage.OutputTokens = %d, want %d", label, got.Usage.OutputTokens, want.usage.output)
	}
	if got.Usage.CacheReadInputTokens != want.usage.cacheRead {
		t.Errorf("%s: Usage.CacheReadInputTokens = %d, want %d", label, got.Usage.CacheReadInputTokens, want.usage.cacheRead)
	}
	if got.Usage.CacheCreationInputTokens != want.usage.cacheWrite {
		t.Errorf("%s: Usage.CacheCreationInputTokens = %d, want %d", label, got.Usage.CacheCreationInputTokens, want.usage.cacheWrite)
	}
	if got.Usage.NumTurns != want.usage.numTurns {
		t.Errorf("%s: Usage.NumTurns = %d, want %d", label, got.Usage.NumTurns, want.usage.numTurns)
	}
	if got.Usage.DurationMS != want.usage.durationMs {
		t.Errorf("%s: Usage.DurationMS = %d, want %d", label, got.Usage.DurationMS, want.usage.durationMs)
	}
}

// TestParseSinglePassFixtureLines feeds every line of both fixtures through the
// parser (in stream order, so cross-line run_end -> result state is exercised)
// and asserts the exact event type and key fields each line produces.
func TestParseSinglePassFixtureLines(t *testing.T) {
	fixtures := []struct {
		name  string
		file  string
		wants [][]wantEvent
	}{
		{
			name: "no-tool",
			file: noToolFixture,
			wants: [][]wantEvent{
				{{typ: harness.EventSessionID, sessionID: "a52eea0d-c51e-41be-8381-19f377f71367"}}, // 1 run_start
				nil, // 2 turn_start
				nil, // 3 message_start
				nil, // 4 model_request_start
				nil, // 5 model_trace
				nil, // 6 thinking_start
				{{typ: harness.EventReasoning, reasoning: "The"}},         // 7
				{{typ: harness.EventReasoning, reasoning: " user wants"}}, // 8
				{{typ: harness.EventReasoning, reasoning: " exactly"}},    // 9
				{{typ: harness.EventReasoning, reasoning: " \""}},         // 10
				{{typ: harness.EventReasoning, reasoning: "OK"}},          // 11
				{{typ: harness.EventReasoning, reasoning: "\"."}},         // 12
				nil,                                    // 13 thinking_end
				{{typ: harness.EventText, text: "OK"}}, // 14 text_delta
				nil,                                    // 15 message_update
				nil,                                    // 16 model_request_end
				nil,                                    // 17 message_end
				nil,                                    // 18 turn_end
				nil,                                    // 19 run_end (records turnCount 1)
				{{ // 20 result
					typ:    harness.EventResult,
					result: "OK",
					usage:  &wantUsage{input: 19928, output: 9, cacheRead: 4096, cacheWrite: 0, numTurns: 1, durationMs: 5128},
				}},
			},
		},
		{
			name: "tools",
			file: toolsFixture,
			wants: [][]wantEvent{
				{{typ: harness.EventSessionID, sessionID: "41405ec4-9a47-4e9c-917e-0b7e336e4c27"}}, // 1 run_start
				nil, // 2 turn_start
				nil, // 3 message_start
				nil, // 4 model_request_start
				nil, // 5 model_trace
				nil, // 6 thinking_start
				{{typ: harness.EventReasoning, reasoning: "Simple"}}, // 7
				{{typ: harness.EventReasoning, reasoning: " read"}},  // 8
				{{typ: harness.EventReasoning, reasoning: "."}},      // 9
				nil, // 10 thinking_end
				nil, // 11 message_update
				nil, // 12 model_request_end
				nil, // 13 message_end
				{{ // 14 tool_queued
					typ:      harness.EventToolCall,
					toolID:   "call_00_ryk0yec75jsvx08mvvkq1v3g",
					toolName: "read_file",
					toolArgs: `{"file_path":"/etc/hostname"}`,
				}},
				nil, // 15 tool_running
				{{ // 16 tool_completed
					typ:        harness.EventToolResult,
					toolID:     "call_00_ryk0yec75jsvx08mvvkq1v3g",
					toolName:   "read_file",
					toolOutput: "Read 1/1 file, 1 line\n1: arch.merf",
					toolError:  false,
				}},
				nil, // 17 turn_end
				nil, // 18 turn_start
				nil, // 19 message_start
				nil, // 20 model_request_start
				nil, // 21 model_trace
				{{typ: harness.EventText, text: "The file"}}, // 22
				nil, // 23 message_update
				{{typ: harness.EventText, text: " contains:"}}, // 24
				nil, // 25 message_update
				{{typ: harness.EventText, text: " `arch.mer"}}, // 26
				nil,                                    // 27 message_update
				{{typ: harness.EventText, text: "f`"}}, // 28
				nil,                                    // 29 message_update
				nil,                                    // 30 model_request_end
				nil,                                    // 31 message_end
				nil,                                    // 32 turn_end
				nil,                                    // 33 run_end (records turnCount 2)
				{{ // 34 result
					typ:    harness.EventResult,
					result: "The file contains: `arch.merf`",
					usage:  &wantUsage{input: 40035, output: 57, cacheRead: 30080, cacheWrite: 0, numTurns: 2, durationMs: 15602},
				}},
			},
		},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", fx.file))
			if err != nil {
				t.Fatalf("read fixture %s: %v", fx.file, err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != len(fx.wants) {
				t.Fatalf("fixture %s has %d lines, table has %d expectations", fx.file, len(lines), len(fx.wants))
			}

			p := New("").(*provider)
			for i, line := range lines {
				got := p.ParseStreamLine(line)
				want := fx.wants[i]
				label := fx.file + " L" + itoa(i+1)

				if len(got) != len(want) {
					t.Fatalf("%s: %d events, want %d; line=%s", label, len(got), len(want), line)
				}
				for j := range want {
					checkEvent(t, label, got[j], want[j])
				}
			}
		})
	}
}

// itoa is a tiny local integer formatter to avoid importing strconv solely for
// table labels.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestParseSinglePassEdgeLines exercises garbage, empty, non-object, unknown
// type and malformed-usage lines. A fresh parser is used per row so no row
// depends on another.
func TestParseSinglePassEdgeLines(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []wantEvent
	}{
		{"empty", "", nil},
		{"not json", "not json at all", nil},
		{"truncated", `{"type":"event","event":{`, nil},
		{"json array", `[1,2,3]`, nil},
		{"unknown outer type", `{"type":"wat"}`, nil},
		{"event without inner object", `{"type":"event"}`, nil},
		{"unknown inner type", `{"type":"event","event":{"type":"nope"}}`, nil},
		{"event with non-object inner", `{"type":"event","event":42}`, nil},
		{"run_start without session", `{"type":"event","event":{"type":"run_start"}}`, nil},
		{"empty text delta", `{"type":"event","event":{"type":"text_delta","delta":""}}`, nil},
		{"empty thinking delta", `{"type":"event","event":{"type":"thinking_delta","delta":""}}`, nil},
		{
			"tool_completed with error item", `{"type":"event","event":{"type":"tool_completed","toolCallId":"c1","toolName":"bash","result":[{"type":"error","text":"boom"}]}}`,
			[]wantEvent{{typ: harness.EventToolResult, toolID: "c1", toolName: "bash", toolOutput: "boom", toolError: true}},
		},
		{
			"result malformed usage", `{"type":"result","finalText":"X","usage":"nope"}`,
			[]wantEvent{{typ: harness.EventResult, result: "X", usage: &wantUsage{}}},
		},
		{
			"result absent usage", `{"type":"result","finalText":"Y"}`,
			[]wantEvent{{typ: harness.EventResult, result: "Y", usage: nil}},
		},
		{
			"result full usage", `{"type":"result","subtype":"success","usage":{"inputTokens":1,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":7},"durationMs":5,"finalText":"z"}`,
			[]wantEvent{{typ: harness.EventResult, result: "z", usage: &wantUsage{input: 1, output: 2, cacheRead: 3, cacheWrite: 7, numTurns: 0, durationMs: 5}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New("").(*provider)
			got := p.ParseStreamLine(tt.line)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseStreamLine(%q) = %d events, want %d", tt.line, len(got), len(tt.want))
			}
			for j := range tt.want {
				checkEvent(t, tt.name, got[j], tt.want[j])
			}
		})
	}
}
