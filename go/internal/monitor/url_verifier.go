package monitor

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type urlVerificationTarget struct {
	Company string
	Source  string
	URL     string
}

type urlVerificationResult struct {
	CheckedAt  string
	HTTPStatus int
	Class      string
	FinalURL   string
	Title      string
	Error      string
	DurationMs int64
}

type urlVerificationWorkResult struct {
	URL    string
	Result urlVerificationResult
}

func verifyJobURLsEnabled() bool {
	return parseBoolEnv("VERIFY_JOB_URLS", true)
}

func verifyJobURLsWorkers() int {
	return parseIntEnvBounded("VERIFY_JOB_URLS_WORKERS", 8, 1, 64)
}

func verifyJobURLsTimeoutSeconds() int {
	return parseIntEnvBounded("VERIFY_JOB_URLS_TIMEOUT_SECONDS", 25, 5, 120)
}

func verifyJobURLsMaxURLs() int {
	raw := strings.TrimSpace(os.Getenv("VERIFY_JOB_URLS_MAX_URLS"))
	if raw == "" {
		return 500
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 500
	}
	if parsed <= 0 {
		return 0
	}
	if parsed > 10000 {
		return 10000
	}
	return parsed
}

func parseIntEnvBounded(name string, defaultValue int, minValue int, maxValue int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	if parsed < minValue {
		parsed = minValue
	}
	if maxValue > 0 && parsed > maxValue {
		parsed = maxValue
	}
	return parsed
}

func urlVerificationOutputPath(reportPath string) string {
	if configured := strings.TrimSpace(os.Getenv("VERIFY_JOB_URLS_OUTPUT_PATH")); configured != "" {
		return configured
	}
	trimmed := strings.TrimSpace(reportPath)
	if trimmed == "" {
		return filepath.Join(".state", "last_run_report_url_verification.tsv")
	}
	dir := filepath.Dir(trimmed)
	base := filepath.Base(trimmed)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.TrimSpace(name) == "" {
		name = "last_run_report"
	}
	return filepath.Join(dir, name+"_url_verification.tsv")
}

func collectUniqueJobURLTargets(outcomes []CrawlOutcome) []urlVerificationTarget {
	out := make([]urlVerificationTarget, 0)
	seen := make(map[string]struct{})
	for _, outcome := range outcomes {
		for _, job := range outcome.Jobs {
			rawURL := strings.TrimSpace(job.URL)
			if rawURL == "" {
				continue
			}
			if _, ok := seen[rawURL]; ok {
				continue
			}
			seen[rawURL] = struct{}{}

			company := strings.TrimSpace(job.Company)
			if company == "" {
				company = strings.TrimSpace(outcome.Company)
			}
			source := strings.TrimSpace(job.Source)
			if source == "" {
				source = strings.TrimSpace(outcome.SelectedSource)
			}
			out = append(out, urlVerificationTarget{
				Company: company,
				Source:  source,
				URL:     rawURL,
			})
		}
	}
	return out
}

func VerifyOutcomeJobURLs(outcomes []CrawlOutcome, reportPath string) (URLVerificationSummary, error) {
	summary := URLVerificationSummary{Enabled: true}
	targets := collectUniqueJobURLTargets(outcomes)
	summary.TotalURLs = len(targets)

	maxURLs := verifyJobURLsMaxURLs()
	checkedTargets := targets
	if maxURLs > 0 && len(checkedTargets) > maxURLs {
		checkedTargets = checkedTargets[:maxURLs]
	}
	skippedTargets := targets[len(checkedTargets):]
	summary.CheckedURLs = len(checkedTargets)
	summary.SkippedURLs = len(skippedTargets)

	started := time.Now()
	resultsByURL := verifyJobURLTargets(checkedTargets)
	for _, target := range checkedTargets {
		result, ok := resultsByURL[target.URL]
		if !ok {
			summary.ErrorCount++
			continue
		}
		switch result.Class {
		case "ok":
			summary.OKCount++
		case "blocked":
			summary.BlockedCount++
		default:
			summary.ErrorCount++
		}
	}
	summary.DurationMs = time.Since(started).Milliseconds()

	artifactPath := urlVerificationOutputPath(reportPath)
	if err := writeJobURLVerificationArtifact(artifactPath, checkedTargets, skippedTargets, resultsByURL); err != nil {
		return summary, err
	}
	summary.ArtifactPath = artifactPath
	return summary, nil
}

func verifyJobURLTargets(targets []urlVerificationTarget) map[string]urlVerificationResult {
	out := make(map[string]urlVerificationResult, len(targets))
	if len(targets) == 0 {
		return out
	}

	urls := make([]string, 0, len(targets))
	for _, target := range targets {
		urls = append(urls, target.URL)
	}

	workers := verifyJobURLsWorkers()
	if workers > len(urls) {
		workers = len(urls)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan string)
	results := make(chan urlVerificationWorkResult, len(urls))
	var wg sync.WaitGroup

	for idx := 0; idx < workers; idx++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{
				Timeout: time.Duration(verifyJobURLsTimeoutSeconds()) * time.Second,
			}
			for rawURL := range jobs {
				results <- urlVerificationWorkResult{
					URL:    rawURL,
					Result: checkJobURL(client, rawURL),
				}
			}
		}()
	}

	for _, rawURL := range urls {
		jobs <- rawURL
	}
	close(jobs)
	wg.Wait()
	close(results)

	for item := range results {
		out[item.URL] = item.Result
	}
	return out
}

func checkJobURL(client *http.Client, rawURL string) urlVerificationResult {
	started := time.Now()
	result := urlVerificationResult{
		CheckedAt: utcNow(),
		Class:     "error",
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		result.Error = normalizeTextSnippet(err.Error(), 220)
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CareerMonitorURLVerifier/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		result.Error = normalizeTextSnippet(err.Error(), 220)
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode
	if resp.Request != nil && resp.Request.URL != nil {
		result.FinalURL = strings.TrimSpace(resp.Request.URL.String())
	}

	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 350000))
	if len(payload) > 0 {
		result.Title = normalizeTextSnippet(firstTitleFromHTML(string(payload)), 200)
	}
	if readErr != nil {
		result.Error = normalizeTextSnippet(readErr.Error(), 220)
	}

	switch {
	case classifyHTTPBlock(resp.StatusCode, result.Title):
		result.Class = "blocked"
		if strings.TrimSpace(result.Error) == "" {
			result.Error = fmt.Sprintf("blocked with HTTP %d", resp.StatusCode)
		}
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		result.Class = "ok"
		result.Error = ""
	default:
		result.Class = "error"
		if strings.TrimSpace(result.Error) == "" {
			result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
	}

	result.DurationMs = time.Since(started).Milliseconds()
	return result
}

func writeJobURLVerificationArtifact(path string, checkedTargets []urlVerificationTarget, skippedTargets []urlVerificationTarget, byURL map[string]urlVerificationResult) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	if _, err := writer.WriteString("checked_at\tcompany\tsource\turl\thttp_status\tclass\tfinal_url\ttitle\terror\tduration_ms\n"); err != nil {
		return err
	}

	for _, target := range checkedTargets {
		result, ok := byURL[target.URL]
		if !ok {
			result = urlVerificationResult{
				CheckedAt: utcNow(),
				Class:     "error",
				Error:     "missing verification result",
			}
		}
		line := fmt.Sprintf(
			"%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%d\n",
			sanitizeTSVField(result.CheckedAt),
			sanitizeTSVField(target.Company),
			sanitizeTSVField(target.Source),
			sanitizeTSVField(target.URL),
			result.HTTPStatus,
			sanitizeTSVField(result.Class),
			sanitizeTSVField(result.FinalURL),
			sanitizeTSVField(result.Title),
			sanitizeTSVField(result.Error),
			result.DurationMs,
		)
		if _, err := writer.WriteString(line); err != nil {
			return err
		}
	}

	for _, target := range skippedTargets {
		line := fmt.Sprintf(
			"%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%d\n",
			utcNow(),
			sanitizeTSVField(target.Company),
			sanitizeTSVField(target.Source),
			sanitizeTSVField(target.URL),
			0,
			"skipped",
			"",
			"",
			"skipped due VERIFY_JOB_URLS_MAX_URLS limit",
			0,
		)
		if _, err := writer.WriteString(line); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func sanitizeTSVField(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}
