package pi

import "github.com/rumpl/harness"

// parseStreamLine handles the Pi JSON streaming format.
// It recognises three event shapes:
//   - {"type":"message_update","content":[...]} → text events
//   - {"type":"tool_execution_start",...} → tool_call events
//   - {"type":"agent_end","last_assistant_message":"..."} → result events
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
	content, ok := obj["content"].([]any)
	if !ok {
		return nil
	}

	var texts []string
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if bt, _ := block["type"].(string); bt == "text_delta" {
			if t, ok := block["text"].(string); ok {
				texts = append(texts, t)
			}
		}
	}
	if len(texts) == 0 {
		return nil
	}

	combined := ""
	for _, t := range texts {
		combined += t
	}
	return []harness.Event{{Type: harness.EventText, Text: combined}}
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
	result, ok := obj["last_assistant_message"].(string)
	if !ok {
		return nil
	}
	return []harness.Event{{
		Type:   harness.EventResult,
		Result: result,
		Usage:  harness.ExtractUsage(obj),
	}}
}
