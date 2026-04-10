package codex

import "github.com/rumpl/harness"

// parseStreamLine handles the Codex JSON streaming format.
// It recognises two event shapes:
//   - {"type":"item.completed","item":{"type":"agent_message","content":"..."}} → text + result
//   - {"type":"item.started","item":{"type":"command_execution","command":"..."}} → tool_call
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
	content, ok := item["content"].(string)
	if !ok {
		return nil
	}
	return []harness.Event{
		{Type: harness.EventText, Text: content},
		{Type: harness.EventResult, Result: content, Usage: harness.ExtractUsage(obj)},
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
