package monitor

import (
	"encoding/json"
	"strings"
)

func veevaBaseOrigin(company Company) string {
	return originFromURL(company.CareersURL, "https://careers.veeva.com")
}

func veevaQuery(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "VEEVA_QUERY")); value != "" {
		return value
	}
	return queryValueFromURL(company.CareersURL, "search")
}

func veevaRegionFilter(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "VEEVA_REGION")); value != "" {
		return value
	}
	return queryValueFromURL(company.CareersURL, "regions")
}

func veevaTeamFilter(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "VEEVA_TEAM")); value != "" {
		return value
	}
	return queryValueFromURL(company.CareersURL, "ts")
}

func veevaRemoteFilter(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "VEEVA_REMOTE")); value != "" {
		return value
	}
	return queryValueFromURL(company.CareersURL, "remote")
}

func veevaMatchesFilter(haystack string, filter string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	haystack = strings.ToLower(strings.TrimSpace(haystack))
	return haystack != "" && strings.Contains(haystack, filter)
}

func veevaMatchesQuery(row map[string]any, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		asString(row["job_title"]),
		asString(row["team"]),
		asString(row["keywords"]),
		asString(row["slug"]),
	}, " "))
	if haystack == "" {
		return false
	}
	for _, term := range strings.Fields(query) {
		if strings.Contains(haystack, term) {
			return true
		}
	}
	return false
}

func veevaJobLocation(row map[string]any) string {
	parts := []string{}
	if asString(row["remote"]) == "1" {
		parts = append(parts, "Remote")
	}
	for _, value := range []string{
		asString(row["city"]),
		asString(row["country"]),
		asString(row["region"]),
	} {
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return summarizeUniqueStrings(parts, 4, 220, ", ")
}

func veevaJobURL(company Company, row map[string]any) string {
	baseURL := strings.TrimSpace(commandEnvValue(company.CommandEnv, "VEEVA_JOB_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimRight(veevaBaseOrigin(company), "/") + "/job"
	}
	leverID := strings.TrimSpace(asString(row["lever_id"]))
	slug := strings.TrimSpace(asString(row["slug"]))
	if leverID == "" || slug == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/" + leverID + "/" + slug + "/?lever-source=veeva-career-site"
}

func fetchVeeva(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	_, body, err := performRequest(client, "GET", company.CareersURL, nil, nil)
	if err != nil {
		return nil, err
	}

	payloadBytes := extractAssignedJSONArrayLiteral(body, "let allJobs =")
	if len(payloadBytes) == 0 {
		return nil, newFetchError("veeva payload missing for %s", company.Name)
	}

	var payload []any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, newFetchError("veeva payload parse failed for %s: %v", company.Name, err)
	}

	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	query := veevaQuery(company)
	regionFilter := veevaRegionFilter(company)
	teamFilter := veevaTeamFilter(company)
	remoteFilter := strings.ToLower(strings.TrimSpace(veevaRemoteFilter(company)))

	jobs := make([]Job, 0, min(len(payload), maxLinks))
	seen := map[string]struct{}{}
	for _, rowAny := range payload {
		row := asMap(rowAny)
		title := normalizeTextSnippet(asString(row["job_title"]), 280)
		if title == "" || isNoiseTitle(title) {
			continue
		}
		if !veevaMatchesQuery(row, query) {
			continue
		}
		if !veevaMatchesFilter(asString(row["region"]), regionFilter) {
			continue
		}
		if !veevaMatchesFilter(asString(row["team"]), teamFilter) {
			continue
		}
		if remoteFilter == "true" && asString(row["remote"]) != "1" {
			continue
		}
		if remoteFilter == "false" && asString(row["remote"]) == "1" {
			continue
		}

		jobURL := veevaJobURL(company, row)
		if jobURL == "" {
			continue
		}
		if _, exists := seen[jobURL]; exists {
			continue
		}
		seen[jobURL] = struct{}{}

		jobs = append(jobs, Job{
			Company:     company.Name,
			Source:      "veeva",
			Title:       title,
			URL:         jobURL,
			ExternalID:  strings.TrimSpace(asString(row["lever_id"])),
			Location:    veevaJobLocation(row),
			Team:        normalizeTextSnippet(asString(row["team"]), 180),
			Description: normalizeTextSnippet(asString(row["keywords"]), 2200),
		})
		if len(jobs) >= maxLinks {
			break
		}
	}

	return jobs, nil
}
