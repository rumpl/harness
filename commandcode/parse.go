package commandcode

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/rumpl/harness"
)

// parser carries the small amount of cross-line state the command-code
// NDJSON stream requires: the turn count reported by a run_end event, kept so
// it can be attached to the EventResult emitted for the result line (the
// result line itself carries no turn count).
//
// Two line shapes are understood, and each line is decoded exactly once:
//
//	{"type":"event","event":{...}}   -> dispatch on the inner event.type
//	{"type":"result", ...}           -> final result (finalText + usage)
type parser struct {
	// pendingNumTurns is the turnCount seen on the most recent run_end event.
	// The observed fixture ordering is run_end (L33) then result (L34), so the
	// count is recorded first and applied to the following EventResult. If a
	// stream ever emits the result line before run_end, the result is emitted
	// without NumTurns and nothing extra is emitted for run_end (no duplicate
	// EventResult).
	pendingNumTurns int
}

// envelope is the single decoded form of any NDJSON line. Its fields union the
// two line shapes: Type/Event describe shape (a) and FinalText/DurationMS/Usage
// describe shape (b). Event and Usage are kept raw so the inner object and a
// malformed usage value can be handled without re-decoding the whole line.
type envelope struct {
	Type       string          `json:"type"`
	Event      json.RawMessage `json:"event"`
	FinalText  string          `json:"finalText"`
	DurationMS int             `json:"durationMs"`
	Usage      json.RawMessage `json:"usage"`
}

// innerEvent is the decoded form of the "event" object of a shape (a) line.
type innerEvent struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"sessionId"`
	Delta      string          `json:"delta"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Input      json.RawMessage `json:"input"`
	Result     []resultItem    `json:"result"`
}

// resultItem is one entry of an inner event's "result" array.
type resultItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// parseLine decodes a single NDJSON line into the events it carries. The whole
// line is unmarshalled once into an envelope; the inner event object (shape a)
// or the result fields (shape b) are read from that envelope rather than by
// re-parsing the line from scratch.
func (p *parser) parseLine(line string) []harness.Event {
	// Match harness.ParseJSON's guard: only a line that is a JSON object is
	// considered. This keeps arrays, empty strings and other non-object lines
	// mapping to nil.
	if line == "" || line[0] != '{' {
		return nil
	}

	var env envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return nil
	}

	switch env.Type {
	case "event":
		return p.parseEvent(env.Event)
	case "result":
		return p.parseResult(env)
	}
	return nil
}

// parseEvent handles shape (a): {"type":"event","event":{...}}. raw is the
// already-decoded "event" value from the line's single envelope decode.
func (p *parser) parseEvent(raw json.RawMessage) []harness.Event {
	if len(raw) == 0 {
		return nil
	}

	// run_end carries the aggregate turn count (result.turnCount) but produces
	// no events of its own. Record it so the following result line can report
	// Usage.NumTurns. Note run_end's "result" is an object (not the array used
	// by tool_completed), so it is probed before the innerEvent decode.
	var probe struct {
		Type   string `json:"type"`
		Result struct {
			TurnCount int `json:"turnCount"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.Type == "run_end" {
		if probe.Result.TurnCount > 0 {
			p.pendingNumTurns = probe.Result.TurnCount
		}
		return nil
	}

	var ev innerEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil
	}

	switch ev.Type {
	case "run_start":
		if ev.SessionID == "" {
			return nil
		}
		return []harness.Event{{Type: harness.EventSessionID, SessionID: ev.SessionID}}
	case "text_delta":
		if ev.Delta == "" {
			return nil
		}
		return []harness.Event{{Type: harness.EventText, Text: ev.Delta}}
	case "thinking_delta":
		if ev.Delta == "" {
			return nil
		}
		return []harness.Event{{Type: harness.EventReasoning, Reasoning: ev.Delta}}
	case "tool_queued":
		return []harness.Event{{
			Type:     harness.EventToolCall,
			ToolID:   ev.ToolCallID,
			ToolName: ev.ToolName,
			ToolArgs: jsonObjectString(ev.Input),
		}}
	case "tool_completed":
		output, isError := joinResults(ev.Result)
		return []harness.Event{{
			Type:       harness.EventToolResult,
			ToolID:     ev.ToolCallID,
			ToolName:   ev.ToolName,
			ToolOutput: output,
			ToolError:  isError,
		}}
	}
	return nil
}

// parseResult handles shape (b): {"type":"result", "finalText":..., "usage":...}
// using the result fields of the line's single envelope decode.
func (p *parser) parseResult(env envelope) []harness.Event {
	var usage *harness.Usage
	if len(env.Usage) > 0 {
		// Duration is top-level, so it is preserved even when usage is bad.
		usage = &harness.Usage{DurationMS: env.DurationMS}
		var rawUsage struct {
			InputTokens      int `json:"inputTokens"`
			OutputTokens     int `json:"outputTokens"`
			CacheReadTokens  int `json:"cacheReadTokens"`
			CacheWriteTokens int `json:"cacheWriteTokens"`
		}
		// A malformed usage value (e.g. "usage":"nope") must not drop the
		// result: on decode failure usage stays the zero value above and the
		// EventResult is still emitted.
		if err := json.Unmarshal(env.Usage, &rawUsage); err == nil {
			usage.InputTokens = rawUsage.InputTokens
			usage.OutputTokens = rawUsage.OutputTokens
			usage.CacheReadInputTokens = rawUsage.CacheReadTokens
			usage.CacheCreationInputTokens = rawUsage.CacheWriteTokens
		}
	}

	// Attach the turn count recorded from the preceding run_end event, if any.
	if p.pendingNumTurns > 0 {
		if usage == nil {
			usage = &harness.Usage{DurationMS: env.DurationMS}
		}
		usage.NumTurns = p.pendingNumTurns
		p.pendingNumTurns = 0
	}

	return []harness.Event{{
		Type:   harness.EventResult,
		Result: env.FinalText,
		Usage:  usage,
	}}
}

// jsonObjectString renders a raw JSON object in canonical form (sorted keys,
// no stray whitespace), returning "" when absent or not an object. It decodes
// into a map and re-encodes through a json.Encoder, which reproduces the exact
// key ordering and escaping a map round-trip produced before.
func jsonObjectString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil || input == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(input); err != nil {
		return ""
	}
	// Encoder appends a newline; trim it to match a compact marshal.
	return strings.TrimRight(buf.String(), "\n")
}

// joinResults concatenates the text of every result item and reports whether
// any item is an error.
func joinResults(items []resultItem) (string, bool) {
	var (
		parts   []string
		isError bool
	)
	for _, item := range items {
		if item.Type == "error" {
			isError = true
		}
		if item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, ""), isError
}
