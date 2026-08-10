package harness

import (
	"context"
	"testing"
	"time"
)

type testContext struct{}

func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Err() error                  { return nil }
func (testContext) Value(any) any               { return nil }

func TestShellEscape(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "'hello'"},
		{"it's a test", "'it'\\''s a test'"},
		{"", "''"},
		{"a'b'c", "'a'\\''b'\\''c'"},
	}
	for _, tt := range tests {
		got := ShellEscape(tt.in)
		if got != tt.want {
			t.Errorf("ShellEscape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractUsage(t *testing.T) {
	t.Run("returns nil when no usage key", func(t *testing.T) {
		obj := map[string]any{"foo": "bar"}
		if got := ExtractUsage(obj); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("returns nil when usage is not a map", func(t *testing.T) {
		obj := map[string]any{"usage": "not a map"}
		if got := ExtractUsage(obj); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("returns nil when input_tokens missing", func(t *testing.T) {
		obj := map[string]any{
			"usage": map[string]any{
				"output_tokens": float64(50),
			},
		}
		if got := ExtractUsage(obj); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("extracts full usage", func(t *testing.T) {
		obj := map[string]any{
			"usage": map[string]any{
				"input_tokens":                float64(100),
				"output_tokens":               float64(50),
				"cache_read_input_tokens":     float64(10),
				"cache_creation_input_tokens": float64(5),
			},
			"total_cost_usd": float64(0.01),
			"num_turns":      float64(3),
			"duration_ms":    float64(5000),
		}
		got := ExtractUsage(obj)
		if got == nil {
			t.Fatal("expected non-nil usage")
		}
		if got.InputTokens != 100 {
			t.Errorf("InputTokens = %d, want 100", got.InputTokens)
		}
		if got.OutputTokens != 50 {
			t.Errorf("OutputTokens = %d, want 50", got.OutputTokens)
		}
		if got.CacheReadInputTokens != 10 {
			t.Errorf("CacheReadInputTokens = %d, want 10", got.CacheReadInputTokens)
		}
		if got.CacheCreationInputTokens != 5 {
			t.Errorf("CacheCreationInputTokens = %d, want 5", got.CacheCreationInputTokens)
		}
		if got.TotalCostUSD != 0.01 {
			t.Errorf("TotalCostUSD = %f, want 0.01", got.TotalCostUSD)
		}
		if got.NumTurns != 3 {
			t.Errorf("NumTurns = %d, want 3", got.NumTurns)
		}
		if got.DurationMS != 5000 {
			t.Errorf("DurationMS = %d, want 5000", got.DurationMS)
		}
	})

	t.Run("defaults optional fields to zero", func(t *testing.T) {
		obj := map[string]any{
			"usage": map[string]any{
				"input_tokens":  float64(100),
				"output_tokens": float64(50),
			},
		}
		got := ExtractUsage(obj)
		if got == nil {
			t.Fatal("expected non-nil usage")
		}
		if got.CacheReadInputTokens != 0 {
			t.Errorf("CacheReadInputTokens = %d, want 0", got.CacheReadInputTokens)
		}
		if got.TotalCostUSD != 0 {
			t.Errorf("TotalCostUSD = %f, want 0", got.TotalCostUSD)
		}
	})
}

type customRunProvider struct {
	called bool
	prompt string
}

func (p *customRunProvider) Name() string { return "custom" }

func (p *customRunProvider) PrintCommand(string) string {
	return "false"
}

func (p *customRunProvider) InteractiveArgs(string) []string { return nil }

func (p *customRunProvider) ParseStreamLine(string) []Event { return nil }

func (p *customRunProvider) Run(_ context.Context, prompt string, fn func(Event)) error {
	p.called = true
	p.prompt = prompt
	fn(Event{Type: EventText, Text: "streamed"})
	return nil
}

func TestRunUsesCustomStreamingProvider(t *testing.T) {
	p := &customRunProvider{}
	var got []Event
	if err := Run(testContext{}, p, "hello", func(ev Event) {
		got = append(got, ev)
	}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !p.called {
		t.Fatal("custom Run was not called")
	}
	if p.prompt != "hello" {
		t.Fatalf("prompt = %q, want hello", p.prompt)
	}
	if len(got) != 1 || got[0].Type != EventText || got[0].Text != "streamed" {
		t.Fatalf("events = %+v, want streamed text", got)
	}
}

type customResumeProvider struct {
	customRunProvider

	sessionID string
}

func (p *customResumeProvider) Resume(_ context.Context, sessionID, prompt string, fn func(Event)) error {
	p.called = true
	p.sessionID = sessionID
	p.prompt = prompt
	fn(Event{Type: EventSessionID, SessionID: sessionID})
	return nil
}

func TestResumeUsesCustomStreamingProvider(t *testing.T) {
	p := &customResumeProvider{}
	var got Event
	if err := Resume(testContext{}, p, "session-123", "follow up", func(ev Event) {
		got = ev
	}); err != nil {
		t.Fatalf("Resume returned error: %v", err)
	}
	if !p.called || p.sessionID != "session-123" || p.prompt != "follow up" {
		t.Fatalf("resume call = called:%v session:%q prompt:%q", p.called, p.sessionID, p.prompt)
	}
	if got.Type != EventSessionID || got.SessionID != "session-123" {
		t.Fatalf("event = %+v, want session ID", got)
	}
}

func TestResumeRejectsEmptyOrUnsupportedSession(t *testing.T) {
	p := &customRunProvider{}
	if err := Resume(testContext{}, p, "", "follow up", func(Event) {}); err == nil {
		t.Fatal("expected empty session ID error")
	}
	if err := Resume(testContext{}, p, "session-123", "follow up", func(Event) {}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestParseJSON(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		obj, ok := ParseJSON(`{"type":"test"}`)
		if !ok {
			t.Fatal("expected ok")
		}
		if obj["type"] != "test" {
			t.Errorf("type = %v, want test", obj["type"])
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, ok := ParseJSON("")
		if ok {
			t.Fatal("expected not ok")
		}
	})

	t.Run("non-object", func(t *testing.T) {
		_, ok := ParseJSON("not json")
		if ok {
			t.Fatal("expected not ok")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, ok := ParseJSON("{bad json")
		if ok {
			t.Fatal("expected not ok")
		}
	})
}

// lineProvider parses each stdout line into one EventText carrying the line's
// byte length, so a test can assert the whole (possibly very large) line was
// delivered intact.
type lineProvider struct{ command string }

func (p *lineProvider) Name() string                    { return "line" }
func (p *lineProvider) PrintCommand(string) string      { return p.command }
func (p *lineProvider) InteractiveArgs(string) []string { return nil }
func (p *lineProvider) ParseStreamLine(line string) []Event {
	if line == "" {
		return nil
	}
	return []Event{{Type: EventText, Text: line}}
}

// TestRunHandlesLargeStreamLines guards against the 64KB bufio.Scanner default:
// an agent CLI that emits a single line bigger than 64KB must not stall the
// reader (which previously left the child blocked on a full stdout pipe).
func TestRunHandlesLargeStreamLines(t *testing.T) {
	const size = 512 * 1024 // 512 KiB, far past the old 64KB limit
	// Print one giant line, then a small one, then exit 0.
	command := "printf 'a%.0s' $(seq 1 " + itoa(size) + "); printf '\\n'; printf 'end\\n'"
	p := &lineProvider{command: command}

	var lines []int
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), p, "", func(ev Event) {
			lines = append(lines, len(ev.Text))
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run hung on a large stream line")
	}

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(lines), lines)
	}
	if lines[0] != size {
		t.Fatalf("first line length = %d, want %d", lines[0], size)
	}
	if lines[1] != 3 {
		t.Fatalf("second line length = %d, want 3 (\"end\")", lines[1])
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
