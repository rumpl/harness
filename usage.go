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
		CacheReadInputTokens:     jsonNumberOr(usageMap, "cache_read_input_tokens", 0),
		CacheCreationInputTokens: jsonNumberOr(usageMap, "cache_creation_input_tokens", 0),
		TotalCostUSD:             jsonFloatOr(obj, "total_cost_usd", 0),
		NumTurns:                 jsonNumberOr(obj, "num_turns", 0),
		DurationMS:               jsonNumberOr(obj, "duration_ms", 0),
	}
	return u
}

// ParseJSON is a convenience that unmarshals a line into a map. It returns
// nil, false if the line is not valid JSON or does not start with '{'.
func ParseJSON(line string) (map[string]any, bool) {
	if len(line) == 0 || line[0] != '{' {
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

func jsonNumberOr(m map[string]any, key string, fallback int) int {
	v, ok := jsonNumber(m, key)
	if !ok {
		return fallback
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
