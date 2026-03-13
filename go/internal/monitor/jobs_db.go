package monitor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func jobsDBEnabled() bool {
	return parseBoolEnv("JOBS_DB_ENABLED", true)
}

func jobsDBPath(statePath string) string {
	if configured := strings.TrimSpace(os.Getenv("JOBS_DB_PATH")); configured != "" {
		return configured
	}
	root := strings.TrimSpace(filepath.Dir(strings.TrimSpace(statePath)))
	if root == "" || root == "." {
		return ".state/jobs.db"
	}
	return filepath.Join(root, "jobs.db")
}

func jobsDBObservationRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("JOBS_DB_OBSERVATION_RETENTION_DAYS"))
	if raw == "" {
		return 4
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		return 4
	}
	return parsed
}

func jobsDBRunRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("JOBS_DB_RUN_RETENTION_DAYS"))
	if raw == "" {
		return 4
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		return 4
	}
	return parsed
}

func jobsDBDailyStatsRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("JOBS_DB_DAILY_STATS_RETENTION_DAYS"))
	if raw == "" {
		return 365
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 7 {
		return 365
	}
	return parsed
}

func jobsDBInactiveRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("JOBS_DB_INACTIVE_RETENTION_DAYS"))
	if raw == "" {
		return 120
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 7 {
		return 120
	}
	return parsed
}

func jobsDBSLMScoreRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("JOBS_DB_SLM_SCORE_RETENTION_DAYS"))
	if raw == "" {
		return 90
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 7 {
		return 90
	}
	return parsed
}

func normalizePostedAtForDB(value string) string {
	if posted, ok := parsePostedTime(value); ok {
		return posted.UTC().Format(time.RFC3339)
	}
	return ""
}

func normalizeJobApplicationStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "clear", "none", "unknown":
		return ""
	case "applied":
		return "applied"
	case "not_applied", "not-applied", "not applied":
		return "not_applied"
	default:
		return ""
	}
}

func ensureJobsDBColumn(db *sql.DB, table string, column string, definition string) error {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(name), column) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, definition))
	return err
}

func openJobsDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	pragmaStatements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA temp_store=MEMORY`,
		`PRAGMA foreign_keys=ON`,
	}
	for _, statement := range pragmaStatements {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := ensureJobsDBSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureJobsDBSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS jobs (
			fingerprint TEXT PRIMARY KEY,
			company TEXT NOT NULL,
			title TEXT,
			url TEXT,
			source TEXT,
			external_id TEXT,
			location TEXT,
			team TEXT,
			posted_at TEXT,
			posted_at_ts TEXT,
			description TEXT,
			heuristic_context_hash TEXT NOT NULL DEFAULT '',
			feed_relevance_ok INTEGER NOT NULL DEFAULT 0,
			deterministic_role_decision TEXT NOT NULL DEFAULT 'ambiguous',
			deterministic_role_reasons_json TEXT NOT NULL DEFAULT '[]',
			deterministic_internship_status TEXT NOT NULL DEFAULT 'not_applicable',
			needs_role_slm INTEGER NOT NULL DEFAULT 0,
			needs_internship_slm INTEGER NOT NULL DEFAULT 0,
			heuristic_match_score INTEGER NOT NULL DEFAULT 0,
			heuristic_match_reasons_json TEXT NOT NULL DEFAULT '[]',
			heuristic_recommended_resume TEXT,
			work_auth_status TEXT NOT NULL DEFAULT 'unknown',
			work_auth_notes_json TEXT NOT NULL DEFAULT '[]',
			application_status TEXT NOT NULL DEFAULT '',
			application_updated_at TEXT,
			first_seen TEXT,
			last_seen TEXT,
			active INTEGER NOT NULL DEFAULT 1
			)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_company_active ON jobs(company, active)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_active_source ON jobs(active, source)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_active_sort ON jobs(active, posted_at_ts DESC, first_seen DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_company_title_ci ON jobs(lower(company), lower(title))`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_posted_at_ts ON jobs(posted_at_ts)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_last_seen ON jobs(last_seen)`,
		`CREATE TABLE IF NOT EXISTS job_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint TEXT NOT NULL,
			observed_at TEXT NOT NULL,
			run_id TEXT NOT NULL,
			company TEXT NOT NULL,
			status TEXT NOT NULL,
			source TEXT,
			posted_at TEXT,
			posted_at_ts TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_job_observations_observed_at ON job_observations(observed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_job_observations_fingerprint ON job_observations(fingerprint)`,
		`CREATE TRIGGER IF NOT EXISTS trg_job_observations_touch_job
			AFTER INSERT ON job_observations
			BEGIN
				UPDATE jobs
				SET
					last_seen = CASE
						WHEN COALESCE(last_seen, '') = '' THEN NEW.observed_at
						WHEN last_seen < NEW.observed_at THEN NEW.observed_at
						ELSE last_seen
					END,
					active = 1
				WHERE fingerprint = NEW.fingerprint;
			END`,
		`CREATE TRIGGER IF NOT EXISTS trg_jobs_delete_observations
			AFTER DELETE ON jobs
			BEGIN
				DELETE FROM job_observations WHERE fingerprint = OLD.fingerprint;
			END`,
		`CREATE TABLE IF NOT EXISTS company_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			company TEXT NOT NULL,
			outcome_status TEXT NOT NULL,
			selected_source TEXT,
			attempted_sources TEXT,
			jobs_found INTEGER NOT NULL DEFAULT 0,
			message TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_company_runs_created_at ON company_runs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_company_runs_run_id ON company_runs(run_id)`,
		`CREATE TABLE IF NOT EXISTS daily_job_stats (
			day TEXT NOT NULL,
			company TEXT NOT NULL,
			active_jobs INTEGER NOT NULL,
			new_jobs INTEGER NOT NULL,
			seen_jobs INTEGER NOT NULL,
			blocked_events INTEGER NOT NULL,
			with_posted_date INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(day, company)
		)`,
		`CREATE TABLE IF NOT EXISTS slm_scores (
			cache_key TEXT PRIMARY KEY,
			model TEXT NOT NULL,
			provider TEXT NOT NULL,
			prompt_hash TEXT NOT NULL,
			job_fingerprint TEXT NOT NULL,
			role_fit INTEGER NOT NULL,
			work_auth_status TEXT NOT NULL,
			internship_status TEXT NOT NULL DEFAULT 'not_applicable',
			match_score INTEGER NOT NULL,
			reasons_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_used_at TEXT NOT NULL,
			use_count INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_slm_scores_last_used ON slm_scores(last_used_at)`,
		`CREATE INDEX IF NOT EXISTS idx_slm_scores_fingerprint ON slm_scores(job_fingerprint)`,
		`CREATE VIEW IF NOT EXISTS v_jobs_company_rollup AS
			SELECT
				company,
				COUNT(1) AS seen_jobs,
				SUM(CASE WHEN active = 1 THEN 1 ELSE 0 END) AS active_jobs,
				SUM(CASE WHEN COALESCE(posted_at_ts, '') <> '' THEN 1 ELSE 0 END) AS with_posted_date
			FROM jobs
			GROUP BY company`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureJobsDBColumn(db, "jobs", "application_status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "application_updated_at", "TEXT"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_sync_at", "TEXT"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_source", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_outcome", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_trace_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_manual_session_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_auto_submit_eligible", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_review_pending_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "assistant_last_confirmation_detected", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "heuristic_context_hash", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "feed_relevance_ok", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "deterministic_role_decision", "TEXT NOT NULL DEFAULT 'ambiguous'"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "deterministic_role_reasons_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "deterministic_internship_status", "TEXT NOT NULL DEFAULT 'not_applicable'"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "needs_role_slm", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "needs_internship_slm", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "heuristic_match_score", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "heuristic_match_reasons_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "heuristic_recommended_resume", "TEXT"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "work_auth_status", "TEXT NOT NULL DEFAULT 'unknown'"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "jobs", "work_auth_notes_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := ensureJobsDBColumn(db, "slm_scores", "internship_status", "TEXT NOT NULL DEFAULT 'not_applicable'"); err != nil {
		return err
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nonEmpty(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func collectCompanyNamesFromState(state MonitorState) []string {
	seen := map[string]struct{}{}
	for company := range state.Seen {
		seen[company] = struct{}{}
	}
	for company := range state.CompanyState {
		seen[company] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for company := range seen {
		names = append(names, company)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

type jobsDBFeedSnapshot struct {
	TotalJobs int
	Companies []string
	Sources   []string
	Jobs      []DashboardJob
}

type jobsDBCompanyRollup struct {
	SeenJobs       int
	ActiveJobs     int
	WithPostedDate int
}

func loadJobsCompanyRollupsFromDB(statePath string) (map[string]jobsDBCompanyRollup, error) {
	rollups := map[string]jobsDBCompanyRollup{}
	if !jobsDBEnabled() {
		return rollups, fmt.Errorf("jobs db disabled")
	}
	dbPath := jobsDBPath(statePath)
	db, err := openJobsDB(dbPath)
	if err != nil {
		return rollups, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT company, seen_jobs, active_jobs, with_posted_date FROM v_jobs_company_rollup`)
	if err != nil {
		return rollups, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			company        sql.NullString
			seenJobs       int
			activeJobs     int
			withPostedDate int
		)
		if err := rows.Scan(&company, &seenJobs, &activeJobs, &withPostedDate); err != nil {
			return rollups, err
		}
		name := strings.TrimSpace(company.String)
		if name == "" {
			continue
		}
		rollups[name] = jobsDBCompanyRollup{
			SeenJobs:       seenJobs,
			ActiveJobs:     activeJobs,
			WithPostedDate: withPostedDate,
		}
	}
	if err := rows.Err(); err != nil {
		return rollups, err
	}
	return rollups, nil
}

func jobsDBOrderClause(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "oldest":
		return `CASE WHEN COALESCE(NULLIF(posted_at_ts, ''), '') = '' THEN 1 ELSE 0 END ASC, NULLIF(posted_at_ts, '') ASC, first_seen ASC, lower(company) ASC, lower(title) ASC`
	case "company":
		return `lower(company) ASC, lower(title) ASC`
	case "title":
		return `lower(title) ASC, lower(company) ASC`
	default:
		return `CASE WHEN COALESCE(NULLIF(posted_at_ts, ''), '') = '' THEN 1 ELSE 0 END ASC, NULLIF(posted_at_ts, '') DESC, first_seen DESC, lower(company) ASC, lower(title) ASC`
	}
}

func jobsDBCandidateLimit(limit int, includeNoise bool, sortBy string, query string, company string) int {
	candidate := limit
	if candidate <= 0 {
		candidate = 500
	}
	if candidate > 5000 {
		candidate = 5000
	}
	if includeNoise {
		return candidate
	}
	if strings.TrimSpace(query) != "" || strings.TrimSpace(company) != "" {
		return 5000
	}
	scaled := candidate * 6
	if strings.EqualFold(strings.TrimSpace(sortBy), "best_match") {
		scaled = candidate * 8
	}
	if scaled < 600 {
		scaled = 600
	}
	if scaled > 5000 {
		scaled = 5000
	}
	return scaled
}

func appendJobsDBQueryFilters(where []string, args []any, source string, postedWithin string) ([]string, []any) {
	source = normalizeSourceFilter(source)
	if source != "" {
		where = append(where, `source = ?`)
		args = append(args, source)
	}

	postedWithin = normalizePostedWithinFilter(postedWithin)

	if postedWithin != "" {
		now := time.Now()
		var cutoff time.Time
		switch postedWithin {
		case "24h":
			cutoff = now.Add(-24 * time.Hour)
		case "48h":
			cutoff = now.Add(-48 * time.Hour)
		case "7d":
			cutoff = now.Add(-7 * 24 * time.Hour)
		}
		if !cutoff.IsZero() {
			where = append(where, `posted_at_ts <> '' AND posted_at_ts >= ?`)
			args = append(args, cutoff.UTC().Format(time.RFC3339))
		}
	}
	return where, args
}

func loadJobsFeedFromDB(statePath string, query string, company string, source string, postedWithin string, sortBy string, limit int, includeNoise bool, includeInactive bool) (jobsDBFeedSnapshot, error) {
	snapshot := jobsDBFeedSnapshot{
		Companies: []string{},
		Sources:   []string{},
		Jobs:      []DashboardJob{},
	}
	if !jobsDBEnabled() {
		return snapshot, fmt.Errorf("jobs db disabled")
	}
	dbPath := jobsDBPath(statePath)
	db, err := openJobsDB(dbPath)
	if err != nil {
		return snapshot, err
	}
	defer db.Close()

	baseWhere := []string{}
	baseArgs := make([]any, 0)
	if !includeInactive {
		baseWhere = append(baseWhere, `active = 1`)
	}
	baseClause := "1=1"
	if len(baseWhere) > 0 {
		baseClause = strings.Join(baseWhere, " AND ")
	}
	if err := db.QueryRow(`SELECT COUNT(1) FROM jobs WHERE `+baseClause, baseArgs...).Scan(&snapshot.TotalJobs); err != nil {
		return snapshot, err
	}

	companyRows, err := db.Query(`SELECT company, team, url, source FROM jobs WHERE `+baseClause, baseArgs...)
	if err != nil {
		return snapshot, err
	}
	defer companyRows.Close()
	companyLabels := map[string]string{}
	sourceLabels := map[string]string{}
	for companyRows.Next() {
		var (
			companyValue sql.NullString
			teamValue    sql.NullString
			urlValue     sql.NullString
			sourceValue  sql.NullString
		)
		if err := companyRows.Scan(&companyValue, &teamValue, &urlValue, &sourceValue); err != nil {
			return snapshot, err
		}
		displayCompany := dashboardDisplayCompany(
			strings.TrimSpace(companyValue.String),
			strings.TrimSpace(teamValue.String),
			strings.TrimSpace(urlValue.String),
		)
		key := companyFilterKey(displayCompany)
		if key == "" {
			continue
		}
		companyLabels[key] = choosePreferredCompanyLabel(companyLabels[key], displayCompany)
		normalizedSource := normalizeSourceFilter(sourceValue.String)
		if normalizedSource != "" {
			sourceLabels[normalizedSource] = normalizedSource
		}
	}
	if err := companyRows.Err(); err != nil {
		return snapshot, err
	}
	snapshot.Companies = make([]string, 0, len(companyLabels))
	for _, label := range companyLabels {
		if label == "" {
			continue
		}
		snapshot.Companies = append(snapshot.Companies, label)
	}
	sort.Slice(snapshot.Companies, func(i, j int) bool {
		return strings.ToLower(snapshot.Companies[i]) < strings.ToLower(snapshot.Companies[j])
	})
	snapshot.Sources = make([]string, 0, len(sourceLabels))
	for _, label := range sourceLabels {
		if label == "" {
			continue
		}
		snapshot.Sources = append(snapshot.Sources, label)
	}
	sort.Slice(snapshot.Sources, func(i, j int) bool {
		return strings.ToLower(snapshot.Sources[i]) < strings.ToLower(snapshot.Sources[j])
	})

	candidateWhere := append([]string{}, baseWhere...)
	candidateArgs := append([]any{}, baseArgs...)
	candidateWhere, candidateArgs = appendJobsDBQueryFilters(candidateWhere, candidateArgs, source, postedWithin)
	candidateClause := "1=1"
	if len(candidateWhere) > 0 {
		candidateClause = strings.Join(candidateWhere, " AND ")
	}
	orderClause := jobsDBOrderClause(sortBy)
	candidateLimit := jobsDBCandidateLimit(limit, includeNoise, sortBy, query, company)

	querySQL := `
		SELECT fingerprint, company, title, url, source, location, team, posted_at, description,
		       heuristic_context_hash, feed_relevance_ok, deterministic_role_decision, deterministic_role_reasons_json,
		       deterministic_internship_status, needs_role_slm, needs_internship_slm,
		       heuristic_match_score, heuristic_match_reasons_json,
		       heuristic_recommended_resume, work_auth_status, work_auth_notes_json,
		       application_status, application_updated_at,
		       assistant_last_sync_at, assistant_last_source, assistant_last_outcome,
		       assistant_last_auto_submit_eligible, assistant_last_review_pending_count,
		       assistant_last_confirmation_detected,
		       first_seen, last_seen, active
		FROM jobs
		WHERE ` + candidateClause + `
		ORDER BY ` + orderClause + `
		LIMIT ?
	`
	candidateArgs = append(candidateArgs, candidateLimit)

	rows, err := db.Query(querySQL, candidateArgs...)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			fingerprintValue              sql.NullString
			companyValue                  sql.NullString
			titleValue                    sql.NullString
			urlValue                      sql.NullString
			sourceValue                   sql.NullString
			locationValue                 sql.NullString
			teamValue                     sql.NullString
			postedAtValue                 sql.NullString
			descriptionValue              sql.NullString
			heuristicContextValue         sql.NullString
			feedRelevanceValue            int
			deterministicRoleValue        sql.NullString
			deterministicRoleReasonsValue sql.NullString
			deterministicInternshipValue  sql.NullString
			needsRoleSLMValue             int
			needsInternshipSLMValue       int
			heuristicScoreValue           int
			heuristicReasonsValue         sql.NullString
			heuristicResumeValue          sql.NullString
			workAuthStatusValue           sql.NullString
			workAuthNotesValue            sql.NullString
			applicationStatusValue        sql.NullString
			applicationUpdatedValue       sql.NullString
			assistantLastSyncValue        sql.NullString
			assistantLastSourceValue      sql.NullString
			assistantLastOutcomeValue     sql.NullString
			assistantLastAutoSubmitValue  int
			assistantLastReviewValue      int
			assistantLastConfirmValue     int
			firstSeenValue                sql.NullString
			lastSeenValue                 sql.NullString
			activeValue                   int
		)
		if err := rows.Scan(
			&fingerprintValue,
			&companyValue,
			&titleValue,
			&urlValue,
			&sourceValue,
			&locationValue,
			&teamValue,
			&postedAtValue,
			&descriptionValue,
			&heuristicContextValue,
			&feedRelevanceValue,
			&deterministicRoleValue,
			&deterministicRoleReasonsValue,
			&deterministicInternshipValue,
			&needsRoleSLMValue,
			&needsInternshipSLMValue,
			&heuristicScoreValue,
			&heuristicReasonsValue,
			&heuristicResumeValue,
			&workAuthStatusValue,
			&workAuthNotesValue,
			&applicationStatusValue,
			&applicationUpdatedValue,
			&assistantLastSyncValue,
			&assistantLastSourceValue,
			&assistantLastOutcomeValue,
			&assistantLastAutoSubmitValue,
			&assistantLastReviewValue,
			&assistantLastConfirmValue,
			&firstSeenValue,
			&lastSeenValue,
			&activeValue,
		); err != nil {
			return snapshot, err
		}
		companyName := strings.TrimSpace(companyValue.String)
		teamName := strings.TrimSpace(teamValue.String)
		url := strings.TrimSpace(urlValue.String)
		displayCompany := dashboardDisplayCompany(companyName, teamName, url)
		firstSeen := strings.TrimSpace(firstSeenValue.String)
		lastSeen := strings.TrimSpace(lastSeenValue.String)
		if lastSeen == "" {
			lastSeen = firstSeen
		}
		postedAt := strings.TrimSpace(postedAtValue.String)
		snapshot.Jobs = append(snapshot.Jobs, DashboardJob{
			Company:                       displayCompany,
			Fingerprint:                   strings.TrimSpace(fingerprintValue.String),
			Title:                         strings.TrimSpace(titleValue.String),
			URL:                           url,
			FirstSeen:                     firstSeen,
			FirstSeenLocal:                isoToLocal(firstSeen),
			LastSeen:                      lastSeen,
			LastSeenLocal:                 isoToLocal(lastSeen),
			Active:                        activeValue != 0,
			Source:                        strings.TrimSpace(sourceValue.String),
			Location:                      strings.TrimSpace(locationValue.String),
			Team:                          teamName,
			PostedAt:                      postedAt,
			PostedAtLocal:                 isoToLocal(postedAt),
			Description:                   strings.TrimSpace(descriptionValue.String),
			MatchScore:                    heuristicScoreValue,
			MatchReasons:                  decodeStringSliceJSON(heuristicReasonsValue.String),
			RecommendedResume:             strings.TrimSpace(heuristicResumeValue.String),
			WorkAuthStatus:                strings.TrimSpace(workAuthStatusValue.String),
			WorkAuthNotes:                 decodeStringSliceJSON(workAuthNotesValue.String),
			ApplicationStatus:             normalizeJobApplicationStatus(applicationStatusValue.String),
			ApplicationUpdatedAt:          strings.TrimSpace(applicationUpdatedValue.String),
			AssistantLastSyncAt:           strings.TrimSpace(assistantLastSyncValue.String),
			AssistantLastSource:           strings.TrimSpace(assistantLastSourceValue.String),
			AssistantLastOutcome:          strings.TrimSpace(assistantLastOutcomeValue.String),
			AssistantLastAutoSubmit:       assistantLastAutoSubmitValue != 0,
			AssistantLastReviewPending:    assistantLastReviewValue,
			AssistantLastConfirmed:        assistantLastConfirmValue != 0,
			HeuristicCached:               strings.TrimSpace(heuristicContextValue.String) != "",
			HeuristicContextHash:          strings.TrimSpace(heuristicContextValue.String),
			FeedRelevantCached:            feedRelevanceValue != 0,
			DeterministicRoleDecision:     normalizeRoleDecision(deterministicRoleValue.String),
			DeterministicRoleReasons:      decodeStringSliceJSON(deterministicRoleReasonsValue.String),
			DeterministicInternshipStatus: normalizeSLMInternshipStatus(deterministicInternshipValue.String),
			NeedsRoleSLM:                  needsRoleSLMValue != 0,
			NeedsInternshipSLM:            needsInternshipSLMValue != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func UpdateJobApplicationStatus(statePath string, fingerprint string, status string) (JobApplicationStatusUpdate, error) {
	update := JobApplicationStatusUpdate{
		OK:          false,
		Fingerprint: strings.TrimSpace(fingerprint),
		Status:      normalizeJobApplicationStatus(status),
	}
	if update.Fingerprint == "" {
		return update, fmt.Errorf("missing fingerprint")
	}
	if !jobsDBEnabled() {
		return update, fmt.Errorf("jobs db disabled")
	}

	dbPath := jobsDBPath(statePath)
	db, err := openJobsDB(dbPath)
	if err != nil {
		return update, err
	}
	defer db.Close()

	updatedAt := ""
	if update.Status != "" {
		updatedAt = utcNow()
	}
	result, err := db.Exec(
		`UPDATE jobs SET application_status = ?, application_updated_at = ? WHERE fingerprint = ?`,
		update.Status,
		updatedAt,
		update.Fingerprint,
	)
	if err != nil {
		return update, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return update, err
	}
	if rowsAffected == 0 {
		return update, fmt.Errorf("job not found")
	}

	update.OK = true
	update.UpdatedAt = updatedAt
	return update, nil
}

func persistJobsToDB(tx *sql.Tx, state MonitorState) error {
	statement, err := tx.Prepare(`
		INSERT INTO jobs (
			fingerprint, company, title, url, source, external_id, location, team,
			posted_at, posted_at_ts, description, heuristic_context_hash, feed_relevance_ok,
			deterministic_role_decision, deterministic_role_reasons_json, deterministic_internship_status,
			needs_role_slm, needs_internship_slm,
			heuristic_match_score, heuristic_match_reasons_json, heuristic_recommended_resume,
			work_auth_status, work_auth_notes_json, first_seen, last_seen, active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			company=excluded.company,
			title=excluded.title,
			url=excluded.url,
			source=excluded.source,
			external_id=excluded.external_id,
			location=excluded.location,
			team=excluded.team,
			posted_at=excluded.posted_at,
			posted_at_ts=excluded.posted_at_ts,
			description=CASE
				WHEN excluded.description <> '' THEN excluded.description
				ELSE jobs.description
			END,
			heuristic_context_hash=excluded.heuristic_context_hash,
			feed_relevance_ok=excluded.feed_relevance_ok,
			deterministic_role_decision=excluded.deterministic_role_decision,
			deterministic_role_reasons_json=excluded.deterministic_role_reasons_json,
			deterministic_internship_status=excluded.deterministic_internship_status,
			needs_role_slm=excluded.needs_role_slm,
			needs_internship_slm=excluded.needs_internship_slm,
			heuristic_match_score=excluded.heuristic_match_score,
			heuristic_match_reasons_json=excluded.heuristic_match_reasons_json,
			heuristic_recommended_resume=excluded.heuristic_recommended_resume,
			work_auth_status=excluded.work_auth_status,
			work_auth_notes_json=excluded.work_auth_notes_json,
			first_seen=CASE
				WHEN COALESCE(jobs.first_seen, '') = '' THEN excluded.first_seen
				ELSE jobs.first_seen
			END,
			last_seen=excluded.last_seen,
			active=excluded.active
	`)
	if err != nil {
		return err
	}
	defer statement.Close()

	for company, entries := range state.Seen {
		for fingerprint, entry := range entries {
			postedAt := strings.TrimSpace(entry.PostedAt)
			postedAtTS := normalizePostedAtForDB(postedAt)
			firstSeen := nonEmpty(strings.TrimSpace(entry.FirstSeen), utcNow())
			lastSeen := nonEmpty(strings.TrimSpace(entry.LastSeen), firstSeen)
			artifacts := jobsFeedHeuristicArtifactsForJob(seenEntryToJob(company, entry))
			if _, err := statement.Exec(
				fingerprint,
				company,
				entry.Title,
				entry.URL,
				entry.Source,
				entry.ExternalID,
				entry.Location,
				entry.Team,
				postedAt,
				postedAtTS,
				entry.Description,
				artifacts.ContextHash,
				boolToInt(artifacts.RelevantForFeed),
				normalizeRoleDecision(artifacts.RoleDecision),
				encodeStringSliceJSON(artifacts.RoleDecisionReasons),
				normalizeSLMInternshipStatus(artifacts.InternshipStatus),
				boolToInt(artifacts.NeedsRoleSLM),
				boolToInt(artifacts.NeedsInternshipSLM),
				artifacts.MatchScore,
				encodeStringSliceJSON(artifacts.MatchReasons),
				artifacts.RecommendedResume,
				artifacts.WorkAuthStatus,
				encodeStringSliceJSON(artifacts.WorkAuthNotes),
				firstSeen,
				lastSeen,
				boolToInt(entry.Active),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func persistCompanyRunsToDB(tx *sql.Tx, runID string, outcomes []CrawlOutcome, observedAt string) error {
	if len(outcomes) == 0 {
		return nil
	}
	statement, err := tx.Prepare(`
		INSERT INTO company_runs (
			run_id, company, outcome_status, selected_source, attempted_sources, jobs_found, message, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer statement.Close()

	for _, outcome := range outcomes {
		attemptedSourcesJSON, _ := json.Marshal(outcome.AttemptedSources)
		if _, err := statement.Exec(
			runID,
			outcome.Company,
			strings.ToLower(strings.TrimSpace(outcome.Status)),
			strings.TrimSpace(outcome.SelectedSource),
			string(attemptedSourcesJSON),
			len(outcome.Jobs),
			strings.TrimSpace(truncateRunes(outcome.Message, 800)),
			observedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func persistJobObservationsToDB(tx *sql.Tx, runID string, outcomes []CrawlOutcome, newJobs []Job, observedAt string) error {
	if len(outcomes) == 0 {
		return nil
	}
	newSet := map[string]struct{}{}
	for _, job := range newJobs {
		newSet[jobFingerprint(job)] = struct{}{}
	}

	statement, err := tx.Prepare(`
		INSERT INTO job_observations (
			fingerprint, observed_at, run_id, company, status, source, posted_at, posted_at_ts
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer statement.Close()

	for _, outcome := range outcomes {
		if strings.ToLower(strings.TrimSpace(outcome.Status)) != "ok" {
			continue
		}
		for _, job := range outcome.Jobs {
			fingerprint := jobFingerprint(job)
			status := "seen"
			if _, isNew := newSet[fingerprint]; isNew {
				status = "new"
			}
			if _, err := statement.Exec(
				fingerprint,
				observedAt,
				runID,
				outcome.Company,
				status,
				job.Source,
				strings.TrimSpace(job.PostedAt),
				normalizePostedAtForDB(job.PostedAt),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func persistDailyStatsToDB(tx *sql.Tx, state MonitorState, newJobs []Job, observedAt string) error {
	day := time.Now().UTC().Format("2006-01-02")
	newByCompany := map[string]int{}
	for _, job := range newJobs {
		newByCompany[job.Company]++
	}
	blockedByCompany := map[string]int{}
	for company, blocked := range state.Blocked {
		blockedByCompany[company] = len(blocked)
	}

	type rollup struct {
		SeenJobs       int
		ActiveJobs     int
		WithPostedDate int
	}
	rollups := map[string]rollup{}
	rows, err := tx.Query(`SELECT company, seen_jobs, active_jobs, with_posted_date FROM v_jobs_company_rollup`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			company        string
			seenJobs       int
			activeJobs     int
			withPostedDate int
		)
		if err := rows.Scan(&company, &seenJobs, &activeJobs, &withPostedDate); err != nil {
			return err
		}
		company = strings.TrimSpace(company)
		if company == "" {
			continue
		}
		rollups[company] = rollup{
			SeenJobs:       seenJobs,
			ActiveJobs:     activeJobs,
			WithPostedDate: withPostedDate,
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	companySet := map[string]struct{}{}
	for company := range rollups {
		companySet[company] = struct{}{}
	}
	for company := range newByCompany {
		companySet[company] = struct{}{}
	}
	for company := range blockedByCompany {
		companySet[company] = struct{}{}
	}
	for _, company := range collectCompanyNamesFromState(state) {
		companySet[company] = struct{}{}
	}
	companies := make([]string, 0, len(companySet))
	for company := range companySet {
		companies = append(companies, company)
	}
	sort.Slice(companies, func(i, j int) bool {
		return strings.ToLower(companies[i]) < strings.ToLower(companies[j])
	})

	statement, err := tx.Prepare(`
		INSERT INTO daily_job_stats (
			day, company, active_jobs, new_jobs, seen_jobs, blocked_events, with_posted_date, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day, company) DO UPDATE SET
			active_jobs=excluded.active_jobs,
			new_jobs=excluded.new_jobs,
			seen_jobs=excluded.seen_jobs,
			blocked_events=excluded.blocked_events,
			with_posted_date=excluded.with_posted_date,
			updated_at=excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer statement.Close()

	for _, company := range companies {
		companyRollup := rollups[company]
		if _, err := statement.Exec(
			day,
			company,
			companyRollup.ActiveJobs,
			newByCompany[company],
			companyRollup.SeenJobs,
			blockedByCompany[company],
			companyRollup.WithPostedDate,
			observedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func deleteStoredNoiseJobsFromDB(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT fingerprint, company, title, url, source, external_id, location, team, posted_at, description
		FROM jobs
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fingerprints := make([]string, 0)
	for rows.Next() {
		var (
			fingerprint      string
			companyValue     sql.NullString
			titleValue       sql.NullString
			urlValue         sql.NullString
			sourceValue      sql.NullString
			externalIDValue  sql.NullString
			locationValue    sql.NullString
			teamValue        sql.NullString
			postedAtValue    sql.NullString
			descriptionValue sql.NullString
		)
		if err := rows.Scan(
			&fingerprint,
			&companyValue,
			&titleValue,
			&urlValue,
			&sourceValue,
			&externalIDValue,
			&locationValue,
			&teamValue,
			&postedAtValue,
			&descriptionValue,
		); err != nil {
			return err
		}
		job := Job{
			Company:     strings.TrimSpace(companyValue.String),
			Title:       strings.TrimSpace(titleValue.String),
			URL:         strings.TrimSpace(urlValue.String),
			Source:      strings.TrimSpace(sourceValue.String),
			ExternalID:  strings.TrimSpace(externalIDValue.String),
			Location:    strings.TrimSpace(locationValue.String),
			Team:        strings.TrimSpace(teamValue.String),
			PostedAt:    strings.TrimSpace(postedAtValue.String),
			Description: strings.TrimSpace(descriptionValue.String),
		}
		if shouldPruneStoredJob(job) {
			fingerprints = append(fingerprints, fingerprint)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(fingerprints) == 0 {
		return nil
	}

	statement, err := tx.Prepare(`DELETE FROM jobs WHERE fingerprint = ?`)
	if err != nil {
		return err
	}
	defer statement.Close()

	for _, fingerprint := range fingerprints {
		if _, err := statement.Exec(fingerprint); err != nil {
			return err
		}
	}
	return nil
}

func cleanupJobsDB(tx *sql.Tx) error {
	now := time.Now().UTC()
	observationCutoff := now.AddDate(0, 0, -jobsDBObservationRetentionDays()).Format(time.RFC3339)
	runCutoff := now.AddDate(0, 0, -jobsDBRunRetentionDays()).Format(time.RFC3339)
	inactiveCutoff := now.AddDate(0, 0, -jobsDBInactiveRetentionDays()).Format(time.RFC3339)
	dailyCutoff := now.AddDate(0, 0, -jobsDBDailyStatsRetentionDays()).Format("2006-01-02")
	slmScoreCutoff := now.AddDate(0, 0, -jobsDBSLMScoreRetentionDays()).Format(time.RFC3339)

	if err := deleteStoredNoiseJobsFromDB(tx); err != nil {
		return err
	}

	statements := []struct {
		query string
		args  []any
	}{
		{query: `DELETE FROM job_observations WHERE observed_at < ?`, args: []any{observationCutoff}},
		{query: `DELETE FROM company_runs WHERE created_at < ?`, args: []any{runCutoff}},
		{
			query: `DELETE FROM jobs WHERE active = 0 AND COALESCE(last_seen, '') <> '' AND last_seen < ?`,
			args:  []any{inactiveCutoff},
		},
		{query: `DELETE FROM daily_job_stats WHERE day < ?`, args: []any{dailyCutoff}},
		{query: `DELETE FROM slm_scores WHERE last_used_at < ?`, args: []any{slmScoreCutoff}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			return err
		}
	}
	return nil
}

func persistRunToJobsDB(db *sql.DB, state MonitorState, outcomes []CrawlOutcome, newJobs []Job, runID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	observedAt := utcNow()
	if err = persistJobsToDB(tx, state); err != nil {
		return err
	}
	if err = persistCompanyRunsToDB(tx, runID, outcomes, observedAt); err != nil {
		return err
	}
	if err = persistJobObservationsToDB(tx, runID, outcomes, newJobs, observedAt); err != nil {
		return err
	}
	if err = persistDailyStatsToDB(tx, state, newJobs, observedAt); err != nil {
		return err
	}
	if err = cleanupJobsDB(tx); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func PersistJobsRunToDB(statePath string, state MonitorState, outcomes []CrawlOutcome, newJobs []Job) error {
	if !jobsDBEnabled() {
		return nil
	}
	path := jobsDBPath(statePath)
	db, err := openJobsDB(path)
	if err != nil {
		return fmt.Errorf("open jobs db: %w", err)
	}
	defer db.Close()
	runID := fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
	if err := persistRunToJobsDB(db, state, outcomes, newJobs, runID); err != nil {
		return fmt.Errorf("persist run: %w", err)
	}
	return nil
}

func EnsureJobsDBBootstrapFromState(statePath string) error {
	if !jobsDBEnabled() {
		return nil
	}
	path := jobsDBPath(statePath)
	db, err := openJobsDB(path)
	if err != nil {
		return fmt.Errorf("open jobs db: %w", err)
	}
	defer db.Close()

	var existingRows int
	if err := db.QueryRow(`SELECT COUNT(1) FROM jobs`).Scan(&existingRows); err != nil {
		return fmt.Errorf("count jobs: %w", err)
	}
	if existingRows > 0 {
		return nil
	}

	state, err := LoadState(statePath)
	if err != nil {
		return fmt.Errorf("load state for bootstrap: %w", err)
	}
	if len(state.Seen) == 0 {
		return nil
	}
	if err := persistRunToJobsDB(db, state, nil, nil, "bootstrap"); err != nil {
		return fmt.Errorf("bootstrap persist: %w", err)
	}
	return nil
}
