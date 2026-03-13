package monitor

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type eVerifyCacheFile struct {
	UpdatedAt string                          `json:"updated_at,omitempty"`
	Companies map[string]EVerifyCompanyRecord `json:"companies"`
}

type eVerifyJobContext struct {
	Company       string
	CompanyKey    string
	PostingSignal bool
}

var eVerifyRuntime = struct {
	mu        sync.RWMutex
	cache     map[string]EVerifyCompanyRecord
	cachePath string
	loaded    bool
	running   bool
}{
	cache: map[string]EVerifyCompanyRecord{},
}

func eVerifyEnabled() bool {
	return parseBoolEnv("EVERIFY_RESOLUTION_ENABLED", true)
}

func normalizeEVerifyStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enrolled", "yes", "active", "registered":
		return "enrolled"
	case "not_found", "not-found", "missing", "no_match", "no-match":
		return "not_found"
	case "not_enrolled", "not-enrolled", "unenrolled", "terminated", "inactive", "no":
		return "not_enrolled"
	default:
		return "unknown"
	}
}

func normalizeEVerifyFilter(value string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" || raw == "all" {
		return ""
	}
	return normalizeEVerifyStatus(raw)
}

func eVerifyFilterOptions() []string {
	return []string{"all", "enrolled", "unknown", "not_found", "not_enrolled"}
}

func normalizeCompanyLookupKey(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"&", " and ",
		".", " ",
		",", " ",
		"(", " ",
		")", " ",
		"/", " ",
		"\\", " ",
		"-", " ",
		"_", " ",
	)
	lower = replacer.Replace(lower)
	tokens := strings.Fields(lower)
	if len(tokens) == 0 {
		return ""
	}
	suffixes := map[string]struct{}{
		"inc": {}, "incorporated": {}, "corp": {}, "corporation": {}, "co": {}, "company": {}, "llc": {}, "ltd": {}, "limited": {}, "plc": {}, "holdings": {},
	}
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, blocked := suffixes[token]; blocked {
			continue
		}
		filtered = append(filtered, token)
	}
	if len(filtered) == 0 {
		filtered = tokens
	}
	return strings.Join(filtered, " ")
}

func eVerifyDefaultCachePath(statePath string) string {
	if configured := strings.TrimSpace(os.Getenv("EVERIFY_CACHE_PATH")); configured != "" {
		return configured
	}
	root := strings.TrimSpace(filepath.Dir(strings.TrimSpace(statePath)))
	if root == "" || root == "." {
		return ".state/everify_cache.json"
	}
	return filepath.Join(root, "everify_cache.json")
}

func parseEnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func eVerifyRefreshWindow() time.Duration {
	days := parseEnvInt("EVERIFY_REFRESH_DAYS", 21)
	if days <= 0 {
		days = 1
	}
	return time.Duration(days) * 24 * time.Hour
}

func eVerifyUnknownRetryWindow() time.Duration {
	hours := parseEnvInt("EVERIFY_UNKNOWN_RETRY_HOURS", 72)
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

func eVerifyResolverTimeout() time.Duration {
	seconds := parseEnvInt("EVERIFY_RESOLVER_TIMEOUT_SECONDS", 20)
	if seconds <= 0 {
		seconds = 20
	}
	return time.Duration(seconds) * time.Second
}

func eVerifyResolverCommand() string {
	return strings.TrimSpace(os.Getenv("EVERIFY_RESOLVER_CMD"))
}

func eVerifyShouldRefresh(record EVerifyCompanyRecord, now time.Time) bool {
	checkedAt := parseISOTime(record.CheckedAt)
	if checkedAt.IsZero() {
		return true
	}
	age := now.Sub(checkedAt)
	if age < 0 {
		return false
	}
	status := normalizeEVerifyStatus(record.Status)
	if status == "unknown" || status == "not_found" {
		return age >= eVerifyUnknownRetryWindow()
	}
	return age >= eVerifyRefreshWindow()
}

func readEVerifyCache(path string) map[string]EVerifyCompanyRecord {
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]EVerifyCompanyRecord{}
	}
	var payload eVerifyCacheFile
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]EVerifyCompanyRecord{}
	}
	if payload.Companies == nil {
		return map[string]EVerifyCompanyRecord{}
	}
	out := make(map[string]EVerifyCompanyRecord, len(payload.Companies))
	for key, record := range payload.Companies {
		normalizedKey := normalizeCompanyLookupKey(key)
		if normalizedKey == "" {
			normalizedKey = normalizeCompanyLookupKey(record.Company)
		}
		if normalizedKey == "" {
			continue
		}
		record.Status = normalizeEVerifyStatus(record.Status)
		if record.Company == "" {
			record.Company = key
		}
		out[normalizedKey] = record
	}
	return out
}

func writeEVerifyCache(path string, records map[string]EVerifyCompanyRecord) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := eVerifyCacheFile{
		UpdatedAt: utcNow(),
		Companies: records,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func ensureEVerifyCacheLoaded(statePath string) {
	cachePath := strings.TrimSpace(eVerifyDefaultCachePath(statePath))
	eVerifyRuntime.mu.Lock()
	defer eVerifyRuntime.mu.Unlock()
	if eVerifyRuntime.loaded && eVerifyRuntime.cachePath == cachePath {
		return
	}
	eVerifyRuntime.cachePath = cachePath
	eVerifyRuntime.cache = readEVerifyCache(cachePath)
	eVerifyRuntime.loaded = true
}

func eVerifyRecordForCompany(company string) EVerifyCompanyRecord {
	company = strings.TrimSpace(company)
	if company == "" {
		return EVerifyCompanyRecord{Status: "unknown"}
	}
	key := normalizeCompanyLookupKey(company)
	eVerifyRuntime.mu.RLock()
	record, ok := eVerifyRuntime.cache[key]
	eVerifyRuntime.mu.RUnlock()
	if !ok {
		return EVerifyCompanyRecord{
			Company: company,
			Status:  "unknown",
		}
	}
	record.Status = normalizeEVerifyStatus(record.Status)
	if record.Company == "" {
		record.Company = company
	}
	if record.Status == "unknown" {
		record.Note = ""
	}
	return record
}

func jobHasEVerifyMention(job DashboardJob) bool {
	text := strings.TrimSpace(strings.Join([]string{
		normalizeTextSnippet(job.Title, 240),
		normalizeTextSnippet(job.Team, 220),
		normalizeTextSnippet(job.Description, 2400),
	}, " "))
	if text == "" {
		return false
	}
	return eVerifyFriendlyRE.MatchString(text)
}

func eVerifyContextsFromJobs(jobs []DashboardJob) map[string]eVerifyJobContext {
	out := map[string]eVerifyJobContext{}
	for _, job := range jobs {
		company := strings.TrimSpace(job.Company)
		if company == "" {
			continue
		}
		key := normalizeCompanyLookupKey(company)
		if key == "" {
			continue
		}
		contextRow := out[key]
		if contextRow.Company == "" {
			contextRow.Company = company
			contextRow.CompanyKey = key
		}
		if !contextRow.PostingSignal && jobHasEVerifyMention(job) {
			contextRow.PostingSignal = true
		}
		out[key] = contextRow
	}
	return out
}

func parseEVerifyCommandOutput(company string, output []byte) (EVerifyCompanyRecord, error) {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return EVerifyCompanyRecord{}, fmt.Errorf("empty resolver output")
	}

	var asMap map[string]any
	if err := json.Unmarshal(output, &asMap); err == nil {
		status := normalizeEVerifyStatus(asString(asMap["status"]))
		if status == "unknown" && strings.TrimSpace(asString(asMap["status"])) != "" {
			return EVerifyCompanyRecord{}, fmt.Errorf("unsupported status %q", asString(asMap["status"]))
		}
		source := strings.TrimSpace(asString(asMap["source"]))
		if source == "" {
			source = "resolver_command"
		}
		record := EVerifyCompanyRecord{
			Company: company,
			Status:  status,
			Source:  source,
			Note:    strings.TrimSpace(asString(asMap["note"])),
		}
		if checked := strings.TrimSpace(asString(asMap["checked_at"])); checked != "" {
			record.CheckedAt = checked
		}
		return record, nil
	}

	status := normalizeEVerifyStatus(text)
	if status == "unknown" {
		return EVerifyCompanyRecord{}, fmt.Errorf("unable to parse resolver output %q", text)
	}
	return EVerifyCompanyRecord{
		Company: company,
		Status:  status,
		Source:  "resolver_command",
	}, nil
}

func resolveEVerifyFromCommand(company string) (EVerifyCompanyRecord, error) {
	command := eVerifyResolverCommand()
	if command == "" {
		return EVerifyCompanyRecord{}, fmt.Errorf("resolver command is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), eVerifyResolverTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	cmd.Env = append(os.Environ(), "EVERIFY_COMPANY="+company)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return EVerifyCompanyRecord{}, fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseEVerifyCommandOutput(company, output)
}

func loadEVerifyOverridesCSV(path string) map[string]EVerifyCompanyRecord {
	file, err := os.Open(path)
	if err != nil {
		return map[string]EVerifyCompanyRecord{}
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil || len(rows) == 0 {
		return map[string]EVerifyCompanyRecord{}
	}

	headers := map[string]int{}
	for idx, cell := range rows[0] {
		headers[strings.ToLower(strings.TrimSpace(cell))] = idx
	}
	companyIndex, okCompany := headers["company"]
	statusIndex, okStatus := headers["status"]
	if !okCompany || !okStatus {
		return map[string]EVerifyCompanyRecord{}
	}
	noteIndex, hasNote := headers["note"]
	sourceIndex, hasSource := headers["source"]

	out := map[string]EVerifyCompanyRecord{}
	for _, row := range rows[1:] {
		if companyIndex >= len(row) || statusIndex >= len(row) {
			continue
		}
		company := strings.TrimSpace(row[companyIndex])
		if company == "" {
			continue
		}
		key := normalizeCompanyLookupKey(company)
		if key == "" {
			continue
		}
		status := normalizeEVerifyStatus(row[statusIndex])
		note := ""
		source := "override_file"
		if hasNote && noteIndex < len(row) {
			note = strings.TrimSpace(row[noteIndex])
		}
		if hasSource && sourceIndex < len(row) {
			candidate := strings.TrimSpace(row[sourceIndex])
			if candidate != "" {
				source = candidate
			}
		}
		out[key] = EVerifyCompanyRecord{
			Company: company,
			Status:  status,
			Source:  source,
			Note:    note,
		}
	}
	return out
}

func loadEVerifyOverridesJSON(path string) map[string]EVerifyCompanyRecord {
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]EVerifyCompanyRecord{}
	}

	out := map[string]EVerifyCompanyRecord{}

	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil && len(asMap) > 0 {
		for company, value := range asMap {
			key := normalizeCompanyLookupKey(company)
			if key == "" {
				continue
			}
			record := EVerifyCompanyRecord{Company: company, Source: "override_file"}
			switch typed := value.(type) {
			case string:
				record.Status = normalizeEVerifyStatus(typed)
			case map[string]any:
				record.Status = normalizeEVerifyStatus(asString(typed["status"]))
				record.Note = strings.TrimSpace(asString(typed["note"]))
				if source := strings.TrimSpace(asString(typed["source"])); source != "" {
					record.Source = source
				}
			default:
				continue
			}
			if record.Status == "" {
				record.Status = "unknown"
			}
			out[key] = record
		}
		return out
	}

	var asRows []map[string]any
	if err := json.Unmarshal(raw, &asRows); err == nil {
		for _, row := range asRows {
			company := strings.TrimSpace(asString(row["company"]))
			if company == "" {
				continue
			}
			key := normalizeCompanyLookupKey(company)
			if key == "" {
				continue
			}
			record := EVerifyCompanyRecord{
				Company: company,
				Status:  normalizeEVerifyStatus(asString(row["status"])),
				Source:  "override_file",
				Note:    strings.TrimSpace(asString(row["note"])),
			}
			if source := strings.TrimSpace(asString(row["source"])); source != "" {
				record.Source = source
			}
			out[key] = record
		}
	}
	return out
}

func loadEVerifyOverrides() map[string]EVerifyCompanyRecord {
	path := strings.TrimSpace(os.Getenv("EVERIFY_OVERRIDES_FILE"))
	if path == "" {
		return map[string]EVerifyCompanyRecord{}
	}
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".csv":
		return loadEVerifyOverridesCSV(path)
	default:
		return loadEVerifyOverridesJSON(path)
	}
}

func runEVerifyRefresh(pending map[string]eVerifyJobContext, trigger string) {
	overrides := loadEVerifyOverrides()
	resolverCmd := eVerifyResolverCommand()

	keys := make([]string, 0, len(pending))
	for key := range pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	eVerifyRuntime.mu.RLock()
	cachePath := eVerifyRuntime.cachePath
	existing := make(map[string]EVerifyCompanyRecord, len(eVerifyRuntime.cache))
	for key, record := range eVerifyRuntime.cache {
		existing[key] = record
	}
	eVerifyRuntime.mu.RUnlock()

	changed := false
	now := time.Now()
	for _, key := range keys {
		contextRow := pending[key]
		if contextRow.Company == "" {
			continue
		}
		record, hasRecord := existing[key]
		if hasRecord && !eVerifyShouldRefresh(record, now) {
			continue
		}

		next := EVerifyCompanyRecord{
			Company: contextRow.Company,
			Status:  "unknown",
			Source:  "unresolved",
			Note:    "",
		}

		if override, ok := overrides[key]; ok {
			next = override
			next.Company = contextRow.Company
			if strings.TrimSpace(next.Source) == "" {
				next.Source = "override_file"
			}
		} else if contextRow.PostingSignal {
			next = EVerifyCompanyRecord{
				Company: contextRow.Company,
				Status:  "enrolled",
				Source:  "job_posting_signal",
				Note:    "E-Verify participation is mentioned in at least one posting.",
			}
		} else if resolverCmd != "" {
			resolved, err := resolveEVerifyFromCommand(contextRow.Company)
			if err != nil {
				next.Status = "unknown"
				next.Source = "resolver_command"
				next.Note = truncateRunes(err.Error(), 240)
				log.Printf("[everify] lookup failed company=%q trigger=%s err=%s", contextRow.Company, trigger, err)
			} else {
				next = resolved
				next.Company = contextRow.Company
			}
		}

		next.Status = normalizeEVerifyStatus(next.Status)
		next.CheckedAt = utcNow()
		existing[key] = next
		changed = true
	}

	if changed {
		if err := writeEVerifyCache(cachePath, existing); err != nil {
			log.Printf("[everify] write cache failed: %v", err)
		}
		eVerifyRuntime.mu.Lock()
		eVerifyRuntime.cache = existing
		eVerifyRuntime.mu.Unlock()
	}

	eVerifyRuntime.mu.Lock()
	eVerifyRuntime.running = false
	eVerifyRuntime.mu.Unlock()
}

func triggerEVerifyRefreshFromJobs(statePath string, jobs []DashboardJob, trigger string) {
	if !eVerifyEnabled() {
		return
	}
	if len(jobs) == 0 {
		return
	}
	ensureEVerifyCacheLoaded(statePath)
	contexts := eVerifyContextsFromJobs(jobs)
	if len(contexts) == 0 {
		return
	}
	now := time.Now()
	pending := map[string]eVerifyJobContext{}

	eVerifyRuntime.mu.Lock()
	for key, contextRow := range contexts {
		record, hasRecord := eVerifyRuntime.cache[key]
		if hasRecord && !eVerifyShouldRefresh(record, now) {
			continue
		}
		pending[key] = contextRow
	}
	if len(pending) == 0 || eVerifyRuntime.running {
		eVerifyRuntime.mu.Unlock()
		return
	}
	eVerifyRuntime.running = true
	eVerifyRuntime.mu.Unlock()

	go runEVerifyRefresh(pending, trigger)
}
