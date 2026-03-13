package monitor

import "strings"

func applyOllamaModelTuning(payload map[string]any, model string) {
	if payload == nil {
		return
	}
	if ollamaNeedsThinkingDisabled(model) {
		payload["think"] = false
	}
}

func ollamaNeedsThinkingDisabled(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(normalized, "qwen3")
}
