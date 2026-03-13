package monitor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resetSLMScoreCacheForTests() {
	slmScoreCache.mu.Lock()
	defer slmScoreCache.mu.Unlock()
	slmScoreCache.items = map[string]slmJobResult{}
	slmScoreCache.order = nil
}

func TestMergeSLMScoreWithGuardrailsCapsBlockedInternship(t *testing.T) {
	base := DashboardJob{
		Title:             "Software Engineering Intern",
		Location:          "Remote (US)",
		Description:       "Build backend services for the AI platform.",
		MatchScore:        58,
		MatchReasons:      []string{"Best resume: AI Systems"},
		RecommendedResume: "AI Systems",
	}
	scored := slmJobResult{
		RoleFit:          true,
		InternshipStatus: "blocked",
		MatchScore:       92,
		Reasons:          []string{"Current student status required"},
	}

	score, reasons := mergeSLMScoreWithGuardrails(base, scored)
	if score > 22 {
		t.Fatalf("expected blocked internship cap, got %d", score)
	}
	if !strings.Contains(strings.Join(reasons, " | "), "current enrollment") {
		t.Fatalf("expected blocked internship reason, got %v", reasons)
	}
}

func TestNormalizeSLMInternshipStatusDefaultsToNotApplicable(t *testing.T) {
	if got := normalizeSLMInternshipStatus("maybe"); got != "not_applicable" {
		t.Fatalf("normalizeSLMInternshipStatus(%q) = %q, want %q", "maybe", got, "not_applicable")
	}
}

func TestApplySLMMatchScoresIncludesAmbiguousInternshipOutsideBudget(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    summary: Applied AI engineer focused on llm systems.
    focus_keywords: ["ai", "llm"]
    role_keywords: ["software engineer", "ai engineer"]
    stack_keywords: ["python", "fastapi"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	t.Setenv("SLM_SCORING", "true")
	t.Setenv("SLM_SCORING_MAX_JOBS", "1")
	t.Setenv("SLM_SCORING_PERSIST_CACHE", "false")
	t.Setenv("JOBS_DB_ENABLED", "false")
	resetResumeProfilesCacheForTests()
	resetSLMScoreCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)
	t.Cleanup(resetSLMScoreCacheForTests)

	jobs := []DashboardJob{
		{
			Company:      "Alpha",
			Title:        "Software Engineer I",
			Location:     "Remote (US)",
			Description:  "Build backend platform services in Go and AWS.",
			MatchScore:   80,
			MatchReasons: []string{"Relevant engineering role title"},
		},
		{
			Company:           "Beta",
			Title:             "AI Software Engineering Intern",
			Location:          "Remote (US)",
			Description:       "Build AI platform intern services in Python and FastAPI during the summer internship program.",
			MatchScore:        28,
			MatchReasons:      []string{"Internship eligibility needs SLM review for student-status requirements"},
			RecommendedResume: "AI Systems",
		},
	}

	slmCacheStoreResult(slmCacheKey(dashboardAsSourceJob(jobs[0])), slmJobResult{
		RoleFit:          true,
		InternshipStatus: "not_applicable",
		MatchScore:       70,
		Reasons:          []string{"Cached score"},
	})
	slmCacheStoreResult(slmCacheKey(dashboardAsSourceJob(jobs[1])), slmJobResult{
		RoleFit:          true,
		InternshipStatus: "allowed",
		MatchScore:       76,
		Reasons:          []string{"No student restriction found"},
	})

	summary := applySLMMatchScoresWithProgress(jobs, "best_match", 1, nil)
	if summary.ScheduledJobs != 1 {
		t.Fatalf("scheduled jobs = %d, want %d", summary.ScheduledJobs, 1)
	}
	if jobs[1].MatchScore <= 28 {
		t.Fatalf("expected ambiguous internship to be reranked, got %d", jobs[1].MatchScore)
	}
	if jobs[0].MatchScore != 80 {
		t.Fatalf("expected deterministic in-role job to keep heuristic score without rerank, got %d", jobs[0].MatchScore)
	}
	if !strings.Contains(strings.Join(jobs[1].MatchReasons, " | "), "not appear restricted to current students") {
		t.Fatalf("expected allowed internship reason, got %v", jobs[1].MatchReasons)
	}
}

func TestApplySLMMatchScoresSkipsDeterministicInRolesWithoutRerank(t *testing.T) {
	t.Setenv("SLM_SCORING", "true")
	t.Setenv("SLM_SCORING_MAX_JOBS", "2")
	t.Setenv("SLM_SCORING_PERSIST_CACHE", "false")
	t.Setenv("JOBS_DB_ENABLED", "false")
	resetSLMScoreCacheForTests()
	t.Cleanup(resetSLMScoreCacheForTests)

	jobs := []DashboardJob{
		{
			Company:      "Alpha",
			Title:        "Backend Engineer",
			Location:     "Remote (US)",
			Description:  "Build backend services in Go.",
			MatchScore:   72,
			MatchReasons: []string{"Relevant engineering role title"},
		},
	}

	summary := applySLMMatchScoresWithProgress(jobs, "best_match", 1, nil)
	if summary.ScheduledJobs != 0 {
		t.Fatalf("scheduled jobs = %d, want %d", summary.ScheduledJobs, 0)
	}
	if jobs[0].MatchScore != 72 {
		t.Fatalf("expected deterministic in-role job to keep heuristic score, got %d", jobs[0].MatchScore)
	}
}

func TestApplySLMMatchScoresReportsProgressBeforeAllWorkersFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		payload := string(body)
		if strings.Contains(payload, "Slow Role") {
			time.Sleep(300 * time.Millisecond)
		}
		_, _ = w.Write([]byte(`{"message":{"content":"{\"role_fit\":true,\"work_auth_status\":\"friendly\",\"internship_status\":\"not_applicable\",\"match_score\":76,\"reasons\":[\"Strong fit\"]}"}}`))
	}))
	defer server.Close()

	t.Setenv("SLM_SCORING", "true")
	t.Setenv("SLM_SCORING_URL", server.URL)
	t.Setenv("SLM_SCORING_TIMEOUT_SECONDS", "2")
	t.Setenv("SLM_SCORING_MAX_JOBS", "2")
	t.Setenv("SLM_SCORING_WORKERS", "2")
	t.Setenv("SLM_SCORING_RERANK_TOP_MATCHES", "true")
	t.Setenv("SLM_SCORING_PERSIST_CACHE", "false")
	t.Setenv("JOBS_DB_ENABLED", "false")
	resetSLMScoreCacheForTests()
	t.Cleanup(resetSLMScoreCacheForTests)

	jobs := []DashboardJob{
		{
			Company:      "Alpha",
			Title:        "Fast Role",
			Location:     "Remote (US)",
			Description:  "Build backend services in Go.",
			MatchScore:   72,
			MatchReasons: []string{"Relevant engineering role title"},
		},
		{
			Company:      "Beta",
			Title:        "Slow Role",
			Location:     "Remote (US)",
			Description:  "Build backend services in Go.",
			MatchScore:   71,
			MatchReasons: []string{"Relevant engineering role title"},
		},
	}

	progressCh := make(chan slmApplyProgress, 8)
	doneCh := make(chan slmApplySummary, 1)
	go func() {
		doneCh <- applySLMMatchScoresWithProgress(jobs, "best_match", 2, func(progress slmApplyProgress) {
			if progress.CompletedJobs > 0 {
				progressCh <- progress
			}
		})
	}()

	select {
	case progress := <-progressCh:
		if progress.CompletedJobs != 1 {
			t.Fatalf("first in-flight progress completed_jobs = %d, want 1", progress.CompletedJobs)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("timed out waiting for incremental progress before all workers finished")
	}

	select {
	case summary := <-doneCh:
		if summary.CompletedJobs != 2 {
			t.Fatalf("completed jobs = %d, want 2", summary.CompletedJobs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scoring summary")
	}
}

func TestApplyCachedSLMMatchScoresUsesOnlyCachedResults(t *testing.T) {
	t.Setenv("SLM_SCORING", "true")
	t.Setenv("SLM_SCORING_PERSIST_CACHE", "false")
	t.Setenv("JOBS_DB_ENABLED", "false")
	t.Setenv("SLM_SCORING_MAX_JOBS", "2")
	t.Setenv("SLM_SCORING_RERANK_TOP_MATCHES", "true")
	resetSLMScoreCacheForTests()
	t.Cleanup(resetSLMScoreCacheForTests)

	jobs := []DashboardJob{
		{
			Company:      "Alpha",
			Title:        "Backend Engineer",
			Location:     "Remote (US)",
			Description:  "Build backend services in Go.",
			MatchScore:   72,
			MatchReasons: []string{"Relevant engineering role title"},
		},
		{
			Company:      "Beta",
			Title:        "Platform Engineer",
			Location:     "Remote (US)",
			Description:  "Build distributed systems in Go.",
			MatchScore:   71,
			MatchReasons: []string{"Relevant engineering role title"},
		},
	}

	slmCacheStoreResult(slmCacheKey(dashboardAsSourceJob(jobs[0])), slmJobResult{
		RoleFit:          true,
		InternshipStatus: "not_applicable",
		MatchScore:       80,
		Reasons:          []string{"Cached strong fit"},
	})

	summary := applyCachedSLMMatchScoresWithProgress(jobs, "best_match", 2, nil)
	if summary.ScheduledJobs != 1 {
		t.Fatalf("scheduled jobs = %d, want %d", summary.ScheduledJobs, 1)
	}
	if summary.CacheMissJobs != 1 {
		t.Fatalf("cache miss jobs = %d, want %d", summary.CacheMissJobs, 1)
	}
	if summary.SuccessJobs != 1 {
		t.Fatalf("success jobs = %d, want %d", summary.SuccessJobs, 1)
	}
	if jobs[0].MatchScore <= 72 {
		t.Fatalf("expected cached score to apply, got %d", jobs[0].MatchScore)
	}
	if jobs[1].MatchScore != 71 {
		t.Fatalf("expected unscored job to keep heuristic score when rerank cache misses, got %d", jobs[1].MatchScore)
	}
	if jobs[0].DecisionSource != "slm" {
		t.Fatalf("decision source = %q, want %q", jobs[0].DecisionSource, "slm")
	}
	if jobs[0].RoleDecision != roleDecisionIn {
		t.Fatalf("role decision = %q, want %q", jobs[0].RoleDecision, roleDecisionIn)
	}
	if jobs[0].InternshipDecision != "not_applicable" {
		t.Fatalf("internship decision = %q, want %q", jobs[0].InternshipDecision, "not_applicable")
	}
	if jobs[1].DecisionSource != "" {
		t.Fatalf("expected cache-miss job to keep deterministic decision source, got %q", jobs[1].DecisionSource)
	}
}

func TestApplyCachedSLMMatchScoresRespectsModelOverride(t *testing.T) {
	t.Setenv("SLM_SCORING", "true")
	t.Setenv("SLM_SCORING_MODEL", "qwen2.5:3b")
	t.Setenv("SLM_SCORING_PERSIST_CACHE", "false")
	t.Setenv("JOBS_DB_ENABLED", "false")
	t.Setenv("SLM_SCORING_MAX_JOBS", "1")
	t.Setenv("SLM_SCORING_RERANK_TOP_MATCHES", "true")
	resetSLMScoreCacheForTests()
	t.Cleanup(resetSLMScoreCacheForTests)

	job := DashboardJob{
		Company:      "Alpha",
		Title:        "Backend Engineer",
		Location:     "Remote (US)",
		Description:  "Build backend services in Go.",
		MatchScore:   72,
		MatchReasons: []string{"Relevant engineering role title"},
	}
	override := slmScoringOptionsForModel("ministral-3:3b")
	slmCacheStoreResult(slmCacheKey(dashboardAsSourceJob(job), override), slmJobResult{
		RoleFit:          true,
		InternshipStatus: "not_applicable",
		MatchScore:       82,
		Reasons:          []string{"Cached override fit"},
	})

	defaultJobs := []DashboardJob{job}
	defaultSummary := applyCachedSLMMatchScoresWithProgress(defaultJobs, "best_match", 1, nil)
	if defaultSummary.SuccessJobs != 0 {
		t.Fatalf("default model should not consume override cache entry, got %d successes", defaultSummary.SuccessJobs)
	}

	overrideJobs := []DashboardJob{job}
	overrideSummary := applyCachedSLMMatchScoresWithProgress(overrideJobs, "best_match", 1, nil, override)
	if overrideSummary.SuccessJobs != 1 {
		t.Fatalf("override model success jobs = %d, want %d", overrideSummary.SuccessJobs, 1)
	}
	if overrideJobs[0].MatchScore <= job.MatchScore {
		t.Fatalf("expected override cache score to apply, got %d", overrideJobs[0].MatchScore)
	}
}

func TestSLMCacheKeyDiffersByTaskModel(t *testing.T) {
	t.Setenv("SLM_SCORING_MODEL", "qwen2.5:3b")
	t.Setenv("SLM_ROLE_MODEL", "ministral-3:3b")
	t.Setenv("SLM_INTERNSHIP_MODEL", "qwen2.5:3b")

	job := Job{
		Company:     "Alpha",
		Title:       "Software Engineer Intern",
		Location:    "Remote (US)",
		PostedAt:    "2026-03-07T00:00:00Z",
		Description: "Build AI platform services.",
	}

	roleKey := slmCacheKey(job, slmScoringOptions{}.withTask(slmTaskRole))
	internshipKey := slmCacheKey(job, slmScoringOptions{}.withTask(slmTaskInternship))
	if roleKey == internshipKey {
		t.Fatalf("expected different cache keys for role vs internship models")
	}
}

func TestApplyCachedSLMMatchScoresUsesSeparateTaskModels(t *testing.T) {
	t.Setenv("SLM_SCORING", "true")
	t.Setenv("SLM_SCORING_MODEL", "qwen2.5:3b")
	t.Setenv("SLM_ROLE_MODEL", "ministral-3:3b")
	t.Setenv("SLM_INTERNSHIP_MODEL", "qwen2.5:3b")
	t.Setenv("SLM_SCORING_PERSIST_CACHE", "false")
	t.Setenv("JOBS_DB_ENABLED", "false")
	t.Setenv("SLM_SCORING_MAX_JOBS", "2")
	resetSLMScoreCacheForTests()
	t.Cleanup(resetSLMScoreCacheForTests)

	jobs := []DashboardJob{
		{
			Company:                       "Alpha",
			Title:                         "Silicon Verification Infrastructure Engineer",
			Location:                      "Austin, Texas",
			Description:                   "Verification infrastructure role.",
			MatchScore:                    34,
			MatchReasons:                  []string{"Borderline engineering scope"},
			HeuristicCached:               true,
			HeuristicContextHash:          jobsFeedHeuristicContextHash(),
			FeedRelevantCached:            true,
			DeterministicRoleDecision:     roleDecisionAmbiguous,
			NeedsRoleSLM:                  true,
			WorkAuthStatus:                "unknown",
			DeterministicInternshipStatus: "not_applicable",
		},
		{
			Company:                       "Beta",
			Title:                         "Software Engineering Intern",
			Location:                      "Remote (US)",
			Description:                   "Summer internship on backend services.",
			MatchScore:                    29,
			MatchReasons:                  []string{"Internship eligibility needs SLM review for student-status requirements"},
			RecommendedResume:             "Backend Resume",
			HeuristicCached:               true,
			HeuristicContextHash:          jobsFeedHeuristicContextHash(),
			FeedRelevantCached:            true,
			DeterministicRoleDecision:     roleDecisionIn,
			WorkAuthStatus:                "unknown",
			DeterministicInternshipStatus: "unknown",
			NeedsInternshipSLM:            true,
		},
	}

	roleOptions := slmScoringOptions{}.withTask(slmTaskRole)
	internshipOptions := slmScoringOptions{}.withTask(slmTaskInternship)
	slmCacheStoreResult(slmCacheKey(dashboardAsSourceJob(jobs[0]), roleOptions), slmJobResult{
		RoleFit:          false,
		InternshipStatus: "not_applicable",
		MatchScore:       18,
		Reasons:          []string{"Cached role model decision"},
	})
	slmCacheStoreResult(slmCacheKey(dashboardAsSourceJob(jobs[1]), internshipOptions), slmJobResult{
		RoleFit:          true,
		InternshipStatus: "unknown",
		MatchScore:       52,
		Reasons:          []string{"Cached internship model decision"},
	})

	summary := applyCachedSLMMatchScoresWithProgress(jobs, "best_match", 2, nil)
	if summary.SuccessJobs != 2 {
		t.Fatalf("success jobs = %d, want %d", summary.SuccessJobs, 2)
	}
	if jobs[0].DecisionSource != "slm" || jobs[1].DecisionSource != "slm" {
		t.Fatalf("expected both jobs to use cached SLM decisions, got %q and %q", jobs[0].DecisionSource, jobs[1].DecisionSource)
	}
	if jobs[0].RoleDecision != roleDecisionOut {
		t.Fatalf("role decision = %q, want %q", jobs[0].RoleDecision, roleDecisionOut)
	}
	if jobs[1].InternshipDecision != "unknown" {
		t.Fatalf("internship decision = %q, want %q", jobs[1].InternshipDecision, "unknown")
	}
}
