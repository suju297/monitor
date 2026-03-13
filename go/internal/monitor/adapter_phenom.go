package monitor

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func phenomMaxPages(company Company, maxLinks int, pageSize int) int {
	if pageSize < 1 {
		pageSize = 10
	}
	fallback := (maxLinks / pageSize) + 2
	if fallback < 4 {
		fallback = 4
	}
	maxPages := commandEnvInt(company.CommandEnv, "PHENOM_MAX_PAGES", fallback, 1)
	if maxPages > 40 {
		return 40
	}
	return maxPages
}

func phenomPageURL(baseURL string, from int) string {
	if from <= 0 {
		return baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	query := parsed.Query()
	query.Set("from", strconv.Itoa(from))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func extractAssignedJSONObject(body []byte, marker string) []byte {
	idx := bytes.Index(body, []byte(marker))
	if idx < 0 {
		return nil
	}
	start := bytes.IndexByte(body[idx:], '{')
	if start < 0 {
		return nil
	}
	start += idx
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(body); i++ {
		ch := body[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[start : i+1]
			}
		}
	}
	return nil
}

func extractBalancedJSONSegment(body []byte, start int) []byte {
	if start < 0 || start >= len(body) {
		return nil
	}
	open := body[start]
	close := byte(0)
	switch open {
	case '{':
		close = '}'
	case '[':
		close = ']'
	default:
		return nil
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(body); i++ {
		ch := body[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return body[start : i+1]
			}
		}
	}
	return nil
}

func extractAssignedJSONArray(body []byte, marker string) []byte {
	idx := bytes.Index(body, []byte(marker))
	if idx < 0 {
		return nil
	}
	dataIdx := bytes.Index(body[idx:], []byte("data:"))
	if dataIdx < 0 {
		return nil
	}
	dataIdx += idx
	start := bytes.IndexByte(body[dataIdx:], '[')
	if start < 0 {
		return nil
	}
	start += dataIdx
	return extractBalancedJSONSegment(body, start)
}

func extractAssignedJSONArrayLiteral(body []byte, marker string) []byte {
	idx := bytes.Index(body, []byte(marker))
	if idx < 0 {
		return nil
	}
	start := bytes.IndexByte(body[idx:], '[')
	if start < 0 {
		return nil
	}
	start += idx
	return extractBalancedJSONSegment(body, start)
}

func queryValueFromURL(rawURL string, key string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get(key))
}

func looksLikePhenomJobRecord(row map[string]any) bool {
	return strings.TrimSpace(asString(row["jobSeqNo"])) != "" &&
		(strings.TrimSpace(asString(row["title"])) != "" ||
			strings.TrimSpace(asString(row["applyUrl"])) != "" ||
			strings.TrimSpace(asString(row["descriptionTeaser"])) != "")
}

func collectPhenomJobs(value any, seen map[string]struct{}, out *[]map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		if looksLikePhenomJobRecord(typed) {
			jobSeqNo := strings.TrimSpace(asString(typed["jobSeqNo"]))
			if _, exists := seen[jobSeqNo]; !exists {
				seen[jobSeqNo] = struct{}{}
				*out = append(*out, typed)
			}
			return
		}
		for _, child := range typed {
			collectPhenomJobs(child, seen, out)
		}
	case []any:
		for _, child := range typed {
			collectPhenomJobs(child, seen, out)
		}
	}
}

func phenomJobURL(row map[string]any, fallbackBase string) string {
	applyURL := strings.TrimSpace(asString(row["applyUrl"]))
	if applyURL == "" {
		return fallbackBase
	}
	parsed, err := url.Parse(applyURL)
	if err != nil {
		return strings.TrimSuffix(applyURL, "/apply")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/apply")
	return parsed.String()
}

func phenomTeam(row map[string]any) string {
	values := []string{}
	appendValue := func(value string) {
		value = normalizeTextSnippet(value, 140)
		if value == "" {
			return
		}
		if internshipRoleRE.MatchString(value) {
			return
		}
		for _, existing := range values {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		values = append(values, value)
	}
	appendValue(asString(row["category"]))
	for _, item := range asSlice(row["multi_category_array"]) {
		appendValue(asString(asMap(item)["category"]))
	}
	for _, item := range asSlice(row["multi_category"]) {
		appendValue(asString(item))
	}
	return strings.Join(values, " / ")
}

func fetchPhenom(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	pageSize := commandEnvInt(company.CommandEnv, "PHENOM_PAGE_SIZE", 10, 1)
	if pageSize > 100 {
		pageSize = 100
	}
	maxPages := phenomMaxPages(company, maxLinks, pageSize)

	locationIncludePattern := strings.TrimSpace(commandEnvValue(company.CommandEnv, "PHENOM_LOCATION_INCLUDE_PATTERN"))
	var locationIncludeRE *regexp.Regexp
	if locationIncludePattern != "" {
		compiled, err := regexp.Compile(locationIncludePattern)
		if err != nil {
			return nil, newFetchError("invalid PHENOM_LOCATION_INCLUDE_PATTERN for %s: %v", company.Name, err)
		}
		locationIncludeRE = compiled
	}

	seenRecords := map[string]struct{}{}
	seenURLs := map[string]struct{}{}
	jobs := make([]Job, 0, maxLinks)
	for page := 0; page < maxPages && len(jobs) < maxLinks; page++ {
		pageURL := phenomPageURL(company.CareersURL, page*pageSize)
		_, body, err := performRequest(client, "GET", pageURL, nil, nil)
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}
		payloadBytes := extractAssignedJSONObject(body, "phApp.ddo =")
		if len(payloadBytes) == 0 {
			if page == 0 {
				return nil, newFetchError("phenom payload missing for %s", company.Name)
			}
			break
		}

		var payload any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			if page == 0 {
				return nil, newFetchError("phenom payload parse failed for %s: %v", company.Name, err)
			}
			break
		}

		rows := []map[string]any{}
		collectPhenomJobs(payload, seenRecords, &rows)
		pageNewJobs := 0
		for _, row := range rows {
			title := normalizeTextSnippet(asString(row["title"]), 280)
			jobURL := phenomJobURL(row, pageURL)
			locationValue := strings.TrimSpace(asString(row["location"]))
			if locationValue == "" {
				locationValue = strings.TrimSpace(asString(row["cityStateCountry"]))
			}
			location := normalizeLocationSnippet(locationValue)
			if locationIncludeRE != nil && location != "" && !locationIncludeRE.MatchString(location) {
				continue
			}
			if title == "" || jobURL == "" || isNoiseTitle(title) {
				continue
			}
			if _, exists := seenURLs[jobURL]; exists {
				continue
			}
			seenURLs[jobURL] = struct{}{}
			externalID := strings.TrimSpace(asString(row["reqId"]))
			if externalID == "" {
				externalID = strings.TrimSpace(asString(row["jobSeqNo"]))
			}
			if externalID == "" {
				externalID = strings.TrimSpace(asString(row["jobId"]))
			}
			jobs = append(jobs, Job{
				Company:     company.Name,
				Source:      "phenom",
				Title:       title,
				URL:         jobURL,
				ExternalID:  externalID,
				Location:    location,
				Team:        phenomTeam(row),
				PostedAt:    normalizeCreatedAt(row["postedDate"]),
				Description: normalizeTextSnippet(asString(row["descriptionTeaser"]), 2200),
			})
			pageNewJobs++
			if len(jobs) >= maxLinks {
				break
			}
		}

		if pageNewJobs == 0 {
			break
		}
	}

	return jobs, nil
}
