package monitor

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	htmlstd "html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	//go:embed assets/dashboard.html
	embeddedDashboardHTML string
	//go:embed assets/jobs.html
	embeddedJobsHTML string
	//go:embed assets/mail.html
	embeddedMailHTML string
)

func readJSONFile(path string, fallback any) any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return fallback
	}
	if out == nil {
		return fallback
	}
	return out
}

func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	case float64:
		return v != 0
	case float32:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint64:
		return v != 0
	default:
		return false
	}
}

func isoToLocal(isoValue string) string {
	isoValue = strings.TrimSpace(isoValue)
	if isoValue == "" {
		return "-"
	}
	parsed, err := time.Parse(time.RFC3339, strings.ReplaceAll(isoValue, "Z", "+00:00"))
	if err != nil {
		return isoValue
	}
	return parsed.Local().Format("2006-01-02 15:04:05")
}

func parseISOTime(isoValue string) time.Time {
	isoValue = strings.TrimSpace(isoValue)
	if isoValue == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, strings.ReplaceAll(isoValue, "Z", "+00:00"))
	if err != nil {
		normalized := normalizePossibleDate(isoValue)
		if normalized == "" {
			return time.Time{}
		}
		parsed, err = time.Parse(time.RFC3339, strings.ReplaceAll(normalized, "Z", "+00:00"))
		if err != nil {
			return time.Time{}
		}
	}
	return parsed
}

func jobPostedSortTime(job DashboardJob) time.Time {
	postedAt, ok := parsePostedTime(job.PostedAt)
	if !ok {
		return time.Time{}
	}
	return postedAt
}

func jobDiscoverySortTime(job DashboardJob) time.Time {
	return parseISOTime(job.FirstSeen)
}

func dashboardJobAlphaLess(left DashboardJob, right DashboardJob) bool {
	if strings.EqualFold(left.Company, right.Company) {
		return strings.ToLower(left.Title) < strings.ToLower(right.Title)
	}
	return strings.ToLower(left.Company) < strings.ToLower(right.Company)
}

func dashboardFreshnessLess(left DashboardJob, right DashboardJob, newest bool) bool {
	leftPostedAt := jobPostedSortTime(left)
	rightPostedAt := jobPostedSortTime(right)
	leftHasPostedAt := !leftPostedAt.IsZero()
	rightHasPostedAt := !rightPostedAt.IsZero()
	if leftHasPostedAt != rightHasPostedAt {
		return leftHasPostedAt
	}
	if leftHasPostedAt && rightHasPostedAt {
		if !leftPostedAt.Equal(rightPostedAt) {
			if newest {
				return leftPostedAt.After(rightPostedAt)
			}
			return leftPostedAt.Before(rightPostedAt)
		}
	} else {
		leftDiscoveredAt := jobDiscoverySortTime(left)
		rightDiscoveredAt := jobDiscoverySortTime(right)
		if !leftDiscoveredAt.Equal(rightDiscoveredAt) {
			if leftDiscoveredAt.IsZero() {
				return false
			}
			if rightDiscoveredAt.IsZero() {
				return true
			}
			if newest {
				return leftDiscoveredAt.After(rightDiscoveredAt)
			}
			return leftDiscoveredAt.Before(rightDiscoveredAt)
		}
	}
	return dashboardJobAlphaLess(left, right)
}

func normalizePostedWithinFilter(value string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	switch raw {
	case "", "all", "any", "none":
		return ""
	case "latest", "24h", "1d", "today":
		return "24h"
	case "48h", "2d":
		return "48h"
	case "7d", "week", "last_week":
		return "7d"
	default:
		return ""
	}
}

func postedWithinOptions() []string {
	return []string{"all", "24h", "48h", "7d"}
}

func normalizeSourceFilter(value string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	switch raw {
	case "", "all", "any", "none":
		return ""
	default:
		return raw
	}
}

func jobHasParsedPostedDate(job DashboardJob) bool {
	_, ok := parsePostedTime(job.PostedAt)
	return ok
}

func matchesPostedWithinFilter(job DashboardJob, postedWithin string, now time.Time) bool {
	filterValue := normalizePostedWithinFilter(postedWithin)
	if filterValue == "" {
		return true
	}
	jobTime, ok := parsePostedTime(job.PostedAt)
	if !ok || jobTime.IsZero() {
		return false
	}
	jobTime = jobTime.In(now.Location())
	var threshold time.Time
	switch filterValue {
	case "24h":
		threshold = now.Add(-24 * time.Hour)
	case "48h":
		threshold = now.Add(-48 * time.Hour)
	case "7d":
		threshold = now.Add(-7 * 24 * time.Hour)
	default:
		return true
	}
	return !jobTime.Before(threshold)
}

func dashboardAsSourceJob(job DashboardJob) Job {
	return Job{
		Company:     job.Company,
		Source:      job.Source,
		Title:       job.Title,
		URL:         job.URL,
		Location:    job.Location,
		Team:        job.Team,
		PostedAt:    job.PostedAt,
		Description: job.Description,
	}
}

func looksLikeNoiseDashboardJob(job DashboardJob) bool {
	return looksLikeNoiseJob(dashboardAsSourceJob(job))
}

func dashboardHasStrongJobPathHint(rawURL string) bool {
	return hasStrongJobPathHint(rawURL)
}

func dashboardHasRoleKeyword(title string) bool {
	return hasRoleKeywordInTitle(title)
}

func jobSignalScore(job DashboardJob) int {
	return sourceJobSignalScore(dashboardAsSourceJob(job))
}

func dashboardDisplayCompany(rawCompany string, rawTeam string, rawURL string) string {
	company := strings.TrimSpace(rawCompany)
	team := strings.TrimSpace(rawTeam)
	jobURL := strings.TrimSpace(rawURL)
	if !strings.EqualFold(company, "My Greenhouse") {
		return company
	}
	if team != "" {
		return team
	}
	if employer := greenhouseEmployerFromJobURL(jobURL); employer != "" {
		return employer
	}
	return company
}

func companyFilterKey(value string) string {
	normalized := normalizeCompanyLookupKey(value)
	if normalized == "" {
		normalized = strings.ToLower(strings.TrimSpace(value))
	}
	if normalized == "" {
		return ""
	}
	return strings.Join(strings.Fields(normalized), "")
}

func choosePreferredCompanyLabel(existing string, candidate string) string {
	existing = strings.TrimSpace(existing)
	candidate = strings.TrimSpace(candidate)
	if existing == "" {
		return candidate
	}
	if candidate == "" {
		return existing
	}
	score := func(value string) int {
		out := 0
		if strings.Contains(value, " ") {
			out += 3
		}
		if value != strings.ToLower(value) {
			out++
		}
		if strings.ContainsAny(value, ".,&-/") {
			out++
		}
		if len(value) >= 6 {
			out++
		}
		return out
	}
	existingScore := score(existing)
	candidateScore := score(candidate)
	if candidateScore > existingScore {
		return candidate
	}
	if candidateScore == existingScore && len(candidate) > len(existing) {
		return candidate
	}
	return existing
}

func collectJobsFromSeen(seen map[string]any, activeOnly bool) []DashboardJob {
	jobs := make([]DashboardJob, 0)
	for company, rawEntries := range seen {
		entries := asMap(rawEntries)
		for _, rowAny := range entries {
			row := asMap(rowAny)
			title := asString(row["title"])
			jobURL := asString(row["url"])
			if title == "" && jobURL == "" {
				continue
			}
			displayCompany := dashboardDisplayCompany(company, asString(row["team"]), jobURL)
			active := asBool(row["active"])
			if activeOnly && !active {
				continue
			}
			firstSeen := asString(row["first_seen"])
			lastSeen := asString(row["last_seen"])
			if lastSeen == "" {
				lastSeen = firstSeen
			}
			postedAt := asString(row["posted_at"])
			jobs = append(jobs, DashboardJob{
				Company:        displayCompany,
				Title:          title,
				URL:            jobURL,
				FirstSeen:      firstSeen,
				FirstSeenLocal: isoToLocal(firstSeen),
				LastSeen:       lastSeen,
				LastSeenLocal:  isoToLocal(lastSeen),
				Active:         active,
				Source:         asString(row["source"]),
				Location:       asString(row["location"]),
				Team:           asString(row["team"]),
				PostedAt:       postedAt,
				PostedAtLocal:  isoToLocal(postedAt),
				Description:    asString(row["description"]),
			})
		}
	}
	return jobs
}

func prettifyEmployerSlug(value string) string {
	cleaned := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "-", " "), "_", " "))
	if cleaned == "" {
		return ""
	}
	tokens := strings.Fields(cleaned)
	if len(tokens) == 0 {
		return ""
	}
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if isOnlyDigits(lower) {
			out = append(out, lower)
			continue
		}
		runes := []rune(lower)
		if len(runes) <= 3 {
			out = append(out, strings.ToUpper(lower))
			continue
		}
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		out = append(out, string(runes))
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

func isOnlyDigits(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func greenhouseEmployerFromJobURL(rawURL string) string {
	candidate := strings.TrimSpace(rawURL)
	if candidate == "" {
		return ""
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if !strings.Contains(host, "greenhouse.io") {
		return ""
	}
	parts := make([]string, 0, 6)
	for _, part := range strings.Split(parsed.Path, "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) < 3 {
		return ""
	}
	if strings.ToLower(parts[1]) != "jobs" {
		return ""
	}
	return prettifyEmployerSlug(parts[0])
}

func companyListFromJobs(jobs []DashboardJob) []string {
	labels := map[string]string{}
	for _, job := range jobs {
		name := strings.TrimSpace(job.Company)
		if name == "" {
			continue
		}
		key := companyFilterKey(name)
		if key == "" {
			continue
		}
		labels[key] = choosePreferredCompanyLabel(labels[key], name)
	}
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == "" {
			continue
		}
		out = append(out, label)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func sourceListFromJobs(jobs []DashboardJob) []string {
	labels := map[string]string{}
	for _, job := range jobs {
		source := normalizeSourceFilter(job.Source)
		if source == "" {
			continue
		}
		labels[source] = source
	}
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == "" {
			continue
		}
		out = append(out, label)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func countActiveSeenEntries(rawEntries any) int {
	entries := asMap(rawEntries)
	count := 0
	for _, rowAny := range entries {
		row := asMap(rowAny)
		if asBool(row["active"]) {
			count++
		}
	}
	return count
}

func sortDashboardJobs(jobs []DashboardJob, sortBy string) {
	mode := strings.ToLower(strings.TrimSpace(sortBy))
	if mode == "" {
		mode = "newest"
	}
	sort.Slice(jobs, func(i, j int) bool {
		left := jobs[i]
		right := jobs[j]
		switch mode {
		case "best_match":
			if left.MatchScore == right.MatchScore {
				return dashboardFreshnessLess(left, right, true)
			}
			return left.MatchScore > right.MatchScore
		case "oldest":
			return dashboardFreshnessLess(left, right, false)
		case "company":
			return dashboardJobAlphaLess(left, right)
		case "title":
			if strings.EqualFold(left.Title, right.Title) {
				return strings.ToLower(left.Company) < strings.ToLower(right.Company)
			}
			return strings.ToLower(left.Title) < strings.ToLower(right.Title)
		default:
			return dashboardFreshnessLess(left, right, true)
		}
	})
}

type MonitorRunner struct {
	mu            sync.Mutex
	status        RunnerStatus
	cfg           MonitorRunnerConfig
	progressIndex map[string]int
	coordinator   *BackgroundTaskCoordinator
}

func NewMonitorRunner(cfg MonitorRunnerConfig) *MonitorRunner {
	return &MonitorRunner{
		cfg: cfg,
		status: RunnerStatus{
			Running:      false,
			LastExitCode: 0,
		},
		progressIndex: map[string]int{},
	}
}

func (m *MonitorRunner) SetCoordinator(coordinator *BackgroundTaskCoordinator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.coordinator = coordinator
}

func (m *MonitorRunner) Snapshot() RunnerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := m.status
	snapshot.Scoring = slmScoringStatusSnapshot()
	if len(m.status.Progress) > 0 {
		snapshot.Progress = make([]RunnerCompanyProgress, len(m.status.Progress))
		for i, item := range m.status.Progress {
			snapshot.Progress[i] = item
			if len(item.AttemptedSources) > 0 {
				snapshot.Progress[i].AttemptedSources = append([]string(nil), item.AttemptedSources...)
			}
		}
	}
	return snapshot
}

func (m *MonitorRunner) resetProgressLocked() {
	m.progressIndex = map[string]int{}
	m.status.TotalCompanies = 0
	m.status.CompletedCompanies = 0
	m.status.Progress = nil

	companies, err := LoadCompanies(m.cfg.ConfigPath)
	if err != nil {
		return
	}
	names := make([]string, 0, len(companies))
	for _, company := range companies {
		names = append(names, company.Name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	progress := make([]RunnerCompanyProgress, 0, len(names))
	for _, name := range names {
		idx := len(progress)
		progress = append(progress, RunnerCompanyProgress{
			Company:   name,
			Phase:     "queued",
			JobsFound: 0,
		})
		m.progressIndex[name] = idx
	}
	m.status.Progress = progress
	m.status.TotalCompanies = len(progress)
}

func (m *MonitorRunner) ensureProgressEntryLocked(company string) int {
	if idx, ok := m.progressIndex[company]; ok {
		return idx
	}
	idx := len(m.status.Progress)
	m.status.Progress = append(m.status.Progress, RunnerCompanyProgress{
		Company:   company,
		Phase:     "queued",
		JobsFound: 0,
	})
	m.progressIndex[company] = idx
	m.status.TotalCompanies = len(m.status.Progress)
	return idx
}

func (m *MonitorRunner) recomputeCompletedLocked() {
	completed := 0
	for _, item := range m.status.Progress {
		if item.Phase == "done" {
			completed++
		}
	}
	m.status.CompletedCompanies = completed
}

func liveOutcomeStatusLine(outcome CrawlOutcome) string {
	switch strings.ToLower(strings.TrimSpace(outcome.Status)) {
	case "ok":
		return fmt.Sprintf("[OK] %s (%s): %d found", outcome.Company, outcome.SelectedSource, len(outcome.Jobs))
	case "blocked":
		return fmt.Sprintf("[BLOCKED] %s (%s): %s", outcome.Company, strings.Join(outcome.AttemptedSources, " -> "), outcome.Message)
	case "error":
		return fmt.Sprintf("[ERROR] %s (%s): %s", outcome.Company, strings.Join(outcome.AttemptedSources, " -> "), outcome.Message)
	default:
		return ""
	}
}

func (m *MonitorRunner) markCompanyStart(company string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.ensureProgressEntryLocked(company)
	entry := m.status.Progress[idx]
	if entry.Phase == "done" {
		return
	}
	entry.Phase = "running"
	if strings.TrimSpace(entry.StartedAt) == "" {
		entry.StartedAt = utcNow()
	}
	m.status.Progress[idx] = entry
}

func (m *MonitorRunner) markCompanyDone(outcome CrawlOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.ensureProgressEntryLocked(outcome.Company)
	entry := m.status.Progress[idx]
	if strings.TrimSpace(entry.StartedAt) == "" {
		entry.StartedAt = utcNow()
	}
	entry.Phase = "done"
	entry.OutcomeStatus = strings.ToLower(strings.TrimSpace(outcome.Status))
	entry.Source = strings.TrimSpace(outcome.SelectedSource)
	if entry.Source == "" && len(outcome.AttemptedSources) > 0 {
		entry.Source = strings.Join(outcome.AttemptedSources, " -> ")
	}
	entry.JobsFound = len(outcome.Jobs)
	entry.AttemptedSources = append([]string(nil), outcome.AttemptedSources...)
	entry.Message = outcome.Message
	entry.FinishedAt = utcNow()
	m.status.Progress[idx] = entry
	m.recomputeCompletedLocked()

	line := strings.TrimSpace(liveOutcomeStatusLine(outcome))
	if line != "" {
		if strings.TrimSpace(m.status.LastStdout) == "" {
			m.status.LastStdout = line
		} else {
			m.status.LastStdout = m.status.LastStdout + "\n" + line
		}
	}
}

func (m *MonitorRunner) Trigger(dryRun bool) taskTriggerResult {
	if m.coordinator == nil {
		if m.startRun(dryRun) {
			return taskTriggerResult{Accepted: true, Started: true, Message: "Run started."}
		}
		return taskTriggerResult{Message: "Tracker run already in progress."}
	}
	result := m.coordinator.RequestCrawl(dryRun)
	if result.Queued {
		m.markQueued(dryRun)
	}
	return result
}

func (m *MonitorRunner) startRun(dryRun bool) bool {
	m.mu.Lock()
	if m.status.Running {
		m.mu.Unlock()
		return false
	}
	m.status.Running = true
	m.status.Queued = false
	m.status.QueuedMode = ""
	m.status.LastStart = utcNow()
	if dryRun {
		m.status.LastMode = "dry-run"
	} else {
		m.status.LastMode = "live-run"
	}
	m.status.LastStdout = ""
	m.status.LastStderr = ""
	m.status.LastError = ""
	m.resetProgressLocked()
	m.mu.Unlock()

	go m.runWorker(dryRun)
	return true
}

func (m *MonitorRunner) markQueued(dryRun bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Queued = true
	if dryRun {
		m.status.QueuedMode = "dry-run"
	} else {
		m.status.QueuedMode = "live-run"
	}
}

func (m *MonitorRunner) runWorker(dryRun bool) {
	opts := RunOptions{
		ConfigPath:     m.cfg.ConfigPath,
		StatePath:      m.cfg.StatePath,
		ReportPath:     m.cfg.ReportPath,
		DotenvPath:     m.cfg.DotenvPath,
		Workers:        m.cfg.Workers,
		DryRun:         dryRun,
		AlertOnBlocked: m.cfg.AlertOnBlocked,
		OnCompanyStart: m.markCompanyStart,
		OnCompanyDone:  m.markCompanyDone,
	}

	result, err := RunMonitor(opts)
	if err == nil && slmScoringEnabled() && slmScoringPrecomputeOnRun() {
		go precomputeSLMScoresFromState(m.cfg.StatePath, "post-run")
	}
	m.mu.Lock()
	if err != nil {
		m.status.LastExitCode = 1
		m.status.LastError = err.Error()
	} else {
		m.status.LastExitCode = 0
		m.status.LastStdout = strings.Join(result.StatusLines, "\n")
	}
	m.recomputeCompletedLocked()
	m.status.Running = false
	m.status.LastEnd = utcNow()
	m.mu.Unlock()
	if m.coordinator != nil {
		m.coordinator.Finish(backgroundTaskCrawl)
	}
}

type DashboardApp struct {
	CWD           string
	StatePath     string
	ConfigPath    string
	ReportPath    string
	Runner        *MonitorRunner
	Scheduler     *CrawlScheduler
	MailRunner    *MailSyncRunner
	MailScheduler *MailScheduler
}

var slmPrecomputeState = struct {
	mu     sync.RWMutex
	status SLMScoringStatus
}{
	status: SLMScoringStatus{Phase: "idle"},
}

var jobsFeedProgressState = struct {
	mu     sync.RWMutex
	status JobsFeedProgress
}{
	status: JobsFeedProgress{Phase: "idle"},
}

func slmScoringStatusSnapshot() SLMScoringStatus {
	slmPrecomputeState.mu.RLock()
	defer slmPrecomputeState.mu.RUnlock()
	return slmPrecomputeState.status
}

func updateSLMScoringStatus(mutator func(status *SLMScoringStatus)) {
	slmPrecomputeState.mu.Lock()
	defer slmPrecomputeState.mu.Unlock()
	mutator(&slmPrecomputeState.status)
}

func jobsFeedProgressSnapshot() JobsFeedProgress {
	jobsFeedProgressState.mu.RLock()
	defer jobsFeedProgressState.mu.RUnlock()
	return jobsFeedProgressState.status
}

func startJobsFeedProgress() string {
	runID := fmt.Sprintf("jobs-feed-%d", time.Now().UnixNano())
	now := utcNow()
	jobsFeedProgressState.mu.Lock()
	jobsFeedProgressState.status = JobsFeedProgress{
		RunID:           runID,
		Running:         true,
		Phase:           "starting",
		Message:         "Preparing jobs feed",
		ProgressPercent: 2,
		StartedAt:       now,
		UpdatedAt:       now,
		FinishedAt:      "",
		Scoring: JobsFeedScoringProgress{
			Running: false,
		},
	}
	jobsFeedProgressState.mu.Unlock()
	return runID
}

func updateJobsFeedProgress(runID string, mutator func(status *JobsFeedProgress)) {
	if strings.TrimSpace(runID) == "" {
		return
	}
	jobsFeedProgressState.mu.Lock()
	defer jobsFeedProgressState.mu.Unlock()
	if jobsFeedProgressState.status.RunID != runID {
		return
	}
	mutator(&jobsFeedProgressState.status)
	if jobsFeedProgressState.status.ProgressPercent < 0 {
		jobsFeedProgressState.status.ProgressPercent = 0
	}
	if jobsFeedProgressState.status.ProgressPercent > 100 {
		jobsFeedProgressState.status.ProgressPercent = 100
	}
	jobsFeedProgressState.status.UpdatedAt = utcNow()
}

func (app *DashboardApp) BuildOverview() DashboardOverview {
	stateValue := readJSONFile(app.StatePath, map[string]any{"seen": map[string]any{}, "company_status": map[string]any{}, "blocked_events": map[string]any{}, "last_run": ""})
	reportValue := readJSONFile(app.ReportPath, map[string]any{})

	state := asMap(stateValue)
	report := asMap(reportValue)
	seen := asMap(state["seen"])
	companyStatus := asMap(state["company_status"])
	blockedEvents := asMap(state["blocked_events"])
	reportSummary := asMap(report["summary"])

	activeJobs := make([]DashboardJob, 0)
	activeJobsByCompany := map[string]int{}
	totalSeenJobs := 0
	if snapshot, err := loadJobsFeedFromDB(app.StatePath, "", "", "", "", "newest", 5000, true, false); err == nil {
		activeJobs = snapshot.Jobs
		totalSeenJobs = snapshot.TotalJobs
		if rollups, rollupErr := loadJobsCompanyRollupsFromDB(app.StatePath); rollupErr == nil {
			for companyName, rollup := range rollups {
				activeJobsByCompany[companyName] = rollup.ActiveJobs
			}
		}
		if len(activeJobsByCompany) == 0 {
			for _, job := range activeJobs {
				activeJobsByCompany[job.Company]++
			}
		}
	}
	if len(activeJobs) == 0 && len(activeJobsByCompany) == 0 && totalSeenJobs == 0 {
		activeJobs = collectJobsFromSeen(seen, true)
		totalSeenJobs = len(activeJobs)
		for companyName := range seen {
			activeJobsByCompany[companyName] = countActiveSeenEntries(seen[companyName])
		}
	}

	nameSet := map[string]struct{}{}
	for name := range activeJobsByCompany {
		nameSet[name] = struct{}{}
	}
	for name := range companyStatus {
		nameSet[name] = struct{}{}
	}
	for name := range blockedEvents {
		nameSet[name] = struct{}{}
	}
	companyNames := make([]string, 0, len(nameSet))
	for name := range nameSet {
		companyNames = append(companyNames, name)
	}
	sort.Slice(companyNames, func(i, j int) bool {
		return strings.ToLower(companyNames[i]) < strings.ToLower(companyNames[j])
	})

	statusCounts := map[string]int{"ok": 0, "blocked": 0, "error": 0, "unknown": 0}
	companies := make([]DashboardCompany, 0, len(companyNames))
	for _, name := range companyNames {
		statusPayload := asMap(companyStatus[name])
		status := strings.ToLower(strings.TrimSpace(asString(statusPayload["status"])))
		if _, ok := statusCounts[status]; !ok {
			status = "unknown"
		}
		statusCounts[status]++
		seenCount := activeJobsByCompany[name]
		blockedCount := 0
		if rows := asSlice(blockedEvents[name]); rows != nil {
			blockedCount = len(rows)
		}
		attempted := make([]string, 0)
		for _, entry := range asSlice(statusPayload["attempted_sources"]) {
			attempted = append(attempted, asString(entry))
		}
		companies = append(companies, DashboardCompany{
			Name:            name,
			Status:          status,
			SelectedSource:  asString(statusPayload["selected_source"]),
			AttemptedSource: attempted,
			Message:         asString(statusPayload["message"]),
			UpdatedAt:       asString(statusPayload["updated_at"]),
			UpdatedAtLocal:  isoToLocal(asString(statusPayload["updated_at"])),
			SeenJobs:        seenCount,
			BlockedEvents:   blockedCount,
		})
	}

	blockedRecent := make([]DashboardBlocked, 0)
	for company, rawEvents := range blockedEvents {
		rows := asSlice(rawEvents)
		for _, rowAny := range rows {
			row := asMap(rowAny)
			attempted := make([]string, 0)
			for _, source := range asSlice(row["attempted_sources"]) {
				attempted = append(attempted, asString(source))
			}
			blockedRecent = append(blockedRecent, DashboardBlocked{
				Company:          company,
				At:               asString(row["at"]),
				AtLocal:          isoToLocal(asString(row["at"])),
				Message:          asString(row["message"]),
				AttemptedSources: attempted,
			})
		}
	}
	sort.Slice(blockedRecent, func(i, j int) bool {
		return blockedRecent[i].At > blockedRecent[j].At
	})
	if len(blockedRecent) > 60 {
		blockedRecent = blockedRecent[:60]
	}

	triggerEVerifyRefreshFromJobs(app.StatePath, activeJobs, "overview")
	newJobs := filterJobsForFeed(activeJobs, "", "", "", "", "", false)
	sortDashboardJobs(newJobs, "newest")
	if len(newJobs) > 200 {
		newJobs = newJobs[:200]
	}

	summary := DashboardSummary{
		GeneratedAt:       utcNow(),
		LastRun:           asString(state["last_run"]),
		LastRunLocal:      isoToLocal(asString(state["last_run"])),
		CompaniesTotal:    len(companyNames),
		StatusCounts:      statusCounts,
		TotalSeenJobs:     totalSeenJobs,
		NewJobsLastReport: parseIntDefault(reportSummary["new_jobs_count"], len(newJobs)),
		BlockedLastReport: parseIntDefault(reportSummary["blocked_count"], 0),
	}

	mailSummary := MailOverviewSummary{
		GeneratedAt: utcNow(),
		EventCounts: map[string]int{},
	}
	if mailOverview, err := loadMailOverview(app.StatePath); err == nil {
		mailSummary = mailOverview.Summary
	}

	return DashboardOverview{
		Summary:       summary,
		Companies:     companies,
		BlockedRecent: blockedRecent,
		NewJobs:       newJobs,
		Mail:          mailSummary,
		Runner:        app.Runner.Snapshot(),
		Paths: map[string]string{
			"state_file":  app.StatePath,
			"report_file": app.ReportPath,
			"workspace":   app.CWD,
		},
	}
}

func frontendBaseURL() string {
	value := strings.TrimSpace(os.Getenv("FRONTEND_BASE_URL"))
	if value == "" {
		value = "http://127.0.0.1:5173"
	}
	return strings.TrimRight(value, "/")
}

func redirectToFrontend(w http.ResponseWriter, r *http.Request, baseURL string, fallbackPath string) {
	path := strings.TrimSpace(r.URL.Path)
	if path == "" || path == "/" {
		path = fallbackPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := baseURL + path
	if query := strings.TrimSpace(r.URL.RawQuery); query != "" {
		target = target + "?" + query
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

func frontendRouteReady(baseURL string, path string) bool {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return false
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	client := &http.Client{
		Timeout: 450 * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func serveEmbeddedHTML(w http.ResponseWriter, r *http.Request, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(html))
}

func serveFrontendOrEmbeddedHTML(w http.ResponseWriter, r *http.Request, frontendBase string, fallbackPath string, embeddedHTML string) {
	if frontendRouteReady(frontendBase, fallbackPath) {
		redirectToFrontend(w, r, frontendBase, fallbackPath)
		return
	}
	serveEmbeddedHTML(w, r, embeddedHTML)
}

func (app *DashboardApp) BuildJobsFeed(query string, company string, source string, everifyFilter string, postedWithin string, sortBy string, limit int, includeNoise bool, includeInactive bool, slmModelOverride string) DashboardJobsResponse {
	runID := startJobsFeedProgress()
	setSLMScoringCacheStatePath(app.StatePath)
	slmOptions := slmScoringOptionsForModel(slmModelOverride)
	effectiveSLMModel := slmOptions.effectiveModelLabel()
	defer func() {
		updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
			if !status.Running {
				return
			}
			status.Running = false
			status.Phase = "done"
			status.Message = "Jobs feed ready"
			status.ProgressPercent = 100
			status.FinishedAt = utcNow()
		})
	}()

	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	query = strings.ToLower(strings.TrimSpace(query))
	company = strings.TrimSpace(company)
	source = normalizeSourceFilter(source)
	everifyFilter = normalizeEVerifyFilter(everifyFilter)
	postedWithin = normalizePostedWithinFilter(postedWithin)
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	if sortBy == "" {
		sortBy = "best_match"
	}

	updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
		status.Phase = "loading_snapshot"
		status.Message = "Loading jobs snapshot"
		status.ProgressPercent = 10
		status.SLMModel = effectiveSLMModel
		status.TotalJobs = 0
		status.FilteredJobs = 0
		status.Scoring = JobsFeedScoringProgress{}
	})

	var (
		allJobs        []DashboardJob
		companies      []string
		sources        []string
		totalJobs      int
		snapshotSource = "state"
	)

	if snapshot, err := loadJobsFeedFromDB(app.StatePath, query, company, source, postedWithin, sortBy, limit, includeNoise, includeInactive); err == nil {
		allJobs = snapshot.Jobs
		companies = snapshot.Companies
		sources = snapshot.Sources
		totalJobs = snapshot.TotalJobs
		snapshotSource = "jobs-db"
	} else {
		stateValue := readJSONFile(app.StatePath, map[string]any{"seen": map[string]any{}})
		state := asMap(stateValue)
		seen := asMap(state["seen"])
		allJobs = collectJobsFromSeen(seen, !includeInactive)
		companies = companyListFromJobs(allJobs)
		sources = sourceListFromJobs(allJobs)
		totalJobs = len(allJobs)
	}

	updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
		status.Phase = "snapshot_loaded"
		status.Message = fmt.Sprintf("Loaded %d active jobs from %s", totalJobs, snapshotSource)
		status.ProgressPercent = 26
		status.TotalJobs = totalJobs
	})

	triggerEVerifyRefreshFromJobs(app.StatePath, allJobs, "jobs-feed")

	filterQuery := query
	filterCompany := company
	filterSource := source
	filterPostedWithin := postedWithin

	updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
		status.Phase = "filtering"
		status.Message = "Filtering jobs"
		status.ProgressPercent = 38
	})

	filtered := filterJobsForFeed(allJobs, filterQuery, filterCompany, filterSource, everifyFilter, filterPostedWithin, includeNoise)
	updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
		status.Phase = "filtered"
		status.Message = fmt.Sprintf("Filter complete: %d jobs matched", len(filtered))
		status.ProgressPercent = 50
		status.FilteredJobs = len(filtered)
	})

	scoringSummary := slmApplySummary{
		EligibleJobs: len(filtered),
		Skipped:      true,
		SkipReason:   "include-noise",
	}
	if !includeNoise {
		updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
			status.Phase = "scoring"
			status.Message = fmt.Sprintf("Scoring top matches with %s", effectiveSLMModel)
			status.ProgressPercent = 56
			status.Scoring = JobsFeedScoringProgress{
				Running:       true,
				ScheduledJobs: 0,
				QueuedJobs:    0,
				CompletedJobs: 0,
				SuccessJobs:   0,
				FailedJobs:    0,
			}
		})
		scoringSummary = applyCachedSLMMatchScoresWithProgress(filtered, sortBy, limit, func(progress slmApplyProgress) {
			progressPercent := 56
			if progress.ScheduledJobs > 0 {
				progressPercent += (progress.CompletedJobs * 24) / progress.ScheduledJobs
			}
			updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
				status.Phase = "scoring"
				status.Message = fmt.Sprintf("Scoring top matches with %s: %d/%d complete", effectiveSLMModel, progress.CompletedJobs, progress.ScheduledJobs)
				status.ProgressPercent = progressPercent
				status.Scoring = JobsFeedScoringProgress{
					Running:       progress.ScheduledJobs > 0 && progress.CompletedJobs < progress.ScheduledJobs,
					ScheduledJobs: progress.ScheduledJobs,
					QueuedJobs:    progress.QueuedJobs,
					CompletedJobs: progress.CompletedJobs,
					SuccessJobs:   progress.SuccessJobs,
					FailedJobs:    progress.FailedJobs,
				}
			})
		}, slmOptions)
		updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
			status.ProgressPercent = 80
			status.Scoring = JobsFeedScoringProgress{
				Running:       false,
				ScheduledJobs: scoringSummary.ScheduledJobs,
				QueuedJobs:    max(0, scoringSummary.ScheduledJobs-scoringSummary.CompletedJobs),
				CompletedJobs: scoringSummary.CompletedJobs,
				SuccessJobs:   scoringSummary.SuccessJobs,
				FailedJobs:    scoringSummary.FailedJobs,
			}
			if scoringSummary.ScheduledJobs == 0 {
				status.Message = fmt.Sprintf("No cached SLM scores available for %s", effectiveSLMModel)
				if scoringSummary.SkipReason != "" {
					status.Message = fmt.Sprintf("Using heuristic ranking with %s: %s", effectiveSLMModel, scoringSummary.SkipReason)
				}
				return
			}
			status.Message = fmt.Sprintf("Applied cached %s scores: %d/%d", effectiveSLMModel, scoringSummary.SuccessJobs, scoringSummary.ScheduledJobs)
		})
		if scoringSummary.CacheMissJobs > 0 {
			go precomputeSLMScoresFromState(app.StatePath, "jobs-feed-cache-miss", slmOptions)
		}
	} else {
		updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
			status.ProgressPercent = 80
			status.Scoring = JobsFeedScoringProgress{Running: false}
			status.Message = "Scoring skipped (noise view enabled)"
		})
	}

	updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
		status.Phase = "sorting"
		status.Message = "Sorting jobs feed"
		status.ProgressPercent = 84
	})

	sortDashboardJobs(filtered, sortBy)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
		status.Phase = "summarizing"
		status.Message = "Preparing summary"
		status.ProgressPercent = 92
		status.FilteredJobs = len(filtered)
	})

	everifyEnrolled := 0
	everifyUnknown := 0
	everifyNotFound := 0
	everifyNotEnrolled := 0
	postedDatedJobs := 0
	for _, job := range filtered {
		if jobHasParsedPostedDate(job) {
			postedDatedJobs++
		}
		switch normalizeEVerifyStatus(job.EVerifyStatus) {
		case "enrolled":
			everifyEnrolled++
		case "not_found":
			everifyNotFound++
		case "not_enrolled":
			everifyNotEnrolled++
		default:
			everifyUnknown++
		}
	}
	latest := ""
	latestAt := time.Time{}
	for _, job := range filtered {
		seenAt := parseISOTime(job.FirstSeen)
		if seenAt.IsZero() {
			continue
		}
		if latestAt.IsZero() || seenAt.After(latestAt) {
			latestAt = seenAt
			latest = job.FirstSeen
		}
	}

	response := DashboardJobsResponse{
		Summary: DashboardJobsSummary{
			GeneratedAt:          utcNow(),
			TotalJobs:            totalJobs,
			FilteredJobs:         len(filtered),
			CompaniesCount:       len(companies),
			LatestFirstSeen:      latest,
			LatestFirstSeenLocal: isoToLocal(latest),
			PostedDatedJobs:      postedDatedJobs,
			MissingPostedDate:    max(0, len(filtered)-postedDatedJobs),
			EVerifyEnrolled:      everifyEnrolled,
			EVerifyUnknown:       everifyUnknown,
			EVerifyNotFound:      everifyNotFound,
			EVerifyNotEnrolled:   everifyNotEnrolled,
		},
		Filters: DashboardJobsFilters{
			Query:           query,
			Company:         company,
			Source:          source,
			SLMModel:        effectiveSLMModel,
			SLMModelOptions: slmScoringCompareModels(),
			Sort:            sortBy,
			Limit:           limit,
			PostedWithin:    postedWithin,
			EVerify:         everifyFilter,
			Companies:       companies,
			SourceOptions:   sources,
			PostedOptions:   postedWithinOptions(),
			EVerifyOptions:  eVerifyFilterOptions(),
		},
		Jobs: filtered,
	}

	updateJobsFeedProgress(runID, func(status *JobsFeedProgress) {
		status.Running = false
		status.Phase = "done"
		status.Message = fmt.Sprintf("Jobs feed ready: %d/%d shown", len(filtered), totalJobs)
		status.ProgressPercent = 100
		status.TotalJobs = totalJobs
		status.FilteredJobs = len(filtered)
		status.Scoring.Running = false
		status.FinishedAt = utcNow()
	})

	return response
}

func filterJobsForFeed(allJobs []DashboardJob, query string, company string, source string, everifyFilter string, postedWithin string, includeNoise bool) []DashboardJob {
	filtered := make([]DashboardJob, 0, len(allJobs))
	now := time.Now()
	selectedCompanyKey := companyFilterKey(company)
	selectedSource := normalizeSourceFilter(source)
	for _, job := range allJobs {
		sourceJob := dashboardAsSourceJob(job)
		artifacts := jobsFeedHeuristicArtifactsForDashboard(job)
		liveRoleDecision, liveRoleReasons := deterministicRoleDecision(sourceJob)
		artifacts.RoleDecision = normalizeRoleDecision(liveRoleDecision)
		artifacts.RoleDecisionReasons = append([]string(nil), liveRoleReasons...)
		artifacts.NeedsRoleSLM = liveRoleDecision == roleDecisionAmbiguous && slmScoringEnabled()
		if artifacts.RoleDecision == roleDecisionOut {
			artifacts.RelevantForFeed = false
			artifacts.RecommendedResume = ""
			if artifacts.MatchScore > 35 {
				artifacts.MatchScore = 35
			}
		}
		job.HeuristicCached = true
		job.HeuristicContextHash = artifacts.ContextHash
		job.FeedRelevantCached = artifacts.RelevantForFeed
		job.DeterministicRoleDecision = normalizeRoleDecision(artifacts.RoleDecision)
		job.DeterministicRoleReasons = append([]string(nil), artifacts.RoleDecisionReasons...)
		job.DeterministicInternshipStatus = normalizeSLMInternshipStatus(artifacts.InternshipStatus)
		job.NeedsRoleSLM = artifacts.NeedsRoleSLM
		job.NeedsInternshipSLM = artifacts.NeedsInternshipSLM
		job.RoleDecision = normalizeRoleDecision(artifacts.RoleDecision)
		job.InternshipDecision = normalizeSLMInternshipStatus(artifacts.InternshipStatus)
		job.DecisionSource = "deterministic"
		eVerifyRecord := eVerifyRecordForCompany(job.Company)
		job.EVerifyStatus = normalizeEVerifyStatus(eVerifyRecord.Status)
		job.EVerifySource = strings.TrimSpace(eVerifyRecord.Source)
		job.EVerifyChecked = strings.TrimSpace(eVerifyRecord.CheckedAt)
		job.EVerifyNote = strings.TrimSpace(eVerifyRecord.Note)
		if job.EVerifyStatus == "unknown" && jobHasEVerifyMention(job) {
			job.EVerifyStatus = "enrolled"
			job.EVerifySource = "job_posting_signal"
			job.EVerifyNote = "E-Verify participation is mentioned in this posting."
		}
		if everifyFilter != "" && job.EVerifyStatus != everifyFilter {
			continue
		}
		if !matchesPostedWithinFilter(job, postedWithin, now) {
			continue
		}
		if selectedSource != "" && normalizeSourceFilter(job.Source) != selectedSource {
			continue
		}
		if !includeNoise {
			if !dashboardHasRoleKeyword(job.Title) && job.PostedAt == "" && len(strings.Fields(strings.TrimSpace(job.Description))) < 18 {
				continue
			}
			if !artifacts.RelevantForFeed {
				continue
			}
			score := jobSignalScore(job)
			if looksLikeNoiseDashboardJob(job) || score < 2 {
				continue
			}
		}
		if strings.TrimSpace(company) != "" {
			if selectedCompanyKey != "" {
				if companyFilterKey(job.Company) != selectedCompanyKey {
					continue
				}
			} else if !strings.EqualFold(company, job.Company) {
				continue
			}
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				job.Company,
				job.Title,
				job.URL,
				job.Location,
				job.Team,
				job.Source,
				job.Description,
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		workAuthStatus := artifacts.WorkAuthStatus
		workAuthNotes := append([]string(nil), artifacts.WorkAuthNotes...)
		if !includeNoise && parseBoolEnv("F1_OPT_EXCLUDE_BLOCKED", false) && workAuthStatus == "blocked" {
			continue
		}
		job.WorkAuthStatus = workAuthStatus
		job.WorkAuthNotes = workAuthNotes
		job.RecommendedResume = artifacts.RecommendedResume
		matchScore := artifacts.MatchScore
		matchReasons := append([]string(nil), artifacts.MatchReasons...)
		if job.EVerifyStatus == "enrolled" && workAuthStatus != "blocked" {
			matchScore = clampScore(matchScore + 3)
			reasonSet := map[string]struct{}{}
			for _, reason := range matchReasons {
				key := strings.ToLower(strings.TrimSpace(reason))
				if key == "" {
					continue
				}
				reasonSet[key] = struct{}{}
			}
			matchReasons = appendUniqueReason(matchReasons, reasonSet, "E-Verify employer signal")
			if len(matchReasons) > 4 {
				matchReasons = matchReasons[:4]
			}
		}
		job.MatchScore = matchScore
		job.MatchReasons = matchReasons
		filtered = append(filtered, job)
	}
	return filtered
}

func precomputeSLMScoresFromState(statePath string, trigger string, options ...slmScoringOptions) {
	if !slmScoringEnabled() || !slmScoringPrecomputeOnRun() {
		return
	}
	setSLMScoringCacheStatePath(statePath)
	slmOptions := resolveSLMScoringOptions(options...)
	effectiveSLMModel := slmOptions.effectiveModelLabel()

	startedAt := time.Now()
	startedAtISO := utcNow()
	slmPrecomputeState.mu.Lock()
	if slmPrecomputeState.status.Running {
		slmPrecomputeState.mu.Unlock()
		if slmScoringDebugEnabled() {
			log.Printf("[slm-scoring] precompute skipped trigger=%s reason=already-running", trigger)
		}
		return
	}
	status := &slmPrecomputeState.status
	status.Running = true
	status.Trigger = trigger
	status.Model = effectiveSLMModel
	status.Phase = "running"
	status.EligibleJobs = 0
	status.ScheduledJobs = 0
	status.QueuedJobs = 0
	status.CompletedJobs = 0
	status.SuccessJobs = 0
	status.FailedJobs = 0
	status.StartedAt = startedAtISO
	status.UpdatedAt = startedAtISO
	status.FinishedAt = ""
	status.DurationMs = 0
	status.LastError = ""
	slmPrecomputeState.mu.Unlock()

	var completedWithError string
	defer func() {
		finishedAt := utcNow()
		duration := time.Since(startedAt).Milliseconds()
		updateSLMScoringStatus(func(status *SLMScoringStatus) {
			status.Running = false
			status.Phase = "done"
			status.QueuedJobs = max(0, status.ScheduledJobs-status.CompletedJobs)
			status.FinishedAt = finishedAt
			status.UpdatedAt = finishedAt
			status.DurationMs = duration
			if completedWithError != "" {
				status.LastError = completedWithError
			}
		})
	}()

	stateValue := readJSONFile(statePath, map[string]any{"seen": map[string]any{}})
	state := asMap(stateValue)
	seen := asMap(state["seen"])
	allJobs := collectJobsFromSeen(seen, true)
	triggerEVerifyRefreshFromJobs(statePath, allJobs, "slm-precompute")
	filtered := filterJobsForFeed(allJobs, "", "", "", "", "", false)
	updateSLMScoringStatus(func(status *SLMScoringStatus) {
		status.EligibleJobs = len(filtered)
		status.UpdatedAt = utcNow()
	})
	if len(filtered) == 0 {
		if slmScoringDebugEnabled() {
			log.Printf("[slm-scoring] precompute trigger=%s no eligible jobs", trigger)
		}
		return
	}

	summary := applySLMMatchScoresWithProgress(filtered, "best_match", len(filtered), func(progress slmApplyProgress) {
		updateSLMScoringStatus(func(status *SLMScoringStatus) {
			status.EligibleJobs = progress.EligibleJobs
			status.ScheduledJobs = progress.ScheduledJobs
			status.QueuedJobs = progress.QueuedJobs
			status.CompletedJobs = progress.CompletedJobs
			status.SuccessJobs = progress.SuccessJobs
			status.FailedJobs = progress.FailedJobs
			status.UpdatedAt = utcNow()
		})
	}, slmOptions)
	updateSLMScoringStatus(func(status *SLMScoringStatus) {
		status.EligibleJobs = summary.EligibleJobs
		status.ScheduledJobs = summary.ScheduledJobs
		status.CompletedJobs = summary.CompletedJobs
		status.SuccessJobs = summary.SuccessJobs
		status.FailedJobs = summary.FailedJobs
		status.QueuedJobs = max(0, summary.ScheduledJobs-summary.CompletedJobs)
		status.UpdatedAt = utcNow()
	})
	if summary.Skipped {
		completedWithError = strings.TrimSpace(summary.SkipReason)
	}

	log.Printf(
		"[slm-scoring] precompute complete trigger=%s model=%s eligible=%d scheduled=%d completed=%d success=%d failed=%d duration=%s",
		trigger,
		effectiveSLMModel,
		summary.EligibleJobs,
		summary.ScheduledJobs,
		summary.CompletedJobs,
		summary.SuccessJobs,
		summary.FailedJobs,
		time.Since(startedAt),
	)
}

func RunDashboard(host string, port int, cfg MonitorRunnerConfig) error {
	if err := LoadDotenv(cfg.DotenvPath); err != nil {
		return fmt.Errorf("load dotenv failed: %w", err)
	}
	setSLMScoringCacheStatePath(cfg.StatePath)
	if err := EnsureJobsDBBootstrapFromState(cfg.StatePath); err != nil {
		log.Printf("Jobs DB bootstrap warning: %v", err)
	}
	runner := NewMonitorRunner(cfg)
	mailRunner := NewMailSyncRunner(MailServiceConfig{StatePath: cfg.StatePath, ConfigPath: cfg.ConfigPath})
	coordinator := NewBackgroundTaskCoordinator()
	runner.SetCoordinator(coordinator)
	mailRunner.SetCoordinator(coordinator)
	coordinator.SetLaunchers(
		func(dryRun bool) {
			if !runner.startRun(dryRun) {
				coordinator.Finish(backgroundTaskCrawl)
			}
		},
		func() {
			if !mailRunner.startRun() {
				coordinator.Finish(backgroundTaskMail)
			}
		},
	)
	scheduler := NewCrawlScheduler(runner, cfg.StatePath, cfg.SchedulePath)
	mailScheduler := NewMailScheduler(mailRunner, cfg.StatePath)
	app := &DashboardApp{
		CWD:           cfg.CWD,
		StatePath:     cfg.StatePath,
		ConfigPath:    cfg.ConfigPath,
		ReportPath:    cfg.ReportPath,
		Runner:        runner,
		Scheduler:     scheduler,
		MailRunner:    mailRunner,
		MailScheduler: mailScheduler,
	}
	frontendBase := frontendBaseURL()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveFrontendOrEmbeddedHTML(w, r, frontendBase, "/monitor", embeddedDashboardHTML)
	})
	mux.HandleFunc("/monitor", func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.URL.Path != "/monitor" {
			http.NotFound(w, r)
			return
		}
		serveFrontendOrEmbeddedHTML(w, r, frontendBase, "/monitor", embeddedDashboardHTML)
	})
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.URL.Path != "/jobs" {
			http.NotFound(w, r)
			return
		}
		serveFrontendOrEmbeddedHTML(w, r, frontendBase, "/jobs", embeddedJobsHTML)
	})
	mux.HandleFunc("/mail", func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.URL.Path != "/mail" {
			http.NotFound(w, r)
			return
		}
		serveFrontendOrEmbeddedHTML(w, r, frontendBase, "/mail", embeddedMailHTML)
	})
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, app.BuildOverview(), http.StatusOK)
	})
	mux.HandleFunc("/api/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		query := r.URL.Query().Get("q")
		company := r.URL.Query().Get("company")
		source := r.URL.Query().Get("source")
		everifyFilter := r.URL.Query().Get("everify")
		postedWithin := r.URL.Query().Get("posted_within")
		slmModel := r.URL.Query().Get("slm_model")
		sortBy := r.URL.Query().Get("sort")
		includeNoise := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_noise")), "1") || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_noise")), "true")
		includeInactive := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_inactive")), "1") || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_inactive")), "true")
		limit := 500
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		sendJSON(w, app.BuildJobsFeed(query, company, source, everifyFilter, postedWithin, sortBy, limit, includeNoise, includeInactive, slmModel), http.StatusOK)
	})
	mux.HandleFunc("/api/mail/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := loadMailOverview(app.StatePath)
		if err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, http.StatusInternalServerError)
			return
		}
		sendJSON(w, payload, http.StatusOK)
	})
	mux.HandleFunc("/api/mail/analytics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provider := strings.TrimSpace(r.URL.Query().Get("provider"))
		if provider != "" && normalizeMailProvider(provider) != string(MailProviderGmail) {
			sendJSON(w, map[string]any{"ok": false, "message": "unsupported provider"}, http.StatusBadRequest)
			return
		}
		accountID := int64(0)
		if raw := strings.TrimSpace(r.URL.Query().Get("account_id")); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				accountID = parsed
			}
		}
		payload, err := loadMailAnalyticsWithConfig(MailServiceConfig{StatePath: app.StatePath, ConfigPath: app.ConfigPath}, MailMessageFilters{
			AccountID: accountID,
			Provider:  provider,
			Company:   r.URL.Query().Get("company"),
		})
		if err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, http.StatusInternalServerError)
			return
		}
		sendJSON(w, payload, http.StatusOK)
	})
	mux.HandleFunc("/api/mail/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provider := strings.TrimSpace(r.URL.Query().Get("provider"))
		if provider != "" && normalizeMailProvider(provider) != string(MailProviderGmail) {
			sendJSON(w, map[string]any{"ok": false, "message": "unsupported provider"}, http.StatusBadRequest)
			return
		}
		accountID := int64(0)
		if raw := strings.TrimSpace(r.URL.Query().Get("account_id")); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				accountID = parsed
			}
		}
		limit := 200
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		payload, err := loadMailMessagesWithConfig(MailServiceConfig{StatePath: app.StatePath, ConfigPath: app.ConfigPath}, MailMessageFilters{
			AccountID:     accountID,
			Provider:      provider,
			EventType:     r.URL.Query().Get("event_type"),
			TriageStatus:  r.URL.Query().Get("triage_status"),
			Company:       r.URL.Query().Get("company"),
			UnreadOnly:    strings.EqualFold(r.URL.Query().Get("unread_only"), "1") || strings.EqualFold(r.URL.Query().Get("unread_only"), "true"),
			ImportantOnly: strings.EqualFold(r.URL.Query().Get("important_only"), "1") || strings.EqualFold(r.URL.Query().Get("important_only"), "true"),
			Limit:         limit,
		})
		if err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, http.StatusInternalServerError)
			return
		}
		sendJSON(w, payload, http.StatusOK)
	})
	mux.HandleFunc("/api/mail/messages/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/mail/messages/")
		path = strings.Trim(path, "/")
		if path == "" {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(path, "/triage") {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			idText := strings.TrimSuffix(path, "/triage")
			idText = strings.Trim(idText, "/")
			id, err := strconv.ParseInt(idText, 10, 64)
			if err != nil {
				sendJSON(w, map[string]any{"ok": false, "message": "Invalid message id"}, http.StatusBadRequest)
				return
			}
			var payload struct {
				TriageStatus string `json:"triage_status"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				sendJSON(w, map[string]any{"ok": false, "message": "Invalid JSON payload"}, http.StatusBadRequest)
				return
			}
			update, err := updateMailMessageTriage(app.StatePath, id, payload.TriageStatus)
			if err != nil {
				status := http.StatusInternalServerError
				if err == sql.ErrNoRows {
					status = http.StatusNotFound
				}
				sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, status)
				return
			}
			sendJSON(w, update, http.StatusOK)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.ParseInt(path, 10, 64)
		if err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": "Invalid message id"}, http.StatusBadRequest)
			return
		}
		message, err := loadMailMessageDetailWithConfig(MailServiceConfig{StatePath: app.StatePath, ConfigPath: app.ConfigPath}, id)
		if err != nil {
			status := http.StatusInternalServerError
			if err == sql.ErrNoRows {
				status = http.StatusNotFound
			}
			sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, status)
			return
		}
		sendJSON(w, MailMessageDetailResponse{Ok: true, Message: message}, http.StatusOK)
	})
	mux.HandleFunc("/api/mail/accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		accounts, err := loadMailAccounts(app.StatePath)
		if err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, http.StatusInternalServerError)
			return
		}
		sendJSON(w, MailAccountsResponse{GeneratedAt: utcNow(), Accounts: accounts}, http.StatusOK)
	})
	mux.HandleFunc("/api/mail/accounts/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/mail/accounts/")
		trimmed = strings.Trim(trimmed, "/")
		if strings.HasSuffix(trimmed, "/disconnect") {
			provider := strings.TrimSuffix(trimmed, "/disconnect")
			provider = strings.Trim(provider, "/")
			account, err := disconnectMailAccount(app.StatePath, provider)
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, sql.ErrNoRows) {
					status = http.StatusNotFound
				}
				sendJSON(w, MailAccountActionResponse{Ok: false, Message: err.Error()}, status)
				return
			}
			sendJSON(w, MailAccountActionResponse{Ok: true, Account: account, Message: provider + " disconnected"}, http.StatusOK)
			return
		}
		if !strings.HasSuffix(trimmed, "/connect/start") {
			http.NotFound(w, r)
			return
		}
		provider := strings.TrimSuffix(trimmed, "/connect/start")
		provider = strings.Trim(provider, "/")
		payload, err := startMailConnect(app.StatePath, provider, mailRequestBaseURL(r))
		if err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, http.StatusBadRequest)
			return
		}
		sendJSON(w, payload, http.StatusOK)
	})
	mux.HandleFunc("/api/mail/auth/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/mail/auth/")
		parts := strings.Split(strings.Trim(trimmed, "/"), "/")
		if len(parts) != 2 || parts[1] != "callback" {
			http.NotFound(w, r)
			return
		}
		account, err := handleMailOAuthCallback(app.StatePath, parts[0], r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h1>Mail connect failed</h1><p>" + htmlstd.EscapeString(err.Error()) + "</p></body></html>"))
			return
		}
		_, _ = w.Write([]byte("<html><body><h1>Mailbox connected</h1><p>" + htmlstd.EscapeString(account.Provider) + " connected for " + htmlstd.EscapeString(account.Email) + ".</p><p>You can close this tab and refresh the dashboard.</p></body></html>"))
	})
	mux.HandleFunc("/api/mail/corpus/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Provider    string `json:"provider"`
			Days        int    `json:"days"`
			MaxMessages int    `json:"max_messages"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
				sendJSON(w, MailCorpusExportResponse{Ok: false, Message: "Invalid JSON payload"}, http.StatusBadRequest)
				return
			}
		}
		provider := strings.TrimSpace(payload.Provider)
		if provider == "" {
			provider = string(MailProviderGmail)
		}
		response, err := exportMailCorpus(app.StatePath, provider, payload.Days, payload.MaxMessages)
		if err != nil {
			sendJSON(w, MailCorpusExportResponse{Ok: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		sendJSON(w, response, http.StatusOK)
	})
	mux.HandleFunc("/api/mail/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		result := app.MailRunner.Trigger()
		status := http.StatusOK
		if !result.Accepted {
			status = http.StatusConflict
		}
		sendJSON(w, map[string]any{"ok": result.Accepted, "queued": result.Queued, "started": result.Started, "message": result.Message}, status)
	})
	mux.HandleFunc("/api/mail/run-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, app.MailRunner.Snapshot(), http.StatusOK)
	})
	mux.HandleFunc("/api/mail-schedule", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sendJSON(w, app.MailScheduler.Snapshot(), http.StatusOK)
			return
		case http.MethodPost:
			var payload struct {
				Enabled         *bool `json:"enabled"`
				IntervalMinutes int   `json:"interval_minutes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				sendJSON(w, map[string]any{"ok": false, "message": "Invalid JSON payload"}, http.StatusBadRequest)
				return
			}
			current := app.MailScheduler.Snapshot()
			nextEnabled := current.Enabled
			if payload.Enabled != nil {
				nextEnabled = *payload.Enabled
			}
			nextInterval := current.IntervalMinutes
			if payload.IntervalMinutes > 0 {
				nextInterval = payload.IntervalMinutes
			}
			next, err := app.MailScheduler.Update(nextEnabled, nextInterval)
			if err != nil {
				sendJSON(w, map[string]any{"ok": false, "message": err.Error(), "schedule": next}, http.StatusInternalServerError)
				return
			}
			sendJSON(w, next, http.StatusOK)
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})
	mux.HandleFunc("/api/jobs/application-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Fingerprint string `json:"fingerprint"`
			Status      string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": "Invalid JSON payload"}, http.StatusBadRequest)
			return
		}
		update, err := UpdateJobApplicationStatus(app.StatePath, payload.Fingerprint, payload.Status)
		if err != nil {
			status := http.StatusInternalServerError
			switch strings.TrimSpace(err.Error()) {
			case "missing fingerprint":
				status = http.StatusBadRequest
			case "job not found":
				status = http.StatusNotFound
			case "jobs db disabled":
				status = http.StatusServiceUnavailable
			}
			sendJSON(w, map[string]any{"ok": false, "message": err.Error()}, status)
			return
		}
		sendJSON(w, update, http.StatusOK)
	})
	mux.HandleFunc("/api/jobs-progress", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, jobsFeedProgressSnapshot(), http.StatusOK)
	})
	mux.HandleFunc("/api/run-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, app.Runner.Snapshot(), http.StatusOK)
	})
	mux.HandleFunc("/api/crawl-schedule", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sendJSON(w, app.Scheduler.Snapshot(), http.StatusOK)
			return
		case http.MethodPost:
			var payload struct {
				Enabled         *bool `json:"enabled"`
				IntervalMinutes int   `json:"interval_minutes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				sendJSON(w, map[string]any{"ok": false, "message": "Invalid JSON payload"}, http.StatusBadRequest)
				return
			}
			current := app.Scheduler.Snapshot()
			nextEnabled := current.Enabled
			if payload.Enabled != nil {
				nextEnabled = *payload.Enabled
			}
			nextInterval := current.IntervalMinutes
			if payload.IntervalMinutes > 0 {
				nextInterval = payload.IntervalMinutes
			}
			next, err := app.Scheduler.Update(nextEnabled, nextInterval)
			if err != nil {
				sendJSON(w, map[string]any{"ok": false, "message": err.Error(), "schedule": next}, http.StatusInternalServerError)
				return
			}
			sendJSON(w, next, http.StatusOK)
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, map[string]any{"ok": true, "time": utcNow()}, http.StatusOK)
	})
	mux.HandleFunc("/api/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			DryRun bool `json:"dry_run"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			sendJSON(w, map[string]any{"ok": false, "message": "Invalid JSON payload"}, http.StatusBadRequest)
			return
		}
		result := app.Runner.Trigger(payload.DryRun)
		status := http.StatusOK
		if !result.Accepted {
			status = http.StatusConflict
		}
		sendJSON(w, map[string]any{"ok": result.Accepted, "queued": result.Queued, "started": result.Started, "message": result.Message}, status)
	})

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("API server running at http://%s (UI uses %s when reachable, embedded fallback otherwise)", addr, frontendBase)
	return http.ListenAndServe(addr, mux)
}

func sendJSON(w http.ResponseWriter, payload any, status int) {
	body, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
