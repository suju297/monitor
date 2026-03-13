package monitor

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
)

type jobsFeedHeuristicArtifacts struct {
	ContextHash         string
	RelevantForFeed     bool
	RoleDecision        string
	RoleDecisionReasons []string
	NeedsRoleSLM        bool
	WorkAuthStatus      string
	WorkAuthNotes       []string
	InternshipStatus    string
	NeedsInternshipSLM  bool
	MatchScore          int
	MatchReasons        []string
	RecommendedResume   string
}

var jobsFeedHeuristicCache = struct {
	mu    sync.RWMutex
	items map[string]jobsFeedHeuristicArtifacts
}{
	items: map[string]jobsFeedHeuristicArtifacts{},
}

func resetJobsFeedHeuristicCacheForTests() {
	jobsFeedHeuristicCache.mu.Lock()
	defer jobsFeedHeuristicCache.mu.Unlock()
	jobsFeedHeuristicCache.items = map[string]jobsFeedHeuristicArtifacts{}
}

func jobsFeedHeuristicContextHash() string {
	parts := []string{
		"decision=v4",
		"resume=" + currentResumeProfilesSignature(),
		"relevance=" + strconv.FormatBool(relevanceFilterEnabled()),
		"include=" + strings.TrimSpace(os.Getenv("RELEVANCE_INCLUDE_REGEX")),
		"exclude=" + strings.TrimSpace(os.Getenv("RELEVANCE_EXCLUDE_REGEX")),
		"f1=" + strconv.FormatBool(parseBoolEnv("F1_OPT_EXCLUDE_BLOCKED", false)),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func jobsFeedHeuristicCacheKey(job Job, contextHash string) string {
	return contextHash + "|" + jobFingerprint(job)
}

func jobsFeedHeuristicArtifactsForJob(job Job) jobsFeedHeuristicArtifacts {
	contextHash := jobsFeedHeuristicContextHash()
	cacheKey := jobsFeedHeuristicCacheKey(job, contextHash)

	jobsFeedHeuristicCache.mu.RLock()
	cached, ok := jobsFeedHeuristicCache.items[cacheKey]
	jobsFeedHeuristicCache.mu.RUnlock()
	if ok {
		return cached
	}

	workAuthStatus, workAuthNotes := workAuthStatusAndNotes(job)
	match := jobMatchSummaryForJob(job)
	roleDecision, roleReasons := deterministicRoleDecision(job)
	internship := internshipEligibilityForCandidate(job, loadCandidatePreferences())
	relevantForFeed := isRelevantCareerJobIgnorePostedRecency(job)
	if normalizeRoleDecision(roleDecision) == roleDecisionOut {
		relevantForFeed = false
		match.RecommendedResume = ""
		if match.MatchScore > 35 {
			match.MatchScore = 35
		}
	}
	artifacts := jobsFeedHeuristicArtifacts{
		ContextHash:         contextHash,
		RelevantForFeed:     relevantForFeed,
		RoleDecision:        roleDecision,
		RoleDecisionReasons: append([]string(nil), roleReasons...),
		NeedsRoleSLM:        needsDeterministicRoleSLMReview(job),
		WorkAuthStatus:      workAuthStatus,
		WorkAuthNotes:       append([]string(nil), workAuthNotes...),
		InternshipStatus:    normalizeSLMInternshipStatus(internship.Status),
		NeedsInternshipSLM:  requiresSLMInternshipEligibilityReview(job, loadCandidatePreferences()),
		MatchScore:          match.MatchScore,
		MatchReasons:        append([]string(nil), match.MatchReasons...),
		RecommendedResume:   match.RecommendedResume,
	}

	jobsFeedHeuristicCache.mu.Lock()
	jobsFeedHeuristicCache.items[cacheKey] = artifacts
	jobsFeedHeuristicCache.mu.Unlock()
	return artifacts
}

func jobsFeedHeuristicArtifactsForDashboard(job DashboardJob) jobsFeedHeuristicArtifacts {
	contextHash := jobsFeedHeuristicContextHash()
	if job.HeuristicCached && strings.TrimSpace(job.HeuristicContextHash) == contextHash {
		return jobsFeedHeuristicArtifacts{
			ContextHash:         contextHash,
			RelevantForFeed:     job.FeedRelevantCached,
			RoleDecision:        normalizeRoleDecision(job.DeterministicRoleDecision),
			RoleDecisionReasons: append([]string(nil), job.DeterministicRoleReasons...),
			NeedsRoleSLM:        job.NeedsRoleSLM,
			WorkAuthStatus:      strings.TrimSpace(job.WorkAuthStatus),
			WorkAuthNotes:       append([]string(nil), job.WorkAuthNotes...),
			InternshipStatus:    normalizeSLMInternshipStatus(job.DeterministicInternshipStatus),
			NeedsInternshipSLM:  job.NeedsInternshipSLM,
			MatchScore:          job.MatchScore,
			MatchReasons:        append([]string(nil), job.MatchReasons...),
			RecommendedResume:   strings.TrimSpace(job.RecommendedResume),
		}
	}
	return jobsFeedHeuristicArtifactsForJob(dashboardAsSourceJob(job))
}

func encodeStringSliceJSON(values []string) string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeStringSliceJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	normalized := out[:0]
	for _, value := range out {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
