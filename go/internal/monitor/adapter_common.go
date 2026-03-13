package monitor

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func summarizeUniqueStrings(values []string, maxItems int, maxChars int, separator string) string {
	if separator == "" {
		separator = ", "
	}
	cleaned := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := normalizeTextSnippet(raw, 160)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, value)
	}
	if len(cleaned) == 0 {
		return ""
	}
	if maxItems > 0 && len(cleaned) > maxItems {
		remaining := len(cleaned) - maxItems
		cleaned = append(cleaned[:maxItems], fmt.Sprintf("+%d more", remaining))
	}
	return normalizeTextSnippet(strings.Join(cleaned, separator), maxChars)
}

func anyListToStrings(value any) []string {
	rows := asSlice(value)
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if text := normalizeTextSnippet(asString(row), 160); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func idString(value any) string {
	switch typed := value.(type) {
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return strings.TrimSpace(asString(value))
	}
}

func firstStringFromList(value any) string {
	for _, item := range anyListToStrings(value) {
		if item != "" {
			return item
		}
	}
	return ""
}

func originFromURL(rawURL string, fallback string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fallback
	}
	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
}
