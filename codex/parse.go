package codex

import "github.com/rumpl/harness"

// parseStreamLine handles the Codex JSON streaming format.
// It recognises three event shapes:
//   - {"type":"item.completed","item":{"type":"agent_message","text":"..."}} → text + result
//   - {"type":"item.started","item":{"type":"command_execution","command":"..."}} → tool_call
//   - {"type":"turn.completed","usage":{...}} → updates usage on prior result
func parseStreamLine(line string) []harness.Event {
	obj, ok := harness.ParseJSON(line)
	if !ok {
		return nil
	}

	typ, _ := obj["type"].(string)

	switch typ {
	case "item.completed":
		return parseItemCompleted(obj)
	case "item.started":
		return parseItemStarted(obj)
	case "turn.completed":
		return parseTurnCompleted(obj)
	}
	return nil
}

func parseItemCompleted(obj map[string]any) []harness.Event {
	item, ok := obj["item"].(map[string]any)
	if !ok {
		return nil
	}
	if itemType, _ := item["type"].(string); itemType != "agent_message" {
		return nil
	}
	text, ok := item["text"].(string)
	if !ok {
		return nil
	}
	return []harness.Event{
		{Type: harness.EventText, Text: text},
		{Type: harness.EventResult, Result: text},
	}
}

func parseItemStarted(obj map[string]any) []harness.Event {
	item, ok := obj["item"].(map[string]any)
	if !ok {
		return nil
	}
	if itemType, _ := item["type"].(string); itemType != "command_execution" {
		return nil
	}
	command, ok := item["command"].(string)
	if !ok {
		return nil
	}
	return []harness.Event{{
		Type:     harness.EventToolCall,
		ToolName: "Bash",
		ToolArgs: command,
	}}
}

func parseTurnCompleted(obj map[string]any) []harness.Event {
	usage := harness.ExtractCodexUsage(obj)
	if usage == nil {
		return nil
	}
	return []harness.Event{{
		Type:  harness.EventResult,
		Usage: usage,
	}}
}
