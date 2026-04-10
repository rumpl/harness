package pi

import "github.com/rumpl/harness"

// parseStreamLine handles the Pi JSON streaming format.
// It recognises three event shapes:
//   - {"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"..."}} → text events
//   - {"type":"tool_execution_start",...} → tool_call events
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
	if !ok {
		return nil
	}
	argField, ok := harness.ToolArgFields[toolName]
	if !ok {
		return nil
	}
	input, ok := obj["input"].(map[string]any)
	if !ok {
		return nil
	}
	argValue, ok := input[argField].(string)
	if !ok {
		return nil
	}
	return []harness.Event{{
		Type:     harness.EventToolCall,
		ToolName: toolName,
		ToolArgs: argValue,
	}}
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
