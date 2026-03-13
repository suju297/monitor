package monitor

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

func capitalOneMaxPages(company Company, maxLinks int) int {
	fallback := (maxLinks / 50) + 2
	if fallback < 4 {
		fallback = 4
	}
	maxPages := commandEnvInt(company.CommandEnv, "CAPITALONE_MAX_PAGES", fallback, 1)
	if maxPages > 40 {
		return 40
	}
	return maxPages
}

func fetchCapitalOne(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	maxPages := capitalOneMaxPages(company, maxLinks)

	jobs := make([]Job, 0, maxLinks)
	seenJobs := map[string]struct{}{}
	pageQueue := []string{company.CareersURL}
	seenPages := map[string]struct{}{}

	for len(pageQueue) > 0 && len(seenPages) < maxPages && len(jobs) < maxLinks {
		pageURL := pageQueue[0]
		pageQueue = pageQueue[1:]
		if _, exists := seenPages[pageURL]; exists {
			continue
		}
		seenPages[pageURL] = struct{}{}

		_, body, err := performRequest(client, "GET", pageURL, nil, nil)
		if err != nil {
			if len(seenPages) == 1 {
				return nil, err
			}
			break
		}
		doc, err := html.Parse(bytes.NewReader(body))
		if err != nil {
			if len(seenPages) == 1 {
				return nil, newFetchError("capital one parser failed for %s: %v", company.Name, err)
			}
			break
		}

		for _, nextPageURL := range genericPaginationLinks(doc, pageURL) {
			if _, exists := seenPages[nextPageURL]; !exists {
				pageQueue = append(pageQueue, nextPageURL)
			}
		}

		var anchors []anchorLink
		gatherAnchors(doc, &anchors)
		pageNewJobs := 0
		for _, link := range anchors {
			if attrValue(link.Node, "data-job-id") == "" && !strings.Contains(strings.ToLower(link.Href), "/job/") {
				continue
			}
			jobURL, err := resolveURL(pageURL, link.Href)
			if err != nil || jobURL == "" {
				continue
			}
			if _, exists := seenJobs[jobURL]; exists {
				continue
			}
			title := firstDescendantTextByTag(link.Node, "h2", 280)
			if title == "" {
				title = normalizeTextSnippet(link.Text, 280)
			}
			if title == "" || isNoiseTitle(title) {
				continue
			}
			postedAt := normalizeCreatedAt(firstDescendantTextByClass(link.Node, "job-date-posted", 120))
			if postedAt == "" {
				postedAt = extractPostedAtNearAnchor(link.Node)
			}
			location := normalizeLocationSnippet(firstDescendantTextByClass(link.Node, "job-location", 180))
			externalID := strings.TrimSpace(attrValue(link.Node, "data-job-id"))
			if externalID == "" {
				externalID = lastURLPathSegment(jobURL)
			}
			jobs = append(jobs, Job{
				Company:    company.Name,
				Source:     "capitalone",
				Title:      title,
				URL:        jobURL,
				ExternalID: externalID,
				Location:   location,
				PostedAt:   postedAt,
			})
			seenJobs[jobURL] = struct{}{}
			pageNewJobs++
			if len(jobs) >= maxLinks {
				break
			}
		}

		if pageNewJobs == 0 && len(pageQueue) == 0 {
			break
		}
	}

	return jobs, nil
}
