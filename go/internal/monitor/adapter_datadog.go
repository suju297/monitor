package monitor

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func datadogTypesenseBaseURL(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "DATADOG_TYPESENSE_BASE_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return "https://gk6e3zbyuntvc5dap.a1.typesense.net"
}

func datadogTypesenseCollection(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "DATADOG_TYPESENSE_COLLECTION")); value != "" {
		return value
	}
	return "careers_alias"
}

func datadogTypesenseKey(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "DATADOG_TYPESENSE_KEY")); value != "" {
		return value
	}
	return "1Hwq7hntXp211hKvRS3CSI2QSU7w2gFm"
}

func datadogSearchQuery(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "DATADOG_QUERY")); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "q"); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "query"); value != "" {
		return value
	}
	return "software"
}

func datadogQueryBy(company Company, query string) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "DATADOG_QUERY_BY")); value != "" {
		return value
	}
	if strings.TrimSpace(query) == "" || strings.TrimSpace(query) == "*" {
		return "title"
	}
	return "title,description,department,team,location_string,time_type,parent_department_Engineering,child_department_Engineering"
}

func datadogPageSize(company Company) int {
	pageSize := commandEnvInt(company.CommandEnv, "DATADOG_PAGE_SIZE", 100, 1)
	if pageSize > 100 {
		return 100
	}
	return pageSize
}

func datadogMaxPages(company Company, maxLinks int, pageSize int) int {
	fallback := (maxLinks / pageSize) + 2
	if fallback < 4 {
		fallback = 4
	}
	maxPages := commandEnvInt(company.CommandEnv, "DATADOG_MAX_PAGES", fallback, 1)
	if maxPages > 20 {
		return 20
	}
	return maxPages
}

func datadogSearchURL(company Company) string {
	baseURL := datadogTypesenseBaseURL(company)
	return fmt.Sprintf("%s/collections/%s/documents/search", baseURL, datadogTypesenseCollection(company))
}

func datadogJobLocation(row map[string]any) string {
	if location := normalizeTextSnippet(asString(row["location_string"]), 220); location != "" {
		return location
	}
	values := append(anyListToStrings(row["location_EMEA"]), anyListToStrings(row["location_AMER"])...)
	return summarizeUniqueStrings(values, 4, 220, "; ")
}

func datadogJobTeam(row map[string]any) string {
	return summarizeUniqueStrings([]string{
		normalizeTextSnippet(asString(row["department"]), 160),
		normalizeTextSnippet(asString(row["team"]), 160),
		normalizeTextSnippet(asString(row["child_department_Engineering"]), 160),
	}, 3, 220, " | ")
}

func fetchDatadog(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	pageSize := datadogPageSize(company)
	maxPages := datadogMaxPages(company, maxLinks, pageSize)
	query := datadogSearchQuery(company)
	if strings.TrimSpace(query) == "" {
		query = "*"
	}

	jobs := make([]Job, 0, maxLinks)
	seen := map[string]struct{}{}
	for page := 1; page <= maxPages && len(jobs) < maxLinks; page++ {
		values := url.Values{}
		values.Set("q", query)
		values.Set("query_by", datadogQueryBy(company, query))
		values.Set("page", strconv.Itoa(page))
		values.Set("per_page", strconv.Itoa(pageSize))

		_, body, err := performRequest(client, "GET", datadogSearchURL(company)+"?"+values.Encode(), map[string]string{
			"Accept":              "application/json",
			"x-typesense-api-key": datadogTypesenseKey(company),
		}, nil)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			if page == 1 {
				return nil, newFetchError("datadog response parse failed for %s: %v", company.Name, err)
			}
			break
		}

		hits := asSlice(payload["hits"])
		if len(hits) == 0 {
			break
		}

		pageNewJobs := 0
		for _, hitAny := range hits {
			doc := asMap(asMap(hitAny)["document"])
			title := normalizeTextSnippet(asString(doc["title"]), 280)
			if title == "" || isNoiseTitle(title) {
				continue
			}

			jobURL := normalizeTextSnippet(asString(doc["absolute_url"]), 700)
			if jobURL == "" {
				if rel := strings.TrimSpace(asString(doc["rel_url"])); rel != "" {
					if resolved, resolveErr := resolveURL(company.CareersURL, rel); resolveErr == nil {
						jobURL = resolved
					}
				}
			}
			if jobURL == "" {
				continue
			}

			externalID := idString(doc["job_id"])
			if externalID == "" {
				externalID = idString(doc["internal_job_id"])
			}
			if externalID == "" {
				externalID = strings.TrimSpace(asString(doc["id"]))
			}
			seenKey := externalID
			if seenKey == "" {
				seenKey = jobURL
			}
			if _, exists := seen[seenKey]; exists {
				continue
			}
			seen[seenKey] = struct{}{}

			jobs = append(jobs, Job{
				Company:     company.Name,
				Source:      "datadog",
				Title:       title,
				URL:         jobURL,
				ExternalID:  externalID,
				Location:    datadogJobLocation(doc),
				Team:        datadogJobTeam(doc),
				PostedAt:    normalizeCreatedAt(doc["last_mod"]),
				Description: normalizeTextSnippet(asString(doc["description"]), 2200),
			})
			pageNewJobs++
			if len(jobs) >= maxLinks {
				break
			}
		}

		if pageNewJobs == 0 || len(hits) < pageSize {
			break
		}
	}

	sort.SliceStable(jobs, func(i, j int) bool {
		return jobs[i].PostedAt > jobs[j].PostedAt
	})
	return jobs, nil
}
