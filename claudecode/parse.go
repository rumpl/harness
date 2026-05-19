package claudecode

import (
	"encoding/json"
	"strings"

	"github.com/rumpl/harness"
)

// parseStreamLine handles a single Claude Code stream-json line without
// retaining cross-line state. Provider.ParseStreamLine uses a stateful parser
// so it can de-duplicate Claude's partial stream events from the full message
// snapshots that follow them.
func parseStreamLine(line string) []harness.Event {
	return newParser().parseLine(line)
}

type parser struct {
	blocks                     map[int]*streamBlock
	streamedTextSinceAssistant bool
	emittedToolIDs             map[string]bool
	emittedToolSignatures      map[string]bool
	toolNames                  map[string]string
}

type streamBlock struct {
	blockType string
	id        string
	name      string
}

func newParser() *parser {
	return &parser{
		blocks:                make(map[int]*streamBlock),
		emittedToolIDs:        make(map[string]bool),
		emittedToolSignatures: make(map[string]bool),
		toolNames:             make(map[string]string),
	}
}

func (p *parser) parseLine(line string) []harness.Event {
	obj, ok := harness.ParseJSON(line)
	if !ok {
		return nil
	}

	typ, _ := obj["type"].(string)

	switch typ {
	case "assistant":
		return p.parseAssistant(obj)
	case "user":
		return p.parseUser(obj)
	case "result":
		return p.parseResult(obj)
	case "stream_event":
		return p.parseStreamEvent(obj)
	}
	return nil
}

func (p *parser) parseStreamEvent(obj map[string]any) []harness.Event {
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
		if inner.ContentBlock == nil {
			return nil
		}
		block := &streamBlock{
			blockType: inner.ContentBlock.Type,
			id:        inner.ContentBlock.ID,
			name:      inner.ContentBlock.Name,
		}
		p.blocks[inner.Index] = block
		if block.blockType != "tool_use" || block.name == "" {
			return nil
		}
		ev := harness.Event{
			Type:     harness.EventToolCallStart,
			ToolID:   block.id,
			ToolName: block.name,
		}
		p.markToolCallEmitted(ev)
		return []harness.Event{ev}
	case "content_block_delta":
		if inner.Delta == nil {
			return nil
		}
		switch inner.Delta.Type {
		case "text_delta":
			if inner.Delta.Text == "" {
				return nil
			}
			p.streamedTextSinceAssistant = true
			return []harness.Event{{Type: harness.EventText, Text: inner.Delta.Text}}
		case "thinking_delta":
			if inner.Delta.Thinking == "" {
				return nil
			}
			return []harness.Event{{Type: harness.EventReasoning, Reasoning: inner.Delta.Thinking}}
		case "input_json_delta":
			block := p.blocks[inner.Index]
			if block == nil || block.blockType != "tool_use" || block.name == "" || inner.Delta.PartialJSON == "" {
				return nil
			}
			return []harness.Event{{
				Type:     harness.EventToolCallDelta,
				ToolID:   block.id,
				ToolName: block.name,
				ToolArgs: inner.Delta.PartialJSON,
			}}
		}
	case "content_block_stop":
		block := p.blocks[inner.Index]
		delete(p.blocks, inner.Index)
		if block == nil || block.blockType != "tool_use" || block.name == "" {
			return nil
		}
		return []harness.Event{{
			Type:     harness.EventToolCall,
			ToolID:   block.id,
			ToolName: block.name,
		}}
	}
	return nil
}

func (p *parser) parseAssistant(obj map[string]any) []harness.Event {
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
	suppressText := p.streamedTextSinceAssistant

	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)

		switch blockType {
		case "text":
			if suppressText {
				continue
			}
			if t, ok := block["text"].(string); ok {
				texts = append(texts, t)
			}
		case "tool_use":
			ev, ok := p.toolCallFromAssistantBlock(block)
			if !ok || p.toolCallEmitted(ev) {
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
			p.markToolCallEmitted(ev)
			events = append(events, ev)
		}
	}

	if len(texts) > 0 {
		events = append(events, harness.Event{
			Type: harness.EventText,
			Text: join(texts),
		})
	}

	p.streamedTextSinceAssistant = false
	return events
}

func (p *parser) parseUser(obj map[string]any) []harness.Event {
	msg, ok := obj["message"].(map[string]any)
	if !ok {
		return nil
	}
	content, ok := msg["content"].([]any)
	if !ok {
		return nil
	}

	var events []harness.Event
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := block["type"].(string); typ != "tool_result" {
			continue
		}
		id, _ := block["tool_use_id"].(string)
		isError, _ := block["is_error"].(bool)
		events = append(events, harness.Event{
			Type:       harness.EventToolResult,
			ToolID:     id,
			ToolName:   p.toolNames[id],
			ToolOutput: extractToolResultContent(block),
			ToolError:  isError,
		})
	}
	return events
}

func (p *parser) parseResult(obj map[string]any) []harness.Event {
	result, ok := obj["result"].(string)
	if !ok {
		return nil
	}
	ev := harness.Event{
		Type:   harness.EventResult,
		Result: result,
		Usage:  harness.ExtractUsage(obj),
	}
	p.reset()
	return []harness.Event{ev}
}

func (p *parser) toolCallFromAssistantBlock(block map[string]any) (harness.Event, bool) {
	name, _ := block["name"].(string)
	if name == "" {
		return harness.Event{}, false
	}
	input, _ := block["input"].(map[string]any)
	id, _ := block["id"].(string)
	return harness.Event{
		Type:     harness.EventToolCall,
		ToolID:   id,
		ToolName: name,
		ToolArgs: jsonObjectString(input),
	}, true
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

func (p *parser) toolCallEmitted(ev harness.Event) bool {
	if ev.ToolID != "" && p.emittedToolIDs[ev.ToolID] {
		return true
	}
	return p.emittedToolSignatures[toolSignature(ev)]
}

func (p *parser) markToolCallEmitted(ev harness.Event) {
	if ev.ToolID != "" {
		p.emittedToolIDs[ev.ToolID] = true
		p.toolNames[ev.ToolID] = ev.ToolName
	}
	p.emittedToolSignatures[toolSignature(ev)] = true
}

func toolSignature(ev harness.Event) string {
	return ev.ToolName + "\x00" + ev.ToolArgs
}

func extractToolResultContent(block map[string]any) string {
	content, ok := block["content"]
	if !ok {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, raw := range v {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		text, _ := v["text"].(string)
		return text
	default:
		return ""
	}
}

func (p *parser) reset() {
	p.blocks = make(map[int]*streamBlock)
	p.streamedTextSinceAssistant = false
	p.emittedToolIDs = make(map[string]bool)
	p.emittedToolSignatures = make(map[string]bool)
	p.toolNames = make(map[string]string)
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
