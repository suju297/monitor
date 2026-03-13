package monitor

import (
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	usCountryRE   = regexp.MustCompile(`(?i)\b(?:united states(?: of america)?|u\.s\.a?\.|usa)\b`)
	usRemoteRE    = regexp.MustCompile(`(?i)\bremote\b(?:[^a-z0-9]{0,12})(?:u\.s\.a?\.|usa|united states|us)\b|\b(?:u\.s\.a?\.|usa|united states|us)\b(?:[^a-z0-9]{0,12})\bremote\b`)
	usStateNameRE = regexp.MustCompile(
		`(?i)\b(?:alabama|alaska|arizona|arkansas|california|colorado|connecticut|delaware|florida|georgia|hawaii|idaho|illinois|indiana|iowa|kansas|kentucky|louisiana|maine|maryland|massachusetts|michigan|minnesota|mississippi|missouri|montana|nebraska|nevada|new hampshire|new jersey|new mexico|new york|north carolina|north dakota|ohio|oklahoma|oregon|pennsylvania|rhode island|south carolina|south dakota|tennessee|texas|utah|vermont|virginia|washington|west virginia|wisconsin|wyoming|district of columbia|washington d\.?c\.?)\b`,
	)
	usStateCodeSet = func() map[string]struct{} {
		codes := map[string]struct{}{}
		for _, code := range strings.Fields("AL AK AZ AR CA CO CT DE FL GA HI IA ID IL IN KS KY LA MA MD ME MI MN MO MS MT NC ND NE NH NJ NM NV NY OH OK OR PA RI SC SD TN TX UT VA VT WA WI WV WY DC") {
			codes[code] = struct{}{}
		}
		return codes
	}()
	customUSRegexOnce      sync.Once
	customUSRegexCompiled  *regexp.Regexp
	techContextRE          = regexp.MustCompile(`(?i)\b(?:software|full[\s-]?stack|cloud|platform|ai|ml|machine learning|data|analytics?|data science|data warehouse|etl|backend|back[- ]?end|frontend|front[- ]?end|devops|sre|site reliability)\b`)
	techRoleWordRE         = regexp.MustCompile(`(?i)\b(?:engineer(?:ing)?|developer|scientist|swe|sde)\b`)
	dataAnalystRE          = regexp.MustCompile(`(?i)\b(?:data|analytics?|business intelligence|bi)\b.*\banalyst\b|\banalyst\b.*\b(?:data|analytics?|business intelligence|bi)\b`)
	earlyCareerRE          = regexp.MustCompile(`(?i)\b(?:intern|internship|new grad(?:uate)?|graduate|entry[- ]?level|junior|jr\.?|associate|early careers?|apprentice|co[- ]?op)\b`)
	seniorityBlockedRE     = regexp.MustCompile(`(?i)\b(?:senior|sr\.?|staff|principal|lead|manager|director|head|vp|vice president|distinguished|fellow|chief)\b`)
	roleLevelTooHighRE     = regexp.MustCompile(`(?i)\b(?:software engineer|engineer|developer|analyst|scientist|swe|sde)\s*(?:iv|v|vi|vii|viii|ix|x|[4-9])\b|\bl[4-9]\b`)
	nonTargetFunctionRE    = regexp.MustCompile(`(?i)\b(?:sales|account executive|customer success|recruit(?:er|ing)?|talent acquisition|hr|human resources|people|attorney|legal counsel|nurs(?:e|ing)|clinician|medical assistant|housekeeper|warehouse|fundraising|construction|solutions?\s+engineer|solutions?\s+architect|solution consultant|sales engineer|customer engineer|technical account manager)\b`)
	fullStackRE            = regexp.MustCompile(`(?i)\b(?:full[\s-]?stack|frontend\s*\+\s*backend|backend\s*\+\s*frontend|web platform)\b`)
	cloudRE                = regexp.MustCompile(`(?i)\b(?:cloud|aws|azure|gcp|kubernetes|k8s|devops|sre|site reliability|platform engineering)\b`)
	aiMLRE                 = regexp.MustCompile(`(?i)\b(?:ai|a\.i\.|ml|m\.l\.|machine learning|llm|genai|generative ai|nlp|computer vision)\b`)
	dataEngineerRE         = regexp.MustCompile(`(?i)\b(?:data engineer|analytics engineer|data science|data scientist|etl|data platform|data warehouse|big data)\b`)
	engineerLevelRE        = regexp.MustCompile(`(?i)\b(?:software engineer|engineer|developer|swe|sde)\s*(i{1,3}|[1-3])\b`)
	sponsorshipDeniedRE    = regexp.MustCompile(`(?i)\b(?:no|without|unable to|unable to sponsor candidates|cannot|can't|will not|won't)\s+(?:to\s+)?(?:provide|offer|support)?\s*(?:visa|immigration|employment|u\.s\.\s+security\s+clearance)?\s*(?:sponsorship|sponsor(?:ship)?)\b|\b(?:no|without)\s+(?:future\s+)?(?:work|visa)\s+sponsorship\b|\b(?:permanent|unrestricted)\s+work\s+authorization\b`)
	optCptDeniedRE         = regexp.MustCompile(`(?i)\b(?:cpt|opt|f-?1)\b[^\n]{0,40}\b(?:not|no|ineligible|unable|cannot|can't|does not)\b|\b(?:not|no|ineligible|unable|cannot|can't|does not)\b[^\n]{0,40}\b(?:cpt|opt|f-?1)\b`)
	clearanceRE            = regexp.MustCompile(`(?i)\b(?:security clearance|active clearance|clearance required|top secret|secret clearance|ts\/sci|public trust|federal clearance|polygraph clearance|with poly(?:graph)?)\b`)
	citizenshipRE          = regexp.MustCompile(`(?i)\b(?:u\.s\.?\s*citizen(?:ship)?|must\s+be\s+a(?:n)?\s+u\.s\.?\s+citizen|citizens?\s+only|only\s+u\.s\.?\s+citizens?|united states citizens?|citizenship is a basic security clearance eligibility requirement|eligibility for access to classified information may only be granted to employees who are united states citizens)\b`)
	usPersonRE             = regexp.MustCompile(`(?i)\b(?:u\.s\.?\s+person(?:s)?\s+required|must\s+be\s+a\s+u\.s\.?\s+person|export control|itar|deemed export)\b`)
	sponsorshipFriendlyRE  = regexp.MustCompile(`(?i)\b(?:visa sponsorship|sponsor(?:ship)?\s+available|will\s+sponsor|h-?1b\s+sponsor|supports?\s+(?:opt|cpt)|f-?1\s*opt)\b`)
	eVerifyFriendlyRE      = regexp.MustCompile(`(?i)\b(?:e-?verify|everify)\b`)
	deterministicRoleOutRE = regexp.MustCompile(`(?i)\b(?:consultant|strategist|architect|product manager|program manager|business partner|writer|marketing|(?:systems|business|people|operations?)\s+analyst|mechanical engineer|electrical engineer|civil engineer|facilities engineer)\b`)
	relevanceIncludeOnce   sync.Once
	relevanceIncludeRE     *regexp.Regexp
	relevanceExcludeOnce   sync.Once
	relevanceExcludeRE     *regexp.Regexp
)

const (
	roleDecisionIn        = "in"
	roleDecisionOut       = "out"
	roleDecisionAmbiguous = "ambiguous"
)

func customUSRegexPattern() string {
	if value := strings.TrimSpace(os.Getenv("US_ONLY_REGEX")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("US_ONLY_JOBS_REGEX"))
}

func customUSRegex() *regexp.Regexp {
	customUSRegexOnce.Do(func() {
		pattern := customUSRegexPattern()
		if pattern == "" {
			return
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return
		}
		customUSRegexCompiled = compiled
	})
	return customUSRegexCompiled
}

func compileRegexFromEnv(primary string, fallback string, once *sync.Once, target **regexp.Regexp) *regexp.Regexp {
	once.Do(func() {
		pattern := strings.TrimSpace(os.Getenv(primary))
		if pattern == "" && fallback != "" {
			pattern = strings.TrimSpace(os.Getenv(fallback))
		}
		if pattern == "" {
			return
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return
		}
		*target = compiled
	})
	return *target
}

func relevanceIncludeRegex() *regexp.Regexp {
	return compileRegexFromEnv(
		"RELEVANCE_INCLUDE_REGEX",
		"ROLE_INCLUDE_REGEX",
		&relevanceIncludeOnce,
		&relevanceIncludeRE,
	)
}

func relevanceExcludeRegex() *regexp.Regexp {
	return compileRegexFromEnv(
		"RELEVANCE_EXCLUDE_REGEX",
		"ROLE_EXCLUDE_REGEX",
		&relevanceExcludeOnce,
		&relevanceExcludeRE,
	)
}

func relevanceFilterEnabled() bool {
	return parseBoolEnv("RELEVANCE_FILTER", true)
}

func postedTodayOnlyEnabled() bool {
	return parseBoolEnv("POSTED_TODAY_ONLY", false)
}

func maxPostedAgeDays() int {
	raw := strings.TrimSpace(os.Getenv("MAX_POSTED_AGE_DAYS"))
	if raw == "" {
		if postedTodayOnlyEnabled() {
			return 0
		}
		return 7
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		if postedTodayOnlyEnabled() {
			return 0
		}
		return 7
	}
	if parsed < 0 {
		return -1
	}
	return parsed
}

func allowUnknownPostedAt() bool {
	return parseBoolEnv("ALLOW_UNKNOWN_POSTED_AT", true)
}

func parsePostedTime(value string) (time.Time, bool) {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return time.Time{}, false
	}
	if normalized := normalizePossibleDate(candidate); normalized != "" {
		if parsed, err := time.Parse(time.RFC3339, normalized); err == nil {
			return parsed, true
		}
	}
	if parsed, ok := parseDateWithLayouts(candidate); ok {
		return parsed, true
	}
	if parsed, ok := parseRelativeDate(candidate); ok {
		return parsed, true
	}
	return time.Time{}, false
}

func isRecentEnoughPostedDate(postedAt string) bool {
	days := maxPostedAgeDays()
	if days < 0 {
		return true
	}
	if strings.TrimSpace(postedAt) == "" {
		return allowUnknownPostedAt()
	}
	posted, ok := parsePostedTime(postedAt)
	if !ok {
		return allowUnknownPostedAt()
	}
	now := time.Now()
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	postedLocal := posted.In(now.Location())
	return !postedLocal.Before(startToday.AddDate(0, 0, -days))
}

func hasUSMarker(value string) bool {
	raw := strings.TrimSpace(value)
	lower := strings.ToLower(raw)
	if lower == "" {
		return false
	}
	if custom := customUSRegex(); custom != nil && custom.MatchString(raw) {
		return true
	}
	if usCountryRE.MatchString(lower) || usRemoteRE.MatchString(lower) || usStateNameRE.MatchString(lower) || hasUSStateCodeMarker(raw) {
		return true
	}
	if strings.Contains(lower, "/en-us/") || strings.Contains(lower, "/us/") {
		return true
	}
	if strings.Contains(lower, "location=united%20states") || strings.Contains(lower, "united-states") {
		return true
	}
	return false
}

func hasUSStateCodeMarker(value string) bool {
	for _, segment := range strings.Split(value, ";") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		commaParts := splitLocationTokens(segment, ",")
		if len(commaParts) == 2 && isLikelyLocationNameToken(commaParts[0]) && isUSStateCodeToken(commaParts[1]) {
			return true
		}
		if len(commaParts) >= 3 &&
			isLikelyLocationNameToken(commaParts[len(commaParts)-3]) &&
			isUSStateCodeToken(commaParts[len(commaParts)-2]) &&
			isExplicitUSCountryToken(commaParts[len(commaParts)-1]) {
			return true
		}

		dashParts := splitLocationTokens(segment, " - ")
		if len(dashParts) == 2 && isLikelyLocationNameToken(dashParts[0]) && isUSStateCodeToken(dashParts[1]) {
			return true
		}
	}
	return false
}

func splitLocationTokens(value string, separator string) []string {
	rawParts := strings.Split(value, separator)
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(strings.Trim(part, "()"))
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func isLikelyLocationNameToken(value string) bool {
	token := strings.TrimSpace(strings.Trim(value, "()"))
	if token == "" {
		return false
	}
	return !(len(token) == 2 && token == strings.ToUpper(token))
}

func isUSStateCodeToken(value string) bool {
	token := strings.ToUpper(strings.TrimSpace(strings.Trim(value, "()")))
	_, ok := usStateCodeSet[token]
	return ok
}

func isExplicitUSCountryToken(value string) bool {
	token := strings.TrimSpace(strings.Trim(value, "()"))
	if token == "" {
		return false
	}
	return usCountryRE.MatchString(token)
}

func isUSBasedJob(job Job) bool {
	for _, candidate := range []string{job.Location, job.Title, job.URL} {
		if hasUSMarker(candidate) {
			return true
		}
	}
	return false
}

func hasTargetDiscipline(title string, combined string) bool {
	if dataAnalystRE.MatchString(title) || dataAnalystRE.MatchString(combined) {
		return true
	}
	if (strings.Contains(title, "software engineer") || strings.Contains(title, "software developer") || strings.Contains(title, "swe") || strings.Contains(title, "sde")) && techRoleWordRE.MatchString(title) {
		return true
	}
	if techRoleWordRE.MatchString(title) && techContextRE.MatchString(title) {
		return true
	}
	if techRoleWordRE.MatchString(combined) && techContextRE.MatchString(combined) {
		// Keep description fallback strict by requiring a role cue in title.
		return techRoleWordRE.MatchString(title) || earlyCareerRE.MatchString(title)
	}
	return false
}

func hasAllowedSeniority(title string) bool {
	if earlyCareerRE.MatchString(title) {
		return true
	}
	if seniorityBlockedRE.MatchString(title) {
		return false
	}
	if roleLevelTooHighRE.MatchString(title) {
		return false
	}
	return true
}

func isRelevantCareerJobWithRecency(job Job, applyPostedRecency bool) bool {
	if !relevanceFilterEnabled() {
		return true
	}
	title := strings.ToLower(normalizeTextSnippet(job.Title, 280))
	team := strings.ToLower(normalizeTextSnippet(job.Team, 280))
	description := strings.ToLower(normalizeTextSnippet(job.Description, 2200))
	urlValue := strings.ToLower(strings.TrimSpace(job.URL))
	combined := strings.Join([]string{title, team, description, urlValue}, " ")
	if strings.TrimSpace(combined) == "" {
		return false
	}
	if nonTargetFunctionRE.MatchString(title) {
		return false
	}
	if exclude := relevanceExcludeRegex(); exclude != nil && exclude.MatchString(combined) {
		return false
	}
	if include := relevanceIncludeRegex(); include != nil && include.MatchString(combined) {
		return true
	}
	if len(loadResumeProfiles()) > 0 {
		if !hasAllowedSeniority(title) {
			return false
		}
		if !resumeAwarePotentialMatch(job) {
			return false
		}
		if parseBoolEnv("F1_OPT_EXCLUDE_BLOCKED", false) {
			workAuthStatus, _ := workAuthStatusAndNotes(job)
			if workAuthStatus == "blocked" {
				return false
			}
		}
		if !applyPostedRecency {
			return true
		}
		return isRecentEnoughPostedDate(job.PostedAt)
	}
	if !hasTargetDiscipline(title, combined) {
		return false
	}
	if !hasAllowedSeniority(title) {
		return false
	}
	if parseBoolEnv("F1_OPT_EXCLUDE_BLOCKED", false) {
		workAuthStatus, _ := workAuthStatusAndNotes(job)
		if workAuthStatus == "blocked" {
			return false
		}
	}
	if !applyPostedRecency {
		return true
	}
	return isRecentEnoughPostedDate(job.PostedAt)
}

func isRelevantCareerJob(job Job) bool {
	return isRelevantCareerJobWithRecency(job, true)
}

func isRelevantCareerJobIgnorePostedRecency(job Job) bool {
	return isRelevantCareerJobWithRecency(job, false)
}

func workAuthStatusAndNotes(job Job) (string, []string) {
	text := strings.TrimSpace(strings.Join([]string{
		normalizeTextSnippet(job.Title, 320),
		normalizeTextSnippet(job.Team, 260),
		normalizeTextSnippet(job.Description, 2600),
	}, " "))
	if text == "" {
		return "unknown", nil
	}

	notes := make([]string, 0, 3)
	noteSet := map[string]struct{}{}
	blocked := false
	friendly := false
	eVerifyMentioned := false

	if sponsorshipDeniedRE.MatchString(text) || optCptDeniedRE.MatchString(text) {
		blocked = true
		notes = appendUniqueReason(notes, noteSet, "No sponsorship / restricted work authorization")
	}
	if clearanceRE.MatchString(text) {
		blocked = true
		notes = appendUniqueReason(notes, noteSet, "Security clearance requirement")
	}
	if citizenshipRE.MatchString(text) || usPersonRE.MatchString(text) {
		blocked = true
		notes = appendUniqueReason(notes, noteSet, "US citizenship or U.S. person requirement")
	}
	if sponsorshipFriendlyRE.MatchString(text) {
		friendly = true
		notes = appendUniqueReason(notes, noteSet, "Visa sponsorship or OPT support mentioned")
	}
	if eVerifyFriendlyRE.MatchString(text) {
		eVerifyMentioned = true
		notes = appendUniqueReason(notes, noteSet, "E-Verify mentioned (not a sponsorship guarantee)")
	}

	switch {
	case blocked:
		if len(notes) > 3 {
			notes = notes[:3]
		}
		return "blocked", notes
	case friendly:
		if len(notes) > 3 {
			notes = notes[:3]
		}
		return "friendly", notes
	default:
		if eVerifyMentioned {
			if len(notes) > 2 {
				notes = notes[:2]
			}
			return "unknown", notes
		}
		return "unknown", nil
	}
}

func normalizeRoleDecision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case roleDecisionIn:
		return roleDecisionIn
	case roleDecisionOut:
		return roleDecisionOut
	default:
		return roleDecisionAmbiguous
	}
}

func deterministicRoleDecision(job Job) (string, []string) {
	title := strings.ToLower(normalizeTextSnippet(job.Title, 320))
	team := strings.ToLower(normalizeTextSnippet(job.Team, 260))
	description := strings.ToLower(normalizeTextSnippet(job.Description, 2200))
	combined := strings.TrimSpace(strings.Join([]string{title, team, description, strings.ToLower(strings.TrimSpace(job.URL))}, " "))

	reasons := make([]string, 0, 3)
	reasonSet := map[string]struct{}{}
	if strings.TrimSpace(combined) == "" {
		reasons = appendUniqueReason(reasons, reasonSet, "Missing role context")
		return roleDecisionAmbiguous, reasons
	}

	if parseBoolEnv("US_ONLY_JOBS", true) && !isUSBasedJob(job) {
		reasons = appendUniqueReason(reasons, reasonSet, "Non-US location")
		return roleDecisionOut, reasons
	}
	if seniorityBlockedRE.MatchString(title) || roleLevelTooHighRE.MatchString(title) {
		reasons = appendUniqueReason(reasons, reasonSet, "Role appears more senior than target")
		return roleDecisionOut, reasons
	}
	if nonTargetFunctionRE.MatchString(title) {
		reasons = appendUniqueReason(reasons, reasonSet, "Non-target function")
		return roleDecisionOut, reasons
	}
	if deterministicRoleOutRE.MatchString(title) {
		reasons = appendUniqueReason(reasons, reasonSet, "Role family is outside target engineering scope")
		return roleDecisionOut, reasons
	}
	if resumeHardExcludeRE.MatchString(title) {
		reasons = appendUniqueReason(reasons, reasonSet, "Role is test, technician, or operations heavy")
		return roleDecisionOut, reasons
	}
	if internshipRoleRE.MatchString(title) && !techRoleWordRE.MatchString(title) && !dataAnalystRE.MatchString(title) {
		reasons = appendUniqueReason(reasons, reasonSet, "Internship title does not indicate a target engineering or data role")
		return roleDecisionOut, reasons
	}
	if dataAnalystRE.MatchString(title) {
		reasons = appendUniqueReason(reasons, reasonSet, "Target data analyst role title")
		return roleDecisionIn, reasons
	}
	if techRoleWordRE.MatchString(title) && hasTargetDiscipline(title, combined) {
		reasons = appendUniqueReason(reasons, reasonSet, "Clear target engineering or data role title")
		return roleDecisionIn, reasons
	}
	if techRoleWordRE.MatchString(title) {
		reasons = appendUniqueReason(reasons, reasonSet, "Role title is technical but scope is unclear")
		return roleDecisionAmbiguous, reasons
	}
	if nonTargetFunctionRE.MatchString(team) {
		reasons = appendUniqueReason(reasons, reasonSet, "Non-target function")
		return roleDecisionOut, reasons
	}
	if techContextRE.MatchString(combined) || dataAnalystRE.MatchString(combined) {
		reasons = appendUniqueReason(reasons, reasonSet, "Technical context present but title fit is unclear")
		return roleDecisionAmbiguous, reasons
	}
	reasons = appendUniqueReason(reasons, reasonSet, "Title does not indicate a target engineering or data role")
	return roleDecisionOut, reasons
}

func needsDeterministicRoleSLMReview(job Job) bool {
	decision, _ := deterministicRoleDecision(job)
	return decision == roleDecisionAmbiguous && slmScoringEnabled()
}

func normalizedURLHostAndPath(rawURL string) (string, string) {
	candidate := strings.TrimSpace(rawURL)
	if candidate == "" {
		return "", ""
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", strings.ToLower(candidate)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	path := strings.ToLower(strings.TrimSpace(parsed.EscapedPath()))
	if path == "" {
		path = "/"
	}
	if path != "/" {
		path = strings.TrimRight(path, "/")
	}
	return host, path
}

func hasExactNonJobTitle(normalized string) bool {
	for _, exact := range []string{
		"careers", "career", "jobs", "search jobs", "search results", "benefits", "culture", "locations",
		"overview", "privacy", "legal", "cookie preferences", "instagram", "youtube", "linkedin", "twitter",
		"facebook", "open positions", "all jobs", "learn more", "view jobs", "see open jobs",
		"see open positions", "join our talent community", "join the talent community", "how we hire",
		"internships", "early careers", "internships and programs", "leadership principles",
		"internal careers site", "development and engineering", "engineering and development", "blog",
		"read more", "read more blogs", "careers blog", "data policy", "search meta careers blog",
		"privacy notice", "privacy policy", "privacy and data", "terms of use", "terms and conditions",
		"explore benefits", "workplace benefits", "check out our benefits",
		"applicant & candidate privacy open_in_new",
	} {
		if normalized == exact {
			return true
		}
	}
	return false
}

func isClearlyNonJobLandingPage(job Job) bool {
	title := strings.ToLower(strings.TrimSpace(job.Title))
	rawURL := strings.TrimSpace(job.URL)
	if title == "" || rawURL == "" {
		return true
	}
	if hasExactNonJobTitle(title) {
		return true
	}
	if strings.HasPrefix(title, "learn more on ") || strings.Contains(title, " available jobs") {
		return true
	}

	host, path := normalizedURLHostAndPath(rawURL)
	switch host {
	case "www.ibm.com", "ibm.com":
		if strings.HasPrefix(path, "/careers/blog") || strings.HasPrefix(path, "/careers/search") {
			return true
		}
	case "jobs.intuit.com":
		if strings.HasPrefix(path, "/blog-") {
			return true
		}
	case "www.intuit.com", "intuit.com":
		if strings.Contains(path, "/blog/") {
			return true
		}
	case "careers.zoom.us":
		if path == "/home" || path == "/benefits" || path == "/dreamjobcta" || strings.HasPrefix(path, "/blogs/") {
			return true
		}
	case "www.metacareers.com", "metacareers.com":
		if path == "/privacy" || path == "/blog" || strings.HasPrefix(path, "/blog/") {
			return true
		}
	case "www.amazon.jobs", "amazon.jobs":
		if path == "/en/benefits" || path == "/benefits/global" || path == "/en/privacy_page" ||
			path == "/en" || strings.HasPrefix(path, "/content/en/") ||
			strings.HasPrefix(path, "/applicant/") || path == "/en/job_categories" {
			return true
		}
	case "email.aboutamazon.com":
		return true
	case "www.capitalonecareers.com", "capitalonecareers.com":
		if path == "/benefits" || path == "/blog" || strings.HasPrefix(path, "/blog/") {
			return true
		}
	case "careers.datadoghq.com":
		if path == "/" || strings.HasPrefix(path, "/benefits") ||
			path == "/early-careers" || strings.HasPrefix(path, "/candidate-experience") ||
			strings.HasPrefix(path, "/inclusion") || strings.HasPrefix(path, "/learning-and-development") ||
			strings.HasPrefix(path, "/general-and-administrative") || strings.HasPrefix(path, "/product-management") ||
			strings.HasPrefix(path, "/technical-solutions") || strings.HasPrefix(path, "/product-design") ||
			path == "/remote" ||
			path == "/apac" {
			return true
		}
	case "jobs.gecareers.com":
		if strings.Contains(path, "/blog") {
			return true
		}
	case "careers.google.com":
		if path == "/privacy-policy" || path == "/jobs/results" {
			return true
		}
	case "www.google.com", "google.com":
		if path == "/about/careers/applications/jobs/results" {
			return true
		}
	case "careers.jpmorgan.com":
		if strings.HasPrefix(path, "/legal/") || strings.HasPrefix(path, "/careers/how-we-hire/") || strings.HasPrefix(path, "/communities/") {
			return true
		}
	case "www.jpmorganchase.com", "jpmorganchase.com":
		if path == "/careers" {
			return true
		}
	case "blogs.oracle.com":
		return true
	case "www.oracle.com", "oracle.com":
		if path == "/careers/life-at-oracle/benefits" || path == "/careers/culture/benefits" {
			return true
		}
	case "www.splunk.com", "splunk.com":
		if strings.Contains(path, "/blog/") {
			return true
		}
	case "jobs.zendesk.com":
		if strings.HasPrefix(path, "/blog") || strings.Contains(path, "/agreements-and-terms/") {
			return true
		}
	case "jobs.ashbyhq.com":
		if strings.Contains(path, "/form/talent-community") {
			return true
		}
	case "www.dropbox.jobs", "dropbox.jobs":
		if strings.Contains(path, "/our-blog/") {
			return true
		}
	case "jobs.careers.microsoft.com":
		if path == "/actioncenter" {
			return true
		}
	case "www.salesforce.com", "salesforce.com":
		if path == "/company/careers/jobs" {
			return true
		}
	case "careers.salesforce.com":
		if path == "/en/jobs" {
			return true
		}
	case "careers.mail.salesforce.com":
		if strings.Contains(path, "accommodation") {
			return true
		}
	case "stripe.com", "www.stripe.com":
		if (path == "/jobs/search" || strings.HasSuffix(path, "/jobs/search")) && !strings.Contains(strings.ToLower(rawURL), "gh_jid=") {
			return true
		}
		if path == "/jobs/life-at-stripe" || path == "/jobs/university" {
			return true
		}
	}
	return false
}

func hasLikelyNonJobPath(rawURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if lower == "" {
		return true
	}
	host, path := normalizedURLHostAndPath(lower)
	switch host {
	case "www.google.com", "google.com":
		if strings.HasPrefix(path, "/about/careers/applications/jobs/results/") {
			return false
		}
	}
	if strings.Contains(path, "/requisitions/preview/") {
		return false
	}
	for _, marker := range []string{
		"/jointalentcommunity",
		"/talentcommunity",
		"/talent-community",
		"/join-our-talent-community",
		"/dreamjobcta",
		"/search-results",
		"/search-jobs",
		"/jobsearch",
		"/careers/search",
		"/locations/",
		"/departments/",
		"/teams/",
		"/benefits/",
		"/our-blog/",
		"/blogs/",
		"/blog-",
		"/culture",
		"/about/",
		"/blog/",
		"/events/",
		"/news/",
		"/why-work-here",
		"/c/",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.Contains(lower, "/apply?") || strings.Contains(lower, "/apply/") {
		// Treat apply funnels as non-job links unless they carry a canonical job path marker.
		return !hasStrongJobPathHint(lower)
	}
	return false
}

func hasDescriptionJobMarker(description string) bool {
	lower := strings.ToLower(strings.TrimSpace(description))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"responsibil",
		"requirement",
		"qualification",
		"about the role",
		"what you'll do",
		"what you will do",
		"experience",
		"job description",
		"the opportunity",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasJobOpeningSignal(job Job) bool {
	if hasStrongJobPathHint(job.URL) {
		return true
	}
	if hasRoleKeywordInTitle(job.Title) {
		return true
	}
	if strings.TrimSpace(job.PostedAt) != "" {
		return true
	}
	if len(strings.Fields(strings.TrimSpace(job.Description))) >= 24 && hasDescriptionJobMarker(job.Description) {
		return true
	}
	return false
}

func looksLikeNoiseJob(job Job) bool {
	title := strings.TrimSpace(job.Title)
	url := strings.TrimSpace(job.URL)
	description := strings.ToLower(strings.TrimSpace(job.Description))
	if parseBoolEnv("US_ONLY_JOBS", true) && !isUSBasedJob(job) {
		return true
	}
	if title == "" || url == "" {
		return true
	}
	if isResourceAssetURL(url) {
		return true
	}
	if isClearlyNonJobLandingPage(job) {
		return true
	}
	if hasLikelyNonJobPath(url) {
		return true
	}
	if isNoiseTitle(title) {
		return true
	}
	if !hasJobOpeningSignal(job) {
		return true
	}
	for _, marker := range []string{
		"<img", "const t=", ".css-", "{display:", "data-gatsby-image", "queryselectorall(",
	} {
		if strings.Contains(description, marker) {
			return true
		}
	}
	return false
}

func sourceJobSignalScore(job Job) int {
	score := 0
	titleWords := len(strings.Fields(strings.TrimSpace(job.Title)))
	if job.PostedAt != "" {
		score += 3
	}
	if titleWords >= 4 {
		score += 3
	} else if titleWords >= 2 {
		score++
	}
	if len(strings.Fields(strings.TrimSpace(job.Description))) >= 18 {
		score += 2
	}
	if hasStrongJobPathHint(job.URL) {
		score += 4
	}
	if hasRoleKeywordInTitle(job.Title) {
		score += 3
	}
	for _, marker := range []string{
		"/teams/", "/locations/", "/benefits/", "/about/", "/blog/", "/events/", "/news/",
	} {
		if strings.Contains(strings.ToLower(job.URL), marker) {
			score -= 3
			break
		}
	}
	if looksLikeNoiseJob(job) {
		score -= 10
	}
	if !hasStrongJobPathHint(job.URL) && job.PostedAt == "" && !hasRoleKeywordInTitle(job.Title) && len(strings.Fields(strings.TrimSpace(job.Description))) < 18 {
		score -= 8
	}
	return score
}

func filterLikelyJobs(jobs []Job) []Job {
	out := make([]Job, 0, len(jobs))
	seen := map[string]struct{}{}
	for _, job := range jobs {
		job.Title = normalizeTextSnippet(job.Title, 280)
		job.Description = normalizeTextSnippet(job.Description, 2200)
		if looksLikeNoiseJob(job) || sourceJobSignalScore(job) < 2 {
			continue
		}
		fingerprint := strings.ToLower(strings.TrimSpace(job.URL)) + "|" + strings.ToLower(strings.TrimSpace(job.Title))
		if _, ok := seen[fingerprint]; ok {
			continue
		}
		seen[fingerprint] = struct{}{}
		out = append(out, job)
	}
	return out
}

func filterRelevantJobs(jobs []Job) []Job {
	if !relevanceFilterEnabled() {
		return jobs
	}
	out := make([]Job, 0, len(jobs))
	for _, job := range jobs {
		if !isRelevantCareerJob(job) {
			continue
		}
		out = append(out, job)
	}
	return out
}

func appendUniqueReason(reasons []string, seen map[string]struct{}, reason string) []string {
	value := strings.TrimSpace(reason)
	if value == "" {
		return reasons
	}
	key := strings.ToLower(value)
	if _, exists := seen[key]; exists {
		return reasons
	}
	seen[key] = struct{}{}
	return append(reasons, value)
}

func clampScore(value int) int {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}

func jobMatchScoreAndReasons(job Job) (int, []string) {
	summary := jobMatchSummaryForJob(job)
	return summary.MatchScore, summary.MatchReasons
}

func legacyJobMatchScoreAndReasons(job Job) (int, []string) {
	title := strings.ToLower(normalizeTextSnippet(job.Title, 320))
	team := strings.ToLower(normalizeTextSnippet(job.Team, 260))
	description := strings.ToLower(normalizeTextSnippet(job.Description, 2200))
	combinedText := strings.Join([]string{title, team, description}, " ")

	score := 0
	reasons := make([]string, 0, 4)
	reasonSet := map[string]struct{}{}
	skillHits := 0

	discipline := 0
	if fullStackRE.MatchString(combinedText) {
		discipline = maxInt(discipline, 34)
		reasons = appendUniqueReason(reasons, reasonSet, "Full Stack alignment")
		skillHits++
	}
	if cloudRE.MatchString(combinedText) {
		discipline = maxInt(discipline, 34)
		reasons = appendUniqueReason(reasons, reasonSet, "Cloud / Platform alignment")
		skillHits++
	}
	if aiMLRE.MatchString(combinedText) {
		discipline = maxInt(discipline, 36)
		reasons = appendUniqueReason(reasons, reasonSet, "AI / ML alignment")
		skillHits++
	}
	if dataAnalystRE.MatchString(combinedText) {
		discipline = maxInt(discipline, 35)
		reasons = appendUniqueReason(reasons, reasonSet, "Data Analyst alignment")
		skillHits++
	} else if dataEngineerRE.MatchString(combinedText) {
		discipline = maxInt(discipline, 33)
		reasons = appendUniqueReason(reasons, reasonSet, "Data engineering alignment")
		skillHits++
	}
	if discipline == 0 && hasTargetDiscipline(title, combinedText) {
		discipline = 22
		reasons = appendUniqueReason(reasons, reasonSet, "Relevant software/data discipline")
	}
	if discipline > 0 && skillHits > 1 {
		discipline += minInt(10, (skillHits-1)*4)
	}
	if discipline > 44 {
		discipline = 44
	}
	score += discipline

	seniority := 0
	switch {
	case earlyCareerRE.MatchString(title):
		seniority = 26
		reasons = appendUniqueReason(reasons, reasonSet, "Early-career friendly level")
	case engineerLevelRE.MatchString(title):
		level := strings.TrimSpace(strings.ToUpper(engineerLevelRE.FindStringSubmatch(title)[1]))
		switch level {
		case "I", "1":
			seniority = 28
			reasons = appendUniqueReason(reasons, reasonSet, "Software Engineer I level")
		case "II", "2":
			seniority = 27
			reasons = appendUniqueReason(reasons, reasonSet, "Software Engineer II level")
		case "III", "3":
			seniority = 25
			reasons = appendUniqueReason(reasons, reasonSet, "Software Engineer III level")
		default:
			seniority = 22
			reasons = appendUniqueReason(reasons, reasonSet, "Engineer level within target range")
		}
	case seniorityBlockedRE.MatchString(title) || roleLevelTooHighRE.MatchString(title):
		seniority = 4
		reasons = appendUniqueReason(reasons, reasonSet, "Role appears more senior than target")
	case techRoleWordRE.MatchString(title):
		seniority = 20
		reasons = appendUniqueReason(reasons, reasonSet, "Engineer/analyst role title")
	default:
		seniority = 12
	}
	score += seniority

	recency := 0
	if posted, ok := parsePostedTime(job.PostedAt); ok {
		now := time.Now()
		startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		postedLocal := posted.In(now.Location())
		daysOld := int(startToday.Sub(time.Date(postedLocal.Year(), postedLocal.Month(), postedLocal.Day(), 0, 0, 0, 0, now.Location())).Hours() / 24)
		switch {
		case daysOld <= 0:
			recency = 20
			reasons = appendUniqueReason(reasons, reasonSet, "Posted today")
		case daysOld == 1:
			recency = 12
			reasons = appendUniqueReason(reasons, reasonSet, "Posted yesterday")
		case daysOld <= 7:
			recency = 6
		case daysOld <= 30:
			recency = 2
		default:
			recency = 0
		}
	}
	score += recency

	contextScore := 0
	if hasStrongJobPathHint(job.URL) {
		contextScore += 4
	}
	if hasDescriptionJobMarker(job.Description) {
		contextScore += 3
	}
	if len(strings.Fields(strings.TrimSpace(job.Description))) >= 45 {
		contextScore += 3
	}
	if isUSBasedJob(job) {
		contextScore += 4
	}
	if contextScore > 12 {
		contextScore = 12
	}
	score += contextScore

	if nonTargetFunctionRE.MatchString(title) {
		score -= 26
		reasons = appendUniqueReason(reasons, reasonSet, "Non-target function")
	}

	workAuthStatus, workAuthNotes := workAuthStatusAndNotes(job)
	switch workAuthStatus {
	case "blocked":
		score -= 38
		reasons = appendUniqueReason(reasons, reasonSet, "F1 OPT constraint detected")
		if len(workAuthNotes) > 0 {
			reasons = appendUniqueReason(reasons, reasonSet, workAuthNotes[0])
		}
	case "friendly":
		score += 8
		if len(workAuthNotes) > 0 {
			reasons = appendUniqueReason(reasons, reasonSet, workAuthNotes[0])
		}
	}

	score = clampScore(score)
	if len(reasons) > 4 {
		reasons = reasons[:4]
	}
	return score, reasons
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func shouldTryFallbackForQuality(rawCount int, acceptedCount int) bool {
	if rawCount < 0 {
		return true
	}
	if rawCount == 0 {
		return false
	}
	if acceptedCount <= 0 {
		return true
	}
	ratio := float64(acceptedCount) / float64(rawCount)
	if rawCount >= 40 && acceptedCount < 5 {
		return true
	}
	if rawCount >= 15 && ratio < 0.2 {
		return true
	}
	return false
}
