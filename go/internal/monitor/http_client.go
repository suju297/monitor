package monitor

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func newHTTPClient(timeoutSeconds int) *http.Client {
	if timeoutSeconds < 5 {
		timeoutSeconds = 5
	}
	return &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
}

func firstTitleFromHTML(html string) string {
	if len(html) > 250000 {
		html = html[:250000]
	}
	re := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	match := re.FindStringSubmatch(html)
	if len(match) < 2 {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(match[1])), " ")
}

func performRequest(client *http.Client, method string, rawURL string, headers map[string]string, body io.Reader) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, nil, newFetchError("build request failed for %s: %v", rawURL, err)
	}
	req.Header.Set("User-Agent", "CareerPageMonitor/3.0 (+go-swarm)")
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, newFetchError("request failed for %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, newFetchError("read response failed for %s: %v", rawURL, err)
	}

	title := ""
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.Contains(contentType, "text/html") {
		title = firstTitleFromHTML(string(payload))
	}
	if classifyHTTPBlock(resp.StatusCode, title) {
		detail := "Blocked with HTTP " + resp.Status
		if title != "" {
			detail += " (" + title + ")"
		}
		return resp, payload, &CrawlBlockedError{Message: detail}
	}
	if resp.StatusCode >= 400 {
		return resp, payload, &CrawlFetchError{Message: "HTTP " + resp.Status + " for " + rawURL}
	}
	return resp, payload, nil
}
