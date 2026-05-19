package dockeragent

import (
	"encoding/json"

	"github.com/rumpl/harness"
)

// parseStreamLine handles the Docker Agent JSON streaming format.
// It recognises three event shapes:
//   - {"type":"agent_choice","content":"..."} → text events
//   - {"type":"tool_call","tool_call":{"function":{"name":"...","arguments":"..."}}} → tool_call events
//   - {"type":"stream_stopped"} with prior token_usage → result events
func parseStreamLine(line string) []harness.Event {
	obj, ok := harness.ParseJSON(line)
	if !ok {
		return nil
	}

	typ, _ := obj["type"].(string)

	switch typ {
	case "agent_choice":
		return parseAgentChoice(obj)
	case "tool_call":
		return parseToolCall(obj)
	case "token_usage":
		return parseTokenUsage(obj)
	case "stream_stopped":
		return parseStreamStopped(obj)
	}
	return nil
}

func parseAgentChoice(obj map[string]any) []harness.Event {
	content, ok := obj["content"].(string)
	if !ok || content == "" {
		return nil
	}
	return []harness.Event{{Type: harness.EventText, Text: content}}
}

func parseToolCall(obj map[string]any) []harness.Event {
	toolCall, ok := obj["tool_call"].(map[string]any)
	if !ok {
		return nil
	}
	fn, ok := toolCall["function"].(map[string]any)
	if !ok {
		return nil
	}
	name, _ := fn["name"].(string)
	if name == "" {
		return nil
	}

	args, _ := fn["arguments"].(string)

	return []harness.Event{{
		Type:     harness.EventToolCall,
		ToolName: name,
		ToolArgs: args,
	}}
}

func parseTokenUsage(obj map[string]any) []harness.Event {
	usage := extractDockerAgentUsage(obj)
	if usage == nil {
		return nil
	}
	// We emit a result event with usage info; the actual result text
	// comes from agent_choice events accumulated earlier
	return []harness.Event{{
		Type:  harness.EventResult,
		Usage: usage,
	}}
}

func parseStreamStopped(_ map[string]any) []harness.Event {
	// stream_stopped indicates the end; we could emit a result here
	// but the token_usage event already handles that
	return nil
}

// extractDockerAgentUsage pulls token usage from a docker-agent token_usage object.
// Format: {"type":"token_usage","usage":{"input_tokens":N,"output_tokens":N,"cost":N,...}}
func extractDockerAgentUsage(obj map[string]any) *harness.Usage {
	usageObj, ok := obj["usage"].(map[string]any)
	if !ok {
		return nil
	}

	inputTokens := jsonNumberOr(usageObj, "input_tokens", 0)
	outputTokens := jsonNumberOr(usageObj, "output_tokens", 0)
	if inputTokens == 0 && outputTokens == 0 {
		return nil
	}

	// Look for nested last_message for cached tokens
	var cacheRead int
	if lastMsg, ok := usageObj["last_message"].(map[string]any); ok {
		cacheRead = jsonNumberOr(lastMsg, "cached_input_tokens", 0)
	}

	return &harness.Usage{
		InputTokens:          inputTokens,
		OutputTokens:         outputTokens,
		CacheReadInputTokens: cacheRead,
		TotalCostUSD:         jsonFloatOr(usageObj, "cost", 0),
	}
}

func jsonNumberOr(m map[string]any, key string, fallback int) int {
	v, ok := m[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return fallback
		}
		return int(i)
	}
	return fallback
}

func jsonFloatOr(m map[string]any, key string, fallback float64) float64 {
	v, ok := m[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return fallback
		}
		return f
	}
	return fallback
}
