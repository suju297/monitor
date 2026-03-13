package monitor

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type slmJobResult struct {
	RoleFit          bool
	WorkAuthStatus   string
	InternshipStatus string
	MatchScore       int
	Reasons          []string
}

type slmCacheStore struct {
	mu    sync.RWMutex
	items map[string]slmJobResult
	order []string
}

var slmScoreCache = slmCacheStore{
	items: map[string]slmJobResult{},
	order: make([]string, 0, 2048),
}

var slmScoreCacheConfig = struct {
	mu        sync.RWMutex
	statePath string
}{
	statePath: ".state/openings_state.json",
}

type slmScoringOptions struct {
	Model string
	Task  string
}

const (
	slmTaskGeneral    = ""
	slmTaskRole       = "role"
	slmTaskInternship = "internship"
	slmTaskRerank     = "rerank"
)

func slmScoringEnabled() bool {
	return parseBoolEnv("SLM_SCORING", false)
}

func slmScoringProvider() string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("SLM_SCORING_PROVIDER")))
	if value == "" {
		return "ollama"
	}
	return value
}

func slmScoringOnlyBestMatch() bool {
	return parseBoolEnv("SLM_SCORING_ONLY_BEST_MATCH", true)
}

func slmScoringDebugEnabled() bool {
	return parseBoolEnv("SLM_SCORING_DEBUG", false)
}

func slmScoringPrecomputeOnRun() bool {
	return parseBoolEnv("SLM_SCORING_PRECOMPUTE_ON_RUN", true)
}

func slmScoringEndpoint() string {
	value := strings.TrimSpace(os.Getenv("SLM_SCORING_URL"))
	if value == "" {
		return "http://127.0.0.1:11434/api/chat"
	}
	return value
}

func slmScoringModel() string {
	value := strings.TrimSpace(os.Getenv("SLM_SCORING_MODEL"))
	if value == "" {
		// Ollama ships qwen2.5:3b as a quantized GGUF model (Q4_K_M).
		return "qwen2.5:3b"
	}
	return value
}

func slmRoleModel() string {
	value := strings.TrimSpace(os.Getenv("SLM_ROLE_MODEL"))
	if value == "" {
		return slmScoringModel()
	}
	return value
}

func slmInternshipModel() string {
	value := strings.TrimSpace(os.Getenv("SLM_INTERNSHIP_MODEL"))
	if value == "" {
		return slmScoringModel()
	}
	return value
}

func slmScoringTimeout() time.Duration {
	timeoutSeconds := 25
	if raw := strings.TrimSpace(os.Getenv("SLM_SCORING_TIMEOUT_SECONDS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			timeoutSeconds = parsed
		}
	}
	return time.Duration(timeoutSeconds) * time.Second
}

func slmScoringMaxJobs() int {
	limit := 12
	if raw := strings.TrimSpace(os.Getenv("SLM_SCORING_MAX_JOBS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 0 {
		return 0
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func slmScoringWorkers() int {
	workers := 1
	if raw := strings.TrimSpace(os.Getenv("SLM_SCORING_WORKERS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			workers = parsed
		}
	}
	if workers < 1 {
		return 1
	}
	if workers > 8 {
		return 8
	}
	return workers
}

func slmScoringRerankTopMatches() bool {
	return parseBoolEnv("SLM_SCORING_RERANK_TOP_MATCHES", false)
}

func slmScoringCacheMax() int {
	limit := 4000
	if raw := strings.TrimSpace(os.Getenv("SLM_SCORING_CACHE_MAX")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit < 200 {
		return 200
	}
	return limit
}

func slmScoringPersistCacheEnabled() bool {
	return parseBoolEnv("SLM_SCORING_PERSIST_CACHE", true) && jobsDBEnabled()
}

func setSLMScoringCacheStatePath(statePath string) {
	trimmed := strings.TrimSpace(statePath)
	if trimmed == "" {
		return
	}
	slmScoreCacheConfig.mu.Lock()
	slmScoreCacheConfig.statePath = trimmed
	slmScoreCacheConfig.mu.Unlock()
}

func slmScoringCacheStatePath() string {
	slmScoreCacheConfig.mu.RLock()
	defer slmScoreCacheConfig.mu.RUnlock()
	trimmed := strings.TrimSpace(slmScoreCacheConfig.statePath)
	if trimmed == "" {
		return ".state/openings_state.json"
	}
	return trimmed
}

func resolveSLMScoringOptions(options ...slmScoringOptions) slmScoringOptions {
	resolved := slmScoringOptions{}
	if len(options) > 0 {
		resolved = options[0]
	}
	resolved.Model = strings.TrimSpace(resolved.Model)
	resolved.Task = strings.ToLower(strings.TrimSpace(resolved.Task))
	return resolved
}

func (options slmScoringOptions) effectiveModel() string {
	if strings.TrimSpace(options.Model) != "" {
		return strings.TrimSpace(options.Model)
	}
	switch options.Task {
	case slmTaskRole:
		return slmRoleModel()
	case slmTaskInternship:
		return slmInternshipModel()
	case slmTaskRerank:
		return slmRoleModel()
	default:
		return slmScoringModel()
	}
}

func slmScoringOptionsForModel(model string) slmScoringOptions {
	return slmScoringOptions{Model: strings.TrimSpace(model)}
}

func (options slmScoringOptions) withTask(task string) slmScoringOptions {
	updated := resolveSLMScoringOptions(options)
	updated.Task = strings.ToLower(strings.TrimSpace(task))
	return updated
}

func (options slmScoringOptions) effectiveModelLabel() string {
	if strings.TrimSpace(options.Model) != "" {
		return strings.TrimSpace(options.Model)
	}
	roleModel := slmRoleModel()
	internshipModel := slmInternshipModel()
	switch options.Task {
	case slmTaskRole:
		return roleModel
	case slmTaskInternship:
		return internshipModel
	case slmTaskRerank:
		return roleModel
	}
	if roleModel == internshipModel {
		return roleModel
	}
	return "mixed(role=" + roleModel + ", internship=" + internshipModel + ")"
}

func slmScoringCompareModels() []string {
	options := make([]string, 0, 4)
	seen := map[string]struct{}{}
	appendModel := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		options = append(options, model)
	}
	appendModel(slmScoringModel())
	appendModel(slmRoleModel())
	appendModel(slmInternshipModel())
	if raw := strings.TrimSpace(os.Getenv("SLM_SCORING_COMPARE_MODELS")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			appendModel(part)
		}
	}
	appendModel("ministral-3:3b")
	return options
}

func slmScoringDBPath() string {
	return jobsDBPath(slmScoringCacheStatePath())
}

func slmCacheKey(job Job, options ...slmScoringOptions) string {
	resolved := resolveSLMScoringOptions(options...)
	companyKey := companyFilterKey(job.Company)
	if companyKey == "" {
		companyKey = strings.ToLower(strings.TrimSpace(job.Company))
	}
	parts := []string{
		strings.ToLower(strings.TrimSpace(resolved.effectiveModel())),
		strings.ToLower(strings.TrimSpace(slmScoringSystemPrompt())),
		companyKey,
		strings.ToLower(strings.TrimSpace(job.Title)),
		strings.ToLower(strings.TrimSpace(job.URL)),
		strings.ToLower(strings.TrimSpace(job.Location)),
		strings.ToLower(strings.TrimSpace(job.PostedAt)),
		strings.ToLower(strings.TrimSpace(job.Team)),
		strings.ToLower(strings.TrimSpace(normalizeTextSnippet(job.Description, 2000))),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func slmPromptHash() string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(slmScoringSystemPrompt()))))
	return hex.EncodeToString(sum[:])
}

func slmJobFingerprint(job Job) string {
	companyKey := companyFilterKey(job.Company)
	if companyKey == "" {
		companyKey = strings.ToLower(strings.TrimSpace(job.Company))
	}
	parts := []string{
		companyKey,
		strings.ToLower(strings.TrimSpace(job.Title)),
		strings.ToLower(strings.TrimSpace(job.URL)),
		strings.ToLower(strings.TrimSpace(job.Location)),
		strings.ToLower(strings.TrimSpace(job.PostedAt)),
		strings.ToLower(strings.TrimSpace(job.Team)),
		strings.ToLower(strings.TrimSpace(normalizeTextSnippet(job.Description, 2000))),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func normalizeSLMReasonsFromStrings(raw []string) []string {
	if len(raw) == 0 {
		return defaultSLMReasons()
	}
	converted := make([]any, 0, len(raw))
	for _, reason := range raw {
		converted = append(converted, reason)
	}
	return normalizeSLMReasons(converted)
}

func slmCacheLookup(key string) (slmJobResult, bool) {
	slmScoreCache.mu.RLock()
	defer slmScoreCache.mu.RUnlock()
	result, ok := slmScoreCache.items[key]
	return result, ok
}

func slmCacheStoreResult(key string, result slmJobResult) {
	slmScoreCache.mu.Lock()
	defer slmScoreCache.mu.Unlock()
	if _, exists := slmScoreCache.items[key]; !exists {
		slmScoreCache.order = append(slmScoreCache.order, key)
	}
	slmScoreCache.items[key] = result
	maxSize := slmScoringCacheMax()
	if len(slmScoreCache.order) <= maxSize {
		return
	}
	trimCount := len(slmScoreCache.order) - maxSize
	for i := 0; i < trimCount; i++ {
		oldest := slmScoreCache.order[0]
		delete(slmScoreCache.items, oldest)
		slmScoreCache.order = slmScoreCache.order[1:]
	}
}

func slmCacheLookupPersisted(key string) (slmJobResult, bool, error) {
	result := slmJobResult{}
	if !slmScoringPersistCacheEnabled() {
		return result, false, nil
	}
	db, err := openJobsDB(slmScoringDBPath())
	if err != nil {
		return result, false, err
	}
	defer db.Close()

	var (
		roleFitInt       int
		workAuthStatus   string
		internshipStatus string
		matchScore       int
		reasonsJSON      string
	)
	err = db.QueryRow(
		`SELECT role_fit, work_auth_status, internship_status, match_score, reasons_json FROM slm_scores WHERE cache_key = ?`,
		key,
	).Scan(&roleFitInt, &workAuthStatus, &internshipStatus, &matchScore, &reasonsJSON)
	if err == sql.ErrNoRows {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}

	reasons := []string{}
	if unmarshalErr := json.Unmarshal([]byte(reasonsJSON), &reasons); unmarshalErr != nil {
		reasons = defaultSLMReasons()
	}
	result = slmJobResult{
		RoleFit:          roleFitInt != 0,
		WorkAuthStatus:   normalizeSLMWorkAuth(workAuthStatus),
		InternshipStatus: normalizeSLMInternshipStatus(internshipStatus),
		MatchScore:       clampScore(matchScore),
		Reasons:          normalizeSLMReasonsFromStrings(reasons),
	}
	_, _ = db.Exec(`UPDATE slm_scores SET last_used_at = ?, use_count = use_count + 1 WHERE cache_key = ?`, utcNow(), key)
	return result, true, nil
}

func slmCacheStorePersisted(key string, job Job, result slmJobResult, options ...slmScoringOptions) error {
	if !slmScoringPersistCacheEnabled() {
		return nil
	}
	resolved := resolveSLMScoringOptions(options...)
	db, err := openJobsDB(slmScoringDBPath())
	if err != nil {
		return err
	}
	defer db.Close()

	reasonsJSON, err := json.Marshal(normalizeSLMReasonsFromStrings(result.Reasons))
	if err != nil {
		return err
	}
	now := utcNow()
	_, err = db.Exec(
		`INSERT INTO slm_scores (
			cache_key, model, provider, prompt_hash, job_fingerprint,
			role_fit, work_auth_status, internship_status, match_score, reasons_json,
			created_at, updated_at, last_used_at, use_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(cache_key) DO UPDATE SET
			model=excluded.model,
			provider=excluded.provider,
			prompt_hash=excluded.prompt_hash,
			job_fingerprint=excluded.job_fingerprint,
			role_fit=excluded.role_fit,
			work_auth_status=excluded.work_auth_status,
			internship_status=excluded.internship_status,
			match_score=excluded.match_score,
			reasons_json=excluded.reasons_json,
			updated_at=excluded.updated_at,
			last_used_at=excluded.last_used_at,
			use_count=slm_scores.use_count + 1`,
		key,
		strings.ToLower(strings.TrimSpace(resolved.effectiveModel())),
		strings.ToLower(strings.TrimSpace(slmScoringProvider())),
		slmPromptHash(),
		slmJobFingerprint(job),
		boolToInt(result.RoleFit),
		normalizeSLMWorkAuth(result.WorkAuthStatus),
		normalizeSLMInternshipStatus(result.InternshipStatus),
		clampScore(result.MatchScore),
		string(reasonsJSON),
		now,
		now,
		now,
	)
	return err
}

func defaultSLMReasons() []string {
	return []string{"Scored by local quantized SLM"}
}

func normalizeSLMWorkAuth(value string) string {
	candidate := strings.ToLower(strings.TrimSpace(value))
	switch candidate {
	case "blocked", "friendly", "unknown":
		return candidate
	default:
		return "unknown"
	}
}

func normalizeSLMInternshipStatus(value string) string {
	candidate := strings.ToLower(strings.TrimSpace(value))
	switch candidate {
	case "allowed", "blocked", "unknown", "not_applicable":
		return candidate
	default:
		return "not_applicable"
	}
}

func normalizeSLMReasons(raw []any) []string {
	if len(raw) == 0 {
		return defaultSLMReasons()
	}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, value := range raw {
		reason := normalizeTextSnippet(asString(value), 160)
		if reason == "" {
			continue
		}
		lower := strings.ToLower(reason)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, reason)
		if len(out) >= 4 {
			break
		}
	}
	if len(out) == 0 {
		return defaultSLMReasons()
	}
	return out
}

func slmScoringSystemPrompt() string {
	profileBlock := resumeProfilesPromptBlock()
	if profileBlock != "" {
		profileBlock += "\n"
	}
	return strings.TrimSpace(`
You review borderline software jobs for this candidate profile.

Deterministic filters already handled explicit blockers. Your job is only to resolve ambiguity for:
- borderline role fit within software/backend/full stack/cloud/platform/AI/ML/data targets
- internship eligibility for an already-graduated candidate when student-status wording is unclear

Candidate profile:
- Looking for (ANY ONE is enough): full stack, backend software engineering, cloud/platform/devops, AI/ML, data engineering, data analyst
- Seniority target: early careers through Software Engineer I/II/III
- US-based jobs only
- Recency: prefer jobs posted within the last 7 days when the date is available; unknown dates are allowed but are a weaker signal than explicitly recent jobs

` + profileBlock + `

Important:
- role_fit is ONLY about role/level/location/recency fit within the target software/data profile.
- internship_status is ONLY for internship/co-op roles and decides whether an already-graduated candidate is eligible based on student-status language.
- A job can be role_fit=true and internship_status=blocked at the same time.
- Do NOT do work-authorization classification. That is already handled deterministically elsewhere.
- Prefer role_fit=true when the text clearly supports an in-scope engineering/data role, even if the title is broad or nonstandard.
- Prefer role_fit=false when the text indicates consulting, sales engineering, solutions architecture, customer advisory, or a clearly out-of-scope function.
- For internships/co-ops, return internship_status=blocked when current enrollment, active degree pursuit, student status, remaining semesters, future graduation window, or return-to-school language is required.
- For internships/co-ops, return internship_status=allowed when recent graduates/completed-degree candidates are explicitly allowed or the text makes clear the role is not restricted to current students.
- For internships/co-ops, return internship_status=unknown when eligibility for an already-graduated candidate is unclear from the provided text.
- Do not return internship_status=blocked only because explicit graduate-friendly wording is missing.
- For non-internships, return internship_status=not_applicable.

Return strict JSON only with:
{
  "role_fit": boolean,
  "internship_status": "allowed"|"blocked"|"unknown"|"not_applicable",
  "reasons": ["short reason", "..."]
}
`)
}

func slmScoringSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role_fit": map[string]any{
				"type": "boolean",
			},
			"internship_status": map[string]any{
				"type": "string",
				"enum": []string{"allowed", "blocked", "unknown", "not_applicable"},
			},
			"reasons": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required":             []string{"role_fit", "internship_status", "reasons"},
		"additionalProperties": false,
	}
}

func slmScoreJobWithOllama(job Job, options ...slmScoringOptions) (slmJobResult, error) {
	result := slmJobResult{}
	resolved := resolveSLMScoringOptions(options...)
	model := resolved.effectiveModel()
	payload := map[string]any{
		"model":  model,
		"stream": false,
		"format": slmScoringSchema(),
		"messages": []map[string]string{
			{"role": "system", "content": slmScoringSystemPrompt()},
			{
				"role": "user",
				"content": strings.TrimSpace(fmt.Sprintf(
					"Title: %s\nLocation: %s\nPosted: %s\nDescription: %s\n",
					normalizeTextSnippet(job.Title, 320),
					normalizeTextSnippet(job.Location, 320),
					normalizeTextSnippet(job.PostedAt, 120),
					normalizeTextSnippet(job.Description, 2200),
				)),
			},
		},
		"options": map[string]any{
			"temperature": 0,
		},
	}
	applyOllamaModelTuning(payload, model)

	rawReq, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), slmScoringTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", slmScoringEndpoint(), bytes.NewReader(rawReq))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("slm scoring request failed with status %d", resp.StatusCode)
	}
	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, err
	}
	ollama := map[string]any{}
	if err := json.Unmarshal(rawResp, &ollama); err != nil {
		return result, err
	}
	message := asMap(ollama["message"])
	content := strings.TrimSpace(asString(message["content"]))
	if content == "" {
		return result, fmt.Errorf("empty slm response content")
	}
	if candidate := extractJSONObject(content); candidate != "" {
		content = candidate
	}
	parsed := map[string]any{}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return result, err
	}

	result.RoleFit = asBool(parsed["role_fit"])
	result.WorkAuthStatus = "unknown"
	result.InternshipStatus = normalizeSLMInternshipStatus(asString(parsed["internship_status"]))
	result.MatchScore = 50
	result.Reasons = normalizeSLMReasons(asSlice(parsed["reasons"]))
	return result, nil
}

func slmScoreJob(job Job, options ...slmScoringOptions) (slmJobResult, error) {
	empty := slmJobResult{}
	if !slmScoringEnabled() {
		return empty, fmt.Errorf("slm scoring disabled")
	}
	if slmScoringProvider() != "ollama" {
		return empty, fmt.Errorf("unsupported slm scoring provider")
	}
	resolved := resolveSLMScoringOptions(options...)
	key := slmCacheKey(job, resolved)
	if cached, ok := slmCacheLookup(key); ok {
		return cached, nil
	}
	if persisted, ok, err := slmCacheLookupPersisted(key); err == nil && ok {
		slmCacheStoreResult(key, persisted)
		return persisted, nil
	} else if err != nil && slmScoringDebugEnabled() {
		log.Printf("[slm-scoring] persisted cache lookup failed: %v", err)
	}
	result, err := slmScoreJobWithOllama(job, resolved)
	if err != nil {
		return empty, err
	}
	slmCacheStoreResult(key, result)
	if err := slmCacheStorePersisted(key, job, result, resolved); err != nil && slmScoringDebugEnabled() {
		log.Printf("[slm-scoring] persisted cache store failed: %v", err)
	}
	return result, nil
}

func mergeSLMScoreWithGuardrails(base DashboardJob, scored slmJobResult) (int, []string) {
	artifacts := jobsFeedHeuristicArtifactsForDashboard(base)
	heuristicScore := clampScore(base.MatchScore)
	score := heuristicScore

	reasonSet := map[string]struct{}{}
	reasons := make([]string, 0, 4)
	for _, reason := range base.MatchReasons {
		reasons = appendUniqueReason(reasons, reasonSet, reason)
	}
	if len(reasons) == 0 {
		for _, reason := range scored.Reasons {
			reasons = appendUniqueReason(reasons, reasonSet, reason)
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, defaultSLMReasons()...)
	}
	internship := internshipEligibilityForCandidate(dashboardAsSourceJob(base), loadCandidatePreferences())
	if artifacts.NeedsRoleSLM {
		if scored.RoleFit {
			score = clampScore(score + 6)
			reasons = appendUniqueReason(reasons, reasonSet, "SLM resolved ambiguous role as in-scope")
		} else {
			if score > 35 {
				score = 35
			}
			reasons = appendUniqueReason(reasons, reasonSet, "SLM resolved ambiguous role as out-of-scope")
		}
	} else if slmScoringRerankTopMatches() {
		if scored.RoleFit {
			score = clampScore(score + 2)
			reasons = appendUniqueReason(reasons, reasonSet, "SLM confirmed heuristic fit")
		} else {
			if score > 35 {
				score = 35
			}
			reasons = appendUniqueReason(reasons, reasonSet, "SLM flagged a heuristic fit mismatch")
		}
	}
	if internship.IsInternship {
		switch scored.InternshipStatus {
		case "blocked":
			if score > 22 {
				score = 22
			}
			prioritized := make([]string, 0, 4)
			prioritizedSet := map[string]struct{}{}
			prioritized = appendUniqueReason(prioritized, prioritizedSet, "Internship likely requires current enrollment or return-to-school status")
			for _, reason := range scored.Reasons {
				prioritized = appendUniqueReason(prioritized, prioritizedSet, reason)
			}
			for _, reason := range reasons {
				prioritized = appendUniqueReason(prioritized, prioritizedSet, reason)
			}
			reasons = prioritized
			reasonSet = prioritizedSet
		case "allowed":
			score = clampScore(score + 4)
			reasons = appendUniqueReason(reasons, reasonSet, "Internship does not appear restricted to current students")
			for _, reason := range scored.Reasons {
				reasons = appendUniqueReason(reasons, reasonSet, reason)
			}
		case "unknown":
			candidate := loadCandidatePreferences()
			if candidate.Graduated && candidate.InternshipsRequirePostGradSignal && score > 42 {
				score = 42
			}
			reasons = appendUniqueReason(reasons, reasonSet, "Internship eligibility remains unclear after SLM review")
			for _, reason := range scored.Reasons {
				reasons = appendUniqueReason(reasons, reasonSet, reason)
			}
		}
	} else {
		for _, reason := range scored.Reasons {
			reasons = appendUniqueReason(reasons, reasonSet, reason)
		}
	}

	if base.WorkAuthStatus == "blocked" {
		if score > 28 {
			score = 28
		}
		merged := make([]string, 0, 4)
		for _, reason := range reasons {
			merged = appendUniqueReason(merged, reasonSet, reason)
		}
		merged = appendUniqueReason(merged, reasonSet, "F1 OPT constraint detected")
		if len(base.WorkAuthNotes) > 0 {
			merged = appendUniqueReason(merged, reasonSet, base.WorkAuthNotes[0])
		}
		reasons = merged
	}
	if len(reasons) > 4 {
		reasons = reasons[:4]
	}
	if slmScoringDebugEnabled() {
		merged := make([]string, 0, 4)
		for _, reason := range reasons {
			merged = appendUniqueReason(merged, reasonSet, reason)
		}
		merged = appendUniqueReason(merged, reasonSet, "SLM reranker applied")
		if len(merged) > 4 {
			merged = merged[:4]
		}
		reasons = merged
	}
	return score, reasons
}

func resolvedSLMRoleDecision(base DashboardJob, scored slmJobResult) string {
	artifacts := jobsFeedHeuristicArtifactsForDashboard(base)
	if artifacts.NeedsRoleSLM || slmScoringRerankTopMatches() {
		if scored.RoleFit {
			return roleDecisionIn
		}
		return roleDecisionOut
	}
	return normalizeRoleDecision(artifacts.RoleDecision)
}

func resolvedSLMInternshipDecision(base DashboardJob, scored slmJobResult) string {
	status := normalizeSLMInternshipStatus(scored.InternshipStatus)
	if status != "not_applicable" {
		return status
	}
	return normalizeSLMInternshipStatus(jobsFeedHeuristicArtifactsForDashboard(base).InternshipStatus)
}

func applySLMDecisionMetadata(job *DashboardJob, roleDecision string, internshipDecision string) {
	job.RoleDecision = normalizeRoleDecision(roleDecision)
	job.InternshipDecision = normalizeSLMInternshipStatus(internshipDecision)
	job.DecisionSource = "slm"
}

type slmApplyProgress struct {
	EligibleJobs  int
	ScheduledJobs int
	QueuedJobs    int
	CompletedJobs int
	SuccessJobs   int
	FailedJobs    int
}

type slmApplySummary struct {
	EligibleJobs  int
	ScheduledJobs int
	CompletedJobs int
	SuccessJobs   int
	FailedJobs    int
	CacheMissJobs int
	Skipped       bool
	SkipReason    string
}

type resultRow struct {
	index            int
	score            int
	reasons          []string
	roleDecision     string
	internshipStatus string
	ok               bool
	errorMessage     string
}

func applySLMMatchScores(jobs []DashboardJob, sortBy string, requestLimit int) {
	_ = applySLMMatchScoresWithProgress(jobs, sortBy, requestLimit, nil)
}

func slmScoreJobCached(job Job, options ...slmScoringOptions) (slmJobResult, bool, error) {
	resolved := resolveSLMScoringOptions(options...)
	key := slmCacheKey(job, resolved)
	if cached, ok := slmCacheLookup(key); ok {
		return cached, true, nil
	}
	persisted, ok, err := slmCacheLookupPersisted(key)
	if err != nil {
		return slmJobResult{}, false, err
	}
	if !ok {
		return slmJobResult{}, false, nil
	}
	slmCacheStoreResult(key, persisted)
	return persisted, true, nil
}

func dashboardJobNeedsSLM(job DashboardJob) bool {
	artifacts := jobsFeedHeuristicArtifactsForDashboard(job)
	return artifacts.NeedsRoleSLM || artifacts.NeedsInternshipSLM
}

func slmTaskForDashboardJob(job DashboardJob) string {
	artifacts := jobsFeedHeuristicArtifactsForDashboard(job)
	if artifacts.NeedsInternshipSLM {
		return slmTaskInternship
	}
	if artifacts.NeedsRoleSLM {
		return slmTaskRole
	}
	return slmTaskRerank
}

func dashboardJobEligibleForSLMRerank(job DashboardJob) bool {
	artifacts := jobsFeedHeuristicArtifactsForDashboard(job)
	if artifacts.NeedsRoleSLM || artifacts.NeedsInternshipSLM {
		return false
	}
	if normalizeRoleDecision(artifacts.RoleDecision) != roleDecisionIn {
		return false
	}
	return normalizeSLMWorkAuth(artifacts.WorkAuthStatus) != "blocked"
}

func selectSLMTargetIndexes(jobs []DashboardJob, requestLimit int, options ...slmScoringOptions) ([]int, map[int][]int) {
	resolved := resolveSLMScoringOptions(options...)
	maxJobs := slmScoringMaxJobs()
	scoreBudget := maxJobs
	if requestLimit > 0 {
		candidate := requestLimit + 5
		if candidate < 8 {
			candidate = 8
		}
		if candidate < scoreBudget {
			scoreBudget = candidate
		}
	}

	indexes := make([]int, 0, len(jobs))
	for idx := range jobs {
		indexes = append(indexes, idx)
	}
	sort.Slice(indexes, func(i, j int) bool {
		left := jobs[indexes[i]]
		right := jobs[indexes[j]]
		if left.MatchScore == right.MatchScore {
			return dashboardFreshnessLess(left, right, true)
		}
		return left.MatchScore > right.MatchScore
	})
	artifactsByIndex := make(map[int]jobsFeedHeuristicArtifacts, len(indexes))
	for _, index := range indexes {
		artifactsByIndex[index] = jobsFeedHeuristicArtifactsForDashboard(jobs[index])
	}
	selected := make([]int, 0, minInt(len(indexes), scoreBudget))
	selectedSet := make(map[int]struct{}, len(indexes))
	addIndex := func(index int) {
		if _, exists := selectedSet[index]; exists {
			return
		}
		selected = append(selected, index)
		selectedSet[index] = struct{}{}
	}
	for _, index := range indexes {
		if artifactsByIndex[index].NeedsInternshipSLM {
			addIndex(index)
		}
	}
	for _, index := range indexes {
		if len(selected) >= scoreBudget {
			break
		}
		if artifactsByIndex[index].NeedsRoleSLM {
			addIndex(index)
		}
	}
	if slmScoringRerankTopMatches() {
		for _, index := range indexes {
			if len(selected) >= scoreBudget {
				break
			}
			if dashboardJobEligibleForSLMRerank(jobs[index]) {
				addIndex(index)
			}
		}
	}
	indexes = selected
	primaryIndexByKey := map[string]int{}
	duplicateIndexesByPrimary := map[int][]int{}
	targetIndexes := make([]int, 0, len(indexes))
	for _, index := range indexes {
		jobOptions := resolved.withTask(slmTaskForDashboardJob(jobs[index]))
		key := slmCacheKey(dashboardAsSourceJob(jobs[index]), jobOptions)
		if primary, exists := primaryIndexByKey[key]; exists {
			duplicateIndexesByPrimary[primary] = append(duplicateIndexesByPrimary[primary], index)
			continue
		}
		primaryIndexByKey[key] = index
		targetIndexes = append(targetIndexes, index)
	}
	return targetIndexes, duplicateIndexesByPrimary
}

func applyCachedSLMMatchScoresWithProgress(
	jobs []DashboardJob,
	sortBy string,
	requestLimit int,
	progress func(slmApplyProgress),
	options ...slmScoringOptions,
) slmApplySummary {
	resolved := resolveSLMScoringOptions(options...)
	summary := slmApplySummary{EligibleJobs: len(jobs)}
	if !slmScoringEnabled() || len(jobs) == 0 {
		summary.Skipped = true
		if !slmScoringEnabled() {
			summary.SkipReason = "disabled"
		} else {
			summary.SkipReason = "no-jobs"
		}
		return summary
	}
	if slmScoringOnlyBestMatch() && strings.ToLower(strings.TrimSpace(sortBy)) != "best_match" {
		summary.Skipped = true
		summary.SkipReason = "sort-not-best-match"
		return summary
	}
	if slmScoringMaxJobs() <= 0 {
		summary.Skipped = true
		summary.SkipReason = "max-jobs-zero"
		return summary
	}

	targetIndexes, duplicateIndexesByPrimary := selectSLMTargetIndexes(jobs, requestLimit, resolved)
	prepared := make([]resultRow, 0, len(targetIndexes))
	for _, index := range targetIndexes {
		jobOptions := resolved.withTask(slmTaskForDashboardJob(jobs[index]))
		scored, ok, err := slmScoreJobCached(dashboardAsSourceJob(jobs[index]), jobOptions)
		if err != nil || !ok {
			summary.CacheMissJobs++
			if err != nil && slmScoringDebugEnabled() {
				log.Printf("[slm-scoring] cache lookup failed index=%d title=%q err=%v", index, jobs[index].Title, err)
			}
			continue
		}
		score, reasons := mergeSLMScoreWithGuardrails(jobs[index], scored)
		prepared = append(prepared, resultRow{
			index:            index,
			score:            score,
			reasons:          reasons,
			roleDecision:     resolvedSLMRoleDecision(jobs[index], scored),
			internshipStatus: scored.InternshipStatus,
			ok:               true,
		})
	}

	summary.ScheduledJobs = len(prepared)
	if progress != nil {
		progress(slmApplyProgress{
			EligibleJobs:  summary.EligibleJobs,
			ScheduledJobs: summary.ScheduledJobs,
			QueuedJobs:    summary.ScheduledJobs,
			CompletedJobs: 0,
			SuccessJobs:   0,
			FailedJobs:    0,
		})
	}
	if len(prepared) == 0 {
		summary.Skipped = true
		if summary.CacheMissJobs > 0 {
			summary.SkipReason = "cache-miss"
		} else {
			summary.SkipReason = "score-budget-zero"
		}
		return summary
	}

	for _, row := range prepared {
		summary.CompletedJobs++
		summary.SuccessJobs++
		jobs[row.index].MatchScore = row.score
		jobs[row.index].MatchReasons = row.reasons
		applySLMDecisionMetadata(&jobs[row.index], row.roleDecision, row.internshipStatus)
		if row.internshipStatus == "blocked" {
			jobs[row.index].RecommendedResume = ""
		}
		for _, duplicateIndex := range duplicateIndexesByPrimary[row.index] {
			jobs[duplicateIndex].MatchScore = row.score
			jobs[duplicateIndex].MatchReasons = append([]string(nil), row.reasons...)
			applySLMDecisionMetadata(&jobs[duplicateIndex], row.roleDecision, row.internshipStatus)
			if row.internshipStatus == "blocked" {
				jobs[duplicateIndex].RecommendedResume = ""
			}
		}
		if progress != nil {
			progress(slmApplyProgress{
				EligibleJobs:  summary.EligibleJobs,
				ScheduledJobs: summary.ScheduledJobs,
				QueuedJobs:    max(0, summary.ScheduledJobs-summary.CompletedJobs),
				CompletedJobs: summary.CompletedJobs,
				SuccessJobs:   summary.SuccessJobs,
				FailedJobs:    summary.FailedJobs,
			})
		}
	}
	return summary
}

func applySLMMatchScoresWithProgress(
	jobs []DashboardJob,
	sortBy string,
	requestLimit int,
	progress func(slmApplyProgress),
	options ...slmScoringOptions,
) slmApplySummary {
	resolved := resolveSLMScoringOptions(options...)
	summary := slmApplySummary{EligibleJobs: len(jobs)}
	if !slmScoringEnabled() || len(jobs) == 0 {
		summary.Skipped = true
		if !slmScoringEnabled() {
			summary.SkipReason = "disabled"
		} else {
			summary.SkipReason = "no-jobs"
		}
		if slmScoringDebugEnabled() {
			log.Printf("[slm-scoring] skipped enabled=%t jobs=%d", slmScoringEnabled(), len(jobs))
		}
		return summary
	}
	if slmScoringOnlyBestMatch() && strings.ToLower(strings.TrimSpace(sortBy)) != "best_match" {
		summary.Skipped = true
		summary.SkipReason = "sort-not-best-match"
		if slmScoringDebugEnabled() {
			log.Printf("[slm-scoring] skipped due to sort=%q", sortBy)
		}
		return summary
	}
	maxJobs := slmScoringMaxJobs()
	if maxJobs <= 0 {
		summary.Skipped = true
		summary.SkipReason = "max-jobs-zero"
		if slmScoringDebugEnabled() {
			log.Printf("[slm-scoring] skipped due to max_jobs=%d", maxJobs)
		}
		return summary
	}
	targetIndexes, duplicateIndexesByPrimary := selectSLMTargetIndexes(jobs, requestLimit, resolved)
	summary.ScheduledJobs = len(targetIndexes)
	if progress != nil {
		progress(slmApplyProgress{
			EligibleJobs:  summary.EligibleJobs,
			ScheduledJobs: summary.ScheduledJobs,
			QueuedJobs:    summary.ScheduledJobs,
			CompletedJobs: 0,
			SuccessJobs:   0,
			FailedJobs:    0,
		})
	}
	if slmScoringDebugEnabled() {
		log.Printf(
			"[slm-scoring] scoring_count=%d deduped_from=%d total_jobs=%d model=%s timeout=%s workers=%d",
			len(targetIndexes),
			len(targetIndexes),
			len(jobs),
			resolved.effectiveModelLabel(),
			slmScoringTimeout(),
			slmScoringWorkers(),
		)
	}
	if len(targetIndexes) == 0 {
		summary.Skipped = true
		summary.SkipReason = "score-budget-zero"
		return summary
	}

	workers := slmScoringWorkers()
	if workers > len(targetIndexes) {
		workers = len(targetIndexes)
	}

	indexCh := make(chan int, len(targetIndexes))
	resultCh := make(chan resultRow, len(targetIndexes))
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range indexCh {
				job := dashboardAsSourceJob(jobs[index])
				jobOptions := resolved.withTask(slmTaskForDashboardJob(jobs[index]))
				scored, err := slmScoreJob(job, jobOptions)
				if err != nil {
					resultCh <- resultRow{
						index:        index,
						ok:           false,
						errorMessage: normalizeTextSnippet(err.Error(), 120),
					}
					continue
				}
				score, reasons := mergeSLMScoreWithGuardrails(jobs[index], scored)
				resultCh <- resultRow{
					index:            index,
					score:            score,
					reasons:          reasons,
					roleDecision:     resolvedSLMRoleDecision(jobs[index], scored),
					internshipStatus: scored.InternshipStatus,
					ok:               true,
				}
			}
		}()
	}
	for _, index := range targetIndexes {
		indexCh <- index
	}
	close(indexCh)
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for row := range resultCh {
		summary.CompletedJobs++
		if !row.ok {
			summary.FailedJobs++
			if slmScoringDebugEnabled() {
				log.Printf("[slm-scoring] fallback index=%d title=%q err=%s", row.index, jobs[row.index].Title, row.errorMessage)
			}
			if slmScoringDebugEnabled() {
				reasonSet := map[string]struct{}{}
				merged := make([]string, 0, 4)
				for _, reason := range jobs[row.index].MatchReasons {
					merged = appendUniqueReason(merged, reasonSet, reason)
				}
				if row.errorMessage != "" {
					merged = appendUniqueReason(merged, reasonSet, "SLM fallback: "+row.errorMessage)
				} else {
					merged = appendUniqueReason(merged, reasonSet, "SLM fallback")
				}
				if len(merged) > 4 {
					merged = merged[:4]
				}
				jobs[row.index].MatchReasons = merged
			}
		} else {
			summary.SuccessJobs++
			jobs[row.index].MatchScore = row.score
			jobs[row.index].MatchReasons = row.reasons
			applySLMDecisionMetadata(&jobs[row.index], row.roleDecision, row.internshipStatus)
			if row.internshipStatus == "blocked" {
				jobs[row.index].RecommendedResume = ""
			}
			for _, duplicateIndex := range duplicateIndexesByPrimary[row.index] {
				jobs[duplicateIndex].MatchScore = row.score
				jobs[duplicateIndex].MatchReasons = append([]string(nil), row.reasons...)
				applySLMDecisionMetadata(&jobs[duplicateIndex], row.roleDecision, row.internshipStatus)
				if row.internshipStatus == "blocked" {
					jobs[duplicateIndex].RecommendedResume = ""
				}
			}
			if slmScoringDebugEnabled() {
				duplicateCount := len(duplicateIndexesByPrimary[row.index])
				if duplicateCount > 0 {
					log.Printf("[slm-scoring] applied index=%d (+%d duplicate) title=%q score=%d", row.index, duplicateCount, jobs[row.index].Title, row.score)
				} else {
					log.Printf("[slm-scoring] applied index=%d title=%q score=%d", row.index, jobs[row.index].Title, row.score)
				}
			}
		}
		if progress != nil {
			progress(slmApplyProgress{
				EligibleJobs:  summary.EligibleJobs,
				ScheduledJobs: summary.ScheduledJobs,
				QueuedJobs:    max(0, summary.ScheduledJobs-summary.CompletedJobs),
				CompletedJobs: summary.CompletedJobs,
				SuccessJobs:   summary.SuccessJobs,
				FailedJobs:    summary.FailedJobs,
			})
		}
	}
	return summary
}
