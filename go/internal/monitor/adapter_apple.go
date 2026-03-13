package monitor

import (
	"bytes"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func appleMaxPages(company Company, maxLinks int) int {
	fallback := (maxLinks / 10) + 2
	if fallback < 6 {
		fallback = 6
	}
	maxPages := commandEnvInt(company.CommandEnv, "APPLE_MAX_PAGES", fallback, 1)
	if maxPages > 40 {
		return 40
	}
	return maxPages
}

func applePageURL(baseURL string, page int) string {
	if page <= 1 {
		return baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	query := parsed.Query()
	query.Set("page", strconv.Itoa(page))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func nodeTextByClass(n *html.Node, classMarkers []string, depth int) string {
	if n == nil || depth < 0 {
		return ""
	}
	if n.Type == html.ElementNode {
		classID := strings.ToLower(strings.TrimSpace(attrValue(n, "class") + " " + attrValue(n, "id")))
		for _, marker := range classMarkers {
			if strings.Contains(classID, marker) {
				if text := normalizeTextSnippet(nodeText(n), 280); text != "" {
					return text
				}
				break
			}
		}
	}
	if depth == 0 {
		return ""
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if text := nodeTextByClass(child, classMarkers, depth-1); text != "" {
			return text
		}
	}
	return ""
}

func appleTeamNearAnchor(anchor *html.Node) string {
	for node, depth := anchor, 0; node != nil && depth < 5; node, depth = node.Parent, depth+1 {
		if text := nodeTextByClass(node, []string{"team-name"}, 3); text != "" {
			return text
		}
	}
	return ""
}

func appleLocationNearAnchor(anchor *html.Node, title string) string {
	team := appleTeamNearAnchor(anchor)
	postedAt := extractPostedAtNearAnchor(anchor)
	bareLocationRE := regexp.MustCompile(`^[A-Za-z][A-Za-z .,'/-]*$`)
	clean := func(value string) string {
		value = normalizeLocationSnippet(value)
		if value == "" {
			return ""
		}
		value = strings.TrimSpace(value)
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "location") {
			value = strings.TrimSpace(strings.TrimLeft(value[len("location"):], ": "))
		}
		if title != "" {
			value = normalizeLocationSnippet(strings.ReplaceAll(value, title, " "))
		}
		if value == "" || normalizePossibleDate(value) != "" {
			return ""
		}
		return value
	}
	isBareLocation := func(value string) bool {
		if value == "" {
			return false
		}
		if len(strings.Fields(value)) > 4 || len(value) > 48 {
			return false
		}
		return bareLocationRE.MatchString(value)
	}
	scanNode := func(root *html.Node, depth int) string {
		var walk func(*html.Node, int) string
		walk = func(n *html.Node, remaining int) string {
			if n == nil || remaining < 0 {
				return ""
			}
			if n.Type == html.ElementNode {
				tag := strings.ToLower(strings.TrimSpace(n.Data))
				if tag == "span" || tag == "div" || tag == "p" {
					text := clean(nodeText(n))
					lower := strings.ToLower(text)
					switch {
					case text == "":
					case title != "" && strings.EqualFold(text, title):
					case team != "" && strings.EqualFold(text, team):
					case postedAt != "" && strings.EqualFold(text, postedAt):
					case strings.Contains(lower, "role description"):
					case strings.Contains(lower, "submit resume"):
					case strings.Contains(lower, "favorite"):
					case strings.Contains(lower, "actions"):
					default:
						if location := extractLocationFromText(text); location != "" {
							return location
						}
						if isBareLocation(text) {
							return text
						}
					}
				}
			}
			if remaining == 0 {
				return ""
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if location := walk(child, remaining-1); location != "" {
					return location
				}
			}
			return ""
		}
		return walk(root, depth)
	}

	for node, depth := anchor, 0; node != nil && depth < 5; node, depth = node.Parent, depth+1 {
		if text := nodeTextByClass(node, []string{"job-title-location", "store-name-container"}, 3); text != "" {
			if location := clean(text); location != "" {
				return location
			}
		}
		if location := scanNode(node, 2); location != "" {
			return location
		}
	}

	return extractLocationNearAnchor(anchor, title)
}

func fetchApple(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	maxPages := appleMaxPages(company, maxLinks)
	seen := map[string]struct{}{}
	jobs := make([]Job, 0, maxLinks)

	for page := 1; page <= maxPages && len(jobs) < maxLinks; page++ {
		pageURL := applePageURL(company.CareersURL, page)
		_, body, err := performRequest(client, "GET", pageURL, nil, nil)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		doc, err := html.Parse(bytes.NewReader(body))
		if err != nil {
			if page == 1 {
				return nil, newFetchError("apple parser failed for %s: %v", company.Name, err)
			}
			break
		}

		var anchors []anchorLink
		gatherAnchors(doc, &anchors)
		pageNewJobs := 0
		for _, link := range anchors {
			href := strings.TrimSpace(link.Href)
			if !strings.Contains(href, "/en-us/details/") {
				continue
			}
			absURL, err := resolveURL(pageURL, href)
			if err != nil {
				continue
			}
			if _, exists := seen[absURL]; exists {
				continue
			}

			title := normalizeTextSnippet(link.Text, 280)
			lowerTitle := strings.ToLower(strings.TrimSpace(title))
			if lowerTitle == "" || strings.Contains(lowerTitle, "see full role description") || strings.Contains(lowerTitle, "submit resume") {
				continue
			}
			if isNoiseTitle(title) {
				continue
			}

			seen[absURL] = struct{}{}
			jobs = append(jobs, Job{
				Company:     company.Name,
				Source:      "apple",
				Title:       title,
				URL:         absURL,
				Location:    appleLocationNearAnchor(link.Node, title),
				Team:        appleTeamNearAnchor(link.Node),
				PostedAt:    extractPostedAtNearAnchor(link.Node),
				Description: descriptionFromAnchorContext(link.Node, title),
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
