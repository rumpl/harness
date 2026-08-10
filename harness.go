// Package harness provides a unified interface for interacting with different
// AI coding agent CLIs (Docker Agent, Claude Code, Pi, Codex, OpenCode). It abstracts away the
// differences in command construction and stream output parsing so that
// switching between agents requires minimal code changes.
//
// The central type is [Provider]. Create one with the constructor from a
// sub-package (e.g. [claudecode.New]), then call its methods to build
// commands or parse streaming output.
package harness

// Provider is the interface that all agent harnesses implement. It lets you
// build shell commands for non-interactive (print) mode, build argument lists
// for interactive mode, and parse newline-delimited JSON streaming output
// into a common event representation.
type Provider interface {
	// Name returns a human-readable identifier for this provider (e.g.
	// "claude-code", "pi", "codex").
	Name() string

	// PrintCommand returns a complete shell command string that runs the
	// agent in non-interactive (print/exec) mode with the given prompt.
	// The returned string is safe to pass to "sh -c".
	PrintCommand(prompt string) string

	// InteractiveArgs returns the argument list (including the binary name
	// as the first element) for launching the agent in interactive mode.
	InteractiveArgs(prompt string) []string

	// ParseStreamLine parses a single line of newline-delimited JSON output
	// from the agent's streaming mode into zero or more [Event] values. Lines
	// that are not valid JSON or not recognized are silently ignored (an empty
	// slice is returned).
	ParseStreamLine(line string) []Event
}

// ResumableProvider is implemented by providers whose CLI can continue an
// existing session. All providers shipped by this module implement it.
// Callers normally use [Resume] rather than invoking ResumeCommand directly.
type ResumableProvider interface {
	Provider

	// ResumeCommand returns a shell command that sends prompt to sessionID.
	// The returned string is safe to pass to "sh -c".
	ResumeCommand(sessionID, prompt string) string
}

// EventType enumerates the kinds of events that a stream can produce.
type EventType string

const (
	// EventText is emitted when the agent produces a chunk of text output.
	EventText EventType = "text"
	// EventResult is emitted when the agent produces its final answer.
	EventResult EventType = "result"
	// EventToolCallStart is emitted when the agent starts building a tool call.
	EventToolCallStart EventType = "tool_call_start"
	// EventToolCallDelta is emitted when the agent streams tool call arguments.
	EventToolCallDelta EventType = "tool_call_delta"
	// EventToolCall is emitted when the agent invokes a tool.
	EventToolCall EventType = "tool_call"
	// EventToolResult is emitted when a tool invocation completes.
	EventToolResult EventType = "tool_result"
	// EventReasoning is emitted when the agent produces reasoning/thinking content.
	// Not all providers support this; check provider documentation.
	EventReasoning EventType = "reasoning"
	// EventSessionID identifies the persistent session used by the run. Save
	// SessionID and pass it to [Resume] to continue the conversation later.
	EventSessionID EventType = "session_id"
)

// Event is a single parsed event from an agent's streaming output. Depending
// on the Type field, different fields are populated:
//
//   - EventText:          Text is set.
//   - EventResult:        Result and (optionally) Usage are set.
//   - EventToolCallStart: ToolID (optional) and ToolName are set.
//   - EventToolCallDelta: ToolID (optional), ToolName (optional), and ToolArgs
//     are set to the raw argument delta.
//   - EventToolCall:      ToolID (optional), ToolName, and ToolArgs are set to
//     the provider's JSON argument object when available.
//   - EventToolResult:    ToolID (optional), ToolName (optional), ToolOutput,
//     and ToolError are set.
//   - EventReasoning:     Reasoning is set.
//   - EventSessionID:     SessionID is set.
type Event struct {
	Type       EventType `json:"type"`
	Text       string    `json:"text,omitempty"`
	Result     string    `json:"result,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	Usage      *Usage    `json:"usage,omitempty"`
	ToolID     string    `json:"tool_id,omitempty"`
	ToolName   string    `json:"name,omitempty"`
	ToolArgs   string    `json:"args,omitempty"`
	ToolOutput string    `json:"output,omitempty"`
	ToolError  bool      `json:"tool_error,omitempty"`
	Reasoning  string    `json:"reasoning,omitempty"` // set when Type == EventReasoning
}

// Usage captures token and cost statistics reported by the agent at the end
// of a run.
type Usage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	TotalCostUSD             float64 `json:"total_cost_usd"`
	NumTurns                 int     `json:"num_turns"`
	DurationMS               int     `json:"duration_ms"`
}
