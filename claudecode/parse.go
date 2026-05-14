package claudecode

import (
	"encoding/json"

	"github.com/rumpl/harness"
)

// parseStreamLine handles the Claude Code stream-json format.
// It recognises two event shapes:
//   - {"type":"assistant","message":{"content":[...]}} → text and tool_call events
//   - {"type":"result","result":"..."} → result event
func parseStreamLine(line string) []harness.Event {
	obj, ok := harness.ParseJSON(line)
	if !ok {
		return nil
	}

	typ, _ := obj["type"].(string)

	switch typ {
	case "assistant":
		return parseAssistant(obj)
	case "result":
		return parseResult(obj)
	case "stream_event":
		return parseStreamEvent(obj)
	}
	return nil
}

func parseStreamEvent(obj map[string]any) []harness.Event {
	raw, ok := obj["event"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var inner struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock *struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
		Delta *struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
			Thinking    string `json:"thinking"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(b, &inner); err != nil {
		return nil
	}
	switch inner.Type {
	case "content_block_start":
		if inner.ContentBlock != nil && inner.ContentBlock.Type == "tool_use" {
			return []harness.Event{{
				Type:     harness.EventToolCall,
				ToolName: inner.ContentBlock.Name,
			}}
		}
	case "content_block_delta":
		if inner.Delta == nil {
			return nil
		}
		switch inner.Delta.Type {
		case "text_delta":
			if inner.Delta.Text != "" {
				return []harness.Event{{Type: harness.EventText, Text: inner.Delta.Text}}
			}
		case "thinking_delta":
			if inner.Delta.Thinking != "" {
				return []harness.Event{{Type: harness.EventReasoning, Reasoning: inner.Delta.Thinking}}
			}
		}
	}
	return nil
}

func parseAssistant(obj map[string]any) []harness.Event {
	msg, ok := obj["message"].(map[string]any)
	if !ok {
		return nil
	}
	content, ok := msg["content"].([]any)
	if !ok {
		return nil
	}

	var events []harness.Event
	var texts []string

	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)

		switch blockType {
		case "text":
			if t, ok := block["text"].(string); ok {
				texts = append(texts, t)
			}
		case "tool_use":
			name, _ := block["name"].(string)
			if name == "" {
				continue
			}
			argField, ok := harness.ToolArgFields[name]
			if !ok {
				continue
			}
			input, ok := block["input"].(map[string]any)
			if !ok {
				continue
			}
			argValue, ok := input[argField].(string)
			if !ok {
				continue
			}
			// Flush accumulated text before the tool call.
			if len(texts) > 0 {
				events = append(events, harness.Event{
					Type: harness.EventText,
					Text: join(texts),
				})
				texts = texts[:0]
			}
			events = append(events, harness.Event{
				Type:     harness.EventToolCall,
				ToolName: name,
				ToolArgs: argValue,
			})
		}
	}

	if len(texts) > 0 {
		events = append(events, harness.Event{
			Type: harness.EventText,
			Text: join(texts),
		})
	}

	return events
}

func parseResult(obj map[string]any) []harness.Event {
	result, ok := obj["result"].(string)
	if !ok {
		return nil
	}
	return []harness.Event{{
		Type:   harness.EventResult,
		Result: result,
		Usage:  harness.ExtractUsage(obj),
	}}
}

func join(ss []string) string {
	if len(ss) == 1 {
		return ss[0]
	}
	out := ""
	for _, s := range ss {
		out += s
	}
	return out
}
