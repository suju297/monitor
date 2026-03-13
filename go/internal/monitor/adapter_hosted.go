package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func fetchGreenhouse(company Company) ([]Job, error) {
	board := strings.TrimSpace(company.GreenhouseBoard)
	if board == "" {
		board = parseGreenhouseBoard(company.CareersURL)
	}
	if board == "" {
		return nil, newFetchError("could not derive greenhouse board from %q", company.CareersURL)
	}

	endpoint := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs?content=true", board)
	client := newHTTPClient(company.TimeoutSeconds)
	_, body, err := performRequest(client, "GET", endpoint, nil, nil)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, newFetchError("greenhouse response parse failed for %s: %v", company.Name, err)
	}
	rows := asSlice(payload["jobs"])
	jobs := make([]Job, 0, len(rows))
	for _, rowAny := range rows {
		row := asMap(rowAny)
		title := asString(row["title"])
		jobURL := asString(row["absolute_url"])
		if jobURL == "" {
			jobURL = asString(row["url"])
		}
		if title == "" || jobURL == "" {
			continue
		}
		team := ""
		departments := asSlice(row["departments"])
		if len(departments) > 0 {
			team = asString(asMap(departments[0])["name"])
		}
		location := asString(asMap(row["location"])["name"])
		jobs = append(jobs, Job{
			Company:     company.Name,
			Source:      "greenhouse",
			Title:       title,
			URL:         jobURL,
			ExternalID:  asString(row["id"]),
			Location:    location,
			Team:        team,
			PostedAt:    normalizeCreatedAt(row["updated_at"]),
			Description: normalizeTextSnippet(asString(row["content"]), 2200),
		})
	}
	return jobs, nil
}

func leverAPIEndpoint(company Company, site string) string {
	baseURL := strings.TrimSpace(commandEnvValue(company.CommandEnv, "LEVER_API_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.lever.co/v0/postings"
	}
	return strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(site) + "?mode=json"
}

func leverBoardURL(company Company, site string) string {
	baseURL := strings.TrimSpace(commandEnvValue(company.CommandEnv, "LEVER_BOARD_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://jobs.lever.co"
	}
	return strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(site)
}

func lastURLPathSegment(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	pathValue := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if pathValue == "" {
		return ""
	}
	parts := strings.Split(pathValue, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func fetchLeverHostedHTML(company Company, site string) ([]Job, error) {
	boardURL := leverBoardURL(company, site)
	client := newHTTPClient(company.TimeoutSeconds)
	_, body, err := performRequest(client, "GET", boardURL, nil, nil)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, newFetchError("lever hosted board parse failed for %s: %v", company.Name, err)
	}

	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	jobs := make([]Job, 0, maxLinks)
	seen := map[string]struct{}{}

	var anchors []anchorLink
	gatherAnchors(doc, &anchors)
	for _, link := range anchors {
		if !nodeClassContains(link.Node, "posting-title") {
			continue
		}
		jobURL, err := resolveURL(boardURL, link.Href)
		if err != nil || jobURL == "" {
			continue
		}
		if _, exists := seen[jobURL]; exists {
			continue
		}
		title := firstDescendantTextByTag(link.Node, "h5", 280)
		if title == "" {
			title = normalizeTextSnippet(link.Text, 280)
		}
		if title == "" || isNoiseTitle(title) {
			continue
		}
		root := ancestorWithClass(link.Node, "posting", 4)
		if root == nil {
			root = link.Node.Parent
		}
		team := firstDescendantTextByClass(root, "team", 180)
		if team == "" {
			team = firstDescendantTextByClass(root, "department", 180)
		}
		if commitment := firstDescendantTextByClass(root, "commitment", 120); commitment != "" {
			team = summarizeUniqueStrings([]string{team, commitment}, 2, 220, " | ")
		}
		if workplace := firstDescendantTextByClass(root, "workplacetypes", 120); workplace != "" {
			team = summarizeUniqueStrings([]string{team, workplace}, 3, 220, " | ")
		}
		jobs = append(jobs, Job{
			Company:    company.Name,
			Source:     "lever",
			Title:      title,
			URL:        jobURL,
			ExternalID: lastURLPathSegment(jobURL),
			Location:   firstDescendantTextByClass(root, "location", 180),
			Team:       team,
			PostedAt:   extractPostedAtNearAnchor(link.Node),
		})
		seen[jobURL] = struct{}{}
		if len(jobs) >= maxLinks {
			break
		}
	}
	return jobs, nil
}

func fetchLever(company Company) ([]Job, error) {
	site := strings.TrimSpace(company.LeverSite)
	if site == "" {
		site = parseLeverSite(company.CareersURL)
	}
	if site == "" {
		return nil, newFetchError("could not derive lever site from %q", company.CareersURL)
	}

	endpoint := leverAPIEndpoint(company, site)
	client := newHTTPClient(company.TimeoutSeconds)
	_, body, err := performRequest(client, "GET", endpoint, nil, nil)
	if err != nil {
		return fetchLeverHostedHTML(company, site)
	}

	var payload []any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fetchLeverHostedHTML(company, site)
	}

	jobs := make([]Job, 0, len(payload))
	for _, rowAny := range payload {
		row := asMap(rowAny)
		title := asString(row["text"])
		jobURL := asString(row["hostedUrl"])
		if jobURL == "" {
			jobURL = asString(row["applyUrl"])
		}
		if title == "" || jobURL == "" {
			continue
		}
		categories := asMap(row["categories"])
		description := asString(row["descriptionPlain"])
		if description == "" {
			description = asString(row["description"])
		}
		jobs = append(jobs, Job{
			Company:     company.Name,
			Source:      "lever",
			Title:       title,
			URL:         jobURL,
			ExternalID:  asString(row["id"]),
			Location:    asString(categories["location"]),
			Team:        asString(categories["team"]),
			PostedAt:    normalizeCreatedAt(row["createdAt"]),
			Description: normalizeTextSnippet(description, 2200),
		})
	}
	if len(jobs) == 0 {
		return fetchLeverHostedHTML(company, site)
	}
	return jobs, nil
}

func icimsLocation(row map[string]any) string {
	primary := normalizeTextSnippet(asString(row["full_location"]), 220)
	if primary != "" {
		return primary
	}
	primary = normalizeTextSnippet(asString(row["location_name"]), 220)
	if primary != "" {
		return primary
	}
	primary = normalizeTextSnippet(asString(row["short_location"]), 220)
	if primary != "" {
		return primary
	}
	parts := []string{
		normalizeTextSnippet(asString(row["city"]), 100),
		normalizeTextSnippet(asString(row["state"]), 100),
		normalizeTextSnippet(asString(row["country"]), 100),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		out = append(out, part)
	}
	return normalizeTextSnippet(strings.Join(out, ", "), 220)
}

func icimsTeam(row map[string]any) string {
	labels := []string{}
	seen := map[string]struct{}{}
	for _, entry := range asSlice(row["categories"]) {
		label := strings.TrimSpace(asString(asMap(entry)["name"]))
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		labels = append(labels, label)
	}
	for _, entry := range asSlice(row["category"]) {
		label := strings.TrimSpace(asString(entry))
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		labels = append(labels, label)
	}
	return normalizeTextSnippet(strings.Join(labels, " | "), 220)
}

func fetchICIMS(company Company) ([]Job, error) {
	endpoint := parseICIMSAPIEndpoint(company.CareersURL)
	if endpoint == "" {
		return nil, newFetchError("could not derive iCIMS API endpoint from %q", company.CareersURL)
	}

	maxPages := commandEnvInt(company.CommandEnv, "ICIMS_MAX_PAGES", 25, 1)
	maxJobs := commandEnvInt(company.CommandEnv, "ICIMS_MAX_JOBS", 800, 1)
	sortBy := strings.TrimSpace(commandEnvValue(company.CommandEnv, "ICIMS_SORT_BY"))
	if sortBy == "" {
		sortBy = "relevance"
	}
	descending := commandEnvBool(company.CommandEnv, "ICIMS_DESCENDING", false)
	internal := commandEnvBool(company.CommandEnv, "ICIMS_INTERNAL", false)

	client := newHTTPClient(company.TimeoutSeconds)
	jobs := make([]Job, 0, maxJobs)
	seen := map[string]struct{}{}
	totalPages := maxPages

	for page := 1; page <= maxPages; page++ {
		reqURL, err := url.Parse(endpoint)
		if err != nil {
			return nil, newFetchError("invalid iCIMS endpoint %q", endpoint)
		}
		values := reqURL.Query()
		values.Set("page", strconv.Itoa(page))
		values.Set("sortBy", sortBy)
		values.Set("descending", strconv.FormatBool(descending))
		values.Set("internal", strconv.FormatBool(internal))
		reqURL.RawQuery = values.Encode()

		_, body, err := performRequest(client, "GET", reqURL.String(), map[string]string{"Accept": "application/json"}, nil)
		if err != nil {
			return nil, err
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, newFetchError("iCIMS response parse failed for %s: %v", company.Name, err)
		}
		if pageCount, ok := payload["count"].(float64); ok && int(pageCount) > 0 {
			totalPages = min(maxPages, int(pageCount))
		}

		rows := asSlice(payload["jobs"])
		if rows == nil {
			return nil, newFetchError("iCIMS payload for %s missing jobs list", company.Name)
		}
		if len(rows) == 0 {
			break
		}

		for _, rowAny := range rows {
			wrapped := asMap(rowAny)
			row := asMap(wrapped["data"])
			if len(row) == 0 {
				continue
			}
			title := normalizeTextSnippet(asString(row["title"]), 280)
			reqID := normalizeTextSnippet(asString(row["req_id"]), 120)
			canonical := normalizeTextSnippet(asString(getNested(row, "meta_data.canonical_url", "")), 700)
			language := normalizeTextSnippet(asString(row["language"]), 40)
			jobURL := buildICIMSJobURL(company.CareersURL, reqID, language, canonical)
			if title == "" || strings.TrimSpace(jobURL) == "" {
				continue
			}

			dedupeKey := strings.ToLower(strings.TrimSpace(reqID))
			if dedupeKey == "" {
				dedupeKey = strings.ToLower(strings.TrimSpace(jobURL))
			}
			if _, ok := seen[dedupeKey]; ok {
				continue
			}
			seen[dedupeKey] = struct{}{}

			description := asString(row["description"])
			if description == "" {
				description = asString(row["responsibilities"])
			}
			if description == "" {
				description = asString(row["qualifications"])
			}
			postedAt := normalizeCreatedAt(row["posted_date"])
			if postedAt == "" {
				postedAt = normalizeCreatedAt(row["create_date"])
			}
			if postedAt == "" {
				postedAt = normalizeCreatedAt(row["update_date"])
			}

			jobs = append(jobs, Job{
				Company:     company.Name,
				Source:      "icims",
				Title:       title,
				URL:         jobURL,
				ExternalID:  reqID,
				Location:    icimsLocation(row),
				Team:        icimsTeam(row),
				PostedAt:    postedAt,
				Description: normalizeTextSnippet(description, 2200),
			})
			if len(jobs) >= maxJobs {
				return jobs, nil
			}
		}
		if page >= totalPages {
			break
		}
	}

	return jobs, nil
}

func parseAshbyQueryTerms(raw string) []string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return nil
	}
	pieces := []string{}
	if strings.Contains(normalized, ",") {
		pieces = strings.Split(normalized, ",")
	} else {
		pieces = strings.Fields(normalized)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		term := strings.TrimSpace(piece)
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func ashbyAddressLocation(address any) string {
	postal := asMap(asMap(address)["postalAddress"])
	parts := []string{
		normalizeTextSnippet(asString(postal["addressLocality"]), 100),
		normalizeTextSnippet(asString(postal["addressRegion"]), 100),
		normalizeTextSnippet(asString(postal["addressCountry"]), 100),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		out = append(out, part)
	}
	return normalizeTextSnippet(strings.Join(out, ", "), 180)
}

func pickAshbyLocation(row map[string]any) string {
	primary := normalizeTextSnippet(asString(row["location"]), 120)
	if address := ashbyAddressLocation(row["address"]); address != "" {
		primary = address
	}

	secondaryOut := []string{}
	seen := map[string]struct{}{}
	if strings.TrimSpace(primary) != "" {
		seen[strings.ToLower(strings.TrimSpace(primary))] = struct{}{}
	}
	for _, entry := range asSlice(row["secondaryLocations"]) {
		secondary := asMap(entry)
		location := normalizeTextSnippet(asString(secondary["location"]), 100)
		if location == "" {
			location = ashbyAddressLocation(secondary["address"])
		}
		location = strings.TrimSpace(location)
		if location == "" {
			continue
		}
		key := strings.ToLower(location)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		secondaryOut = append(secondaryOut, location)
	}
	if len(secondaryOut) == 0 {
		return normalizeTextSnippet(primary, 220)
	}
	suffix := strings.Join(secondaryOut[:min(3, len(secondaryOut))], "; ")
	if strings.TrimSpace(primary) == "" {
		return normalizeTextSnippet(suffix, 220)
	}
	return normalizeTextSnippet(fmt.Sprintf("%s; Other: %s", primary, suffix), 220)
}

func matchesAshbyQuery(job Job, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		normalizeTextSnippet(job.Title, 280),
		normalizeTextSnippet(job.Team, 220),
		normalizeTextSnippet(job.Description, 2200),
		normalizeTextSnippet(job.URL, 700),
	}, " "))
	if strings.TrimSpace(haystack) == "" {
		return false
	}
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			return true
		}
	}
	return false
}

func fetchAshby(company Company) ([]Job, error) {
	board := strings.TrimSpace(commandEnvValue(company.CommandEnv, "ASHBY_BOARD"))
	if board == "" {
		board = parseAshbyBoard(company.CareersURL)
	}
	if board == "" {
		return nil, newFetchError("could not derive ashby board from %q", company.CareersURL)
	}

	timeoutSeconds := commandEnvInt(company.CommandEnv, "ASHBY_TIMEOUT_SECONDS", company.TimeoutSeconds, 5)
	maxJobs := commandEnvInt(company.CommandEnv, "ASHBY_MAX_JOBS", 800, 1)
	usOnly := commandEnvBool(company.CommandEnv, "ASHBY_US_ONLY", true)
	queryTerms := parseAshbyQueryTerms(commandEnvValue(company.CommandEnv, "ASHBY_QUERY"))

	endpoint := fmt.Sprintf("https://api.ashbyhq.com/posting-api/job-board/%s", url.PathEscape(board))
	client := newHTTPClient(timeoutSeconds)
	_, body, err := performRequest(client, "GET", endpoint, map[string]string{"Accept": "application/json"}, nil)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, newFetchError("ashby response parse failed for %s: %v", company.Name, err)
	}
	rows := asSlice(payload["jobs"])
	if rows == nil {
		return nil, newFetchError("ashby payload for %s missing jobs list", company.Name)
	}

	jobs := make([]Job, 0, min(len(rows), maxJobs))
	seen := map[string]struct{}{}
	for _, rowAny := range rows {
		row := asMap(rowAny)
		title := normalizeTextSnippet(asString(row["title"]), 280)
		jobURL := normalizeTextSnippet(asString(row["jobUrl"]), 700)
		if jobURL == "" {
			jobURL = normalizeTextSnippet(asString(row["applyUrl"]), 700)
		}
		if title == "" || jobURL == "" {
			continue
		}
		externalID := normalizeTextSnippet(asString(row["id"]), 120)
		dedupeKey := strings.ToLower(strings.TrimSpace(externalID))
		if dedupeKey == "" {
			dedupeKey = strings.ToLower(strings.TrimSpace(jobURL))
		}
		if _, ok := seen[dedupeKey]; ok {
			continue
		}
		seen[dedupeKey] = struct{}{}

		team := normalizeTextSnippet(asString(row["team"]), 160)
		department := normalizeTextSnippet(asString(row["department"]), 160)
		if team != "" && department != "" && !strings.Contains(strings.ToLower(team), strings.ToLower(department)) {
			team = normalizeTextSnippet(fmt.Sprintf("%s | %s", department, team), 220)
		} else if team == "" {
			team = department
		}
		description := asString(row["descriptionPlain"])
		if description == "" {
			description = asString(row["description"])
		}
		postedAt := normalizeCreatedAt(row["publishedAt"])
		if postedAt == "" {
			postedAt = normalizeCreatedAt(row["updatedAt"])
		}

		job := Job{
			Company:     company.Name,
			Source:      "ashby",
			Title:       title,
			URL:         jobURL,
			ExternalID:  externalID,
			Location:    pickAshbyLocation(row),
			Team:        team,
			PostedAt:    postedAt,
			Description: normalizeTextSnippet(description, 2200),
		}
		if usOnly && !isUSBasedJob(job) {
			continue
		}
		if !matchesAshbyQuery(job, queryTerms) {
			continue
		}

		jobs = append(jobs, job)
		if len(jobs) >= maxJobs {
			break
		}
	}
	return jobs, nil
}
