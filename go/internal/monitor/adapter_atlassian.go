package monitor

import (
	"encoding/json"
	"strings"
)

func atlassianListingsURL(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "ATLASSIAN_LISTINGS_URL")); value != "" {
		return value
	}
	return strings.TrimRight(originFromURL(company.CareersURL, "https://www.atlassian.com"), "/") + "/endpoint/careers/listings"
}

func atlassianLocations(row map[string]any) string {
	parts := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, entry := range asSlice(row["locations"]) {
		value := normalizeTextSnippet(asString(entry), 220)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, value)
	}
	return strings.Join(parts, "; ")
}

func atlassianDescription(row map[string]any) string {
	parts := []string{
		normalizeTextSnippet(asString(row["overview"]), 1200),
		normalizeTextSnippet(asString(row["responsibilities"]), 1200),
		normalizeTextSnippet(asString(row["qualifications"]), 1200),
	}
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		key := strings.ToLower(part)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, part)
	}
	return normalizeTextSnippet(strings.Join(out, " "), 2200)
}

func atlassianJobURL(row map[string]any) string {
	portal := asMap(row["portalJobPost"])
	for _, candidate := range []string{
		asString(portal["portalUrl"]),
		asString(row["applyUrl"]),
	} {
		value := strings.TrimSpace(candidate)
		if value != "" {
			return value
		}
	}
	return ""
}

func fetchAtlassian(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	_, body, err := performRequest(client, "GET", atlassianListingsURL(company), map[string]string{
		"Accept": "application/json",
	}, nil)
	if err != nil {
		return nil, err
	}

	var payload []any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, newFetchError("atlassian response parse failed for %s: %v", company.Name, err)
	}

	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	jobs := make([]Job, 0, min(len(payload), maxLinks))
	for _, rowAny := range payload {
		row := asMap(rowAny)
		title := normalizeTextSnippet(asString(row["title"]), 280)
		jobURL := atlassianJobURL(row)
		if title == "" || jobURL == "" {
			continue
		}
		portal := asMap(row["portalJobPost"])
		externalID := strings.TrimSpace(asString(row["id"]))
		if externalID == "" {
			externalID = strings.TrimSpace(asString(portal["id"]))
		}

		jobs = append(jobs, Job{
			Company:     company.Name,
			Source:      "atlassian",
			Title:       title,
			URL:         jobURL,
			ExternalID:  externalID,
			Location:    atlassianLocations(row),
			Team:        normalizeTextSnippet(asString(row["category"]), 220),
			PostedAt:    normalizeCreatedAt(portal["updatedDate"]),
			Description: atlassianDescription(row),
		})
		if len(jobs) >= maxLinks {
			break
		}
	}
	return jobs, nil
}
