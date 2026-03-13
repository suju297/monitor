package monitor

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

func microsoftBaseOrigin(company Company) string {
	return originFromURL(company.CareersURL, "https://apply.careers.microsoft.com")
}

func microsoftSearchQuery(company Company) string {
	if value := strings.TrimSpace(commandEnvValue(company.CommandEnv, "MICROSOFT_QUERY")); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "query"); value != "" {
		return value
	}
	if value := queryValueFromURL(company.CareersURL, "q"); value != "" {
		return value
	}
	return ""
}

func microsoftMaxPages(company Company, maxLinks int) int {
	fallback := (maxLinks / 10) + 2
	if fallback < 6 {
		fallback = 6
	}
	maxPages := commandEnvInt(company.CommandEnv, "MICROSOFT_MAX_PAGES", fallback, 1)
	if maxPages > 60 {
		return 60
	}
	return maxPages
}

func microsoftDetailWorkers(company Company) int {
	workers := commandEnvInt(company.CommandEnv, "MICROSOFT_DETAIL_WORKERS", 6, 1)
	if workers > 12 {
		return 12
	}
	return workers
}

func microsoftSearchURL(company Company, start int) string {
	values := url.Values{}
	values.Set("domain", "microsoft.com")
	values.Set("query", microsoftSearchQuery(company))
	values.Set("location", "")
	values.Set("start", strconv.Itoa(start))
	values.Set("hl", "en-US")
	return strings.TrimRight(microsoftBaseOrigin(company), "/") + "/api/pcsx/search?" + values.Encode()
}

func microsoftDetailURL(company Company, positionID string) string {
	values := url.Values{}
	values.Set("position_id", strings.TrimSpace(positionID))
	values.Set("domain", "microsoft.com")
	values.Set("hl", "en-US")
	return strings.TrimRight(microsoftBaseOrigin(company), "/") + "/api/pcsx/position_details?" + values.Encode()
}

func microsoftLocation(row map[string]any) string {
	values := anyListToStrings(row["standardizedLocations"])
	if len(values) == 0 {
		values = anyListToStrings(row["locations"])
	}
	if location := normalizeTextSnippet(asString(row["location"]), 160); location != "" {
		values = append([]string{location}, values...)
	}
	if worksite := firstStringFromList(row["efcustomTextWorkSite"]); worksite != "" {
		values = append(values, worksite)
	}
	return summarizeUniqueStrings(values, 4, 220, "; ")
}

func microsoftTeam(row map[string]any) string {
	for _, candidate := range []string{
		normalizeTextSnippet(asString(row["department"]), 140),
		firstStringFromList(row["efcustomTextTaDisciplineName"]),
		firstStringFromList(row["efcustomTextCurrentProfession"]),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func fetchMicrosoftPositionDetail(client *http.Client, company Company, positionID string) (Job, error) {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return Job{}, newFetchError("microsoft detail request missing position id")
	}
	_, body, err := performRequest(client, "GET", microsoftDetailURL(company, positionID), map[string]string{
		"Accept": "application/json",
	}, nil)
	if err != nil {
		return Job{}, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Job{}, newFetchError("microsoft detail parse failed for %s (%s): %v", company.Name, positionID, err)
	}
	row := asMap(payload["data"])
	jobURL := strings.TrimSpace(asString(row["publicUrl"]))
	if jobURL == "" {
		if rel := strings.TrimSpace(asString(row["positionUrl"])); rel != "" {
			if resolved, resolveErr := resolveURL(microsoftBaseOrigin(company), rel); resolveErr == nil {
				jobURL = resolved
			}
		}
	}

	return Job{
		URL:         jobURL,
		Location:    microsoftLocation(row),
		Team:        microsoftTeam(row),
		PostedAt:    normalizeCreatedAt(row["postedTs"]),
		Description: normalizeTextSnippet(asString(row["jobDescription"]), 2200),
	}, nil
}

func fetchMicrosoft(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	maxPages := microsoftMaxPages(company, maxLinks)
	pageSize := 10

	jobs := make([]Job, 0, maxLinks)
	seen := map[string]struct{}{}
	for page := 0; page < maxPages && len(jobs) < maxLinks; page++ {
		_, body, err := performRequest(client, "GET", microsoftSearchURL(company, page*pageSize), map[string]string{
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
				return nil, newFetchError("microsoft response parse failed for %s: %v", company.Name, err)
			}
			break
		}

		rows := asSlice(asMap(payload["data"])["positions"])
		if len(rows) == 0 {
			break
		}

		for _, rowAny := range rows {
			row := asMap(rowAny)
			externalID := idString(row["id"])
			if externalID == "" {
				externalID = idString(row["displayJobId"])
			}
			title := normalizeTextSnippet(asString(row["name"]), 280)
			if externalID == "" || title == "" || isNoiseTitle(title) {
				continue
			}
			if _, exists := seen[externalID]; exists {
				continue
			}
			seen[externalID] = struct{}{}

			jobURL := ""
			if rel := strings.TrimSpace(asString(row["positionUrl"])); rel != "" {
				if resolved, resolveErr := resolveURL(microsoftBaseOrigin(company), rel); resolveErr == nil {
					jobURL = resolved
				}
			}
			if jobURL == "" {
				continue
			}

			jobs = append(jobs, Job{
				Company:    company.Name,
				Source:     "microsoft",
				Title:      title,
				URL:        jobURL,
				ExternalID: externalID,
				Location:   microsoftLocation(row),
				Team:       microsoftTeam(row),
				PostedAt:   normalizeCreatedAt(row["postedTs"]),
			})
			if len(jobs) >= maxLinks {
				break
			}
		}

		if len(rows) < pageSize {
			break
		}
	}

	detailLimit := commandEnvInt(company.CommandEnv, "MICROSOFT_DETAIL_FETCH_LIMIT", len(jobs), 0)
	if detailLimit > len(jobs) {
		detailLimit = len(jobs)
	}
	if detailLimit <= 0 {
		return jobs, nil
	}

	detailClient := newHTTPClient(company.TimeoutSeconds)
	type detailResult struct {
		Index  int
		Detail Job
	}

	indexes := make(chan int)
	results := make(chan detailResult, detailLimit)
	var wg sync.WaitGroup
	workers := microsoftDetailWorkers(company)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range indexes {
				detail, err := fetchMicrosoftPositionDetail(detailClient, company, jobs[index].ExternalID)
				if err != nil {
					continue
				}
				results <- detailResult{Index: index, Detail: detail}
			}
		}()
	}

	go func() {
		for index := 0; index < detailLimit; index++ {
			indexes <- index
		}
		close(indexes)
		wg.Wait()
		close(results)
	}()

	for result := range results {
		if result.Detail.URL != "" {
			jobs[result.Index].URL = result.Detail.URL
		}
		if result.Detail.Location != "" {
			jobs[result.Index].Location = result.Detail.Location
		}
		if result.Detail.Team != "" {
			jobs[result.Index].Team = result.Detail.Team
		}
		if result.Detail.PostedAt != "" {
			jobs[result.Index].PostedAt = result.Detail.PostedAt
		}
		if result.Detail.Description != "" {
			jobs[result.Index].Description = result.Detail.Description
		}
	}

	return jobs, nil
}

var googleJobHrefRE = regexp.MustCompile(`href="(jobs/results/([0-9]+)-[^"]+)"`)
