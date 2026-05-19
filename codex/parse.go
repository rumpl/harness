package codex

import (
	"strings"

	"github.com/rumpl/harness"
)

// parseStreamLine handles the Codex JSON streaming format.
// It recognises these event shapes:
//   - {"type":"item.completed","item":{"type":"agent_message","text":"..."}} → text + result
//   - {"type":"item.started","item":{"type":"command_execution","command":"..."}} → tool_call
//   - {"type":"item.completed","item":{"type":"command_execution",...}} → tool_result
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
	switch itemType, _ := item["type"].(string); itemType {
	case "agent_message":
		return parseAgentMessageCompleted(item)
	case "command_execution":
		return parseCommandExecutionCompleted(item)
	}
	return nil
}

func parseAgentMessageCompleted(item map[string]any) []harness.Event {
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
		ToolID:   firstString(item, "id", "call_id", "tool_call_id"),
		ToolName: "Bash",
		ToolArgs: command,
	}}
}

func parseCommandExecutionCompleted(item map[string]any) []harness.Event {
	return []harness.Event{{
		Type:       harness.EventToolResult,
		ToolID:     firstString(item, "id", "call_id", "tool_call_id"),
		ToolName:   "Bash",
		ToolOutput: commandOutput(item),
		ToolError:  commandErrored(item),
	}}
}

func commandOutput(item map[string]any) string {
	if output := firstString(item, "output", "aggregated_output", "stdout", "stderr"); output != "" {
		return output
	}
	if result, ok := item["result"].(map[string]any); ok {
		return firstString(result, "output", "aggregated_output", "stdout", "stderr")
	}
	return ""
}

func commandErrored(item map[string]any) bool {
	if isError, ok := item["is_error"].(bool); ok {
		return isError
	}
	if exitCode, ok := numberField(item, "exit_code"); ok {
		return exitCode != 0
	}
	status := strings.ToLower(firstString(item, "status"))
	return status == "failed" || status == "error"
}

func numberField(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	}
	return 0, false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok {
			return value
		}
	}
	return ""
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
