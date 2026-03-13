package monitor

import (
	"fmt"
	htmlstd "html"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	slugSeparatorRE = regexp.MustCompile(`[-_]+`)
	whitespaceRE    = regexp.MustCompile(`\s+`)
	htmlTagRE       = regexp.MustCompile(`(?s)<[^>]*>`)
	relativeAgoRE   = regexp.MustCompile(`(?i)\b(\d+)\s*(minute|hour|day|week|month|year)s?\s+ago\b`)
	dateTokenREs    = []*regexp.Regexp{
		regexp.MustCompile(`\b\d{4}-\d{1,2}-\d{1,2}(?:[T ]\d{1,2}:\d{2}(?::\d{2})?)?(?:Z|[+-]\d{2}:?\d{2})?\b`),
		regexp.MustCompile(`\b\d{1,2}/\d{1,2}/\d{2,4}(?:\s+\d{1,2}:\d{2}(?::\d{2})?(?:\s?[APMapm]{2})?)?\b`),
		regexp.MustCompile(`\b(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:t(?:ember)?)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\.?\s+\d{1,2},?\s+\d{4}(?:\s+\d{1,2}:\d{2}(?::\d{2})?(?:\s?[APMapm]{2})?)?\b`),
		regexp.MustCompile(`\b\d{1,2}\s+(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:t(?:ember)?)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\.?,?\s+\d{4}(?:\s+\d{1,2}:\d{2}(?::\d{2})?(?:\s?[APMapm]{2})?)?\b`),
		regexp.MustCompile(`(?i)\b(?:today|yesterday|just now|\d+\s*(?:minute|hour|day|week|month|year)s?\s+ago)\b`),
	}
)

func truncateRunes(value string, maxChars int) string {
	if maxChars <= 0 {
		return strings.TrimSpace(value)
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(runes[:maxChars])) + "..."
}

func normalizeTextSnippet(value string, maxChars int) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ""
	}
	normalized = htmlstd.UnescapeString(normalized)
	normalized = htmlTagRE.ReplaceAllString(normalized, " ")
	normalized = whitespaceRE.ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return ""
	}
	return truncateRunes(normalized, maxChars)
}

func utcNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func parseBoolEnv(name string, defaultValue bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return defaultValue
	}
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func normalizeCreatedAt(value any) string {
	toTime := func(raw int64) string {
		if raw <= 0 {
			return ""
		}
		// Many APIs return Unix seconds while others return Unix milliseconds.
		if raw < 1_000_000_000_000 {
			return time.Unix(raw, 0).UTC().Format(time.RFC3339)
		}
		return time.UnixMilli(raw).UTC().Format(time.RFC3339)
	}
	switch v := value.(type) {
	case nil:
		return ""
	case float64:
		return toTime(int64(v))
	case int64:
		return toTime(v)
	case int:
		return toTime(int64(v))
	case string:
		raw := strings.TrimSpace(v)
		if raw == "" {
			return ""
		}
		if normalized := normalizePossibleDate(raw); normalized != "" {
			return normalized
		}
		return raw
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func parseDateWithLayouts(value string) (time.Time, bool) {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return time.Time{}, false
	}
	candidate = strings.Trim(candidate, "[](){}<>.,;")
	candidate = whitespaceRE.ReplaceAllString(candidate, " ")
	lower := strings.ToLower(candidate)
	for _, prefix := range []string{"posted on", "posted", "updated on", "updated", "date posted", "published on"} {
		if strings.HasPrefix(lower, prefix) {
			candidate = strings.TrimSpace(candidate[len(prefix):])
			candidate = strings.TrimLeft(candidate, ":- ")
			lower = strings.ToLower(candidate)
			break
		}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"2006-01-02",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 3:04 PM",
		"2006-01-02 3:04PM",
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"01/02/2006",
		"1/2/2006",
		"01/02/06",
		"1/2/06",
		"01/02/2006 15:04",
		"1/2/2006 15:04",
		"01/02/2006 3:04 PM",
		"1/2/2006 3:04 PM",
		"Jan 2, 2006",
		"January 2, 2006",
		"Jan 2 2006",
		"January 2 2006",
		"Jan 2, 2006 3:04 PM",
		"January 2, 2006 3:04 PM",
		"Jan 2, 2006 15:04",
		"January 2, 2006 15:04",
		"2 Jan 2006",
		"2 January 2006",
		"2 Jan 2006 3:04 PM",
		"2 January 2006 3:04 PM",
		"2 Jan 2006 15:04",
		"2 January 2006 15:04",
		"Mon, Jan 2, 2006",
		"Mon Jan 2 2006",
		"Mon, Jan 2, 2006 3:04 PM",
		"Mon, Jan 2, 2006 15:04",
	}

	layoutUsesExplicitZone := func(layout string) bool {
		switch layout {
		case time.RFC3339Nano, time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822:
			return true
		default:
			return false
		}
	}

	for _, layout := range layouts {
		if layoutUsesExplicitZone(layout) {
			if parsed, err := time.Parse(layout, candidate); err == nil {
				return parsed, true
			}
			continue
		}
		if parsed, err := time.ParseInLocation(layout, candidate, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func parseRelativeDate(value string) (time.Time, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return time.Time{}, false
	}
	now := time.Now()
	switch {
	case strings.Contains(lower, "just now"):
		return now, true
	case strings.Contains(lower, "yesterday"):
		return now.Add(-24 * time.Hour), true
	case strings.Contains(lower, "today"):
		return now, true
	}

	matches := relativeAgoRE.FindStringSubmatch(lower)
	if len(matches) != 3 {
		return time.Time{}, false
	}
	amount, err := strconv.Atoi(matches[1])
	if err != nil || amount <= 0 {
		return time.Time{}, false
	}
	var duration time.Duration
	switch matches[2] {
	case "minute":
		duration = time.Duration(amount) * time.Minute
	case "hour":
		duration = time.Duration(amount) * time.Hour
	case "day":
		duration = time.Duration(amount) * 24 * time.Hour
	case "week":
		duration = time.Duration(amount) * 7 * 24 * time.Hour
	case "month":
		duration = time.Duration(amount) * 30 * 24 * time.Hour
	case "year":
		duration = time.Duration(amount) * 365 * 24 * time.Hour
	default:
		return time.Time{}, false
	}
	return now.Add(-duration), true
}

func normalizePossibleDate(value string) string {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return ""
	}
	candidate = htmlstd.UnescapeString(candidate)
	candidate = whitespaceRE.ReplaceAllString(candidate, " ")
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}
	if parsed, ok := parseDateWithLayouts(candidate); ok {
		return parsed.UTC().Format(time.RFC3339)
	}
	if parsed, ok := parseRelativeDate(candidate); ok {
		return parsed.UTC().Format(time.RFC3339)
	}

	for _, tokenRE := range dateTokenREs {
		matches := tokenRE.FindAllString(candidate, -1)
		for _, token := range matches {
			if parsed, ok := parseDateWithLayouts(token); ok {
				return parsed.UTC().Format(time.RFC3339)
			}
			if parsed, ok := parseRelativeDate(token); ok {
				return parsed.UTC().Format(time.RFC3339)
			}
		}
	}
	return ""
}

func parseGreenhouseBoard(careersURL string) string {
	parsed, err := url.Parse(careersURL)
	if err != nil {
		return ""
	}
	if !strings.Contains(strings.ToLower(parsed.Host), "greenhouse.io") {
		return ""
	}
	parts := strings.Split(parsed.Path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p != "" {
			return p
		}
	}
	return ""
}

func parseLeverSite(careersURL string) string {
	parsed, err := url.Parse(careersURL)
	if err != nil {
		return ""
	}
	if !strings.Contains(strings.ToLower(parsed.Host), "lever.co") {
		return ""
	}
	parts := strings.Split(parsed.Path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p != "" {
			return p
		}
	}
	return ""
}

func parseAshbyBoard(careersURL string) string {
	parsed, err := url.Parse(careersURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if host == "" {
		return ""
	}
	parts := strings.Split(parsed.Path, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		piece := strings.TrimSpace(part)
		if piece == "" {
			continue
		}
		clean = append(clean, piece)
	}
	if strings.Contains(host, "api.ashbyhq.com") {
		if len(clean) >= 3 && strings.EqualFold(clean[0], "posting-api") && strings.EqualFold(clean[1], "job-board") {
			return clean[2]
		}
		return ""
	}
	if strings.Contains(host, "jobs.ashbyhq.com") {
		if len(clean) > 0 {
			return clean[0]
		}
	}
	return ""
}

func parseICIMSAPIEndpoint(careersURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(careersURL))
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s/api/jobs", parsed.Scheme, parsed.Host)
}

func parseICIMSJobsBasePath(careersURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(careersURL))
	if err != nil {
		return "/jobs"
	}
	parts := strings.Split(parsed.Path, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		piece := strings.TrimSpace(part)
		if piece == "" {
			continue
		}
		clean = append(clean, piece)
	}
	for idx, part := range clean {
		if strings.EqualFold(part, "jobs") {
			return "/" + strings.Join(clean[:idx+1], "/")
		}
	}
	return "/jobs"
}

func buildICIMSJobURL(careersURL string, reqID string, language string, canonical string) string {
	if strings.TrimSpace(canonical) != "" {
		if base, err := url.Parse(careersURL); err == nil {
			if candidate, err := url.Parse(strings.TrimSpace(canonical)); err == nil {
				absolute := base.ResolveReference(candidate)
				if absolute.Scheme != "" && absolute.Host != "" {
					return absolute.String()
				}
			}
		}
	}

	parsed, err := url.Parse(strings.TrimSpace(careersURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimSpace(careersURL)
	}
	base := fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, parseICIMSJobsBasePath(careersURL))
	reqID = strings.TrimSpace(reqID)
	if reqID != "" {
		base += "/" + reqID
	}
	language = strings.TrimSpace(language)
	if language == "" {
		return base
	}
	values := url.Values{}
	values.Set("lang", language)
	return base + "?" + values.Encode()
}

func looksLikeJobLink(text string, rawURL string) bool {
	combined := strings.ToLower(strings.TrimSpace(text + " " + rawURL))
	for _, keyword := range JobKeywords {
		if strings.Contains(combined, keyword) {
			return true
		}
	}
	return false
}

func titleFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "Possible opening"
	}
	parts := strings.Split(parsed.Path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		piece := strings.TrimSpace(parts[i])
		if piece == "" {
			continue
		}
		out := slugSeparatorRE.ReplaceAllString(piece, " ")
		out = strings.Join(strings.Fields(out), " ")
		if out == "" {
			return "Possible opening"
		}
		if len(out) > 150 {
			return out[:150]
		}
		return out
	}
	return "Possible opening"
}

func isChallengeTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	if normalized == "" {
		return false
	}
	for _, marker := range BlockedTitleMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func classifyHTTPBlock(statusCode int, title string) bool {
	if statusCode == 401 || statusCode == 403 || statusCode == 429 {
		return true
	}
	return isChallengeTitle(title)
}

func getNested(value any, path string, defaultValue any) any {
	if strings.TrimSpace(path) == "" {
		return value
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		switch c := current.(type) {
		case map[string]any:
			next, ok := c[part]
			if !ok {
				return defaultValue
			}
			current = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(c) {
				return defaultValue
			}
			current = c[idx]
		default:
			return defaultValue
		}
	}
	return current
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func splitRecipients(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	re := regexp.MustCompile(`[;,]`)
	parts := re.Split(raw, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
