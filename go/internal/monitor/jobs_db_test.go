package monitor

import (
	"path/filepath"
	"testing"
)

func TestUpdateJobApplicationStatusRoundTrip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "openings_state.json")
	dbPath := jobsDBPath(statePath)

	db, err := openJobsDB(dbPath)
	if err != nil {
		t.Fatalf("openJobsDB() error = %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO jobs (
			fingerprint, company, title, url, first_seen, last_seen, active
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "fp-1", "OpenAI", "Software Engineer", "https://jobs.example.com/job/1", "2026-03-05T00:00:00Z", "2026-03-05T00:00:00Z", 1)
	if err != nil {
		t.Fatalf("seed insert failed: %v", err)
	}

	update, err := UpdateJobApplicationStatus(statePath, "fp-1", "applied")
	if err != nil {
		t.Fatalf("UpdateJobApplicationStatus() error = %v", err)
	}
	if !update.OK || update.Status != "applied" || update.UpdatedAt == "" {
		t.Fatalf("unexpected update response: %+v", update)
	}

	snapshot, err := loadJobsFeedFromDB(statePath, "", "", "", "", "newest", 20, false, true)
	if err != nil {
		t.Fatalf("loadJobsFeedFromDB() error = %v", err)
	}
	if len(snapshot.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(snapshot.Jobs))
	}
	if snapshot.Jobs[0].Fingerprint != "fp-1" {
		t.Fatalf("fingerprint = %q, want %q", snapshot.Jobs[0].Fingerprint, "fp-1")
	}
	if snapshot.Jobs[0].ApplicationStatus != "applied" {
		t.Fatalf("application status = %q, want %q", snapshot.Jobs[0].ApplicationStatus, "applied")
	}
	if snapshot.Jobs[0].ApplicationUpdatedAt == "" {
		t.Fatalf("expected application_updated_at to be populated")
	}
}

func TestLoadJobsFeedFromDBIncludesPersistedHeuristicArtifacts(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "openings_state.json")
	dbPath := jobsDBPath(statePath)

	db, err := openJobsDB(dbPath)
	if err != nil {
		t.Fatalf("openJobsDB() error = %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO jobs (
			fingerprint, company, title, url, first_seen, last_seen, active,
			heuristic_context_hash, feed_relevance_ok,
			deterministic_role_decision, deterministic_role_reasons_json,
			deterministic_internship_status, needs_role_slm, needs_internship_slm,
			heuristic_match_score,
			heuristic_match_reasons_json, heuristic_recommended_resume,
			work_auth_status, work_auth_notes_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"fp-heuristic",
		"OpenAI",
		"AI Software Engineer",
		"https://jobs.example.com/job/2",
		"2026-03-05T00:00:00Z",
		"2026-03-05T00:00:00Z",
		1,
		"ctx-123",
		1,
		"in",
		`["Clear target engineering or data role title"]`,
		"unknown",
		0,
		1,
		77,
		`["Best resume: AI Resume","Matched keywords: llm, python"]`,
		"AI Resume",
		"friendly",
		`["E-Verify employer signal"]`,
	)
	if err != nil {
		t.Fatalf("seed insert failed: %v", err)
	}

	snapshot, err := loadJobsFeedFromDB(statePath, "", "", "", "", "newest", 20, false, true)
	if err != nil {
		t.Fatalf("loadJobsFeedFromDB() error = %v", err)
	}
	if len(snapshot.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(snapshot.Jobs))
	}
	job := snapshot.Jobs[0]
	if job.MatchScore != 77 {
		t.Fatalf("match score = %d, want %d", job.MatchScore, 77)
	}
	if job.RecommendedResume != "AI Resume" {
		t.Fatalf("recommended resume = %q, want %q", job.RecommendedResume, "AI Resume")
	}
	if job.WorkAuthStatus != "friendly" {
		t.Fatalf("work auth status = %q, want %q", job.WorkAuthStatus, "friendly")
	}
	if len(job.MatchReasons) != 2 {
		t.Fatalf("match reasons len = %d, want %d", len(job.MatchReasons), 2)
	}
	if !job.HeuristicCached {
		t.Fatalf("expected heuristic cache metadata to be populated")
	}
	if !job.FeedRelevantCached {
		t.Fatalf("expected feed relevance cache to be populated")
	}
	if job.DeterministicRoleDecision != "in" {
		t.Fatalf("deterministic role decision = %q, want %q", job.DeterministicRoleDecision, "in")
	}
	if len(job.DeterministicRoleReasons) != 1 {
		t.Fatalf("deterministic role reasons len = %d, want %d", len(job.DeterministicRoleReasons), 1)
	}
	if job.DeterministicInternshipStatus != "unknown" {
		t.Fatalf("deterministic internship status = %q, want %q", job.DeterministicInternshipStatus, "unknown")
	}
	if job.NeedsRoleSLM {
		t.Fatalf("expected needs_role_slm to remain false")
	}
	if !job.NeedsInternshipSLM {
		t.Fatalf("expected needs_internship_slm to be populated")
	}
}

func TestLoadJobsFeedFromDBNewestKeepsUnknownPostedDatesAfterKnownDates(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "openings_state.json")
	dbPath := jobsDBPath(statePath)

	db, err := openJobsDB(dbPath)
	if err != nil {
		t.Fatalf("openJobsDB() error = %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO jobs (
			fingerprint, company, title, url, first_seen, last_seen, active, posted_at, posted_at_ts
		) VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?),
			(?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"fp-known",
		"Example",
		"Known Posted",
		"https://jobs.example.com/job/known",
		"2026-03-05T00:00:00Z",
		"2026-03-05T00:00:00Z",
		1,
		"2026-03-02T09:00:00Z",
		"2026-03-02T09:00:00Z",
		"fp-unknown",
		"Example",
		"Unknown Posted",
		"https://jobs.example.com/job/unknown",
		"2026-03-06T12:00:00Z",
		"2026-03-06T12:00:00Z",
		1,
		"",
		"",
	)
	if err != nil {
		t.Fatalf("seed insert failed: %v", err)
	}

	snapshot, err := loadJobsFeedFromDB(statePath, "", "", "", "", "newest", 20, false, true)
	if err != nil {
		t.Fatalf("loadJobsFeedFromDB() error = %v", err)
	}
	if len(snapshot.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(snapshot.Jobs))
	}
	if snapshot.Jobs[0].Title != "Known Posted" {
		t.Fatalf("newest first title = %q, want %q", snapshot.Jobs[0].Title, "Known Posted")
	}
}

func TestLoadJobsCompanyRollupsFromDBReturnsActiveCounts(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "openings_state.json")
	dbPath := jobsDBPath(statePath)

	db, err := openJobsDB(dbPath)
	if err != nil {
		t.Fatalf("openJobsDB() error = %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO jobs (
			fingerprint, company, title, url, first_seen, last_seen, active, posted_at, posted_at_ts
		) VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?),
			(?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"fp-active",
		"Example",
		"Active Job",
		"https://jobs.example.com/job/active",
		"2026-03-05T00:00:00Z",
		"2026-03-06T00:00:00Z",
		1,
		"2026-03-04T09:00:00Z",
		"2026-03-04T09:00:00Z",
		"fp-inactive",
		"Example",
		"Inactive Job",
		"https://jobs.example.com/job/inactive",
		"2026-03-01T00:00:00Z",
		"2026-03-02T00:00:00Z",
		0,
		"",
		"",
	)
	if err != nil {
		t.Fatalf("seed insert failed: %v", err)
	}

	rollups, err := loadJobsCompanyRollupsFromDB(statePath)
	if err != nil {
		t.Fatalf("loadJobsCompanyRollupsFromDB() error = %v", err)
	}
	rollup, ok := rollups["Example"]
	if !ok {
		t.Fatalf("expected Example rollup")
	}
	if rollup.SeenJobs != 2 {
		t.Fatalf("seen_jobs = %d, want 2", rollup.SeenJobs)
	}
	if rollup.ActiveJobs != 1 {
		t.Fatalf("active_jobs = %d, want 1", rollup.ActiveJobs)
	}
	if rollup.WithPostedDate != 1 {
		t.Fatalf("with_posted_date = %d, want 1", rollup.WithPostedDate)
	}
}

func TestLoadJobsFeedFromDBFiltersBySource(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "openings_state.json")
	dbPath := jobsDBPath(statePath)

	db, err := openJobsDB(dbPath)
	if err != nil {
		t.Fatalf("openJobsDB() error = %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		INSERT INTO jobs (
			fingerprint, company, title, url, source, first_seen, last_seen, active
		) VALUES
			(?, ?, ?, ?, ?, ?, ?, ?),
			(?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"fp-greenhouse",
		"OpenAI",
		"Greenhouse Job",
		"https://jobs.example.com/job/greenhouse",
		"greenhouse",
		"2026-03-05T00:00:00Z",
		"2026-03-05T00:00:00Z",
		1,
		"fp-ashby",
		"OpenAI",
		"Ashby Job",
		"https://jobs.example.com/job/ashby",
		"ashby",
		"2026-03-05T00:00:00Z",
		"2026-03-05T00:00:00Z",
		1,
	)
	if err != nil {
		t.Fatalf("seed insert failed: %v", err)
	}

	snapshot, err := loadJobsFeedFromDB(statePath, "", "", "greenhouse", "", "newest", 20, true, true)
	if err != nil {
		t.Fatalf("loadJobsFeedFromDB() error = %v", err)
	}
	if len(snapshot.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(snapshot.Jobs))
	}
	if snapshot.Jobs[0].Source != "greenhouse" {
		t.Fatalf("source = %q, want %q", snapshot.Jobs[0].Source, "greenhouse")
	}
	if len(snapshot.Sources) != 2 {
		t.Fatalf("source options len = %d, want %d", len(snapshot.Sources), 2)
	}
}
