package commandcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rumpl/harness"
)

const (
	noToolFixture = "cmd-ndjson.txt"
	toolsFixture  = "cmd-ndjson-tools.txt"
)

func TestName(t *testing.T) {
	if got := New("").Name(); got != "command-code" {
		t.Fatalf("Name() = %q, want %q", got, "command-code")
	}
}

func TestPrintArgs(t *testing.T) {
	tests := []struct {
		name      string
		provider  harness.Provider
		build     func(harness.Provider) []string
		wantFlags []string
		wantLast  string
	}{
		{
			name:      "plain prompt",
			provider:  New(""),
			build:     func(p harness.Provider) []string { return p.(*provider).PrintArgs("hello world") },
			wantFlags: []string{"cmd", "-p", "--output-format", "json", "--yolo"},
			wantLast:  "hello world",
		},
		{
			name:      "with model and effort",
			provider:  New("deepseek/deepseek-v4.1-flash", WithEffort("low")),
			build:     func(p harness.Provider) []string { return p.(*provider).PrintArgs("hi") },
			wantFlags: []string{"cmd", "-p", "--output-format", "json", "--yolo", "--model", "deepseek/deepseek-v4.1-flash", "--effort", "low"},
			wantLast:  "hi",
		},
		{
			name:      "resume with session id",
			provider:  New("", WithEffort("high")),
			build:     func(p harness.Provider) []string { return p.(*provider).ResumeArgs("sess-123", "again") },
			wantFlags: []string{"cmd", "-p", "--output-format", "json", "--yolo", "--effort", "high", "-r", "sess-123"},
			wantLast:  "again",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.build(tt.provider)
			if len(args) != len(tt.wantFlags)+1 {
				t.Fatalf("args = %q, want %d elements", args, len(tt.wantFlags)+1)
			}
			for i, want := range tt.wantFlags {
				if args[i] != want {
					t.Errorf("args[%d] = %q, want %q", i, args[i], want)
				}
			}
			if got := args[len(args)-1]; got != tt.wantLast {
				t.Errorf("last arg = %q, want %q", got, tt.wantLast)
			}
		})
	}
}

func TestPrintCommandMirrorsArgs(t *testing.T) {
	p := New("m1", WithEffort("low")).(*provider)
	cmd := p.PrintCommand("say 'hi'")

	for _, want := range []string{"cmd", "-p", "--output-format", "json", "--yolo", "--model", "m1", "--effort", "low"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("PrintCommand(%q) = %q, missing %q", "say 'hi'", cmd, want)
		}
	}
	if !strings.HasSuffix(cmd, harness.ShellEscape("say 'hi'")) {
		t.Errorf("PrintCommand(%q) = %q, want escaped prompt last", "say 'hi'", cmd)
	}

	resume := p.ResumeCommand("sess-1", "again")
	if !strings.Contains(resume, "-r 'sess-1'") {
		t.Errorf("ResumeCommand = %q, want -r session before prompt", resume)
	}
	if !strings.Contains(resume, harness.ShellEscape("again")) {
		t.Errorf("ResumeCommand = %q, want prompt included", resume)
	}
}

func TestInteractiveArgs(t *testing.T) {
	if got := New("").InteractiveArgs("ignored"); len(got) != 1 || got[0] != "cmd" {
		t.Errorf("InteractiveArgs() = %q, want [cmd]", got)
	}
	got := New("m1").InteractiveArgs("ignored")
	want := []string{"cmd", "--model", "m1"}
	if len(got) != len(want) {
		t.Fatalf("InteractiveArgs() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("InteractiveArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestParseNoToolFixture asserts the plain text/thinking/usage capture stream
// maps to exactly one session id and one final result carrying usage.
func TestParseNoToolFixture(t *testing.T) {
	events := parseFixture(t, noToolFixture)

	var (
		sessionIDs []string
		results    []harness.Event
		texts      []string
		sawText    bool
		sawThink   bool
	)
	for _, ev := range events {
		switch ev.Type {
		case harness.EventSessionID:
			sessionIDs = append(sessionIDs, ev.SessionID)
		case harness.EventResult:
			results = append(results, ev)
		case harness.EventText:
			sawText = true
			texts = append(texts, ev.Text)
		case harness.EventReasoning:
			sawThink = true
		}
	}

	if len(sessionIDs) != 1 {
		t.Fatalf("EventSessionID count = %d (%q), want 1", len(sessionIDs), sessionIDs)
	}
	if want := "a52eea0d-c51e-41be-8381-19f377f71367"; sessionIDs[0] != want {
		t.Errorf("session id = %q, want %q", sessionIDs[0], want)
	}
	if len(results) != 1 {
		t.Fatalf("EventResult count = %d, want 1", len(results))
	}
	if results[0].Result != "OK" {
		t.Errorf("Result = %q, want %q", results[0].Result, "OK")
	}
	if results[0].Usage == nil {
		t.Fatalf("Result usage = nil, want non-nil")
	}
	if results[0].Usage.InputTokens == 0 {
		t.Errorf("Usage.InputTokens = 0, want non-zero")
	}
	if results[0].Usage.OutputTokens == 0 {
		t.Errorf("Usage.OutputTokens = 0, want non-zero")
	}
	if results[0].Usage.CacheReadInputTokens == 0 {
		t.Errorf("Usage.CacheReadInputTokens = 0, want non-zero")
	}
	if results[0].Usage.DurationMS == 0 {
		t.Errorf("Usage.DurationMS = 0, want non-zero")
	}
	if !sawText || strings.Join(texts, "") != "OK" {
		t.Errorf("text deltas = %q (saw=%v), want concatenation %q", texts, sawText, "OK")
	}
	if !sawThink {
		t.Errorf("no EventReasoning emitted, want thinking_delta mapped")
	}
}

// TestParseToolsFixture asserts the tool-using stream maps to a tool call and
// a matching tool result.
func TestParseToolsFixture(t *testing.T) {
	events := parseFixture(t, toolsFixture)

	var calls, results []harness.Event
	for _, ev := range events {
		switch ev.Type {
		case harness.EventToolCall:
			calls = append(calls, ev)
		case harness.EventToolResult:
			results = append(results, ev)
		}
	}

	if len(calls) == 0 {
		t.Fatalf("EventToolCall count = 0, want >= 1")
	}
	if calls[0].ToolName != "read_file" {
		t.Errorf("ToolName = %q, want %q", calls[0].ToolName, "read_file")
	}
	if calls[0].ToolID == "" {
		t.Errorf("ToolID = empty, want the toolCallId from the stream")
	}
	if !strings.Contains(calls[0].ToolArgs, "/etc/hostname") {
		t.Errorf("ToolArgs = %q, want the JSON input object", calls[0].ToolArgs)
	}

	if len(results) == 0 {
		t.Fatalf("EventToolResult count = 0, want >= 1")
	}
	if results[0].ToolOutput == "" {
		t.Errorf("ToolOutput = empty, want the joined result text")
	}
	if results[0].ToolID != calls[0].ToolID {
		t.Errorf("ToolResult ToolID = %q, want %q", results[0].ToolID, calls[0].ToolID)
	}
	if results[0].ToolError {
		t.Errorf("ToolError = true, want false for a successful read_file")
	}
}

func TestParseStreamLineGarbage(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"not json", "not json at all"},
		{"truncated json", `{"type":"event","event":{`},
		{"json array", `[1,2,3]`},
		{"unknown outer type", `{"type":"wat"}`},
		{"event without inner object", `{"type":"event"}`},
		{"unknown inner type", `{"type":"event","event":{"type":"nope"}}`},
		{"event with non-object inner", `{"type":"event","event":42}`},
		{"run_start without session", `{"type":"event","event":{"type":"run_start"}}`},
	}

	p := New("").(*provider)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.ParseStreamLine(tt.line); len(got) != 0 {
				t.Errorf("ParseStreamLine(%q) = %#v, want empty", tt.line, got)
			}
		})
	}
}

func TestParseResultTolerant(t *testing.T) {
	p := New("").(*provider)

	// A result line with no usage must still produce the final text.
	got := p.ParseStreamLine(`{"type":"result","finalText":"done"}`)
	if len(got) != 1 || got[0].Type != harness.EventResult || got[0].Result != "done" {
		t.Fatalf("ParseStreamLine = %#v, want one EventResult with Result %q", got, "done")
	}
	if got[0].Usage != nil {
		t.Errorf("Usage = %#v, want nil when the stream omits usage", got[0].Usage)
	}
}

// TestParseToolsFixtureNumTurns asserts the turn count from run_end is
// attached to the emitted EventResult. NOTE: the fixture ordering is run_end
// (L33) BEFORE result (L34), so the parser records turnCount on run_end and
// attaches it to the result event parsed afterwards. No extra EventResult is
// emitted for run_end itself.
func TestParseToolsFixtureNumTurns(t *testing.T) {
	events := parseFixture(t, toolsFixture)

	var results []harness.Event
	for _, ev := range events {
		if ev.Type == harness.EventResult {
			results = append(results, ev)
		}
	}

	if len(results) != 1 {
		t.Fatalf("EventResult count = %d, want 1", len(results))
	}
	if results[0].Usage == nil {
		t.Fatalf("Result usage = nil, want non-nil")
	}
	if got := results[0].Usage.NumTurns; got != 2 {
		t.Errorf("Usage.NumTurns = %d, want 2 (from run_end.result.turnCount)", got)
	}
}

// TestParseResultTolerantUsage asserts a malformed or absent usage value never
// discards the result event.
func TestParseResultTolerantUsage(t *testing.T) {
	p := New("").(*provider)

	// Malformed usage (a string instead of an object) must survive as a result
	// event carrying a zero-value Usage, not be dropped.
	got := p.ParseStreamLine(`{"type":"result","finalText":"X","usage":"nope"}`)
	if len(got) != 1 || got[0].Type != harness.EventResult || got[0].Result != "X" {
		t.Fatalf("ParseStreamLine(malformed usage) = %#v, want one EventResult with Result %q", got, "X")
	}
	if got[0].Usage == nil {
		t.Fatalf("Usage = nil, want a zero-value Usage when usage is present but malformed")
	}
	if got[0].Usage.InputTokens != 0 || got[0].Usage.OutputTokens != 0 || got[0].Usage.NumTurns != 0 {
		t.Errorf("Usage = %#v, want zero token fields for malformed usage", got[0].Usage)
	}

	// Usage absent must still yield the result (and a nil Usage, as before).
	absent := p.ParseStreamLine(`{"type":"result","finalText":"Y"}`)
	if len(absent) != 1 || absent[0].Type != harness.EventResult || absent[0].Result != "Y" {
		t.Fatalf("ParseStreamLine(absent usage) = %#v, want one EventResult with Result %q", absent, "Y")
	}
	if absent[0].Usage != nil {
		t.Errorf("Usage = %#v, want nil when the stream omits usage", absent[0].Usage)
	}
}

// TestParseEmptyDeltasSkipped asserts empty/absent text and thinking deltas
// emit no event, matching the sibling adapters.
func TestParseEmptyDeltasSkipped(t *testing.T) {
	p := New("").(*provider)
	for _, line := range []string{
		`{"type":"event","event":{"type":"text_delta","delta":""}}`,
		`{"type":"event","event":{"type":"text_delta"}}`,
		`{"type":"event","event":{"type":"thinking_delta","delta":""}}`,
		`{"type":"event","event":{"type":"thinking_delta"}}`,
	} {
		if got := p.ParseStreamLine(line); len(got) != 0 {
			t.Errorf("ParseStreamLine(%q) = %#v, want empty", line, got)
		}
	}
}

// TestParseUsageCacheWriteTokens asserts cacheWriteTokens maps to
// CacheCreationInputTokens with a non-zero value (both fixtures carry 0).
func TestParseUsageCacheWriteTokens(t *testing.T) {
	p := New("").(*provider)
	got := p.ParseStreamLine(`{"type":"result","finalText":"z","durationMs":5,"usage":{"inputTokens":1,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":7}}`)
	if len(got) != 1 || got[0].Usage == nil {
		t.Fatalf("ParseStreamLine = %#v, want one EventResult with usage", got)
	}
	if got[0].Usage.CacheCreationInputTokens != 7 {
		t.Errorf("CacheCreationInputTokens = %d, want 7", got[0].Usage.CacheCreationInputTokens)
	}
	if got[0].Usage.CacheReadInputTokens != 3 {
		t.Errorf("CacheReadInputTokens = %d, want 3", got[0].Usage.CacheReadInputTokens)
	}
	if got[0].Usage.DurationMS != 5 {
		t.Errorf("DurationMS = %d, want 5", got[0].Usage.DurationMS)
	}
}

// TestParseToolCompletedError asserts an error result item sets ToolError.
func TestParseToolCompletedError(t *testing.T) {
	p := New("").(*provider)
	got := p.ParseStreamLine(`{"type":"event","event":{"type":"tool_completed","toolCallId":"c1","toolName":"bash","result":[{"type":"error","text":"boom"}]}}`)
	if len(got) != 1 || got[0].Type != harness.EventToolResult {
		t.Fatalf("ParseStreamLine = %#v, want one EventToolResult", got)
	}
	if !got[0].ToolError {
		t.Errorf("ToolError = false, want true for an error result item")
	}
	if got[0].ToolOutput != "boom" {
		t.Errorf("ToolOutput = %q, want %q", got[0].ToolOutput, "boom")
	}
	if got[0].ToolID != "c1" {
		t.Errorf("ToolID = %q, want %q", got[0].ToolID, "c1")
	}
}

// parseFixture reads a NDJSON fixture from testdata and parses every line.
func parseFixture(t *testing.T, name string) []harness.Event {
	t.Helper()

	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	p := New("").(*provider)
	var events []harness.Event
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		events = append(events, p.ParseStreamLine(line)...)
	}
	return events
}
