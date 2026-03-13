package monitor

import (
	"encoding/json"
	htmlstd "html"
	"net/url"
	"strconv"
	"strings"
)

func googleBaseResultsURL(company Company) string {
	rawURL := strings.TrimSpace(company.CareersURL)
	if rawURL == "" {
		return "https://www.google.com/about/careers/applications/jobs/results/"
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "https://www.google.com/about/careers/applications/jobs/results/"
	}
	if !strings.Contains(parsed.Path, "/jobs/results") {
		parsed.Path = "/about/careers/applications/jobs/results/"
	}
	parsed.Fragment = ""
	return parsed.String()
}

func googleSearchQuery(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "GOOGLE_QUERY")); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "q"); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "query"); value != "" {
		return value
	}
	return ""
}

func googleMaxPages(company Company, maxLinks int) int {
	fallback := (maxLinks / 20) + 2
	if fallback < 4 {
		fallback = 4
	}
	maxPages := commandEnvInt(company.CommandEnv, "GOOGLE_MAX_PAGES", fallback, 1)
	if maxPages > 50 {
		return 50
	}
	return maxPages
}

func googlePageURL(company Company, page int) string {
	parsed, err := url.Parse(googleBaseResultsURL(company))
	if err != nil {
		return googleBaseResultsURL(company)
	}
	values := parsed.Query()
	if query := googleSearchQuery(company); query != "" {
		values.Set("q", query)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	} else {
		values.Del("page")
	}
	parsed.RawQuery = values.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func googleJobHrefMap(body []byte) map[string]string {
	matches := googleJobHrefRE.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make(map[string]string, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		externalID := strings.TrimSpace(string(match[2]))
		href := strings.TrimSpace(htmlstd.UnescapeString(string(match[1])))
		if externalID == "" || href == "" {
			continue
		}
		if _, exists := out[externalID]; !exists {
			out[externalID] = href
		}
	}
	return out
}

func googleSlugFromTitle(title string) string {
	var builder strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'):
			builder.WriteRune(char)
			lastDash = false
		case lastDash:
			continue
		default:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func googleJobURL(pageURL string, externalID string, title string, hrefMap map[string]string) string {
	if href := strings.TrimSpace(hrefMap[externalID]); href != "" {
		baseURL := pageURL
		if strings.HasPrefix(href, "jobs/results/") {
			if parsed, err := url.Parse(pageURL); err == nil {
				basePath := "/"
				if idx := strings.Index(parsed.Path, "/jobs/results/"); idx >= 0 {
					basePath = parsed.Path[:idx+1]
				} else if idx := strings.Index(parsed.Path, "/jobs/results"); idx >= 0 {
					basePath = parsed.Path[:idx+1]
				}
				parsed.Path = basePath
				parsed.RawQuery = ""
				parsed.Fragment = ""
				baseURL = parsed.String()
			}
		}
		if resolved, err := resolveURL(baseURL, href); err == nil {
			return resolved
		}
	}

	slug := googleSlugFromTitle(title)
	if slug == "" {
		return ""
	}
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	pathPrefix := "/about/careers/applications/jobs/results"
	if idx := strings.Index(parsed.Path, "/jobs/results"); idx >= 0 {
		pathPrefix = parsed.Path[:idx+len("/jobs/results")]
	}
	searchQuery := parsed.Query().Get("q")
	parsed.Path = strings.TrimRight(pathPrefix, "/") + "/" + externalID + "-" + slug
	if searchQuery != "" {
		parsed.RawQuery = url.Values{"q": []string{searchQuery}}.Encode()
	} else {
		parsed.RawQuery = ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

func googleJobLocation(row []any) string {
	values := []string{}
	for _, locationAny := range asSlice(row[9]) {
		location := asSlice(locationAny)
		if len(location) > 0 {
			values = append(values, asString(location[0]))
		}
	}
	return summarizeUniqueStrings(values, 4, 220, "; ")
}

func googleNestedText(row []any, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	values := asSlice(row[index])
	if len(values) < 2 {
		return ""
	}
	return asString(values[1])
}

func googleJobDescription(row []any) string {
	return summarizeUniqueStrings([]string{
		googleNestedText(row, 10),
		googleNestedText(row, 3),
		googleNestedText(row, 4),
		googleNestedText(row, 18),
	}, 4, 2200, " ")
}

func googleJobPostedAt(row []any) string {
	if len(row) <= 12 {
		return ""
	}
	values := asSlice(row[12])
	if len(values) == 0 {
		return ""
	}
	return normalizeCreatedAt(values[0])
}

func fetchGoogle(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	maxPages := googleMaxPages(company, maxLinks)
	seen := map[string]struct{}{}
	jobs := make([]Job, 0, maxLinks)

	for page := 1; page <= maxPages && len(jobs) < maxLinks; page++ {
		pageURL := googlePageURL(company, page)
		_, body, err := performRequest(client, "GET", pageURL, nil, nil)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}

		payloadBytes := extractAssignedJSONArray(body, "AF_initDataCallback({key: 'ds:1'")
		if len(payloadBytes) == 0 {
			if page == 1 {
				return nil, newFetchError("google jobs payload not found for %s", company.Name)
			}
			break
		}

		var payload []any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			if page == 1 {
				return nil, newFetchError("google jobs parse failed for %s: %v", company.Name, err)
			}
			break
		}

		rows := asSlice(nil)
		if len(payload) > 0 {
			rows = asSlice(payload[0])
		}
		if len(rows) == 0 {
			break
		}

		hrefMap := googleJobHrefMap(body)
		pageNewJobs := 0
		for _, rowAny := range rows {
			row := asSlice(rowAny)
			if len(row) < 2 {
				continue
			}
			externalID := idString(row[0])
			title := normalizeTextSnippet(asString(row[1]), 280)
			if externalID == "" || title == "" || isNoiseTitle(title) {
				continue
			}

			jobURL := googleJobURL(pageURL, externalID, title, hrefMap)
			if jobURL == "" {
				continue
			}
			if _, exists := seen[jobURL]; exists {
				continue
			}
			seen[jobURL] = struct{}{}

			jobs = append(jobs, Job{
				Company:     company.Name,
				Source:      "google",
				Title:       title,
				URL:         jobURL,
				ExternalID:  externalID,
				Location:    googleJobLocation(row),
				PostedAt:    googleJobPostedAt(row),
				Description: googleJobDescription(row),
			})
			pageNewJobs++
			if len(jobs) >= maxLinks {
				break
			}
		}

		if pageNewJobs == 0 || len(rows) < 20 {
			break
		}
	}

	return jobs, nil
}
