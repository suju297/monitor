package monitor

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type ResumeProfile struct {
	Slug          string   `yaml:"slug"`
	Name          string   `yaml:"name"`
	ResumeFile    string   `yaml:"resume_file"`
	Summary       string   `yaml:"summary"`
	FocusKeywords []string `yaml:"focus_keywords"`
	RoleKeywords  []string `yaml:"role_keywords"`
	StackKeywords []string `yaml:"stack_keywords"`

	distinctiveTokens  []string
	resumeText         string
	resolvedResumeFile string
}

type CandidatePreferences struct {
	Graduated                        bool   `yaml:"graduated"`
	GraduationDate                   string `yaml:"graduation_date"`
	InterestedInInternships          bool   `yaml:"interested_in_internships"`
	InternshipsRequirePostGradSignal bool   `yaml:"internships_require_postgrad_signal"`
}

type resumeProfilesConfig struct {
	Candidate CandidatePreferences `yaml:"candidate"`
	Profiles  []ResumeProfile      `yaml:"profiles"`
}

type resumeVariantMatch struct {
	Profile         ResumeProfile
	Score           int
	FocusHits       []string
	RoleHits        []string
	StackHits       []string
	DistinctiveHits []string
}

type jobMatchSummary struct {
	MatchScore        int
	MatchReasons      []string
	RecommendedResume string
}

type internshipEligibility struct {
	IsInternship bool
	Status       string
	Reason       string
}

var (
	resumeTokenRE                  = regexp.MustCompile(`[a-z0-9][a-z0-9+.#/-]*`)
	resumeHardExcludeRE            = regexp.MustCompile(`(?i)\b(?:qa|quality assurance|sdet|software developer in test|developer in test|test engineer|test automation|technician|helpdesk|desktop support|it support|network operations|data center operations|hardware validation)\b`)
	internshipRoleRE               = regexp.MustCompile(`(?i)\b(?:intern|internship|co[- ]?op)\b`)
	internshipEnrollmentRequiredRE = regexp.MustCompile(`(?i)\b(?:currently enrolled|current enrollment|must be enrolled|must be currently enrolled|actively enrolled|be enrolled in|enrolled in (?:a|an|the)?\s*(?:course|coursework|course of study|bachelor'?s|masters?|master'?s|phd|degree|program|university|college)|pursuing (?:a|an|the)?\s*(?:bachelor'?s|masters?|master'?s|phd|degree|program)|working toward (?:a|an|the)?\s*(?:bachelor'?s|masters?|master'?s|phd|degree|program)|must be pursuing|full[- ]?time student|enrolled full[- ]?time|matriculated student|student status|required student status|return(?:ing)? to school|return(?:ing)? to (?:a )?(?:degree|university|college|graduate) program|continuing student|continue your studies|expected graduation|graduate between|semester[s]? remaining)\b`)
	internshipPostGradAllowedRE    = regexp.MustCompile(`(?i)\b(?:recent graduates?|recently graduated|graduated within|degree completed|completed (?:your|a|their) degree|open to graduates?|graduates? are eligible|post[- ]?graduate internship|internship for graduates?|new graduates? eligible|no current enrollment required|not currently enrolled)\b`)
	resumeProfileStopwords         = map[string]struct{}{
		"about": {}, "across": {}, "adults": {}, "agent": {}, "agents": {}, "analytics": {}, "applied": {},
		"application": {}, "applications": {}, "architected": {}, "backend": {}, "building": {},
		"candidate": {}, "cloud": {}, "code": {}, "college": {}, "company": {}, "core": {}, "data": {},
		"delivered": {}, "deployment": {}, "deployments": {}, "designed": {}, "distributed": {},
		"documentclass": {}, "usepackage": {}, "begin": {}, "end": {}, "href": {}, "hfill": {}, "vspace": {},
		"education": {}, "engineer": {}, "engineers": {}, "engineering": {}, "experience": {},
		"focused": {}, "focus": {}, "frameworks": {}, "github": {}, "infrastructure": {},
		"institute": {}, "kubernetes": {}, "languages": {}, "learning": {}, "letterpaper": {}, "llm": {},
		"large": {}, "linkedin": {}, "mailto": {}, "article": {}, "utf8": {},
		"machine": {}, "management": {}, "monitor": {}, "older": {}, "platform": {},
		"production": {}, "profile": {}, "python": {}, "resume": {}, "scale": {}, "service": {},
		"services": {}, "software": {}, "stack": {}, "streaming": {}, "summary": {},
		"support": {}, "systems": {}, "technical": {}, "technology": {}, "tools": {},
		"university": {}, "using": {}, "with": {}, "workflow": {}, "workflows": {},
	}
	resumeProfilesCache struct {
		mu           sync.RWMutex
		path         string
		signature    string
		watchedPaths []string
		candidate    CandidatePreferences
		profiles     []ResumeProfile
	}
)

func resumeProfilesFilePath() string {
	raw := strings.TrimSpace(os.Getenv("RESUME_PROFILES_FILE"))
	if raw != "" {
		return preferredLocalFile(raw, filepath.Base(raw))
	}
	return preferredLocalFile("resume_profiles.yaml", "resume_profiles.yaml")
}

func loadResumeProfiles() []ResumeProfile {
	path := resumeProfilesFilePath()

	resumeProfilesCache.mu.RLock()
	if resumeProfilesCache.path == path &&
		resumeProfilesCache.signature != "" &&
		resumeProfilesCache.signature == watchedFilesSignature(resumeProfilesCache.watchedPaths) {
		profiles := append([]ResumeProfile(nil), resumeProfilesCache.profiles...)
		resumeProfilesCache.mu.RUnlock()
		return profiles
	}
	resumeProfilesCache.mu.RUnlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		resumeProfilesCache.mu.Lock()
		resumeProfilesCache.path = path
		resumeProfilesCache.signature = ""
		resumeProfilesCache.watchedPaths = nil
		resumeProfilesCache.candidate = CandidatePreferences{}
		resumeProfilesCache.profiles = nil
		resumeProfilesCache.mu.Unlock()
		return nil
	}

	config := resumeProfilesConfig{}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		resumeProfilesCache.mu.Lock()
		resumeProfilesCache.path = path
		resumeProfilesCache.signature = ""
		resumeProfilesCache.watchedPaths = nil
		resumeProfilesCache.candidate = CandidatePreferences{}
		resumeProfilesCache.profiles = nil
		resumeProfilesCache.mu.Unlock()
		return nil
	}

	profiles := normalizeResumeProfiles(config.Profiles, path)
	watchedPaths := []string{path}
	for _, profile := range profiles {
		if profile.resolvedResumeFile != "" {
			watchedPaths = append(watchedPaths, profile.resolvedResumeFile)
		}
	}
	resumeProfilesCache.mu.Lock()
	resumeProfilesCache.path = path
	resumeProfilesCache.signature = watchedFilesSignature(watchedPaths)
	resumeProfilesCache.watchedPaths = watchedPaths
	resumeProfilesCache.candidate = config.Candidate
	resumeProfilesCache.profiles = profiles
	resumeProfilesCache.mu.Unlock()
	return append([]ResumeProfile(nil), profiles...)
}

func resetResumeProfilesCacheForTests() {
	resumeProfilesCache.mu.Lock()
	defer resumeProfilesCache.mu.Unlock()
	resumeProfilesCache.path = ""
	resumeProfilesCache.signature = ""
	resumeProfilesCache.watchedPaths = nil
	resumeProfilesCache.candidate = CandidatePreferences{}
	resumeProfilesCache.profiles = nil
}

func loadCandidatePreferences() CandidatePreferences {
	_ = loadResumeProfiles()
	resumeProfilesCache.mu.RLock()
	defer resumeProfilesCache.mu.RUnlock()
	return resumeProfilesCache.candidate
}

func currentResumeProfilesSignature() string {
	_ = loadResumeProfiles()
	resumeProfilesCache.mu.RLock()
	defer resumeProfilesCache.mu.RUnlock()
	return strings.TrimSpace(resumeProfilesCache.signature)
}

func normalizeResumeProfiles(rawProfiles []ResumeProfile, configPath string) []ResumeProfile {
	normalized := make([]ResumeProfile, 0, len(rawProfiles))
	tokenSets := make([]map[string]struct{}, 0, len(rawProfiles))
	docFreq := map[string]int{}

	for _, raw := range rawProfiles {
		resolvedResumeFile := resolveResumeFilePath(configPath, raw.ResumeFile)
		profile := ResumeProfile{
			Slug:               strings.TrimSpace(raw.Slug),
			Name:               strings.TrimSpace(raw.Name),
			ResumeFile:         strings.TrimSpace(raw.ResumeFile),
			Summary:            normalizeTextSnippet(raw.Summary, 280),
			FocusKeywords:      normalizeResumePhraseList(raw.FocusKeywords),
			RoleKeywords:       normalizeResumePhraseList(raw.RoleKeywords),
			StackKeywords:      normalizeResumePhraseList(raw.StackKeywords),
			resumeText:         loadResumeProfileText(resolvedResumeFile),
			resolvedResumeFile: resolvedResumeFile,
		}
		if profile.Name == "" {
			continue
		}
		tokenSet := resumeProfileTokenSet(profile)
		for token := range tokenSet {
			docFreq[token]++
		}
		tokenSets = append(tokenSets, tokenSet)
		normalized = append(normalized, profile)
	}

	for index := range normalized {
		tokens := make([]string, 0, len(tokenSets[index]))
		for token := range tokenSets[index] {
			if docFreq[token] == 1 {
				tokens = append(tokens, token)
			}
		}
		sort.Slice(tokens, func(i int, j int) bool {
			if len(tokens[i]) == len(tokens[j]) {
				return tokens[i] < tokens[j]
			}
			return len(tokens[i]) > len(tokens[j])
		})
		if len(tokens) > 18 {
			tokens = tokens[:18]
		}
		normalized[index].distinctiveTokens = tokens
	}

	return normalized
}

func resolveResumeFilePath(configPath string, rawPath string) string {
	value := strings.TrimSpace(rawPath)
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return value
	}
	baseDir := commandWorkingDir()
	if strings.TrimSpace(configPath) != "" {
		baseDir = filepath.Dir(configPath)
	}
	return filepath.Join(baseDir, value)
}

func watchedFilesSignature(paths []string) string {
	paths = uniqueStrings(paths)
	if len(paths) == 0 {
		return ""
	}
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			parts = append(parts, path+"|missing")
			continue
		}
		parts = append(parts, strings.Join([]string{
			path,
			info.ModTime().UTC().Format(time.RFC3339Nano),
			strconv.FormatInt(info.Size(), 10),
		}, "|"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "||")
}

func stripLatexComments(raw string) string {
	lines := strings.Split(raw, "\n")
	for index, line := range lines {
		for pos := 0; pos < len(line); pos++ {
			if line[pos] != '%' {
				continue
			}
			if pos > 0 && line[pos-1] == '\\' {
				continue
			}
			line = line[:pos]
			break
		}
		lines[index] = line
	}
	return strings.Join(lines, "\n")
}

func latexToPlainText(raw string) string {
	text := stripLatexComments(raw)
	replacements := []struct {
		pattern string
		repl    string
	}{
		{`\\href\s*\{[^{}]*\}\s*\{([^{}]*)\}`, ` $1 `},
		{`\\(?:textbf|textit|emph|uline|underline|textsc|texttt|section\*?|subsection\*?|subsubsection\*?|large|Large)\s*\{([^{}]*)\}`, ` $1 `},
		{`\\begin\{[^{}]*\}`, ` `},
		{`\\end\{[^{}]*\}`, ` `},
		{`https?://[^\s}]+`, ` `},
		{`mailto:[^\s}]+`, ` `},
		{`\\[a-zA-Z@]+\*?(?:\[[^\]]*\])?`, ` `},
		{`[{}$&_^~]`, ` `},
		{`\\`, ` `},
	}
	for _, replacement := range replacements {
		re := regexp.MustCompile(replacement.pattern)
		text = re.ReplaceAllString(text, replacement.repl)
	}
	text = strings.ReplaceAll(text, "|", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.Join(strings.Fields(text), " ")
}

func loadResumeProfileText(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return normalizeTextSnippet(latexToPlainText(string(raw)), 12000)
}

func normalizeResumePhraseList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.ToLower(normalizeTextSnippet(value, 120))
		normalized = strings.Join(strings.Fields(normalized), " ")
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func resumeProfileTokenSet(profile ResumeProfile) map[string]struct{} {
	values := []string{profile.Summary, profile.resumeText}
	values = append(values, profile.FocusKeywords...)
	values = append(values, profile.RoleKeywords...)
	values = append(values, profile.StackKeywords...)

	out := map[string]struct{}{}
	for _, value := range values {
		for _, token := range resumeTokenRE.FindAllString(strings.ToLower(value), -1) {
			if !isMeaningfulResumeToken(token) {
				continue
			}
			out[token] = struct{}{}
		}
	}
	return out
}

func isMeaningfulResumeToken(token string) bool {
	if len(token) < 3 {
		return false
	}
	if _, blocked := resumeProfileStopwords[token]; blocked {
		return false
	}
	if strings.Count(token, ".") > 1 || strings.Count(token, "/") > 1 {
		return false
	}
	return true
}

func matchPhrases(text string, phrases []string, limit int) []string {
	if text == "" || len(phrases) == 0 || limit == 0 {
		return nil
	}
	out := make([]string, 0, minInt(limit, len(phrases)))
	seen := map[string]struct{}{}
	for _, phrase := range phrases {
		if phrase == "" || !resumePhraseMatchesText(text, phrase) {
			continue
		}
		if _, exists := seen[phrase]; exists {
			continue
		}
		seen[phrase] = struct{}{}
		out = append(out, phrase)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func resumePhraseMatchesText(text string, phrase string) bool {
	pattern := `(^|[^a-z0-9])` + regexp.QuoteMeta(phrase) + `($|[^a-z0-9])`
	matched, err := regexp.MatchString(pattern, text)
	if err != nil {
		return false
	}
	return matched
}

func tokenizeJobText(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, token := range resumeTokenRE.FindAllString(strings.ToLower(text), -1) {
		if !isMeaningfulResumeToken(token) {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

func matchDistinctiveResumeTokens(jobText string, profile ResumeProfile, limit int) []string {
	if jobText == "" || len(profile.distinctiveTokens) == 0 || limit == 0 {
		return nil
	}
	jobTokens := tokenizeJobText(jobText)
	out := make([]string, 0, minInt(limit, len(profile.distinctiveTokens)))
	for _, token := range profile.distinctiveTokens {
		if _, exists := jobTokens[token]; !exists {
			continue
		}
		out = append(out, token)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func scoreJobAgainstResumeProfile(job Job, profile ResumeProfile) resumeVariantMatch {
	titleText := strings.ToLower(normalizeTextSnippet(job.Title, 320))
	teamText := strings.ToLower(normalizeTextSnippet(job.Team, 260))
	descriptionText := strings.ToLower(normalizeTextSnippet(job.Description, 2400))
	contextText := strings.Join([]string{
		titleText,
		teamText,
		descriptionText,
		strings.ToLower(normalizeTextSnippet(job.Location, 160)),
		strings.ToLower(strings.TrimSpace(job.URL)),
	}, " ")
	titleAndTeam := strings.TrimSpace(strings.Join([]string{titleText, teamText}, " "))

	roleHits := matchPhrases(titleAndTeam, profile.RoleKeywords, 2)
	if len(roleHits) == 0 {
		roleHits = matchPhrases(contextText, profile.RoleKeywords, 1)
	}
	focusHits := matchPhrases(contextText, profile.FocusKeywords, 4)
	stackHits := matchPhrases(contextText, profile.StackKeywords, 4)
	distinctiveHits := matchDistinctiveResumeTokens(contextText, profile, 3)

	score := 0
	score += minInt(24, len(roleHits)*12)
	score += minInt(22, len(focusHits)*6)
	score += minInt(10, len(stackHits)*3)
	score += minInt(9, len(distinctiveHits)*3)
	if score > 58 {
		score = 58
	}

	return resumeVariantMatch{
		Profile:         profile,
		Score:           score,
		FocusHits:       focusHits,
		RoleHits:        roleHits,
		StackHits:       stackHits,
		DistinctiveHits: distinctiveHits,
	}
}

func bestResumeVariantMatch(job Job) (resumeVariantMatch, bool) {
	profiles := loadResumeProfiles()
	if len(profiles) == 0 {
		return resumeVariantMatch{}, false
	}
	best := resumeVariantMatch{}
	found := false
	for _, profile := range profiles {
		match := scoreJobAgainstResumeProfile(job, profile)
		if !found || match.Score > best.Score {
			best = match
			found = true
			continue
		}
		if match.Score == best.Score {
			leftHits := len(match.FocusHits) + len(match.RoleHits) + len(match.StackHits)
			rightHits := len(best.FocusHits) + len(best.RoleHits) + len(best.StackHits)
			if leftHits > rightHits {
				best = match
				found = true
			}
		}
	}
	return best, found
}

func collectResumeMatchTerms(match resumeVariantMatch) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	for _, group := range [][]string{match.FocusHits, match.RoleHits, match.StackHits, match.DistinctiveHits} {
		for _, value := range group {
			normalized := strings.TrimSpace(value)
			if normalized == "" {
				continue
			}
			if _, exists := seen[normalized]; exists {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out
}

func resumeAwarePotentialMatch(job Job) bool {
	candidate := loadCandidatePreferences()
	internship := internshipEligibilityForCandidate(job, candidate)
	if internship.IsInternship {
		if !candidate.InterestedInInternships {
			return false
		}
		if internship.Status == "blocked" {
			return false
		}
		if internship.Status == "unknown" && candidate.InternshipsRequirePostGradSignal && !shouldReviewAmbiguousInternshipWithSLM(candidate) {
			return false
		}
	}
	title := strings.ToLower(normalizeTextSnippet(job.Title, 320))
	team := strings.ToLower(normalizeTextSnippet(job.Team, 260))
	description := strings.ToLower(normalizeTextSnippet(job.Description, 2200))
	combined := strings.TrimSpace(strings.Join([]string{title, team, description}, " "))
	if combined == "" {
		return false
	}
	if resumeHardExcludeRE.MatchString(title) {
		return false
	}
	best, ok := bestResumeVariantMatch(job)
	if ok && best.Score >= 10 {
		return true
	}
	if techRoleWordRE.MatchString(title) && (techContextRE.MatchString(combined) || earlyCareerRE.MatchString(title)) {
		return true
	}
	return false
}

func jobMatchSummaryForJob(job Job) jobMatchSummary {
	if len(loadResumeProfiles()) == 0 {
		score, reasons := legacyJobMatchScoreAndReasons(job)
		return jobMatchSummary{
			MatchScore:   score,
			MatchReasons: reasons,
		}
	}

	best, ok := bestResumeVariantMatch(job)
	if !ok {
		score, reasons := legacyJobMatchScoreAndReasons(job)
		return jobMatchSummary{
			MatchScore:   score,
			MatchReasons: reasons,
		}
	}

	score := best.Score
	candidate := loadCandidatePreferences()
	internship := internshipEligibilityForCandidate(job, candidate)
	reasons := make([]string, 0, 4)
	reasonSet := map[string]struct{}{}
	if best.Score >= 14 {
		reasons = appendUniqueReason(reasons, reasonSet, "Best resume: "+best.Profile.Name)
	}

	matchTerms := collectResumeMatchTerms(best)
	if len(matchTerms) > 0 {
		label := "Matched keywords: "
		if best.Score < 18 {
			label = "Broad overlap: "
		}
		reasons = appendUniqueReason(reasons, reasonSet, label+strings.Join(matchTerms[:minInt(3, len(matchTerms))], ", "))
	}

	title := strings.ToLower(normalizeTextSnippet(job.Title, 320))
	switch {
	case internship.IsInternship && internship.Status == "allowed":
		score += 14
		reasons = appendUniqueReason(reasons, reasonSet, internship.Reason)
	case internship.IsInternship && internship.Status == "blocked":
		score -= 40
		reasons = appendUniqueReason(reasons, reasonSet, internship.Reason)
	case internship.IsInternship && internship.Status == "unknown" && candidate.InternshipsRequirePostGradSignal:
		if shouldReviewAmbiguousInternshipWithSLM(candidate) {
			score += 12
			reasons = appendUniqueReason(reasons, reasonSet, "Internship eligibility needs SLM review for student-status requirements")
		} else {
			score -= 18
			reasons = appendUniqueReason(reasons, reasonSet, internship.Reason)
		}
	case internship.IsInternship:
		score += 10
		reasons = appendUniqueReason(reasons, reasonSet, "Internship role")
	case earlyCareerRE.MatchString(title):
		score += 18
		reasons = appendUniqueReason(reasons, reasonSet, "Early-career friendly level")
	case engineerLevelRE.MatchString(title):
		score += 17
		reasons = appendUniqueReason(reasons, reasonSet, "Engineer level within target range")
	case seniorityBlockedRE.MatchString(title) || roleLevelTooHighRE.MatchString(title):
		score += 4
		reasons = appendUniqueReason(reasons, reasonSet, "Role appears more senior than target")
	case techRoleWordRE.MatchString(title):
		score += 12
		reasons = appendUniqueReason(reasons, reasonSet, "Relevant engineering role title")
	default:
		score += 7
	}
	if internship.IsInternship && (strings.Contains(title, "engineering") || techRoleWordRE.MatchString(title)) {
		score += 8
		reasons = appendUniqueReason(reasons, reasonSet, "Engineering internship title")
	}

	if posted, ok := parsePostedTime(job.PostedAt); ok {
		now := time.Now()
		startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		postedLocal := posted.In(now.Location())
		daysOld := int(startToday.Sub(time.Date(postedLocal.Year(), postedLocal.Month(), postedLocal.Day(), 0, 0, 0, 0, now.Location())).Hours() / 24)
		switch {
		case daysOld <= 0:
			score += 14
			reasons = appendUniqueReason(reasons, reasonSet, "Posted today")
		case daysOld == 1:
			score += 8
			reasons = appendUniqueReason(reasons, reasonSet, "Posted yesterday")
		case daysOld <= 7:
			score += 3
		}
	}

	contextScore := 0
	if hasStrongJobPathHint(job.URL) {
		contextScore += 3
	}
	if hasDescriptionJobMarker(job.Description) {
		contextScore += 3
	}
	if len(strings.Fields(strings.TrimSpace(job.Description))) >= 45 {
		contextScore += 2
	}
	if isUSBasedJob(job) {
		contextScore += 2
	}
	if contextScore > 8 {
		contextScore = 8
	}
	score += contextScore

	if nonTargetFunctionRE.MatchString(title) {
		score -= 28
		reasons = appendUniqueReason(reasons, reasonSet, "Non-target function")
	}
	if resumeHardExcludeRE.MatchString(title) {
		score -= 24
		reasons = appendUniqueReason(reasons, reasonSet, "Test / technician-heavy role")
	}

	workAuthStatus, workAuthNotes := workAuthStatusAndNotes(job)
	switch workAuthStatus {
	case "blocked":
		score -= 36
		reasons = appendUniqueReason(reasons, reasonSet, "F1 OPT constraint detected")
		if len(workAuthNotes) > 0 {
			reasons = appendUniqueReason(reasons, reasonSet, workAuthNotes[0])
		}
	case "friendly":
		score += 7
		if len(workAuthNotes) > 0 {
			reasons = appendUniqueReason(reasons, reasonSet, workAuthNotes[0])
		}
	}

	score = clampScore(score)
	if len(reasons) == 0 && strings.TrimSpace(best.Profile.Name) != "" {
		reasons = append(reasons, "Best resume: "+best.Profile.Name)
	}
	if len(reasons) > 4 {
		reasons = reasons[:4]
	}

	recommendedResume := ""
	if best.Score >= 10 || (internship.IsInternship && shouldReviewAmbiguousInternshipWithSLM(candidate) && best.Score >= 6) {
		recommendedResume = best.Profile.Name
	}
	if internship.IsInternship && (internship.Status == "blocked" || (internship.Status == "unknown" && candidate.InternshipsRequirePostGradSignal && !shouldReviewAmbiguousInternshipWithSLM(candidate))) {
		recommendedResume = ""
	}

	return jobMatchSummary{
		MatchScore:        score,
		MatchReasons:      reasons,
		RecommendedResume: recommendedResume,
	}
}

func resumeProfilesPromptBlock() string {
	profiles := loadResumeProfiles()
	candidate := loadCandidatePreferences()
	if len(profiles) == 0 {
		return ""
	}
	lines := []string{
		"Candidate has multiple truthful resume variants. Score the job against the single best-matching variant and include one reason exactly in the form `Best resume: <variant>`.",
		candidatePromptLine(candidate),
		"Resume variants:",
	}
	for _, profile := range profiles {
		line := "- " + profile.Name + ": " + profile.Summary
		if len(profile.FocusKeywords) > 0 {
			line += " Focus: " + strings.Join(profile.FocusKeywords[:minInt(5, len(profile.FocusKeywords))], ", ")
		}
		if len(profile.RoleKeywords) > 0 {
			line += ". Target titles: " + strings.Join(profile.RoleKeywords[:minInt(3, len(profile.RoleKeywords))], ", ")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func internshipEligibilityForCandidate(job Job, candidate CandidatePreferences) internshipEligibility {
	title := strings.ToLower(normalizeTextSnippet(job.Title, 320))
	team := strings.ToLower(normalizeTextSnippet(job.Team, 220))
	description := strings.ToLower(normalizeTextSnippet(job.Description, 2600))
	combined := strings.TrimSpace(strings.Join([]string{title, team, description}, " "))
	if !internshipRoleRE.MatchString(title) && !internshipRoleRE.MatchString(team) && !internshipRoleRE.MatchString(description) {
		return internshipEligibility{}
	}
	result := internshipEligibility{IsInternship: true, Status: "unknown"}
	switch {
	case internshipPostGradAllowedRE.MatchString(combined):
		result.Status = "allowed"
		result.Reason = "Internship appears open beyond current students"
	case internshipEnrollmentRequiredRE.MatchString(combined):
		result.Status = "blocked"
		if candidate.Graduated {
			result.Reason = "Internship requires current enrollment or returning to school"
		} else {
			result.Reason = "Internship has explicit student enrollment requirement"
		}
	default:
		result.Status = "unknown"
		if candidate.Graduated {
			result.Reason = "Internship does not clearly state whether current student status is required"
		} else {
			result.Reason = "Internship eligibility is unclear"
		}
	}
	return result
}

func candidatePromptLine(candidate CandidatePreferences) string {
	parts := make([]string, 0, 2)
	if candidate.Graduated {
		if candidate.GraduationDate != "" {
			parts = append(parts, "Candidate has already graduated ("+candidate.GraduationDate+").")
		} else {
			parts = append(parts, "Candidate has already graduated.")
		}
	}
	if candidate.InterestedInInternships {
		if candidate.InternshipsRequirePostGradSignal {
			parts = append(parts, "For internships, block only when the posting requires current enrollment, active degree pursuit, student status, or returning to school. Do not require explicit graduate-eligible wording.")
		} else {
			parts = append(parts, "Internships are allowed.")
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func shouldReviewAmbiguousInternshipWithSLM(candidate CandidatePreferences) bool {
	return candidate.Graduated &&
		candidate.InterestedInInternships &&
		candidate.InternshipsRequirePostGradSignal &&
		slmScoringEnabled()
}

func requiresSLMInternshipEligibilityReview(job Job, candidate CandidatePreferences) bool {
	internship := internshipEligibilityForCandidate(job, candidate)
	return internship.IsInternship &&
		internship.Status == "unknown" &&
		candidate.InternshipsRequirePostGradSignal &&
		shouldReviewAmbiguousInternshipWithSLM(candidate)
}
