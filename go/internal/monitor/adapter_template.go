package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func resolveURL(baseURL string, href string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	next, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	resolved := base.ResolveReference(next)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme")
	}
	return resolved.String(), nil
}

func fetchTemplate(company Company) ([]Job, error) {
	template := company.Template
	if len(template) == 0 {
		return nil, newFetchError("company %q source template requires template config", company.Name)
	}
	method := strings.ToUpper(asString(template["method"]))
	if method == "" {
		method = "GET"
	}
	rawURL := asString(template["url"])
	if rawURL == "" {
		rawURL = company.CareersURL
	}

	query := asMap(template["params"])
	if len(query) > 0 {
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, newFetchError("template invalid URL for %s: %v", company.Name, err)
		}
		q := u.Query()
		for key, value := range query {
			q.Set(key, asString(value))
		}
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}

	headers := asStringMap(template["headers"])
	var requestBody *bytes.Reader
	if value := template["json"]; value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, newFetchError("template json body marshal failed for %s: %v", company.Name, err)
		}
		headers["Content-Type"] = "application/json"
		requestBody = bytes.NewReader(raw)
	} else if value := template["data"]; value != nil {
		switch casted := value.(type) {
		case string:
			requestBody = bytes.NewReader([]byte(casted))
		case map[string]any:
			vals := url.Values{}
			for key, val := range casted {
				vals.Set(key, asString(val))
			}
			headers["Content-Type"] = "application/x-www-form-urlencoded"
			requestBody = bytes.NewReader([]byte(vals.Encode()))
		default:
			raw, err := json.Marshal(casted)
			if err != nil {
				return nil, newFetchError("template data body marshal failed for %s: %v", company.Name, err)
			}
			requestBody = bytes.NewReader(raw)
		}
	} else {
		requestBody = bytes.NewReader(nil)
	}

	client := newHTTPClient(company.TimeoutSeconds)
	_, body, err := performRequest(client, method, rawURL, headers, requestBody)
	if err != nil {
		return nil, err
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, newFetchError("template endpoint did not return valid JSON: %s", rawURL)
	}

	jobsPath := asString(template["jobs_path"])
	fields := asMap(template["fields"])
	listPayload := payload
	if jobsPath != "" {
		listPayload = getNested(payload, jobsPath, nil)
	}
	if object, ok := listPayload.(map[string]any); ok {
		listPayload = object["jobs"]
	}
	list := asSlice(listPayload)
	if list == nil {
		pathValue := jobsPath
		if pathValue == "" {
			pathValue = "(root)"
		}
		return nil, newFetchError("template jobs_path %q did not resolve to list for %s", pathValue, company.Name)
	}

	baseURL := asString(template["base_url"])
	if baseURL == "" {
		baseURL = company.CareersURL
	}

	idField := asString(fields["id"])
	if idField == "" {
		idField = "id"
	}
	titleField := asString(fields["title"])
	if titleField == "" {
		titleField = "title"
	}
	urlField := asString(fields["url"])
	if urlField == "" {
		urlField = "url"
	}
	locationField := asString(fields["location"])
	if locationField == "" {
		locationField = "location"
	}
	teamField := asString(fields["team"])
	if teamField == "" {
		teamField = "team"
	}
	postedAtField := asString(fields["posted_at"])
	if postedAtField == "" {
		postedAtField = "posted_at"
	}
	descriptionField := asString(fields["description"])
	if descriptionField == "" {
		descriptionField = "description"
	}
	descriptionFallbackFields := []string{
		"descriptionPlain",
		"description",
		"Description",
		"DescriptionStr",
		"ShortDescriptionStr",
		"ExternalDescriptionStr",
		"ExternalResponsibilitiesStr",
		"ExternalQualificationsStr",
		"jobDescription",
		"JobDescription",
	}
	postedAtFallbackFields := []string{
		"PostedDate",
		"posted_at",
		"datePosted",
		"DatePosted",
		"published_at",
		"PublishedDate",
		"updated_at",
		"UpdatedDate",
		"lastUpdatedDate",
	}

	jobs := make([]Job, 0, len(list))
	for _, rowAny := range list {
		row := asMap(rowAny)
		title := asString(getNested(row, titleField, ""))
		jobURL := asString(getNested(row, urlField, ""))
		if title == "" || jobURL == "" {
			continue
		}
		resolvedURL, err := resolveURL(baseURL, jobURL)
		if err != nil {
			resolvedURL = jobURL
		}
		description := asString(getNested(row, descriptionField, ""))
		if description == "" {
			for _, field := range descriptionFallbackFields {
				description = asString(getNested(row, field, ""))
				if description != "" {
					break
				}
			}
		}
		postedAtRaw := getNested(row, postedAtField, "")
		postedAt := normalizeCreatedAt(postedAtRaw)
		if postedAt == "" {
			for _, field := range postedAtFallbackFields {
				postedAt = normalizeCreatedAt(getNested(row, field, ""))
				if postedAt != "" {
					break
				}
			}
		}
		jobs = append(jobs, Job{
			Company:     company.Name,
			Source:      "template",
			Title:       title,
			URL:         resolvedURL,
			ExternalID:  asString(getNested(row, idField, "")),
			Location:    asString(getNested(row, locationField, "")),
			Team:        asString(getNested(row, teamField, "")),
			PostedAt:    postedAt,
			Description: normalizeTextSnippet(description, 2200),
		})
	}
	return jobs, nil
}
