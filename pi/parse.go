package pi

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/rumpl/harness"
)

// parseStreamLine handles the Pi JSON streaming format.
// It recognises these event shapes:
//   - {"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"..."}} → text events
//   - {"type":"tool_execution_start","toolCallId":"...","toolName":"...","args":{...}} → tool_call events
//   - {"type":"tool_execution_end","toolCallId":"...","toolName":"...","result":{"content":[...]},"isError":bool} → tool_result events
//   - {"type":"agent_end","messages":[...]} → result events
func parseStreamLine(line string) []harness.Event {
	obj, ok := harness.ParseJSON(line)
	if !ok {
		return nil
	}

	typ, _ := obj["type"].(string)

	switch typ {
	case "session":
		sessionID, _ := obj["id"].(string)
		if sessionID == "" {
			return nil
		}
		return []harness.Event{{Type: harness.EventSessionID, SessionID: sessionID}}
	case "message_update":
		return parseMessageUpdate(obj)
	case "tool_execution_start":
		return parseToolExecution(obj)
	case "tool_execution_end":
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
	toolName, _ := obj["toolName"].(string)
	if toolName == "" {
		return nil
	}
	input, _ := obj["args"].(map[string]any)
	toolCallID, _ := obj["toolCallId"].(string)
	return []harness.Event{{
		Type:     harness.EventToolCall,
		ToolID:   toolCallID,
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
	toolCallID, _ := obj["toolCallId"].(string)
	toolName, _ := obj["toolName"].(string)
	return []harness.Event{{
		Type:       harness.EventToolResult,
		ToolID:     toolCallID,
		ToolName:   toolName,
		ToolOutput: toolExecutionOutput(obj),
		ToolError:  toolExecutionErrored(obj),
	}}
}

// toolExecutionOutput extracts the tool's textual output. Pi places it in
// result.content, a list of typed content blocks such as
// [{"type":"text","text":"..."}].
func toolExecutionOutput(obj map[string]any) string {
	result, ok := obj["result"].(map[string]any)
	if !ok {
		return ""
	}
	return contentBlocksText(result["content"])
}

// contentBlocksText concatenates the text of a Pi content-block array such as
// [{"type":"text","text":"..."}]. Non-text blocks are ignored.
func contentBlocksText(raw any) string {
	blocks, ok := raw.([]any)
	if !ok {
		return ""
	}
	var out strings.Builder
	for _, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if bt, _ := block["type"].(string); bt != "" && bt != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok {
			out.WriteString(text)
		}
	}
	return out.String()
}

func toolExecutionErrored(obj map[string]any) bool {
	isError, _ := obj["isError"].(bool)
	return isError
}

func parseAgentEnd(obj map[string]any) []harness.Event {
	msgs, ok := obj["messages"].([]any)
	if !ok {
		return nil
	}

	// Find the last assistant message and extract its text content.
	var result string
	var lastAssistant map[string]any
	for _, v := range slices.Backward(msgs) {
		msg, ok := v.(map[string]any)
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
	var out strings.Builder
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if bt, _ := block["type"].(string); bt == "text" {
			if t, ok := block["text"].(string); ok {
				out.WriteString(t)
			}
		}
	}
	return out.String()
}
