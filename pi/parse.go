package pi

import (
	"encoding/json"

	"github.com/rumpl/harness"
)

// parseStreamLine handles the Pi JSON streaming format.
// It recognises these event shapes:
//   - {"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"..."}} → text events
//   - {"type":"tool_execution_start",...} → tool_call events
//   - {"type":"tool_execution_result",...} → tool_result events
//   - {"type":"agent_end","messages":[...]} → result events
func parseStreamLine(line string) []harness.Event {
	obj, ok := harness.ParseJSON(line)
	if !ok {
		return nil
	}

	typ, _ := obj["type"].(string)

	switch typ {
	case "message_update":
		return parseMessageUpdate(obj)
	case "tool_execution_start":
		return parseToolExecution(obj)
	case "tool_execution_result", "tool_execution_end", "tool_execution_completed":
		return parseToolExecutionResult(obj)
	case "agent_end":
		return parseAgentEnd(obj)
	}
	return nil
}

func parseMessageUpdate(obj map[string]any) []harness.Event {
	ev, ok := obj["assistantMessageEvent"].(map[string]any)
	if !ok {
		return nil
	}
	evType, _ := ev["type"].(string)
	if evType != "text_delta" {
		return nil
	}
	delta, ok := ev["delta"].(string)
	if !ok || delta == "" {
		return nil
	}
	return []harness.Event{{Type: harness.EventText, Text: delta}}
}

func parseToolExecution(obj map[string]any) []harness.Event {
	toolName, ok := obj["tool_name"].(string)
	if !ok || toolName == "" {
		return nil
	}
	input, _ := obj["input"].(map[string]any)
	return []harness.Event{{
		Type:     harness.EventToolCall,
		ToolID:   firstString(obj, "tool_execution_id", "tool_call_id", "id"),
		ToolName: toolName,
		ToolArgs: jsonObjectString(input),
	}}
}

func jsonObjectString(input map[string]any) string {
	if input == nil {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(b)
}

func parseToolExecutionResult(obj map[string]any) []harness.Event {
	return []harness.Event{{
		Type:       harness.EventToolResult,
		ToolID:     firstString(obj, "tool_execution_id", "tool_call_id", "id"),
		ToolName:   firstString(obj, "tool_name", "name"),
		ToolOutput: toolExecutionOutput(obj),
		ToolError:  toolExecutionErrored(obj),
	}}
}

func toolExecutionOutput(obj map[string]any) string {
	if output := firstString(obj, "output", "result", "content", "stderr", "stdout"); output != "" {
		return output
	}
	if result, ok := obj["result"].(map[string]any); ok {
		return firstString(result, "output", "content", "stderr", "stdout")
	}
	return ""
}

func toolExecutionErrored(obj map[string]any) bool {
	if isError, ok := obj["is_error"].(bool); ok {
		return isError
	}
	if _, ok := obj["error"].(string); ok {
		return true
	}
	return false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok {
			return value
		}
	}
	return ""
}

func parseAgentEnd(obj map[string]any) []harness.Event {
	msgs, ok := obj["messages"].([]any)
	if !ok {
		return nil
	}

	// Find the last assistant message and extract its text content.
	var result string
	var lastAssistant map[string]any
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "assistant" {
			lastAssistant = msg
			result = extractTextContent(msg)
			break
		}
	}
	if lastAssistant == nil {
		return nil
	}

	return []harness.Event{{
		Type:   harness.EventResult,
		Result: result,
		Usage:  harness.ExtractPiUsage(lastAssistant),
	}}
}

// extractTextContent concatenates all text blocks from a message's content array.
func extractTextContent(msg map[string]any) string {
	content, ok := msg["content"].([]any)
	if !ok {
		return ""
	}
	var out string
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if bt, _ := block["type"].(string); bt == "text" {
			if t, ok := block["text"].(string); ok {
				out += t
			}
		}
	}
	return out
}
