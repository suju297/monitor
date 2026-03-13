package monitor

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	mailInterviewRE            = regexp.MustCompile(`(?i)\b(interview|onsite|on-site|screen|screening|recruiter call|technical call|virtual onsite|panel)\b`)
	mailScheduleRE             = regexp.MustCompile(`(?i)\b(schedule|scheduled|booking|calendar invite|invite attached|meeting invite|availability|time slot|book time)\b`)
	mailUpdateRE               = regexp.MustCompile(`(?i)\b(rescheduled|updated|moved|canceled|cancelled|postponed|changed|declined)\b`)
	mailTentativeInterviewRE   = regexp.MustCompile(`(?i)\b(what to expect|stay tuned|we(?:'ll| will| would) contact you(?: soon)? to schedule|if your background and experience seem like a good fit|if your background and experience are a good fit|if your experience seems like a good fit|if your experience is a good fit)\b`)
	mailAckRE                  = regexp.MustCompile(`(?i)\b(thank you for applying|thanks for applying|thank you for your application|application received|we received your application|your application has been received|application submitted|your application has been submitted|your application was sent)\b`)
	mailRejectRE               = regexp.MustCompile(`(?i)\b(unfortunately|decided not to move forward(?: at this time)?|not (?:to )?move forward(?: at this time)?|moving forward with other candidates|move forward with candidates? whose (?:background|experience|skills?) (?:is|are) more closely aligned|other candidates|will not be proceeding|position has been filled|regret to inform you|decline|applications? limit(?: reached)?|reached (?:our|the) applications? limit)\b`)
	mailRecruiterRE            = regexp.MustCompile(`(?i)\b(recruiter|talent acquisition|talent partner|hiring team|hiring manager|came across your profile|let'?s connect|would love to chat)\b`)
	mailRecruiterReplyCueRE    = regexp.MustCompile(`(?i)\b(follow(?:ing)? up|thanks for (?:your|the) response|thanks for getting back|thanks for speaking|great speaking|pleasure speaking|share (?:your )?availability|let me know (?:your )?availability|are you available|would you be available|can we chat|can we connect|can we speak|next steps|do you need sponsorship)\b`)
	mailRecruiterOutreachRE    = regexp.MustCompile(`(?i)\b(came across your profile|came across your background|reaching out|wanted to reach out|would love to chat|would like to chat|let'?s connect|interested in your background|interested in speaking|interested in learning more|quick intro|quick chat|open to a quick chat)\b`)
	mailRejectSubjectRE        = regexp.MustCompile(`(?i)\b(update on your candidacy|update on your application|update on your .*applications?)\b`)
	mailIndiaApplyInviteRE     = regexp.MustCompile(`(?i)\b(job invite from recruiter|you'?re invited to apply|invited to apply to this job|you'?ve been chosen from a large pool|apply now)\b`)
	mailJobInviteRE            = regexp.MustCompile(`(?i)\b(job invite from recruiter|you'?re invited to apply|invited to apply to this job|you'?ve been chosen from a large pool|apply now|get app|urgent vacancy|verify profile details|review stage for the next shortlist round|share your cv|share your resume|promising fit|excellent match)\b`)
	mailIndiaJobMarketDomainRE = regexp.MustCompile(`(?i)@(naukri\.com|jobs\.shine\.com|shine\.com)\b`)
	mailJobRE                  = regexp.MustCompile(`(?i)\b(jobs?|roles?|positions?|applications?|candidates?|careers?|hiring|resume|cv)\b`)
	mailNoiseRE                = regexp.MustCompile(`(?i)\b(newsletter|unsubscribe|promotion|sale|webinar|digest|marketing)\b`)
	mailLocalNewsletterRE      = regexp.MustCompile(`(?i)\b(read online|what'?s on tap today|things to do|start a membership|members-only|quick & dirty headlines|parking ticket scam alert|the b-side)\b`)
	mailVerificationCodeRE     = regexp.MustCompile(`(?i)\b(one[- ]time (verification|security|login)? code|verification code|security code|identity verification|confirm your identity(?: using this code)?|security passcode|passcode|otp|multi[- ]factor|two[- ]factor|2fa)\b`)
	mailAccountSetupNoiseRE    = regexp.MustCompile(`(?i)\b(verify your (email|account)|account verification|email verification|confirm your (email|account|identity)|activate your account|account activation|create (your )?account|set up your account|setup your account|finish setting up your account|complete your account setup|welcome to .*careers|reset your password|password reset)\b`)
	mailApplicationProgressRE  = regexp.MustCompile(`(?i)\b(complete your .*application|finish your .*application|continue your .*application|started your .*application|start your .*application|application in progress)\b`)
	mailHiringOpsRequestRE     = regexp.MustCompile(`(?i)\b(tax filing|tax forms?|w-?2\b|w-?4\b|1099\b|i-?9\b|background check|onboarding|direct deposit|payroll|employment verification|offer letter|work authorization documents?|supporting documents?|required documents?|requested documents?|paperwork|share (?:the|your) documents?|send (?:the|your) documents?|submit (?:the|your) documents?|upload (?:the|your) documents?|provide (?:the|your) documents?)\b`)
)

var mailLocalNewsletterCueSnippets = []string{
	"read online",
	"what's on tap today",
	"what’s on tap today",
	"things to do",
	"start a membership",
	"members-only",
	"quick & dirty headlines",
	"parking ticket scam alert",
	"the b-side",
	"quick question",
	"upcoming local picks",
	"one last thing",
	"thanks for reading",
	"keep up with us",
	"let us know below",
}

var mailAcademicEventCueSnippets = []string{
	"speaker series",
	"speaker event",
	"voices from the field",
	"register here",
	"student registration",
	"fill out form",
	"join us",
	"we hope to see you",
	"campus",
	"network participants",
	"mgen students",
	"mgen operations team",
}

type mailKnownJob struct {
	Company string
	Title   string
	URL     string
}

type mailHistoricalClassifier struct {
	SampleCount        int
	SamplesByEvent     map[string]int
	TokenCounts        map[string]map[string]int
	TotalTokensByEvent map[string]int
	DomainCounts       map[string]map[string]int
	Vocabulary         map[string]struct{}
	DomainVocabulary   map[string]struct{}
}

type mailHistoricalPrediction struct {
	EventType   string
	Confidence  float64
	SampleCount int
}

type mailMatchContext struct {
	Companies            []string
	Jobs                 []mailKnownJob
	HistoricalClassifier *mailHistoricalClassifier
}

type mailMatchContextFileSignature struct {
	Path      string
	Exists    bool
	Size      int64
	ModTimeNS int64
}

var mailMatchContextCache = struct {
	mu       sync.RWMutex
	contexts map[string]mailMatchContext
	builds   int
}{
	contexts: map[string]mailMatchContext{},
}

func normalizeMailMatchContextPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func statMailMatchContextPath(path string) mailMatchContextFileSignature {
	signature := mailMatchContextFileSignature{
		Path: normalizeMailMatchContextPath(path),
	}
	if signature.Path == "" {
		return signature
	}
	info, err := os.Stat(signature.Path)
	if err != nil {
		return signature
	}
	signature.Exists = true
	signature.Size = info.Size()
	signature.ModTimeNS = info.ModTime().UnixNano()
	return signature
}

func (signature mailMatchContextFileSignature) cacheKeyPart(label string) string {
	return fmt.Sprintf("%s=%s:%t:%d:%d", label, signature.Path, signature.Exists, signature.Size, signature.ModTimeNS)
}

func mailMatchContextCacheKey(cfg MailServiceConfig) string {
	statePath := normalizeMailMatchContextPath(cfg.StatePath)
	configPath := normalizeMailMatchContextPath(cfg.ConfigPath)
	mailSignature := statMailMatchContextPath(mailDBPath(cfg.StatePath))
	jobsSignature := statMailMatchContextPath(jobsDBPath(cfg.StatePath))
	configSignature := statMailMatchContextPath(configPath)
	return strings.Join([]string{
		fmt.Sprintf("state=%s", statePath),
		fmt.Sprintf("config=%s", configPath),
		mailSignature.cacheKeyPart("mail"),
		jobsSignature.cacheKeyPart("jobs"),
		configSignature.cacheKeyPart("companies"),
	}, "|")
}

func buildMailMatchContext(cfg MailServiceConfig) (mailMatchContext, bool) {
	context := mailMatchContext{
		Companies:            []string{},
		Jobs:                 []mailKnownJob{},
		HistoricalClassifier: loadMailHistoricalClassifier(cfg.StatePath),
	}
	cacheable := true
	labels := map[string]string{}
	if strings.TrimSpace(cfg.ConfigPath) != "" {
		companies, err := LoadCompanies(cfg.ConfigPath)
		if err != nil {
			cacheable = false
		} else {
			for _, company := range companies {
				label := strings.TrimSpace(company.Name)
				key := companyFilterKey(label)
				if key == "" {
					continue
				}
				labels[key] = choosePreferredCompanyLabel(labels[key], label)
			}
		}
	}
	if snapshot, err := loadJobsFeedFromDB(cfg.StatePath, "", "", "", "", "newest", 5000, true, true); err == nil {
		for _, job := range snapshot.Jobs {
			company := strings.TrimSpace(job.Company)
			title := strings.TrimSpace(job.Title)
			if company != "" {
				key := companyFilterKey(company)
				if key != "" {
					labels[key] = choosePreferredCompanyLabel(labels[key], company)
				}
			}
			if title == "" {
				continue
			}
			context.Jobs = append(context.Jobs, mailKnownJob{
				Company: company,
				Title:   title,
				URL:     strings.TrimSpace(job.URL),
			})
		}
	} else {
		cacheable = false
	}
	for _, label := range labels {
		if label != "" {
			context.Companies = append(context.Companies, label)
		}
	}
	sort.Slice(context.Companies, func(i, j int) bool {
		return strings.ToLower(context.Companies[i]) < strings.ToLower(context.Companies[j])
	})
	return context, cacheable
}

func loadMailMatchContext(cfg MailServiceConfig) mailMatchContext {
	key := mailMatchContextCacheKey(cfg)
	mailMatchContextCache.mu.RLock()
	if cached, ok := mailMatchContextCache.contexts[key]; ok {
		mailMatchContextCache.mu.RUnlock()
		return cached
	}
	mailMatchContextCache.mu.RUnlock()

	mailMatchContextCache.mu.Lock()
	defer mailMatchContextCache.mu.Unlock()
	if cached, ok := mailMatchContextCache.contexts[key]; ok {
		return cached
	}
	context, cacheable := buildMailMatchContext(cfg)
	mailMatchContextCache.builds++
	if cacheable {
		mailMatchContextCache.contexts[key] = context
	}
	return context
}

func resetMailMatchContextCache() {
	mailMatchContextCache.mu.Lock()
	defer mailMatchContextCache.mu.Unlock()
	mailMatchContextCache.contexts = map[string]mailMatchContext{}
	mailMatchContextCache.builds = 0
}

func mailMatchContextBuildCount() int {
	mailMatchContextCache.mu.RLock()
	defer mailMatchContextCache.mu.RUnlock()
	return mailMatchContextCache.builds
}

var mailHistoricalClassifierStopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "this": {}, "that": {}, "your": {}, "you": {}, "are": {}, "our": {}, "from": {}, "have": {}, "has": {}, "will": {}, "was": {}, "were": {}, "but": {}, "can": {}, "not": {}, "all": {}, "any": {}, "about": {}, "hello": {}, "there": {}, "regards": {}, "best": {}, "thanks": {},
}

func mailHistoricalClassifierTokenize(text string) []string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return nil
	}
	tokens := strings.FieldsFunc(normalized, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if len(token) < 3 {
			continue
		}
		if _, err := strconv.Atoi(token); err == nil {
			continue
		}
		if _, blocked := mailHistoricalClassifierStopwords[token]; blocked {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	sort.Strings(out)
	return out
}

func mailHistoricalClassifierSenderDomain(email string) string {
	value := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(value, "@")
	if at < 0 || at == len(value)-1 {
		return ""
	}
	domain := strings.TrimSpace(value[at+1:])
	domain = strings.TrimPrefix(domain, "mail.")
	domain = strings.TrimPrefix(domain, "email.")
	return domain
}

func buildMailHistoricalClassifier(messages []MailMessage) *mailHistoricalClassifier {
	classifier := &mailHistoricalClassifier{
		SamplesByEvent:     map[string]int{},
		TokenCounts:        map[string]map[string]int{},
		TotalTokensByEvent: map[string]int{},
		DomainCounts:       map[string]map[string]int{},
		Vocabulary:         map[string]struct{}{},
		DomainVocabulary:   map[string]struct{}{},
	}
	for _, message := range messages {
		eventType := normalizeMailEventType(message.EventType)
		if eventType == "" {
			continue
		}
		tokens := mailHistoricalClassifierTokenize(mailMessageCombinedText(message))
		domain := mailHistoricalClassifierSenderDomain(message.Sender.Email)
		if len(tokens) == 0 && domain == "" {
			continue
		}
		classifier.SampleCount++
		classifier.SamplesByEvent[eventType]++
		if classifier.TokenCounts[eventType] == nil {
			classifier.TokenCounts[eventType] = map[string]int{}
		}
		if classifier.DomainCounts[eventType] == nil {
			classifier.DomainCounts[eventType] = map[string]int{}
		}
		for _, token := range tokens {
			classifier.TokenCounts[eventType][token]++
			classifier.TotalTokensByEvent[eventType]++
			classifier.Vocabulary[token] = struct{}{}
		}
		if domain != "" {
			classifier.DomainCounts[eventType][domain]++
			classifier.DomainVocabulary[domain] = struct{}{}
		}
	}
	if classifier.SampleCount < 8 || len(classifier.SamplesByEvent) < 2 {
		return nil
	}
	return classifier
}

func loadMailHistoricalClassifier(statePath string) *mailHistoricalClassifier {
	db, err := openMailDB(statePath)
	if err != nil {
		return nil
	}
	rows, err := db.Query(`
		SELECT sender_name, sender_email, subject, snippet, body_text, body_html, event_type, confidence
		FROM mail_messages
		WHERE provider = ?
			AND confidence >= ?
			AND event_type != ''
			AND (subject != '' OR snippet != '' OR body_text != '' OR body_html != '')
		ORDER BY received_at DESC, id DESC
		LIMIT 1500`,
		string(MailProviderOutlook),
		0.84,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	perEventCount := map[string]int{}
	samples := make([]MailMessage, 0, 256)
	for rows.Next() {
		var (
			senderName  string
			senderEmail string
			subject     string
			snippet     string
			bodyText    string
			bodyHTML    string
			eventType   string
			confidence  float64
		)
		if err := rows.Scan(&senderName, &senderEmail, &subject, &snippet, &bodyText, &bodyHTML, &eventType, &confidence); err != nil {
			return nil
		}
		eventType = normalizeMailEventType(eventType)
		if eventType == "" {
			continue
		}
		if perEventCount[eventType] >= 250 {
			continue
		}
		perEventCount[eventType]++
		samples = append(samples, MailMessage{
			Subject:    subject,
			Snippet:    snippet,
			BodyText:   bodyText,
			BodyHTML:   bodyHTML,
			Sender:     MailAddress{Name: senderName, Email: senderEmail},
			EventType:  eventType,
			Confidence: normalizeMailConfidence(confidence),
		})
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return buildMailHistoricalClassifier(samples)
}

func predictMailHistoricalClassifier(message MailMessage, classifier *mailHistoricalClassifier) (mailHistoricalPrediction, bool) {
	if classifier == nil || classifier.SampleCount == 0 || len(classifier.SamplesByEvent) < 2 {
		return mailHistoricalPrediction{}, false
	}
	tokens := mailHistoricalClassifierTokenize(mailMessageCombinedText(message))
	domain := mailHistoricalClassifierSenderDomain(message.Sender.Email)
	if len(tokens) == 0 && domain == "" {
		return mailHistoricalPrediction{}, false
	}

	events := make([]string, 0, len(classifier.SamplesByEvent))
	for eventType := range classifier.SamplesByEvent {
		events = append(events, eventType)
	}
	sort.Strings(events)

	vocabSize := float64(len(classifier.Vocabulary) + 1)
	domainVocabSize := float64(len(classifier.DomainVocabulary) + 1)
	scores := make(map[string]float64, len(events))
	bestEvent := ""
	bestScore := math.Inf(-1)
	secondScore := math.Inf(-1)
	for _, eventType := range events {
		sampleCount := classifier.SamplesByEvent[eventType]
		score := math.Log(float64(sampleCount+1) / float64(classifier.SampleCount+len(events)))
		totalTokens := float64(classifier.TotalTokensByEvent[eventType])
		for _, token := range tokens {
			tokenCount := classifier.TokenCounts[eventType][token]
			score += math.Log(float64(tokenCount+1) / (totalTokens + vocabSize))
		}
		if domain != "" {
			domainCount := classifier.DomainCounts[eventType][domain]
			score += 1.6 * math.Log(float64(domainCount+1)/float64(sampleCount+int(domainVocabSize)))
		}
		scores[eventType] = score
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			bestEvent = eventType
			continue
		}
		if score > secondScore {
			secondScore = score
		}
	}
	if bestEvent == "" {
		return mailHistoricalPrediction{}, false
	}
	maxScore := bestScore
	sumExp := 0.0
	for _, score := range scores {
		sumExp += math.Exp(score - maxScore)
	}
	confidence := 0.0
	if sumExp > 0 {
		confidence = 1 / sumExp
	}
	if !math.IsInf(secondScore, -1) {
		margin := bestScore - secondScore
		if margin > 0 {
			confidence = normalizeMailConfidence(confidence + math.Min(0.18, margin/12))
		}
	}
	return mailHistoricalPrediction{
		EventType:   bestEvent,
		Confidence:  normalizeMailConfidence(confidence),
		SampleCount: classifier.SamplesByEvent[bestEvent],
	}, true
}

func mailHistoricalPredictionReason(eventType string) string {
	switch normalizeMailEventType(eventType) {
	case mailEventRecruiterReply:
		return "Historical corpus classifier matched prior recruiter-reply patterns"
	case mailEventRecruiterOutreach:
		return "Historical corpus classifier matched prior recruiter-outreach patterns"
	case mailEventIndiaJobMarket:
		return "Historical corpus classifier matched prior India-market job-mail patterns"
	case mailEventJobBoardInvite:
		return "Historical corpus classifier matched prior job-invite patterns"
	case mailEventInterviewScheduled:
		return "Historical corpus classifier matched prior interview-scheduled patterns"
	case mailEventInterviewUpdated:
		return "Historical corpus classifier matched prior interview-update patterns"
	case mailEventApplicationAcknowledged:
		return "Historical corpus classifier matched prior application-acknowledgement patterns"
	case mailEventRejection:
		return "Historical corpus classifier matched prior rejection patterns"
	case mailEventOtherJobRelated:
		return "Historical corpus classifier matched prior job-related patterns"
	default:
		return "Historical corpus classifier matched prior mail patterns"
	}
}

func mailClassifierEventImportance(eventType string) bool {
	switch normalizeMailEventType(eventType) {
	case mailEventRecruiterReply, mailEventRecruiterOutreach, mailEventInterviewScheduled, mailEventInterviewUpdated, mailEventRejection:
		return true
	default:
		return false
	}
}

func applyMailHistoricalClassifier(message *MailMessage, context mailMatchContext) {
	combined := mailMessageCombinedText(*message)
	if mailLooksLikeNonJobNewsletter(*message, combined) {
		return
	}
	if mailLooksLikeHiringOpsRequest(*message, combined) {
		return
	}
	prediction, ok := predictMailHistoricalClassifier(*message, context.HistoricalClassifier)
	if !ok {
		return
	}
	currentEvent := normalizeMailEventType(message.EventType)
	hasAckOrProgressSignal := mailHasAcknowledgementOrProgressSignal(combined)
	reason := mailHistoricalPredictionReason(prediction.EventType)
	if prediction.EventType == currentEvent {
		if prediction.Confidence > message.Confidence+0.04 && !containsFoldedReason(message.Reasons, reason) {
			message.Confidence = prediction.Confidence
			message.Reasons = append(message.Reasons, reason)
		}
		return
	}
	if prediction.SampleCount < 3 {
		return
	}
	switch currentEvent {
	case mailEventIgnored:
		if prediction.EventType == mailEventIgnored || prediction.Confidence < 0.84 {
			return
		}
	case mailEventOtherJobRelated:
		if prediction.EventType == mailEventIgnored || prediction.EventType == mailEventOtherJobRelated || prediction.Confidence < 0.82 {
			return
		}
		if hasAckOrProgressSignal && (prediction.EventType == mailEventRecruiterReply || prediction.EventType == mailEventRecruiterOutreach) {
			return
		}
	default:
		return
	}

	message.EventType = prediction.EventType
	message.Importance = mailClassifierEventImportance(prediction.EventType)
	if message.TriageStatus == mailTriageIgnored && prediction.EventType != mailEventIgnored {
		message.TriageStatus = mailTriageNew
	}
	if prediction.Confidence > message.Confidence {
		message.Confidence = prediction.Confidence
	}
	message.DecisionSource = "historical"
	message.Reasons = removeFoldedReasons(
		message.Reasons,
		"Marketing/newsletter signal detected",
		"Weak job-related signal only",
		"Job-related context detected",
		"Application progress reminder detected",
	)
	if !containsFoldedReason(message.Reasons, reason) {
		message.Reasons = append(message.Reasons, reason)
	}
}

func mailMessageCombinedText(message MailMessage) string {
	parts := []string{
		strings.TrimSpace(message.Subject),
		strings.TrimSpace(message.Snippet),
		strings.TrimSpace(message.BodyText),
		strings.TrimSpace(message.BodyHTML),
		strings.TrimSpace(message.Sender.Name),
		strings.TrimSpace(message.Sender.Email),
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

type mailCompanyCandidate struct {
	Label  string
	Rank   int
	Index  int
	Length int
}

func isMailCompanyBoundaryRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsNumber(r)
}

func boundedPhraseIndex(text string, phrase string) int {
	text = strings.ToLower(strings.TrimSpace(text))
	phrase = strings.ToLower(strings.TrimSpace(phrase))
	if text == "" || phrase == "" {
		return -1
	}
	searchFrom := 0
	for {
		offset := strings.Index(text[searchFrom:], phrase)
		if offset < 0 {
			return -1
		}
		index := searchFrom + offset
		leftOK := index == 0
		if !leftOK {
			left, _ := utf8.DecodeLastRuneInString(text[:index])
			leftOK = isMailCompanyBoundaryRune(left)
		}
		rightIndex := index + len(phrase)
		rightOK := rightIndex >= len(text)
		if !rightOK {
			right, _ := utf8.DecodeRuneInString(text[rightIndex:])
			rightOK = isMailCompanyBoundaryRune(right)
		}
		if leftOK && rightOK {
			return index
		}
		searchFrom = index + len(phrase)
		if searchFrom >= len(text) {
			return -1
		}
	}
}

func allowsLooseMailBodyCompanyMatch(label string) bool {
	key := companyFilterKey(label)
	if key == "" {
		return false
	}
	if strings.Contains(strings.TrimSpace(label), " ") {
		return true
	}
	return len(key) >= 5
}

func chooseMailCompanyCandidate(best *mailCompanyCandidate, label string, rank int, index int) {
	if index < 0 || strings.TrimSpace(label) == "" {
		return
	}
	candidate := mailCompanyCandidate{
		Label:  strings.TrimSpace(label),
		Rank:   rank,
		Index:  index,
		Length: len(companyFilterKey(label)),
	}
	if best.Label == "" ||
		candidate.Rank < best.Rank ||
		(candidate.Rank == best.Rank && candidate.Index < best.Index) ||
		(candidate.Rank == best.Rank && candidate.Index == best.Index && candidate.Length > best.Length) {
		*best = candidate
	}
}

func inferMailCompany(message MailMessage, context mailMatchContext) string {
	if mailSuppressesTrackedMatching(message, mailMessageCombinedText(message)) {
		return ""
	}
	subject := strings.TrimSpace(message.Subject)
	snippet := strings.TrimSpace(message.Snippet)
	bodyText := strings.TrimSpace(message.BodyText)
	bodyHTML := strings.TrimSpace(message.BodyHTML)
	senderName := strings.TrimSpace(message.Sender.Name)
	domain := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	if at := strings.LastIndex(domain, "@"); at >= 0 {
		domain = domain[at+1:]
	}
	domain = strings.TrimPrefix(domain, "mail.")
	domain = strings.TrimPrefix(domain, "email.")
	domainKey := companyFilterKey(strings.Split(domain, ".")[0])
	best := mailCompanyCandidate{Rank: 1 << 30, Index: 1 << 30}
	for _, company := range context.Companies {
		label := strings.TrimSpace(company)
		if label == "" {
			continue
		}
		chooseMailCompanyCandidate(&best, label, 0, boundedPhraseIndex(subject, label))
		chooseMailCompanyCandidate(&best, label, 1, boundedPhraseIndex(senderName, label))
		if allowsLooseMailBodyCompanyMatch(label) {
			chooseMailCompanyCandidate(&best, label, 2, boundedPhraseIndex(snippet, label))
			chooseMailCompanyCandidate(&best, label, 3, boundedPhraseIndex(bodyText, label))
			chooseMailCompanyCandidate(&best, label, 4, boundedPhraseIndex(bodyHTML, label))
		}
		if companyFilterKey(label) == domainKey && domainKey != "" {
			chooseMailCompanyCandidate(&best, label, 1, 0)
		}
	}
	return best.Label
}

func inferMailJobTitle(message MailMessage, company string, context mailMatchContext) string {
	combined := mailMessageCombinedText(message)
	if mailSuppressesTrackedMatching(message, combined) {
		return ""
	}
	text := combined
	normalized := strings.ToLower(text)
	best := ""
	bestScore := 0
	for _, job := range context.Jobs {
		title := strings.TrimSpace(job.Title)
		if title == "" {
			continue
		}
		if company != "" && companyFilterKey(job.Company) != "" && companyFilterKey(job.Company) != companyFilterKey(company) {
			continue
		}
		lower := strings.ToLower(title)
		if !strings.Contains(normalized, lower) {
			continue
		}
		score := len(lower)
		if score > bestScore {
			best = title
			bestScore = score
		}
	}
	return best
}

func parseICSDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	layouts := []string{
		"20060102T150405Z",
		"20060102T150405",
		"20060102T1504Z",
		"20060102T1504",
		"20060102",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return normalizePossibleDate(value)
}

func extractInviteMetadata(message *MailMessage, calendar string) {
	lines := strings.Split(strings.ReplaceAll(calendar, "\r\n", "\n"), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "DTSTART"):
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 && message.MeetingStart == "" {
				message.MeetingStart = parseICSDate(parts[1])
			}
		case strings.HasPrefix(line, "DTEND"):
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 && message.MeetingEnd == "" {
				message.MeetingEnd = parseICSDate(parts[1])
			}
		case strings.HasPrefix(line, "LOCATION:"):
			message.MeetingLocation = strings.TrimSpace(strings.TrimPrefix(line, "LOCATION:"))
		case strings.HasPrefix(line, "ORGANIZER"):
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
				message.MeetingOrganizer = strings.TrimPrefix(strings.TrimSpace(parts[1]), "mailto:")
			}
		}
	}
	if message.MeetingOrganizer == "" {
		message.MeetingOrganizer = strings.TrimSpace(message.Sender.Email)
	}
	if message.MeetingStart != "" || message.MeetingEnd != "" || message.MeetingLocation != "" {
		message.HasInvite = true
	}
}

func mailDirectRecipientCount(message MailMessage) int {
	seen := map[string]struct{}{}
	for _, recipient := range message.ToRecipients {
		email := strings.ToLower(strings.TrimSpace(recipient.Email))
		if email == "" {
			continue
		}
		seen[email] = struct{}{}
	}
	return len(seen)
}

func mailHasDirectRecipient(message MailMessage) bool {
	count := mailDirectRecipientCount(message)
	return count == 0 || count <= 2
}

func mailLooksAutomatedSender(message MailMessage) bool {
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	nameLower := strings.ToLower(strings.TrimSpace(message.Sender.Name))
	return strings.Contains(senderLower, "noreply") ||
		strings.Contains(senderLower, "no-reply") ||
		strings.Contains(senderLower, "donotreply") ||
		strings.Contains(senderLower, "do-not-reply") ||
		strings.Contains(nameLower, "[bot]") ||
		strings.Contains(nameLower, "notification") ||
		strings.Contains(nameLower, "alert")
}

func mailLooksLikeIndiaJobMarket(message MailMessage, combined string) bool {
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	nameLower := strings.ToLower(strings.TrimSpace(message.Sender.Name))
	return mailIndiaJobMarketDomainRE.MatchString(senderLower) ||
		strings.Contains(nameLower, "naukri") ||
		strings.Contains(nameLower, "shine") ||
		strings.Contains(combined, "naukri.com") ||
		strings.Contains(combined, "jobs.shine.com") ||
		strings.Contains(combined, "shine.com")
}

func mailLooksLikeIndiaApplyInvite(message MailMessage, combined string) bool {
	if !mailLooksLikeIndiaJobMarket(message, combined) {
		return false
	}
	return mailIndiaApplyInviteRE.MatchString(combined)
}

func mailLooksLikeJobBoardInvite(message MailMessage, combined string) bool {
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	combinedLower := strings.ToLower(strings.TrimSpace(combined))
	laddersDigest := (strings.Contains(senderLower, "theladders.com") ||
		strings.Contains(combinedLower, "theladders.com") ||
		strings.Contains(combinedLower, "ladders logo")) &&
		(strings.Contains(subjectLower, "recruiters want your skills") ||
			strings.Contains(combinedLower, "do these jobs match what you're looking for") ||
			strings.Contains(combinedLower, "because you signed up") ||
			strings.Contains(combinedLower, "high paying jobs"))
	return mailJobInviteRE.MatchString(combined) ||
		strings.Contains(subjectLower, "urgent vacancy") ||
		strings.Contains(subjectLower, "job opportunity") ||
		laddersDigest
}

func mailCountNewsletterCueMatches(combined string) int {
	combinedLower := strings.ToLower(strings.TrimSpace(combined))
	if combinedLower == "" {
		return 0
	}
	count := 0
	for _, cue := range mailLocalNewsletterCueSnippets {
		if cue != "" && strings.Contains(combinedLower, cue) {
			count++
		}
	}
	return count
}

func mailCountAcademicEventCueMatches(combined string) int {
	combinedLower := strings.ToLower(strings.TrimSpace(combined))
	if combinedLower == "" {
		return 0
	}
	count := 0
	for _, cue := range mailAcademicEventCueSnippets {
		if cue != "" && strings.Contains(combinedLower, cue) {
			count++
		}
	}
	return count
}

func mailLooksLikeNonJobNewsletter(message MailMessage, combined string) bool {
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	nameLower := strings.ToLower(strings.TrimSpace(message.Sender.Name))
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	newsletterCueMatches := mailCountNewsletterCueMatches(combined)
	return strings.Contains(nameLower, "the b-side") ||
		strings.Contains(senderLower, "boston.com") ||
		strings.Contains(senderLower, "globe.com") ||
		strings.Contains(subjectLower, "read online") ||
		mailLocalNewsletterRE.MatchString(combined) ||
		newsletterCueMatches >= 3
}

func mailLooksLikeAcademicEvent(message MailMessage, combined string) bool {
	combinedLower := strings.ToLower(strings.TrimSpace(combined))
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	if combinedLower == "" && subjectLower == "" {
		return false
	}
	if mailAckRE.MatchString(combined) ||
		mailRejectRE.MatchString(combined) ||
		mailVerificationCodeRE.MatchString(combined) ||
		mailLooksLikeHiringOpsRequest(message, combined) {
		return false
	}
	academicSource := strings.Contains(senderLower, ".edu") ||
		strings.Contains(combinedLower, ".edu") ||
		strings.Contains(combinedLower, "university") ||
		strings.Contains(combinedLower, "campus")
	if !academicSource {
		return false
	}
	eventCueMatches := mailCountAcademicEventCueMatches(combined)
	hasEventSubject := strings.Contains(subjectLower, "speaker event") ||
		strings.Contains(subjectLower, "speaker series") ||
		(strings.Contains(subjectLower, "event") && strings.Contains(subjectLower, "register")) ||
		(strings.Contains(subjectLower, "reminder") && strings.Contains(subjectLower, "event"))
	return eventCueMatches >= 3 || (eventCueMatches >= 2 && hasEventSubject)
}

func mailLooksLikeTaxFilingSupportFollowUp(message MailMessage, combined string) bool {
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	combinedLower := strings.ToLower(strings.TrimSpace(combined))
	if !strings.Contains(senderLower, "dollartaxfiler.com") {
		return false
	}
	return strings.Contains(subjectLower, "unable to reach you") ||
		strings.Contains(combinedLower, "attempted to contact you") ||
		strings.Contains(combinedLower, "unable to reach you") ||
		strings.Contains(combinedLower, "directed to voicemail") ||
		strings.Contains(combinedLower, "please contact us")
}

func mailLooksLikeHiringOpsRequest(message MailMessage, combined string) bool {
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	if strings.Contains(senderLower, "github.com") || strings.Contains(senderLower, "githubapp.com") {
		return false
	}
	return mailHiringOpsRequestRE.MatchString(combined) ||
		strings.Contains(subjectLower, "tax filing") ||
		(strings.Contains(combined, "documents") && strings.Contains(combined, "discuss over the call")) ||
		mailLooksLikeTaxFilingSupportFollowUp(message, combined)
}

func mailLooksLikeGitHubThreadNotification(message MailMessage, combined string) bool {
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	nameLower := strings.ToLower(strings.TrimSpace(message.Sender.Name))
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	internetMessageIDLower := strings.ToLower(strings.TrimSpace(message.InternetMessageID))

	githubSignals := 0
	if strings.Contains(senderLower, "github.com") ||
		strings.Contains(senderLower, "githubapp.com") ||
		strings.Contains(senderLower, "githubusercontent.com") {
		githubSignals++
	}
	if strings.Contains(nameLower, "[bot]") {
		githubSignals++
	}
	if strings.Contains(subjectLower, "re: [") && strings.Contains(subjectLower, "/") {
		githubSignals++
	}
	if strings.Contains(combined, "github.com/") || strings.Contains(combined, "view it on github") {
		githubSignals++
	}
	if strings.Contains(combined, "reply to this email directly") && strings.Contains(combined, "github") {
		githubSignals++
	}
	if strings.Contains(combined, "you are receiving this because you authored the thread") ||
		strings.Contains(combined, "you are receiving this because you commented") ||
		strings.Contains(combined, "left a comment") ||
		strings.Contains(combined, "issuecomment-") {
		githubSignals++
	}
	if strings.Contains(internetMessageIDLower, "@github.com") {
		githubSignals++
	}
	return githubSignals >= 2
}

func mailSuppressesTrackedMatching(message MailMessage, combined string) bool {
	return mailLooksLikeIndiaJobMarket(message, combined) ||
		mailLooksLikeJobBoardInvite(message, combined) ||
		mailLooksLikeNonJobNewsletter(message, combined) ||
		mailLooksLikeAcademicEvent(message, combined) ||
		mailLooksLikeGitHubThreadNotification(message, combined)
}

func mailLooksLikeRecruiterReply(message MailMessage, combined string) bool {
	if mailLooksLikeGitHubThreadNotification(message, combined) {
		return false
	}
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	if strings.HasPrefix(subjectLower, "re:") || strings.HasPrefix(subjectLower, "message replied:") {
		return true
	}
	if mailHasAcknowledgementOrProgressSignal(combined) {
		return false
	}
	return mailRecruiterReplyCueRE.MatchString(combined) && (strings.Contains(combined, "?") || mailScheduleRE.MatchString(combined))
}

func mailLooksLikeRecruiterOutreach(combined string) bool {
	return mailRecruiterOutreachRE.MatchString(combined) || mailRecruiterRE.MatchString(combined)
}

func mailLooksLikeAutomatedRejectionSubject(message MailMessage) bool {
	subjectLower := strings.ToLower(strings.TrimSpace(message.Subject))
	senderLower := strings.ToLower(strings.TrimSpace(message.Sender.Email))
	return mailRejectSubjectRE.MatchString(subjectLower) &&
		(strings.Contains(senderLower, "no-reply") || strings.Contains(senderLower, "noreply"))
}

func mailHasAcknowledgementOrProgressSignal(combined string) bool {
	return mailAckRE.MatchString(combined) || mailApplicationProgressRE.MatchString(combined)
}

func classifyMailMessageRules(message *MailMessage, context mailMatchContext) {
	bodyText := strings.TrimSpace(message.BodyText)
	if bodyText == "" {
		bodyText = normalizeTextSnippet(message.BodyHTML, 5000)
	}
	message.BodyText = bodyText
	message.BodyHTML = sanitizeEmailHTML(message.BodyHTML)
	if strings.TrimSpace(message.Snippet) == "" {
		message.Snippet = normalizeTextSnippet(bodyText, 320)
	}

	combined := mailMessageCombinedText(*message)
	matchedCompany := inferMailCompany(*message, context)
	matchedJobTitle := inferMailJobTitle(*message, matchedCompany, context)
	message.MatchedCompany = matchedCompany
	message.MatchedJobTitle = matchedJobTitle

	jobSignalCount := 0
	if matchedCompany != "" {
		jobSignalCount++
	}
	if matchedJobTitle != "" {
		jobSignalCount++
	}
	if mailInterviewRE.MatchString(combined) {
		jobSignalCount++
	}
	if mailJobRE.MatchString(combined) {
		jobSignalCount++
	}
	hasIndiaJobMarketSignal := mailLooksLikeIndiaJobMarket(*message, combined)
	hasIndiaApplyInviteSignal := mailLooksLikeIndiaApplyInvite(*message, combined)
	hasJobBoardInviteSignal := mailLooksLikeJobBoardInvite(*message, combined)
	isNoise := mailNoiseRE.MatchString(combined) && matchedCompany == "" && matchedJobTitle == "" && !hasJobBoardInviteSignal
	hasNonJobNewsletterSignal := mailLooksLikeNonJobNewsletter(*message, combined)
	hasAcademicEventSignal := mailLooksLikeAcademicEvent(*message, combined)
	isAutomatedSender := mailLooksAutomatedSender(*message)
	hasDirectRecipient := mailHasDirectRecipient(*message)
	hasGitHubThreadNotificationSignal := mailLooksLikeGitHubThreadNotification(*message, combined)
	hasHiringOpsRequestSignal := mailLooksLikeHiringOpsRequest(*message, combined)
	hasRecruiterReplySignal := mailLooksLikeRecruiterReply(*message, combined)
	hasRecruiterOutreachSignal := mailLooksLikeRecruiterOutreach(combined)

	reasons := make([]string, 0, 4)
	appendReason := func(reason string) {
		reason = normalizeTextSnippet(reason, 120)
		if reason == "" {
			return
		}
		for _, existing := range reasons {
			if strings.EqualFold(existing, reason) {
				return
			}
		}
		reasons = append(reasons, reason)
	}
	if matchedCompany != "" {
		appendReason("Matched tracked company")
	}
	if matchedJobTitle != "" {
		appendReason("Matched tracked job title")
	}
	if message.HasInvite {
		appendReason("Calendar invite metadata detected")
	}

	hasInterviewSignal := mailInterviewRE.MatchString(combined)
	hasScheduleSignal := mailScheduleRE.MatchString(combined)
	hasTentativeInterviewSignal := mailTentativeInterviewRE.MatchString(combined)
	hasConcreteInterviewSchedule := message.HasInvite || (hasInterviewSignal && hasScheduleSignal && !hasTentativeInterviewSignal)
	hasAckOrProgressSignal := mailHasAcknowledgementOrProgressSignal(combined)
	hasReplySubject := strings.HasPrefix(strings.ToLower(strings.TrimSpace(message.Subject)), "re:") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(message.Subject)), "message replied:")

	message.DecisionSource = "rules"
	message.Confidence = 0.35
	message.TriageStatus = mailTriageNew
	message.EventType = mailEventIgnored
	message.Importance = false

	if isNoise {
		appendReason("Marketing/newsletter signal detected")
		message.Importance = false
		message.Confidence = 0.95
		message.TriageStatus = mailTriageIgnored
		message.Reasons = reasons
		return
	}

	switch {
	case hasNonJobNewsletterSignal:
		message.EventType = mailEventIgnored
		message.Importance = false
		message.Confidence = 0.98
		message.TriageStatus = mailTriageIgnored
		appendReason("Local/newsletter digest ignored")
	case hasAcademicEventSignal:
		message.EventType = mailEventIgnored
		message.Importance = false
		message.Confidence = 0.98
		message.TriageStatus = mailTriageIgnored
		appendReason("Academic or campus event ignored")
	case hasGitHubThreadNotificationSignal:
		message.EventType = mailEventIgnored
		message.Importance = false
		message.Confidence = 0.98
		message.TriageStatus = mailTriageIgnored
		appendReason("GitHub thread notification ignored")
	case hasHiringOpsRequestSignal && (jobSignalCount > 0 || mailLooksLikeTaxFilingSupportFollowUp(*message, combined)):
		message.EventType = mailEventOtherJobRelated
		message.Importance = true
		message.Confidence = 0.91
		appendReason("Hiring operations/document request detected")
	case hasIndiaApplyInviteSignal:
		message.EventType = mailEventIgnored
		message.Importance = false
		message.Confidence = 0.97
		message.TriageStatus = mailTriageIgnored
		appendReason("India-market invited-to-apply mail ignored")
	case mailLooksLikeAutomatedRejectionSubject(*message) && (matchedCompany != "" || matchedJobTitle != "" || jobSignalCount > 0):
		message.EventType = mailEventRejection
		message.Importance = true
		message.Confidence = 0.97
		appendReason("Rejection subject/update pattern detected")
	case mailRejectRE.MatchString(combined) && jobSignalCount > 0:
		message.EventType = mailEventRejection
		message.Importance = true
		message.Confidence = 0.98
		appendReason("Rejection language detected")
	case hasConcreteInterviewSchedule && mailUpdateRE.MatchString(combined):
		message.EventType = mailEventInterviewUpdated
		message.Importance = true
		message.Confidence = 0.96
		appendReason("Interview reschedule/update language detected")
	case hasConcreteInterviewSchedule:
		message.EventType = mailEventInterviewScheduled
		message.Importance = true
		message.Confidence = 0.96
		appendReason("Interview scheduling language detected")
	case !isAutomatedSender && hasDirectRecipient && !hasIndiaJobMarketSignal && !hasJobBoardInviteSignal && hasRecruiterReplySignal && jobSignalCount > 0 && (!hasAckOrProgressSignal || hasReplySubject):
		message.EventType = mailEventRecruiterReply
		message.Importance = true
		message.Confidence = 0.93
		appendReason("Direct recruiter reply signal detected")
	case mailAckRE.MatchString(combined) && jobSignalCount > 0:
		message.EventType = mailEventApplicationAcknowledged
		message.Importance = false
		message.Confidence = 0.94
		appendReason("Application acknowledgement language detected")
	case mailApplicationProgressRE.MatchString(combined) && jobSignalCount > 0 && !mailVerificationCodeRE.MatchString(combined):
		message.EventType = mailEventOtherJobRelated
		message.Importance = false
		message.Confidence = 0.84
		appendReason("Application progress reminder detected")
	case !isAutomatedSender && hasDirectRecipient && !hasIndiaJobMarketSignal && !hasJobBoardInviteSignal && !hasAckOrProgressSignal && hasRecruiterOutreachSignal && jobSignalCount > 0:
		message.EventType = mailEventRecruiterOutreach
		message.Importance = true
		message.Confidence = 0.88
		appendReason("Direct recruiter outreach signal detected")
	case hasIndiaJobMarketSignal && jobSignalCount > 0:
		message.EventType = mailEventIndiaJobMarket
		message.Importance = false
		message.Confidence = 0.93
		appendReason("India-market job platform mail detected")
	case hasJobBoardInviteSignal && jobSignalCount > 0:
		message.EventType = mailEventJobBoardInvite
		message.Importance = false
		message.Confidence = 0.92
		appendReason("Job-board invite template detected")
	case mailVerificationCodeRE.MatchString(combined) || mailAccountSetupNoiseRE.MatchString(combined):
		message.EventType = mailEventIgnored
		message.Importance = false
		message.Confidence = 0.97
		appendReason("Account setup or verification language detected")
	case jobSignalCount > 1:
		message.EventType = mailEventOtherJobRelated
		message.Importance = false
		message.Confidence = 0.78
		appendReason("Job-related context detected")
	default:
		message.EventType = mailEventIgnored
		message.Importance = false
		message.Confidence = 0.72
		if jobSignalCount > 0 {
			appendReason("Weak job-related signal only")
		}
	}
	message.Reasons = reasons
}

func mailSLMEnabled() bool {
	return parseBoolEnv("MAIL_SLM_ENABLED", true)
}

func mailSLMEndpoint() string {
	if value := strings.TrimSpace(os.Getenv("MAIL_SLM_URL")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SLM_SCORING_URL")); value != "" {
		return value
	}
	return "http://127.0.0.1:11434/api/chat"
}

func mailSLMModel() string {
	if value := strings.TrimSpace(os.Getenv("MAIL_SLM_MODEL")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SLM_SCORING_MODEL")); value != "" {
		return value
	}
	return "qwen2.5:3b"
}

func mailSLMTimeout() time.Duration {
	timeout := 20
	if raw := strings.TrimSpace(os.Getenv("MAIL_SLM_TIMEOUT_SECONDS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}
	return time.Duration(timeout) * time.Second
}

func mailSLMCacheKey(message MailMessage) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(mailSLMModel())),
		strings.ToLower(strings.TrimSpace(message.Subject)),
		strings.ToLower(strings.TrimSpace(message.Sender.Email)),
		strings.ToLower(strings.TrimSpace(message.ReceivedAt)),
		strings.ToLower(strings.TrimSpace(message.BodyText)),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func mailNeedsSLM(message MailMessage) bool {
	if !mailSLMEnabled() {
		return false
	}
	if status := normalizeMailHydrationStatus(message.HydrationStatus); status != mailHydrationComplete {
		return false
	}
	combined := mailMessageCombinedText(message)
	if mailVerificationCodeRE.MatchString(combined) || mailAccountSetupNoiseRE.MatchString(combined) {
		return false
	}
	if mailLooksLikeNonJobNewsletter(message, combined) ||
		mailLooksLikeAcademicEvent(message, combined) ||
		mailLooksLikeIndiaApplyInvite(message, combined) ||
		mailLooksLikeIndiaJobMarket(message, combined) ||
		mailLooksLikeJobBoardInvite(message, combined) ||
		mailLooksLikeHiringOpsRequest(message, combined) {
		return false
	}
	switch message.EventType {
	case mailEventInterviewScheduled, mailEventInterviewUpdated, mailEventRejection:
		if message.EventType == mailEventRejection {
			return !message.HasInvite && message.Confidence < 0.98
		}
		return !message.HasInvite
	case mailEventRecruiterReply:
		return message.Confidence < 0.92
	case mailEventRecruiterOutreach:
		return message.Confidence < 0.9
	case mailEventApplicationAcknowledged, mailEventOtherJobRelated:
		return true
	case mailEventIndiaJobMarket:
		return false
	case mailEventJobBoardInvite:
		return false
	case mailEventIgnored:
		combined := mailMessageCombinedText(message)
		if mailLooksLikeIndiaApplyInvite(message, combined) {
			return false
		}
		return message.MatchedCompany != "" || message.MatchedJobTitle != "" || mailJobRE.MatchString(combined)
	default:
		return false
	}
}

func mailSLMSystemPrompt() string {
	return strings.TrimSpace(`
You classify job-related emails for a candidate tracking job applications.

Return strict JSON only:
{
  "event_type": "recruiter_reply" | "recruiter_outreach" | "india_job_market" | "job_board_invite" | "interview_scheduled" | "interview_updated" | "application_acknowledged" | "rejection" | "other_job_related" | "ignored",
  "importance": true | false,
  "confidence": 0.0,
  "reasons": ["short reason", "..."]
}

Guidance:
- recruiter_reply: actual human recruiter or hiring-team reply in an ongoing conversation.
- recruiter_outreach: direct personal recruiter outreach that is not an application follow-up reply.
- india_job_market: India-market recruiting platform mail, such as Naukri or Shine alerts and recruiter-network blasts; not a direct recruiter reply.
- job_board_invite: platform or recruiter-network invite/apply-now blast, usually template-driven, not a direct human reply.
- interview_scheduled: interview or screening invite / confirmed meeting.
- interview_updated: reschedule, cancellation, or meeting update for an interview.
- application_acknowledged: explicit application receipt or submission confirmation only.
- rejection: clear rejection or no-longer-moving-forward notice.
- other_job_related: job-related but not one of the categories above. Use this for tax filing, onboarding paperwork, payroll/compliance forms, background-check steps, or document collection tied to an application; these are not recruiter_reply.
- ignored: newsletter, marketing, unrelated email, or account verification/setup/password reset mail.
- importance should be true for recruiter replies/outreach, interview scheduling/updates, rejections, and urgent candidate-action hiring-operations/document requests.
- confidence is between 0 and 1.
`)
}

func mailSLMSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"event_type": map[string]any{
				"type": "string",
				"enum": []string{
					mailEventRecruiterReply,
					mailEventRecruiterOutreach,
					mailEventIndiaJobMarket,
					mailEventJobBoardInvite,
					mailEventInterviewScheduled,
					mailEventInterviewUpdated,
					mailEventApplicationAcknowledged,
					mailEventRejection,
					mailEventOtherJobRelated,
					mailEventIgnored,
				},
			},
			"importance": map[string]any{"type": "boolean"},
			"confidence": map[string]any{"type": "number"},
			"reasons": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"event_type", "importance", "confidence", "reasons"},
		"additionalProperties": false,
	}
}

type mailSLMResult struct {
	EventType  string
	Importance bool
	Confidence float64
	Reasons    []string
}

var mailSLMCache = struct {
	items map[string]mailSLMResult
}{
	items: map[string]mailSLMResult{},
}

func classifyMailMessageWithSLM(message *MailMessage) error {
	key := mailSLMCacheKey(*message)
	if cached, ok := mailSLMCache.items[key]; ok {
		message.EventType = cached.EventType
		message.Importance = cached.Importance
		message.Confidence = cached.Confidence
		message.Reasons = cached.Reasons
		message.DecisionSource = "slm"
		return nil
	}
	payload := map[string]any{
		"model":  mailSLMModel(),
		"stream": false,
		"format": mailSLMSchema(),
		"messages": []map[string]string{
			{"role": "system", "content": mailSLMSystemPrompt()},
			{
				"role": "user",
				"content": strings.TrimSpace(fmt.Sprintf(
					"Subject: %s\nFrom: %s <%s>\nMatched company: %s\nMatched job title: %s\nReceived: %s\nBody: %s\n",
					normalizeTextSnippet(message.Subject, 240),
					normalizeTextSnippet(message.Sender.Name, 160),
					normalizeTextSnippet(message.Sender.Email, 160),
					normalizeTextSnippet(message.MatchedCompany, 160),
					normalizeTextSnippet(message.MatchedJobTitle, 200),
					normalizeTextSnippet(message.ReceivedAt, 64),
					normalizeTextSnippet(message.BodyText, 2200),
				)),
			},
		},
		"options": map[string]any{
			"temperature": 0,
		},
	}
	applyOllamaModelTuning(payload, mailSLMModel())
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), mailSLMTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mailSLMEndpoint(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mail slm request failed with status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	ollama := map[string]any{}
	if err := json.Unmarshal(raw, &ollama); err != nil {
		return err
	}
	content := strings.TrimSpace(asString(asMap(ollama["message"])["content"]))
	if content == "" {
		return fmt.Errorf("empty mail slm response")
	}
	if candidate := extractJSONObject(content); candidate != "" {
		content = candidate
	}
	parsed := map[string]any{}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return err
	}
	result := mailSLMResult{
		EventType:  normalizeMailEventType(asString(parsed["event_type"])),
		Importance: asBool(parsed["importance"]),
		Confidence: normalizeMailConfidence(asFloat64(parsed["confidence"])),
		Reasons:    normalizeSLMReasons(asSlice(parsed["reasons"])),
	}
	mailSLMCache.items[key] = result
	message.EventType = result.EventType
	message.Importance = result.Importance
	message.Confidence = result.Confidence
	message.Reasons = result.Reasons
	message.DecisionSource = "slm"
	return nil
}

func classifyMailMessage(message *MailMessage, context mailMatchContext) {
	classifyMailMessageRules(message, context)
	applyMailHistoricalClassifier(message, context)
	if !mailNeedsSLM(*message) {
		return
	}
	if err := classifyMailMessageWithSLM(message); err != nil {
		log.Printf("[mail] SLM classification fallback for %q: %v", message.Subject, err)
	}
}

func normalizeReadMailMessage(message *MailMessage) {
	combined := mailMessageCombinedText(*message)
	currentEvent := normalizeMailEventType(message.EventType)
	switch {
	case mailLooksLikeGitHubThreadNotification(*message, combined):
		message.EventType = mailEventIgnored
		message.Importance = false
		message.TriageStatus = mailTriageIgnored
		message.MatchedCompany = ""
		message.MatchedJobTitle = ""
		if message.Confidence < 0.98 {
			message.Confidence = 0.98
		}
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
			"Matched tracked company",
			"Matched tracked job title",
			"Weak job-related signal only",
			"Job-related context detected",
		)
		if !containsFoldedReason(message.Reasons, "GitHub thread notification ignored") {
			message.Reasons = append(message.Reasons, "GitHub thread notification ignored")
		}
	case mailLooksLikeAcademicEvent(*message, combined):
		message.EventType = mailEventIgnored
		message.Importance = false
		message.TriageStatus = mailTriageIgnored
		message.MatchedCompany = ""
		message.MatchedJobTitle = ""
		if message.Confidence < 0.98 {
			message.Confidence = 0.98
		}
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Interview scheduling language detected",
			"Interview reschedule/update language detected",
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
			"Job-board invite template detected",
			"Matched tracked company",
			"Matched tracked job title",
			"Weak job-related signal only",
			"Job-related context detected",
		)
		if !containsFoldedReason(message.Reasons, "Academic or campus event ignored") {
			message.Reasons = append(message.Reasons, "Academic or campus event ignored")
		}
	case mailLooksLikeHiringOpsRequest(*message, combined) &&
		(message.MatchedCompany != "" || message.MatchedJobTitle != "" || mailJobRE.MatchString(combined) || mailLooksLikeTaxFilingSupportFollowUp(*message, combined)):
		message.EventType = mailEventOtherJobRelated
		message.Importance = true
		if message.TriageStatus == mailTriageIgnored {
			message.TriageStatus = mailTriageNew
		}
		if message.Confidence < 0.91 {
			message.Confidence = 0.91
		}
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
			"Application acknowledgement language detected",
			"Application progress reminder detected",
			"Weak job-related signal only",
			"Job-related context detected",
		)
		if !containsFoldedReason(message.Reasons, "Hiring operations/document request detected") {
			message.Reasons = append(message.Reasons, "Hiring operations/document request detected")
		}
	case mailLooksLikeNonJobNewsletter(*message, combined):
		message.EventType = mailEventIgnored
		message.Importance = false
		message.TriageStatus = mailTriageIgnored
		message.MatchedCompany = ""
		message.MatchedJobTitle = ""
		if message.Confidence < 0.98 {
			message.Confidence = 0.98
		}
		message.Reasons = []string{"Local/newsletter digest ignored"}
	case currentEvent != mailEventRejection &&
		(mailRejectRE.MatchString(combined) || mailLooksLikeAutomatedRejectionSubject(*message)) &&
		(message.MatchedCompany != "" || message.MatchedJobTitle != "" || currentEvent != mailEventIgnored || mailJobRE.MatchString(combined)):
		message.EventType = mailEventRejection
		message.Importance = true
		message.TriageStatus = mailTriageNew
		if message.Confidence < 0.98 {
			message.Confidence = 0.98
		}
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Interview scheduling language detected",
			"Interview reschedule/update language detected",
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
			"Application acknowledgement language detected",
			"Application progress reminder detected",
			"Weak job-related signal only",
			"Job-related context detected",
		)
		if !containsFoldedReason(message.Reasons, "Rejection language detected") {
			message.Reasons = append(message.Reasons, "Rejection language detected")
		}
	case mailLooksLikeIndiaApplyInvite(*message, combined):
		message.EventType = mailEventIgnored
		message.Importance = false
		message.TriageStatus = mailTriageIgnored
		message.MatchedCompany = ""
		message.MatchedJobTitle = ""
		if message.Confidence < 0.97 {
			message.Confidence = 0.97
		}
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Interview scheduling language detected",
			"Interview reschedule/update language detected",
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
			"India-market job platform mail detected",
			"Job-board invite template detected",
			"Matched tracked company",
			"Matched tracked job title",
		)
		if !containsFoldedReason(message.Reasons, "India-market invited-to-apply mail ignored") {
			message.Reasons = append(message.Reasons, "India-market invited-to-apply mail ignored")
		}
	case (currentEvent == mailEventInterviewScheduled || currentEvent == mailEventInterviewUpdated) &&
		!message.HasInvite &&
		mailAckRE.MatchString(combined) &&
		mailTentativeInterviewRE.MatchString(combined):
		message.EventType = mailEventApplicationAcknowledged
		message.Importance = false
		message.Reasons = removeFoldedReasons(message.Reasons, "Interview scheduling language detected", "Interview reschedule/update language detected")
		if message.Confidence < 0.94 {
			message.Confidence = 0.94
		}
		if !containsFoldedReason(message.Reasons, "Application acknowledgement language detected") {
			message.Reasons = append(message.Reasons, "Application acknowledgement language detected")
		}
		if !containsFoldedReason(message.Reasons, "Tentative interview next-step language detected") {
			message.Reasons = append(message.Reasons, "Tentative interview next-step language detected")
		}
	case !mailAckRE.MatchString(combined) && (mailVerificationCodeRE.MatchString(combined) || mailAccountSetupNoiseRE.MatchString(combined)):
		message.EventType = mailEventIgnored
		message.Importance = false
		message.TriageStatus = mailTriageIgnored
		if message.Confidence < 0.97 {
			message.Confidence = 0.97
		}
		if !containsFoldedReason(message.Reasons, "Account setup or verification language detected") {
			message.Reasons = append(message.Reasons, "Account setup or verification language detected")
		}
	case currentEvent == mailEventApplicationAcknowledged && !mailAckRE.MatchString(combined) && mailApplicationProgressRE.MatchString(combined):
		message.EventType = mailEventOtherJobRelated
		message.Importance = false
		if message.Confidence < 0.84 {
			message.Confidence = 0.84
		}
		if !containsFoldedReason(message.Reasons, "Application progress reminder detected") {
			message.Reasons = append(message.Reasons, "Application progress reminder detected")
		}
	case (currentEvent == mailEventRecruiterReply || currentEvent == mailEventRecruiterOutreach || currentEvent == mailEventJobBoardInvite) &&
		mailLooksLikeIndiaJobMarket(*message, combined):
		message.EventType = mailEventIndiaJobMarket
		message.Importance = false
		message.MatchedCompany = ""
		message.MatchedJobTitle = ""
		if message.Confidence < 0.93 {
			message.Confidence = 0.93
		}
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
			"Job-board invite template detected",
			"Matched tracked company",
			"Matched tracked job title",
		)
		if !containsFoldedReason(message.Reasons, "India-market job platform mail detected") {
			message.Reasons = append(message.Reasons, "India-market job platform mail detected")
		}
	case (currentEvent == mailEventRecruiterReply || currentEvent == mailEventRecruiterOutreach || currentEvent == mailEventJobBoardInvite) &&
		mailLooksLikeJobBoardInvite(*message, combined):
		message.EventType = mailEventJobBoardInvite
		message.Importance = false
		message.MatchedCompany = ""
		message.MatchedJobTitle = ""
		if message.Confidence < 0.92 {
			message.Confidence = 0.92
		}
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
			"Matched tracked company",
			"Matched tracked job title",
		)
		if !containsFoldedReason(message.Reasons, "Job-board invite template detected") {
			message.Reasons = append(message.Reasons, "Job-board invite template detected")
		}
	case (currentEvent == mailEventRecruiterReply || currentEvent == mailEventRecruiterOutreach) &&
		mailHasAcknowledgementOrProgressSignal(combined) &&
		!mailLooksLikeRecruiterReply(*message, combined):
		message.Importance = false
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
		)
		if mailAckRE.MatchString(combined) {
			message.EventType = mailEventApplicationAcknowledged
			if message.Confidence < 0.94 {
				message.Confidence = 0.94
			}
			if !containsFoldedReason(message.Reasons, "Application acknowledgement language detected") {
				message.Reasons = append(message.Reasons, "Application acknowledgement language detected")
			}
		} else {
			message.EventType = mailEventOtherJobRelated
			if message.Confidence < 0.84 {
				message.Confidence = 0.84
			}
			if !containsFoldedReason(message.Reasons, "Application progress reminder detected") {
				message.Reasons = append(message.Reasons, "Application progress reminder detected")
			}
		}
	}
	if eventType := normalizeMailEventType(message.EventType); eventType != mailEventRecruiterReply && eventType != mailEventRecruiterOutreach {
		message.Reasons = removeFoldedReasons(
			message.Reasons,
			"Direct recruiter reply signal detected",
			"Direct recruiter outreach signal detected",
			"Recruiter reply or outreach signal detected",
		)
	}
}

func normalizeReadMailMessageWithContext(message *MailMessage, context mailMatchContext) {
	normalizeReadMailMessage(message)
	if len(context.Companies) == 0 {
		return
	}
	previousCompany := strings.TrimSpace(message.MatchedCompany)
	inferredCompany := inferMailCompany(*message, context)
	message.MatchedCompany = strings.TrimSpace(inferredCompany)
	if message.MatchedCompany == "" && previousCompany != "" {
		message.Reasons = removeFoldedReasons(message.Reasons, "Matched tracked company")
	}
	if message.MatchedCompany != "" && !containsFoldedReason(message.Reasons, "Matched tracked company") {
		message.Reasons = append(message.Reasons, "Matched tracked company")
	}
	previousJobTitle := strings.TrimSpace(message.MatchedJobTitle)
	inferredJobTitle := inferMailJobTitle(*message, message.MatchedCompany, context)
	message.MatchedJobTitle = strings.TrimSpace(inferredJobTitle)
	if message.MatchedJobTitle == "" && previousJobTitle != "" {
		message.Reasons = removeFoldedReasons(message.Reasons, "Matched tracked job title")
	}
	if message.MatchedJobTitle != "" && !containsFoldedReason(message.Reasons, "Matched tracked job title") {
		message.Reasons = append(message.Reasons, "Matched tracked job title")
	}
}

func containsFoldedReason(reasons []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, reason := range reasons {
		if strings.EqualFold(strings.TrimSpace(reason), target) {
			return true
		}
	}
	return false
}

func removeFoldedReasons(reasons []string, targets ...string) []string {
	if len(reasons) == 0 || len(targets) == 0 {
		return reasons
	}
	normalized := map[string]struct{}{}
	for _, target := range targets {
		target = strings.ToLower(strings.TrimSpace(target))
		if target != "" {
			normalized[target] = struct{}{}
		}
	}
	filtered := reasons[:0]
	for _, reason := range reasons {
		key := strings.ToLower(strings.TrimSpace(reason))
		if _, drop := normalized[key]; drop {
			continue
		}
		filtered = append(filtered, reason)
	}
	return filtered
}

func asFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return parsed
		}
	}
	return 0
}
