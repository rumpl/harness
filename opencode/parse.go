package opencode

import (
	"encoding/json"

	"github.com/rumpl/harness"
)

// opencodeToolArgs maps opencode tool names to the input field that contains
// the human-readable argument for display. opencode uses lowercase tool names
// distinct from Claude Code's, so this is kept separate from the global
// [harness.ToolArgFields] allowlist.
var opencodeToolArgs = map[string]string{
	"bash":        "command",
	"webfetch":    "url",
	"websearch":   "query",
	"read":        "filePath",
	"read_file":   "filePath",
	"edit":        "filePath",
	"edit_file":   "filePath",
	"write":       "filePath",
	"grep":        "pattern",
	"glob":        "pattern",
	"apply_patch": "patch",
	"task":        "description",
	"skill":       "name",
}

// parser carries state across lines so we can emit a final EventResult
// (carrying the accumulated assistant text and token usage) when opencode
// reports that the final step finished.
//
// opencode's JSON stream has changed across releases. We support both the
// current run output seen in opencode 1.15.x:
//   - {"type":"text","part":{"type":"text","text":"...","time":{"end":...}}}
//   - {"type":"tool_use","part":{"type":"tool","tool":"bash","state":{...}}}
//   - {"type":"step_finish","part":{"reason":"stop","tokens":{...},"cost":...}}
//
// and the server event stream shape used for true token streaming:
//   - {"type":"message.part.updated","properties":{"part":{...}}}
//   - {"type":"message.part.delta","properties":{"partID":"...","field":"text","delta":"..."}}
//   - {"type":"session.status","properties":{"status":"idle"}}
type parser struct {
	lastText      string
	usage         harness.Usage
	haveUsage     bool
	startedAt     int
	partTypes     map[string]string
	streamedParts map[string]bool
}

func (p *parser) parseLine(line string) []harness.Event {
	obj, ok := harness.ParseJSON(line)
	if !ok {
		return nil
	}

	typ, _ := obj["type"].(string)
	switch typ {
	case "message.part.updated":
		return p.parsePartUpdated(obj)
	case "message.part.delta":
		return p.parsePartDelta(obj)
	case "session.status":
		return p.parseSessionStatus(obj)
	case "step_start":
		return p.parseStepStart(obj)
	case "step_finish":
		return p.parseStepFinish(obj)
	case "text":
		part, ok := eventPart(obj)
		if !ok {
			return nil
		}
		return p.parseTextPart(part)
	case "reasoning":
		part, ok := eventPart(obj)
		if !ok {
			return nil
		}
		return p.parseReasoningPart(part)
	case "tool_use", "tool":
		part, ok := eventPart(obj)
		if !ok {
			return nil
		}
		return parseToolPart(part)
	case "usage":
		return p.parseUsageEvent(obj)
	}
	return nil
}

func (p *parser) parsePartUpdated(obj map[string]any) []harness.Event {
	part, ok := eventPart(obj)
	if !ok {
		return nil
	}

	p.rememberPart(part)

	partType, _ := part["type"].(string)
	switch partType {
	case "text":
		return p.parseTextPart(part)
	case "reasoning":
		return p.parseReasoningPart(part)
	case "tool":
		return parseToolPart(part)
	case "step-start":
		return p.startStep(eventTimestamp(obj))
	case "step-finish":
		return p.finishStep(part, eventTimestamp(obj))
	}
	return nil
}

// parseTextPart emits an EventText only when the text part is final
// (has a non-zero time.end), matching how opencode renders assistant
// messages. It also records the text so it can be re-emitted as the
// final EventResult when the step/session completes.
func (p *parser) parseTextPart(part map[string]any) []harness.Event {
	if !hasTimeEnd(part) {
		return nil
	}
	text, ok := part["text"].(string)
	if !ok || text == "" {
		return nil
	}
	if p.wasStreamed(part) {
		return nil
	}
	p.lastText += text
	return []harness.Event{{Type: harness.EventText, Text: text}}
}

func (p *parser) parseReasoningPart(part map[string]any) []harness.Event {
	if !hasTimeEnd(part) {
		return nil
	}
	text, ok := part["text"].(string)
	if !ok || text == "" {
		return nil
	}
	if p.wasStreamed(part) {
		return nil
	}
	return []harness.Event{{Type: harness.EventReasoning, Reasoning: text}}
}

func (p *parser) parsePartDelta(obj map[string]any) []harness.Event {
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return nil
	}
	field, _ := props["field"].(string)
	if field != "text" {
		return nil
	}
	delta, ok := props["delta"].(string)
	if !ok || delta == "" {
		return nil
	}

	partID, _ := props["partID"].(string)
	p.markStreamed(partID)
	if p.partTypes[partID] == "reasoning" {
		return []harness.Event{{Type: harness.EventReasoning, Reasoning: delta}}
	}

	p.lastText += delta
	return []harness.Event{{Type: harness.EventText, Text: delta}}
}

// parseToolPart emits an EventToolCall when the tool reaches a terminal
// state (completed or error). The argument is pulled from state.input using
// the opencode tool-name allowlist.
func parseToolPart(part map[string]any) []harness.Event {
	name, _ := part["tool"].(string)
	if name == "" {
		name, _ = part["name"].(string)
	}
	if name == "" {
		return nil
	}
	state, ok := part["state"].(map[string]any)
	if !ok {
		return nil
	}
	status, _ := state["status"].(string)
	if status != "completed" && status != "error" {
		return nil
	}

	argField, ok := opencodeToolArgs[name]
	if !ok {
		return nil
	}
	input, ok := state["input"].(map[string]any)
	if !ok {
		return nil
	}
	argValue, ok := input[argField].(string)
	if !ok {
		return nil
	}
	return []harness.Event{{
		Type:     harness.EventToolCall,
		ToolName: name,
		ToolArgs: argValue,
	}}
}

func (p *parser) parseStepStart(obj map[string]any) []harness.Event {
	return p.startStep(eventTimestamp(obj))
}

func (p *parser) startStep(startedAt int) []harness.Event {
	// A new assistant step starts a new visible text block. Keep usage until
	// the final step so multi-step runs (tool call + answer) get aggregated.
	p.lastText = ""
	p.partTypes = nil
	p.streamedParts = nil
	if p.startedAt == 0 {
		p.startedAt = startedAt
	}
	return nil
}

func (p *parser) parseStepFinish(obj map[string]any) []harness.Event {
	part, ok := eventPart(obj)
	if !ok {
		return nil
	}
	return p.finishStep(part, eventTimestamp(obj))
}

func (p *parser) finishStep(part map[string]any, endedAt int) []harness.Event {
	if usage := extractOpencodeUsage(part); usage != nil {
		p.addUsage(usage)
	}

	reason, _ := part["reason"].(string)
	if !isFinalStepReason(reason) {
		// This step ended because the model requested tools. The final answer
		// should come in a later step, so do not use any pre-tool text as the
		// result.
		p.lastText = ""
		p.partTypes = nil
		p.streamedParts = nil
		return nil
	}

	return p.emitResult(endedAt)
}

// parseUsageEvent records a standalone usage event if opencode emits one.
// Current opencode places tokens/cost on step_finish, but accepting this shape
// keeps the parser tolerant of versions that split usage into its own event.
func (p *parser) parseUsageEvent(obj map[string]any) []harness.Event {
	if usage := extractOpencodeUsage(obj); usage != nil {
		p.addUsage(usage)
	}
	if p.lastText == "" {
		return nil
	}
	endedAt := intFieldOr(obj, "timestamp", 0)
	return p.emitResult(endedAt)
}

// parseSessionStatus turns a legacy "session.status" event with status ==
// "idle" into an EventResult carrying the last completed text.
func (p *parser) parseSessionStatus(obj map[string]any) []harness.Event {
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return nil
	}
	status, _ := props["status"].(string)
	if status != "idle" {
		return nil
	}
	return p.emitResult(0)
}

func (p *parser) addUsage(u *harness.Usage) {
	if u == nil {
		return
	}
	p.haveUsage = true
	p.usage.InputTokens += u.InputTokens
	p.usage.OutputTokens += u.OutputTokens
	p.usage.CacheReadInputTokens += u.CacheReadInputTokens
	p.usage.CacheCreationInputTokens += u.CacheCreationInputTokens
	p.usage.TotalCostUSD += u.TotalCostUSD
	p.usage.DurationMS += u.DurationMS
	if u.NumTurns != 0 {
		p.usage.NumTurns += u.NumTurns
	} else {
		p.usage.NumTurns++
	}
}

func (p *parser) emitResult(endedAt int) []harness.Event {
	if p.lastText == "" && !p.haveUsage {
		return nil
	}

	var usage *harness.Usage
	if p.haveUsage {
		u := p.usage
		if p.startedAt != 0 && endedAt > p.startedAt {
			u.DurationMS = endedAt - p.startedAt
		}
		usage = &u
	}

	result := p.lastText
	p.lastText = ""
	p.usage = harness.Usage{}
	p.haveUsage = false
	p.startedAt = 0
	p.partTypes = nil
	p.streamedParts = nil

	return []harness.Event{{
		Type:   harness.EventResult,
		Result: result,
		Usage:  usage,
	}}
}

func (p *parser) rememberPart(part map[string]any) {
	id, _ := part["id"].(string)
	partType, _ := part["type"].(string)
	if id == "" || partType == "" {
		return
	}
	p.ensurePartState()
	p.partTypes[id] = partType
}

func (p *parser) markStreamed(partID string) {
	if partID == "" {
		return
	}
	p.ensurePartState()
	p.streamedParts[partID] = true
}

func (p *parser) wasStreamed(part map[string]any) bool {
	id, _ := part["id"].(string)
	if id == "" || p.streamedParts == nil {
		return false
	}
	return p.streamedParts[id]
}

func (p *parser) ensurePartState() {
	if p.partTypes == nil {
		p.partTypes = make(map[string]string)
	}
	if p.streamedParts == nil {
		p.streamedParts = make(map[string]bool)
	}
}

func eventPart(obj map[string]any) (map[string]any, bool) {
	if part, ok := obj["part"].(map[string]any); ok {
		return part, true
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	part, ok := props["part"].(map[string]any)
	return part, ok
}

func eventTimestamp(obj map[string]any) int {
	if timestamp := intFieldOr(obj, "timestamp", 0); timestamp != 0 {
		return timestamp
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return 0
	}
	return intFieldOr(props, "time", 0)
}

func isFinalStepReason(reason string) bool {
	switch reason {
	case "", "tool-calls", "tool_calls":
		return false
	default:
		return true
	}
}

func extractOpencodeUsage(obj map[string]any) *harness.Usage {
	if part, ok := obj["part"].(map[string]any); ok {
		if usage := extractOpencodeUsageFromMap(part); usage != nil {
			return usage
		}
	}
	return extractOpencodeUsageFromMap(obj)
}

func extractOpencodeUsageFromMap(obj map[string]any) *harness.Usage {
	// Claude Code-compatible usage shape, useful for standalone usage events.
	if usage := harness.ExtractUsage(obj); usage != nil {
		return usage
	}

	if raw, ok := obj["usage"].(map[string]any); ok {
		if usage := extractOpencodeTokenUsage(raw, obj); usage != nil {
			return usage
		}
	}
	if raw, ok := obj["tokens"].(map[string]any); ok {
		return extractOpencodeTokenUsage(raw, obj)
	}
	return nil
}

func extractOpencodeTokenUsage(tokens, container map[string]any) *harness.Usage {
	inputTokens, ok := intAnyField(tokens, "input", "input_tokens")
	if !ok {
		return nil
	}
	outputTokens, ok := intAnyField(tokens, "output", "output_tokens")
	if !ok {
		return nil
	}

	u := &harness.Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}

	if cache, ok := tokens["cache"].(map[string]any); ok {
		u.CacheReadInputTokens = intFieldOr(cache, "read", 0)
		u.CacheCreationInputTokens = intFieldOr(cache, "write", 0)
	}
	if v, ok := intAnyField(tokens, "cache_read_input_tokens", "cached_input_tokens", "cacheRead"); ok {
		u.CacheReadInputTokens = v
	}
	if v, ok := intAnyField(tokens, "cache_creation_input_tokens", "cacheWrite"); ok {
		u.CacheCreationInputTokens = v
	}

	if v, ok := floatAnyField(container, "cost", "total_cost_usd"); ok {
		u.TotalCostUSD = v
	} else if v, ok := floatAnyField(tokens, "cost", "total_cost_usd"); ok {
		u.TotalCostUSD = v
	}

	u.NumTurns = intFieldOr(container, "num_turns", intFieldOr(tokens, "num_turns", 0))
	u.DurationMS = intFieldOr(container, "duration_ms", intFieldOr(tokens, "duration_ms", 0))

	return u
}

// hasTimeEnd reports whether part.time.end is present and non-zero.
func hasTimeEnd(part map[string]any) bool {
	t, ok := part["time"].(map[string]any)
	if !ok {
		return false
	}
	end, ok := t["end"]
	if !ok || end == nil {
		return false
	}
	switch v := end.(type) {
	case float64:
		return v != 0
	case int:
		return v != 0
	case json.Number:
		i, err := v.Int64()
		return err == nil && i != 0
	case bool:
		return v
	case string:
		return v != ""
	}
	return true
}

func intFieldOr(m map[string]any, key string, fallback int) int {
	v, ok := intAnyField(m, key)
	if !ok {
		return fallback
	}
	return v
}

func intAnyField(m map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		v, ok := m[key]
		if !ok {
			continue
		}
		if n, ok := intValue(v); ok {
			return n, true
		}
	}
	return 0, false
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func floatAnyField(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		v, ok := m[key]
		if !ok {
			continue
		}
		if f, ok := floatValue(v); ok {
			return f, true
		}
	}
	return 0, false
}

func floatValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
