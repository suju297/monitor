package monitor

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

func amazonBaseOrigin(company Company) string {
	return originFromURL(company.CareersURL, "https://www.amazon.jobs")
}

func amazonSearchQuery(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "AMAZON_QUERY")); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "base_query"); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "q"); value != "" {
		return value
	}
	return "software"
}

func amazonLocationQuery(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "AMAZON_LOC_QUERY")); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "loc_query"); value != "" {
		return value
	}
	return "United States"
}

func amazonSort(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "AMAZON_SORT")); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "sort"); value != "" {
		return value
	}
	return "recent"
}

func amazonPageSize(company Company) int {
	pageSize := commandEnvInt(company.CommandEnv, "AMAZON_PAGE_SIZE", 100, 1)
	if pageSize > 100 {
		return 100
	}
	return pageSize
}

func amazonMaxPages(company Company, maxLinks int, pageSize int) int {
	fallback := (maxLinks / pageSize) + 2
	if fallback < 4 {
		fallback = 4
	}
	maxPages := commandEnvInt(company.CommandEnv, "AMAZON_MAX_PAGES", fallback, 1)
	if maxPages > 40 {
		return 40
	}
	return maxPages
}

func amazonSearchURL(company Company, offset int, pageSize int) string {
	values := url.Values{}
	values.Set("base_query", amazonSearchQuery(company))
	values.Set("loc_query", amazonLocationQuery(company))
	values.Set("offset", strconv.Itoa(offset))
	values.Set("result_limit", strconv.Itoa(pageSize))
	values.Set("sort", amazonSort(company))
	return strings.TrimRight(amazonBaseOrigin(company), "/") + "/en/search.json?" + values.Encode()
}

func amazonJobLocation(row map[string]any) string {
	for _, candidate := range []string{
		normalizeTextSnippet(asString(row["normalized_location"]), 220),
		normalizeTextSnippet(asString(row["location"]), 220),
	} {
		if candidate != "" {
			return candidate
		}
	}
	parts := []string{
		normalizeTextSnippet(asString(row["city"]), 120),
		normalizeTextSnippet(asString(row["state"]), 120),
		normalizeTextSnippet(asString(row["country_code"]), 120),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return normalizeTextSnippet(strings.Join(out, ", "), 220)
}

func amazonJobTeam(row map[string]any) string {
	for _, candidate := range []string{
		normalizeTextSnippet(asString(row["job_category"]), 180),
		normalizeTextSnippet(asString(row["job_family"]), 180),
		normalizeTextSnippet(asString(row["team"]), 180),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func fetchAmazon(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	pageSize := amazonPageSize(company)
	maxPages := amazonMaxPages(company, maxLinks, pageSize)

	jobs := make([]Job, 0, maxLinks)
	seen := map[string]struct{}{}
	for page := 0; page < maxPages && len(jobs) < maxLinks; page++ {
		_, body, err := performRequest(client, "GET", amazonSearchURL(company, page*pageSize, pageSize), map[string]string{
			"Accept": "application/json",
		}, nil)
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			if page == 0 {
				return nil, newFetchError("amazon response parse failed for %s: %v", company.Name, err)
			}
			break
		}

		rows := asSlice(payload["jobs"])
		if len(rows) == 0 {
			break
		}

		pageNewJobs := 0
		for _, rowAny := range rows {
			row := asMap(rowAny)
			title := normalizeTextSnippet(asString(row["title"]), 280)
			if title == "" || isNoiseTitle(title) {
				continue
			}

			jobPath := strings.TrimSpace(asString(row["job_path"]))
			jobURL, err := resolveURL(amazonBaseOrigin(company), jobPath)
			if err != nil || jobURL == "" {
				continue
			}

			externalID := strings.TrimSpace(asString(row["id_icims"]))
			if externalID == "" {
				externalID = strings.TrimSpace(asString(row["id"]))
			}
			seenKey := externalID
			if seenKey == "" {
				seenKey = jobURL
			}
			if _, exists := seen[seenKey]; exists {
				continue
			}
			seen[seenKey] = struct{}{}

			description := normalizeTextSnippet(asString(row["description"]), 2200)
			if description == "" {
				description = normalizeTextSnippet(asString(row["description_short"]), 2200)
			}

			jobs = append(jobs, Job{
				Company:     company.Name,
				Source:      "amazon",
				Title:       title,
				URL:         jobURL,
				ExternalID:  externalID,
				Location:    amazonJobLocation(row),
				Team:        amazonJobTeam(row),
				PostedAt:    normalizeCreatedAt(row["posted_date"]),
				Description: description,
			})
			pageNewJobs++
			if len(jobs) >= maxLinks {
				break
			}
		}

		if pageNewJobs == 0 || len(rows) < pageSize {
			break
		}
	}

	return jobs, nil
}
