package monitor

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type anchorLink struct {
	Href string
	Text string
	Node *html.Node
}

var (
	genericCityStateRE = regexp.MustCompile(
		`(?i)\b[A-Z][A-Za-z .'\-]{1,40},\s*(?:AL|AK|AZ|AR|CA|CO|CT|DE|FL|GA|HI|IA|ID|IL|IN|KS|KY|LA|MA|MD|ME|MI|MN|MO|MS|MT|NC|ND|NE|NH|NJ|NM|NV|NY|OH|OK|OR|PA|RI|SC|SD|TN|TX|UT|VA|VT|WA|WI|WV|WY|DC)\b`,
	)
	genericRemoteUSRE       = regexp.MustCompile(`(?i)\b(?:remote|hybrid|on[\s-]?site)\b[^\n]{0,24}\b(?:u\.s\.a?\.|usa|united states|us)\b|\b(?:u\.s\.a?\.|usa|united states|us)\b[^\n]{0,24}\b(?:remote|hybrid|on[\s-]?site)\b`)
	genericUSOnlyRE         = regexp.MustCompile(`(?i)\b(?:united states(?: of america)?|u\.s\.a?\.|usa)\b`)
	genericPaginationTextRE = regexp.MustCompile(`(?i)^(?:next(?:\s+page)?|older(?:\s+jobs?)?|more\s+jobs|load\s+more|show\s+more|see\s+more(?:\s+jobs?)?|view\s+more(?:\s+jobs?)?)$`)
	genericPaginationSkipRE = regexp.MustCompile(`(?i)\b(?:previous|prev|back|first|last)\b`)
	genericPaginationHrefRE = regexp.MustCompile(`(?i)(?:[?&](?:page|p|pg|offset|start|from)=\d+|/page/\d+\b|#page=\d+)`)
	genericDigitsOnlyRE     = regexp.MustCompile(`^\d+$`)
)

func gatherAnchors(n *html.Node, out *[]anchorLink) {
	if n == nil {
		return
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, "a") {
		href := ""
		for _, attr := range n.Attr {
			if strings.EqualFold(attr.Key, "href") {
				href = strings.TrimSpace(attr.Val)
				break
			}
		}
		if href != "" {
			text := strings.Join(strings.Fields(nodeText(n)), " ")
			*out = append(*out, anchorLink{Href: href, Text: text, Node: n})
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		gatherAnchors(child, out)
	}
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	builder := strings.Builder{}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(nodeText(child))
		builder.WriteString(" ")
	}
	return builder.String()
}

func attrValue(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func nodeClassContains(n *html.Node, marker string) bool {
	if n == nil {
		return false
	}
	classValue := strings.ToLower(strings.TrimSpace(attrValue(n, "class")))
	marker = strings.ToLower(strings.TrimSpace(marker))
	return classValue != "" && marker != "" && strings.Contains(classValue, marker)
}

func nodeHasClassToken(n *html.Node, token string) bool {
	if n == nil {
		return false
	}
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	for _, className := range strings.Fields(strings.ToLower(attrValue(n, "class"))) {
		if className == token {
			return true
		}
	}
	return false
}

func firstDescendantText(root *html.Node, maxLen int, match func(*html.Node) bool) string {
	if root == nil {
		return ""
	}
	out := ""
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || out != "" {
			return
		}
		if match(n) {
			out = normalizeTextSnippet(nodeText(n), maxLen)
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if out != "" {
				return
			}
		}
	}
	walk(root)
	return out
}

func firstDescendantTextByTag(root *html.Node, tag string, maxLen int) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return ""
	}
	return firstDescendantText(root, maxLen, func(n *html.Node) bool {
		return n.Type == html.ElementNode && strings.EqualFold(n.Data, tag)
	})
}

func firstDescendantTextByClass(root *html.Node, marker string, maxLen int) string {
	marker = strings.ToLower(strings.TrimSpace(marker))
	if marker == "" {
		return ""
	}
	return firstDescendantText(root, maxLen, func(n *html.Node) bool {
		return n.Type == html.ElementNode && nodeClassContains(n, marker)
	})
}

func ancestorWithClass(root *html.Node, marker string, maxDepth int) *html.Node {
	marker = strings.ToLower(strings.TrimSpace(marker))
	for node, depth := root, 0; node != nil && depth <= maxDepth; node, depth = node.Parent, depth+1 {
		if node.Type == html.ElementNode && nodeHasClassToken(node, marker) {
			return node
		}
	}
	return nil
}

func normalizeDateSnippet(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	compact := strings.Join(strings.Fields(text), " ")
	if len(compact) > 260 {
		compact = compact[:260]
	}
	return normalizePossibleDate(compact)
}

func postedAtFromNode(n *html.Node, depth int) string {
	if n == nil || depth < 0 {
		return ""
	}
	if value := attrValue(n, "datetime"); value != "" {
		if normalized := normalizePossibleDate(value); normalized != "" {
			return normalized
		}
	}
	for _, key := range []string{
		"data-date", "data-datetime", "data-posted", "data-posted-at", "data-created-at",
		"data-updated-at", "data-time", "content", "title", "aria-label",
	} {
		if value := attrValue(n, key); value != "" {
			if normalized := normalizePossibleDate(value); normalized != "" {
				return normalized
			}
		}
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, "time") {
		if normalized := normalizeDateSnippet(nodeText(n)); normalized != "" {
			return normalized
		}
	}
	if depth == 0 {
		return ""
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if normalized := postedAtFromNode(child, depth-1); normalized != "" {
			return normalized
		}
	}
	return ""
}

func scanTextForPostedAt(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.ElementNode {
		tag := strings.ToLower(strings.TrimSpace(n.Data))
		if tag == "body" || tag == "html" {
			return ""
		}
	}
	return normalizeDateSnippet(nodeText(n))
}

func normalizeLocationSnippet(text string) string {
	compact := strings.Join(strings.Fields(text), " ")
	if compact == "" {
		return ""
	}
	return truncateRunes(compact, 180)
}

func extractLocationFromText(text string) string {
	candidate := normalizeLocationSnippet(text)
	if candidate == "" {
		return ""
	}
	if match := genericCityStateRE.FindString(candidate); match != "" {
		return normalizeLocationSnippet(match)
	}
	if match := genericRemoteUSRE.FindString(candidate); match != "" {
		return normalizeLocationSnippet(match)
	}
	if match := genericUSOnlyRE.FindString(candidate); match != "" {
		return normalizeLocationSnippet(match)
	}
	return ""
}

func extractLocationNearAnchor(anchor *html.Node, title string) string {
	if anchor == nil {
		return ""
	}
	title = strings.TrimSpace(title)
	seen := map[string]struct{}{}
	tryText := func(value string) string {
		if value == "" {
			return ""
		}
		normalized := normalizeLocationSnippet(value)
		if normalized == "" {
			return ""
		}
		if title != "" {
			normalized = normalizeLocationSnippet(strings.ReplaceAll(normalized, title, " "))
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			return ""
		}
		seen[key] = struct{}{}
		return extractLocationFromText(normalized)
	}

	if location := tryText(nodeText(anchor)); location != "" {
		return location
	}
	for node, level := anchor.Parent, 0; node != nil && level < 4; node, level = node.Parent, level+1 {
		if location := tryText(nodeText(node)); location != "" {
			return location
		}
		for sibling, steps := node.PrevSibling, 0; sibling != nil && steps < 3; sibling, steps = sibling.PrevSibling, steps+1 {
			if location := tryText(nodeText(sibling)); location != "" {
				return location
			}
		}
		for sibling, steps := node.NextSibling, 0; sibling != nil && steps < 3; sibling, steps = sibling.NextSibling, steps+1 {
			if location := tryText(nodeText(sibling)); location != "" {
				return location
			}
		}
	}
	return ""
}

func extractPostedAtNearAnchor(anchor *html.Node) string {
	if anchor == nil {
		return ""
	}
	if normalized := postedAtFromNode(anchor, 2); normalized != "" {
		return normalized
	}
	if normalized := scanTextForPostedAt(anchor); normalized != "" {
		return normalized
	}

	for node, level := anchor.Parent, 0; node != nil && level < 4; node, level = node.Parent, level+1 {
		if normalized := postedAtFromNode(node, 2); normalized != "" {
			return normalized
		}
		if normalized := scanTextForPostedAt(node); normalized != "" {
			return normalized
		}
		for sibling, steps := node.PrevSibling, 0; sibling != nil && steps < 3; sibling, steps = sibling.PrevSibling, steps+1 {
			if normalized := postedAtFromNode(sibling, 2); normalized != "" {
				return normalized
			}
			if normalized := scanTextForPostedAt(sibling); normalized != "" {
				return normalized
			}
		}
		for sibling, steps := node.NextSibling, 0; sibling != nil && steps < 3; sibling, steps = sibling.NextSibling, steps+1 {
			if normalized := postedAtFromNode(sibling, 2); normalized != "" {
				return normalized
			}
			if normalized := scanTextForPostedAt(sibling); normalized != "" {
				return normalized
			}
		}
	}
	return ""
}

func hasDigitRun(value string, minRun int) bool {
	if minRun <= 0 {
		return true
	}
	run := 0
	for _, char := range value {
		if char >= '0' && char <= '9' {
			run++
			if run >= minRun {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

func genericLinkScore(title string, rawURL string, postedAt string) int {
	normalizedTitle := strings.ToLower(strings.TrimSpace(title))
	normalizedURL := strings.ToLower(strings.TrimSpace(rawURL))
	words := strings.Fields(normalizedTitle)

	score := 0
	if postedAt != "" {
		score += 6
	}
	if hasDigitRun(normalizedURL, 4) {
		score += 4
	}
	for _, marker := range []string{"jobid=", "gh_jid", "/details/", "/job/", "/position/"} {
		if strings.Contains(normalizedURL, marker) {
			score += 3
			break
		}
	}
	if len(words) >= 4 {
		score += 2
	} else if len(words) >= 2 {
		score++
	}
	if len(normalizedTitle) >= 18 {
		score += 2
	}
	for _, marker := range []string{
		"/jobs/type/", "/jobs/location/", "/jobs/category/", "/jobs/create",
		"/jobsearch", "/careers/list", "/locations/", "/departments/",
		"/about/", "/company/", "?department=", "language=",
	} {
		if strings.Contains(normalizedURL, marker) {
			score -= 4
			break
		}
	}
	if normalizedTitle == "" || normalizedTitle == "jobs" || normalizedTitle == "careers" || normalizedTitle == "english" || normalizedTitle == "简体中文" || normalizedTitle == "español (internacional)" {
		score -= 3
	}
	if len(words) <= 1 && len(normalizedTitle) <= 8 {
		score -= 2
	}
	return score
}

func isResourceAssetURL(rawURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if lower == "" {
		return true
	}
	for _, marker := range []string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico",
		".css", ".js", ".mjs", ".map", ".woff", ".woff2", ".ttf", ".otf", ".pdf",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isNoiseTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	if normalized == "" {
		return true
	}
	if len([]rune(normalized)) > 240 {
		return true
	}
	for _, marker := range []string{
		"<img", "const t=", ".css-", "{display:", "data-gatsby-image", "queryselectorall(", "loading=in",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	if hasExactNonJobTitle(normalized) {
		return true
	}
	if (strings.HasPrefix(normalized, "view ") || strings.HasPrefix(normalized, "browse ") || strings.HasPrefix(normalized, "see ")) && strings.Contains(normalized, " jobs") {
		return true
	}
	if strings.HasPrefix(normalized, "apply now about ") {
		return true
	}
	if strings.Contains(normalized, " available jobs") {
		return true
	}
	for _, marker := range []string{
		"provides investment management solutions",
		"envisions, builds, and deploys",
		"identifies, monitors, evaluates",
		"enables business to flow",
		"discover life at",
		"explore our firm",
		"advice and tips",
		"programs for professionals",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	words := strings.Fields(normalized)
	if len(words) <= 2 {
		for _, shortNoise := range []string{
			"benefits", "culture", "locations", "overview", "jobs", "careers", "legal", "privacy", "search", "home",
			"engineering", "sales", "marketing", "operations", "finance",
		} {
			if normalized == shortNoise {
				return true
			}
		}
	}
	return false
}

func hasStrongJobPathHint(rawURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"/job/", "/jobs/", "/details/", "/careers/job", "/position/", "/vacancy/", "/openings/",
		"jobid=", "job_id=", "gh_jid=", "reqid=", "requisition",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasRoleKeywordInTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	if normalized == "" {
		return false
	}
	for _, keyword := range []string{
		"engineer", "developer", "scientist", "analyst", "architect", "manager",
		"specialist", "intern", "internship", "director", "principal", "staff", "lead", "consultant",
		"sre", "devops", "qa", "site reliability", "product manager", "program manager", "researcher",
		"administrator", "technician", "recruiter", "designer", "owner", "swe",
	} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}

func textScoreForDescription(text string) int {
	wordCount := len(strings.Fields(text))
	if wordCount < 8 {
		return -10
	}
	score := 0
	if wordCount >= 16 {
		score += 6
	}
	if wordCount >= 35 {
		score += 5
	}
	if wordCount >= 65 {
		score += 4
	}
	if wordCount > 300 {
		score -= (wordCount - 300) / 8
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"responsibil", "requirement", "qualification", "about the role", "what you'll do",
		"what you will do", "about you", "experience", "job description", "position",
	} {
		if strings.Contains(lower, marker) {
			score += 5
			break
		}
	}
	return score
}

func descriptionScoreForNode(node *html.Node, text string) int {
	score := textScoreForDescription(text)
	if score < 0 {
		return score
	}
	if node != nil && node.Type == html.ElementNode {
		tag := strings.ToLower(strings.TrimSpace(node.Data))
		switch tag {
		case "article", "section", "li":
			score += 4
		case "div":
			score += 2
		case "main":
			score += 1
		}
		classID := strings.ToLower(strings.TrimSpace(attrValue(node, "class") + " " + attrValue(node, "id")))
		for _, marker := range []string{"description", "job-description", "details", "responsibilities", "requirements", "posting"} {
			if strings.Contains(classID, marker) {
				score += 7
				break
			}
		}
		for _, marker := range []string{"nav", "menu", "footer", "header", "language", "breadcrumb", "social"} {
			if strings.Contains(classID, marker) {
				score -= 7
				break
			}
		}
	}
	return score
}

func descriptionFromAnchorContext(anchor *html.Node, title string) string {
	if anchor == nil {
		return ""
	}
	best := ""
	bestScore := -1
	title = strings.TrimSpace(title)
	for node, depth := anchor, 0; node != nil && depth < 5; node, depth = node.Parent, depth+1 {
		text := normalizeTextSnippet(nodeText(node), 2400)
		if text == "" {
			continue
		}
		if title != "" {
			text = strings.TrimSpace(strings.ReplaceAll(text, title, " "))
		}
		text = normalizeTextSnippet(text, 700)
		if text == "" {
			continue
		}
		score := descriptionScoreForNode(node, text)
		if score > bestScore {
			bestScore = score
			best = text
		}
	}
	if bestScore < 7 {
		return ""
	}
	return best
}

func genericDescriptionFetchLimit() int {
	limit := 8
	if raw := strings.TrimSpace(os.Getenv("GENERIC_DESCRIPTION_FETCH_LIMIT")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 0 {
		return 0
	}
	if limit > 25 {
		return 25
	}
	return limit
}

func isDescriptionCandidateNode(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	tag := strings.ToLower(strings.TrimSpace(n.Data))
	if tag == "article" || tag == "main" {
		return true
	}
	if tag != "section" && tag != "div" && tag != "li" {
		return false
	}
	classID := strings.ToLower(strings.TrimSpace(attrValue(n, "class") + " " + attrValue(n, "id")))
	for _, marker := range []string{
		"job-description", "description", "responsibil", "requirement", "qualification",
		"details", "about-role", "about_the_role", "job-detail", "posting",
	} {
		if strings.Contains(classID, marker) {
			return true
		}
	}
	return false
}

func metaDescription(doc *html.Node) string {
	out := ""
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || out != "" {
			return
		}
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "meta") {
			name := strings.ToLower(attrValue(n, "name"))
			property := strings.ToLower(attrValue(n, "property"))
			if name == "description" || property == "og:description" || property == "twitter:description" {
				out = normalizeTextSnippet(attrValue(n, "content"), 1200)
				if out != "" {
					return
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if out != "" {
				return
			}
		}
	}
	walk(doc)
	return out
}

func descriptionFromDocument(doc *html.Node) string {
	best := ""
	bestScore := -1
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if isDescriptionCandidateNode(n) {
			text := normalizeTextSnippet(nodeText(n), 3200)
			if text != "" {
				score := descriptionScoreForNode(n, text)
				if score > bestScore {
					bestScore = score
					best = truncateRunes(text, 2200)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if bestScore >= 8 {
		return best
	}
	return metaDescription(doc)
}

func postedAtFromDocument(doc *html.Node) string {
	if doc == nil {
		return ""
	}
	out := ""
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || out != "" {
			return
		}
		if n.Type == html.ElementNode {
			tag := strings.ToLower(strings.TrimSpace(n.Data))
			if tag == "meta" {
				name := strings.ToLower(attrValue(n, "name"))
				property := strings.ToLower(attrValue(n, "property"))
				itemprop := strings.ToLower(attrValue(n, "itemprop"))
				if name == "date" || name == "publish-date" || name == "lastmod" ||
					property == "article:published_time" || property == "og:updated_time" ||
					itemprop == "dateposted" || itemprop == "datepublished" {
					if normalized := normalizePossibleDate(attrValue(n, "content")); normalized != "" {
						out = normalized
						return
					}
				}
			}
			if tag == "time" {
				if normalized := postedAtFromNode(n, 2); normalized != "" {
					out = normalized
					return
				}
			}
			classID := strings.ToLower(strings.TrimSpace(attrValue(n, "class") + " " + attrValue(n, "id")))
			if strings.Contains(classID, "posted") || strings.Contains(classID, "publish") || strings.Contains(classID, "date") {
				if normalized := postedAtFromNode(n, 2); normalized != "" {
					out = normalized
					return
				}
				if normalized := scanTextForPostedAt(n); normalized != "" {
					out = normalized
					return
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if out != "" {
				return
			}
		}
	}
	walk(doc)
	return out
}

type jobPageDetails struct {
	Description string
	PostedAt    string
}

func fetchDetailsFromJobPage(client *http.Client, jobURL string) jobPageDetails {
	out := jobPageDetails{}
	_, body, err := performRequest(client, "GET", jobURL, nil, nil)
	if err != nil {
		return out
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return out
	}
	out.Description = descriptionFromDocument(doc)
	out.PostedAt = postedAtFromDocument(doc)
	return out
}

func descriptionNeedsDetailFetch(description string) bool {
	normalized := normalizeTextSnippet(description, 3200)
	if normalized == "" {
		return true
	}
	wordCount := len(strings.Fields(normalized))
	if wordCount < 28 {
		return true
	}
	lower := strings.ToLower(normalized)
	if strings.Contains(lower, "posted:") && wordCount < 42 {
		return true
	}
	return false
}

func postedAtNeedsDetailFetch(postedAt string) bool {
	return strings.TrimSpace(postedAt) == ""
}

func genericMaxPages(company Company) int {
	maxPages := commandEnvInt(company.CommandEnv, "GENERIC_MAX_PAGES", 6, 1)
	if maxPages > 25 {
		return 25
	}
	return maxPages
}

func genericPaginationLinks(doc *html.Node, currentURL string) []string {
	current, err := url.Parse(currentURL)
	if err != nil {
		return nil
	}
	var anchors []anchorLink
	gatherAnchors(doc, &anchors)

	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, link := range anchors {
		href := strings.TrimSpace(link.Href)
		if href == "" || strings.HasPrefix(href, "#") {
			continue
		}
		absoluteURL, err := resolveURL(currentURL, href)
		if err != nil {
			continue
		}
		if absoluteURL == currentURL || isResourceAssetURL(absoluteURL) {
			continue
		}
		nextURL, err := url.Parse(absoluteURL)
		if err != nil || !strings.EqualFold(current.Host, nextURL.Host) {
			continue
		}

		text := strings.ToLower(strings.Join(strings.Fields(link.Text), " "))
		aria := strings.ToLower(normalizeTextSnippet(attrValue(link.Node, "aria-label"), 160))
		titleAttr := strings.ToLower(normalizeTextSnippet(attrValue(link.Node, "title"), 160))
		relAttr := strings.ToLower(normalizeTextSnippet(attrValue(link.Node, "rel"), 40))
		classID := strings.ToLower(strings.TrimSpace(attrValue(link.Node, "class") + " " + attrValue(link.Node, "id")))
		combined := strings.TrimSpace(strings.Join([]string{text, aria, titleAttr, relAttr, classID}, " "))
		if combined == "" {
			combined = strings.ToLower(absoluteURL)
		}
		if genericPaginationSkipRE.MatchString(combined) {
			continue
		}

		isPagination := false
		switch {
		case strings.Contains(relAttr, "next"):
			isPagination = true
		case genericPaginationTextRE.MatchString(text) || genericPaginationTextRE.MatchString(aria) || genericPaginationTextRE.MatchString(titleAttr):
			isPagination = true
		case genericPaginationHrefRE.MatchString(absoluteURL) && (genericDigitsOnlyRE.MatchString(text) || strings.Contains(combined, "page") || strings.Contains(combined, "next")):
			isPagination = true
		}
		if !isPagination {
			continue
		}
		if _, exists := seen[absoluteURL]; exists {
			continue
		}
		seen[absoluteURL] = struct{}{}
		out = append(out, absoluteURL)
	}
	return out
}

func fetchGeneric(company Company) ([]Job, error) {
	client := newHTTPClient(company.TimeoutSeconds)
	maxLinks := company.MaxLinks
	if maxLinks < 1 {
		maxLinks = 200
	}
	maxPages := genericMaxPages(company)
	candidateCap := maxLinks * 10
	if candidateCap < 300 {
		candidateCap = 300
	}
	if candidateCap > 5000 {
		candidateCap = 5000
	}

	type scoredJob struct {
		Score int
		Job   Job
	}

	seen := map[string]struct{}{}
	candidates := make([]scoredJob, 0, maxLinks*2)
	pageQueue := []string{company.CareersURL}
	seenPages := map[string]struct{}{}
	for len(pageQueue) > 0 && len(seenPages) < maxPages && len(candidates) < candidateCap {
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
				return nil, newFetchError("generic parser failed for %s: %v", company.Name, err)
			}
			break
		}

		for _, nextPageURL := range genericPaginationLinks(doc, pageURL) {
			if _, exists := seenPages[nextPageURL]; exists {
				continue
			}
			pageQueue = append(pageQueue, nextPageURL)
		}

		var anchors []anchorLink
		gatherAnchors(doc, &anchors)
		for _, link := range anchors {
			href := strings.TrimSpace(link.Href)
			if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") || strings.HasPrefix(strings.ToLower(href), "javascript:") {
				continue
			}
			absURL, err := resolveURL(pageURL, href)
			if err != nil {
				continue
			}
			if isResourceAssetURL(absURL) {
				continue
			}
			if !looksLikeJobLink(link.Text, absURL) {
				continue
			}
			if _, exists := seen[absURL]; exists {
				continue
			}
			seen[absURL] = struct{}{}
			title := strings.TrimSpace(link.Text)
			if title == "" {
				title = titleFromURL(absURL)
			}
			if isNoiseTitle(title) {
				continue
			}
			postedAt := extractPostedAtNearAnchor(link.Node)
			description := descriptionFromAnchorContext(link.Node, title)
			location := extractLocationNearAnchor(link.Node, title)
			if location == "" && description != "" {
				location = extractLocationFromText(description)
			}
			jobPathHint := hasStrongJobPathHint(absURL)
			if !jobPathHint && postedAt == "" && !hasRoleKeywordInTitle(title) && len(strings.Fields(description)) < 18 {
				continue
			}
			score := genericLinkScore(title, absURL, postedAt)
			if jobPathHint {
				score += 3
			}
			if hasRoleKeywordInTitle(title) {
				score += 2
			}
			candidates = append(candidates, scoredJob{
				Score: score,
				Job: Job{
					Company:     company.Name,
					Source:      "generic",
					Title:       title,
					URL:         absURL,
					Location:    location,
					PostedAt:    postedAt,
					Description: description,
				},
			})
			if len(candidates) >= candidateCap {
				break
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.Score == right.Score {
			if left.Job.PostedAt == right.Job.PostedAt {
				return strings.ToLower(left.Job.Title) < strings.ToLower(right.Job.Title)
			}
			return left.Job.PostedAt > right.Job.PostedAt
		}
		return left.Score > right.Score
	})

	jobs := make([]Job, 0, maxLinks)
	minStrongScore := 2
	for _, candidate := range candidates {
		if candidate.Score < minStrongScore {
			continue
		}
		jobs = append(jobs, Job{
			Company:     company.Name,
			Source:      "generic",
			Title:       candidate.Job.Title,
			URL:         candidate.Job.URL,
			Location:    candidate.Job.Location,
			PostedAt:    candidate.Job.PostedAt,
			Description: candidate.Job.Description,
		})
		if len(jobs) >= maxLinks {
			break
		}
	}
	if len(jobs) < maxLinks/4 {
		for _, candidate := range candidates {
			if candidate.Score < 0 {
				continue
			}
			duplicate := false
			for _, existing := range jobs {
				if existing.URL == candidate.Job.URL {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			jobs = append(jobs, Job{
				Company:     company.Name,
				Source:      "generic",
				Title:       candidate.Job.Title,
				URL:         candidate.Job.URL,
				Location:    candidate.Job.Location,
				PostedAt:    candidate.Job.PostedAt,
				Description: candidate.Job.Description,
			})
			if len(jobs) >= maxLinks {
				break
			}
		}
	}

	detailFetchLimit := genericDescriptionFetchLimit()
	if detailFetchLimit > 0 {
		detailClient := newHTTPClient(company.TimeoutSeconds)
		fetched := 0
		for i := range jobs {
			needsDescription := descriptionNeedsDetailFetch(jobs[i].Description)
			needsPostedAt := postedAtNeedsDetailFetch(jobs[i].PostedAt)
			if !needsDescription && !needsPostedAt {
				continue
			}
			if fetched >= detailFetchLimit {
				break
			}
			details := fetchDetailsFromJobPage(detailClient, jobs[i].URL)
			if needsDescription && details.Description != "" {
				jobs[i].Description = details.Description
			}
			if needsPostedAt && details.PostedAt != "" {
				jobs[i].PostedAt = details.PostedAt
			}
			fetched++
		}
	}

	return jobs, nil
}
