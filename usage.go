package harness

import "encoding/json"

// ExtractUsage pulls token usage information out of a generic JSON object.
// Returns nil if the required fields are missing.
func ExtractUsage(obj map[string]any) *Usage {
	raw, ok := obj["usage"]
	if !ok {
		return nil
	}
	usageMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	inputTokens, ok := jsonNumber(usageMap, "input_tokens")
	if !ok {
		return nil
	}
	outputTokens, ok := jsonNumber(usageMap, "output_tokens")
	if !ok {
		return nil
	}

	u := &Usage{
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheReadInputTokens:     jsonNumberOr(usageMap, "cache_read_input_tokens"),
		CacheCreationInputTokens: jsonNumberOr(usageMap, "cache_creation_input_tokens"),
		TotalCostUSD:             jsonFloatOr(obj, "total_cost_usd", 0),
		NumTurns:                 jsonNumberOr(obj, "num_turns"),
		DurationMS:               jsonNumberOr(obj, "duration_ms"),
	}
	return u
}

// ParseJSON is a convenience that unmarshals a line into a map. It returns
// nil, false if the line is not valid JSON or does not start with '{'.
func ParseJSON(line string) (map[string]any, bool) {
	if line == "" || line[0] != '{' {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return nil, false
	}
	return obj, true
}

func jsonNumber(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func jsonNumberOr(m map[string]any, key string) int {
	v, ok := jsonNumber(m, key)
	if !ok {
		return 0
	}
	return v
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

// ExtractPiUsage pulls token usage from a Pi assistant message object.
// Pi uses a different schema: {"usage":{"input":N,"output":N,"cacheRead":N,"cacheWrite":N,"totalTokens":N,"cost":{...}}}
func ExtractPiUsage(msg map[string]any) *Usage {
	raw, ok := msg["usage"]
	if !ok {
		return nil
	}
	usageMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	inputTokens, ok := jsonNumber(usageMap, "input")
	if !ok {
		return nil
	}
	outputTokens, ok := jsonNumber(usageMap, "output")
	if !ok {
		return nil
	}

	u := &Usage{
		InputTokens:          inputTokens,
		OutputTokens:         outputTokens,
		CacheReadInputTokens: jsonNumberOr(usageMap, "cacheRead"),
	}

	// Cost info is nested: usage.cost.total
	if costMap, ok := usageMap["cost"].(map[string]any); ok {
		u.TotalCostUSD = jsonFloatOr(costMap, "total", 0)
	}

	return u
}

// ExtractCodexUsage pulls token usage from a Codex turn.completed object.
// Codex uses: {"usage":{"input_tokens":N,"output_tokens":N,"cached_input_tokens":N}}
func ExtractCodexUsage(obj map[string]any) *Usage {
	raw, ok := obj["usage"]
	if !ok {
		return nil
	}
	usageMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	inputTokens, ok := jsonNumber(usageMap, "input_tokens")
	if !ok {
		return nil
	}
	outputTokens, ok := jsonNumber(usageMap, "output_tokens")
	if !ok {
		return nil
	}

	return &Usage{
		InputTokens:          inputTokens,
		OutputTokens:         outputTokens,
		CacheReadInputTokens: jsonNumberOr(usageMap, "cached_input_tokens"),
	}
}
