package monitor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var jobExternalIDTextRE = regexp.MustCompile(`(?i)\b(?:req(?:uisition)?|job|posting|reference)\s*(?:id|#|number|no\.?)?\s*[:#-]?\s*([A-Z0-9][A-Z0-9._/-]{1,39})\b`)

func normalizeJobExternalID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "[](){}<>\"'.,;:")
	if value == "" {
		return ""
	}
	if strings.EqualFold(value, "job") || strings.EqualFold(value, "req") || strings.EqualFold(value, "id") {
		return ""
	}
	return value
}

func inferJobExternalID(job Job) string {
	if candidate := normalizeJobExternalID(job.ExternalID); candidate != "" {
		return candidate
	}
	if rawURL := strings.TrimSpace(job.URL); rawURL != "" {
		if parsed, err := url.Parse(rawURL); err == nil {
			queryValues := parsed.Query()
			for _, key := range []string{"gh_jid", "jobid", "job_id", "reqid", "req_id", "requisitionid", "requisition_id", "postingid", "posting_id"} {
				if candidate := normalizeJobExternalID(queryValues.Get(key)); candidate != "" {
					return candidate
				}
			}
		}
	}
	joinedText := strings.Join([]string{
		strings.TrimSpace(job.Title),
		strings.TrimSpace(job.Description),
	}, "\n")
	matches := jobExternalIDTextRE.FindAllStringSubmatch(joinedText, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if candidate := normalizeJobExternalID(match[1]); candidate != "" {
			return candidate
		}
	}
	return ""
}

func normalizedJobIdentity(job Job) Job {
	job.ExternalID = inferJobExternalID(job)
	job.Company = strings.TrimSpace(job.Company)
	job.Source = strings.TrimSpace(job.Source)
	job.Title = strings.TrimSpace(job.Title)
	job.URL = strings.TrimSpace(job.URL)
	job.Location = strings.TrimSpace(job.Location)
	job.Team = strings.TrimSpace(job.Team)
	job.PostedAt = strings.TrimSpace(job.PostedAt)
	job.Description = strings.TrimSpace(job.Description)
	return job
}

func fingerprintHash(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(hash[:])
}

func legacyJobFingerprint(job Job) string {
	normalized := normalizedJobIdentity(job)
	return fingerprintHash(
		strings.ToLower(normalized.Company),
		"",
		strings.ToLower(normalized.URL),
		strings.ToLower(normalized.Title),
	)
}

func jobFingerprint(job Job) string {
	normalized := normalizedJobIdentity(job)
	if normalized.ExternalID != "" {
		return fingerprintHash(
			strings.ToLower(normalized.Company),
			strings.ToLower(normalized.Source),
			strings.ToLower(normalized.ExternalID),
		)
	}
	return fingerprintHash(
		strings.ToLower(normalized.Company),
		"",
		strings.ToLower(normalized.URL),
		strings.ToLower(normalized.Title),
	)
}

func groupedJobs(jobs []Job) map[string][]Job {
	grouped := map[string][]Job{}
	for _, job := range jobs {
		grouped[job.Company] = append(grouped[job.Company], job)
	}
	for company := range grouped {
		sort.Slice(grouped[company], func(i, j int) bool {
			return strings.ToLower(grouped[company][i].Title) < strings.ToLower(grouped[company][j].Title)
		})
	}
	return grouped
}

func sortedCompanyNames(m map[string][]Job) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func seenEntryToJob(company string, entry SeenEntry) Job {
	return normalizedJobIdentity(Job{
		Company:     strings.TrimSpace(company),
		Title:       strings.TrimSpace(entry.Title),
		URL:         strings.TrimSpace(entry.URL),
		Source:      strings.TrimSpace(entry.Source),
		ExternalID:  strings.TrimSpace(entry.ExternalID),
		Location:    strings.TrimSpace(entry.Location),
		Team:        strings.TrimSpace(entry.Team),
		PostedAt:    strings.TrimSpace(entry.PostedAt),
		Description: strings.TrimSpace(entry.Description),
	})
}

func shouldPruneStoredJob(job Job) bool {
	title := strings.TrimSpace(job.Title)
	url := strings.TrimSpace(job.URL)
	if title == "" || url == "" {
		return true
	}
	if isResourceAssetURL(url) {
		return true
	}
	if isClearlyNonJobLandingPage(job) {
		return true
	}
	if isNoiseTitle(title) && !hasStrongJobPathHint(url) {
		return true
	}
	joined := strings.ToLower(strings.TrimSpace(strings.Join([]string{title, job.Description}, " ")))
	for _, marker := range []string{
		"<img", "const t=", ".css-", "{display:", "data-gatsby-image", "queryselectorall(", "loading=in",
	} {
		if strings.Contains(joined, marker) {
			return true
		}
	}
	return false
}

func pruneStoredNoiseJobs(state *MonitorState) int {
	if state == nil || state.Seen == nil {
		return 0
	}
	removed := 0
	for company, entries := range state.Seen {
		if entries == nil {
			delete(state.Seen, company)
			continue
		}
		for fingerprint, entry := range entries {
			if !shouldPruneStoredJob(seenEntryToJob(company, entry)) {
				continue
			}
			delete(entries, fingerprint)
			removed++
		}
		if len(entries) == 0 {
			delete(state.Seen, company)
		}
	}
	return removed
}

func ApplyOutcomesToState(outcomes []CrawlOutcome, state *MonitorState) ([]Job, []CrawlOutcome, []string) {
	if state.Seen == nil {
		state.Seen = map[string]map[string]SeenEntry{}
	}
	if state.CompanyState == nil {
		state.CompanyState = map[string]CompanyStatus{}
	}
	if state.Blocked == nil {
		state.Blocked = map[string][]BlockedEvent{}
	}

	newJobs := make([]Job, 0)
	blockedOutcomes := make([]CrawlOutcome, 0)
	statusLines := make([]string, 0, len(outcomes))

	for _, outcome := range outcomes {
		state.CompanyState[outcome.Company] = CompanyStatus{
			Status:           outcome.Status,
			SelectedSource:   outcome.SelectedSource,
			AttemptedSources: outcome.AttemptedSources,
			Message:          outcome.Message,
			UpdatedAt:        utcNow(),
		}

		switch outcome.Status {
		case "ok":
			companySeen, ok := state.Seen[outcome.Company]
			if !ok || companySeen == nil {
				companySeen = map[string]SeenEntry{}
				state.Seen[outcome.Company] = companySeen
			}
			now := utcNow()
			currentSet := map[string]struct{}{}
			newCount := 0
			for _, job := range outcome.Jobs {
				job = normalizedJobIdentity(job)
				fp := jobFingerprint(job)
				currentSet[fp] = struct{}{}
				if _, exists := companySeen[fp]; !exists && job.ExternalID != "" {
					legacyFP := legacyJobFingerprint(job)
					if legacyFP != fp {
						if legacyEntry, legacyExists := companySeen[legacyFP]; legacyExists {
							delete(companySeen, legacyFP)
							companySeen[fp] = legacyEntry
						}
					}
				}
				if existing, exists := companySeen[fp]; exists {
					changed := false
					if existing.Title == "" && job.Title != "" {
						existing.Title = job.Title
						changed = true
					}
					if existing.URL == "" && job.URL != "" {
						existing.URL = job.URL
						changed = true
					}
					if existing.Source == "" && job.Source != "" {
						existing.Source = job.Source
						changed = true
					}
					if existing.ExternalID == "" && job.ExternalID != "" {
						existing.ExternalID = job.ExternalID
						changed = true
					}
					if existing.Location == "" && job.Location != "" {
						existing.Location = job.Location
						changed = true
					}
					if existing.Team == "" && job.Team != "" {
						existing.Team = job.Team
						changed = true
					}
					if existing.PostedAt == "" && job.PostedAt != "" {
						existing.PostedAt = job.PostedAt
						changed = true
					}
					if existing.Description == "" && job.Description != "" {
						existing.Description = job.Description
						changed = true
					}
					if existing.LastSeen != now {
						existing.LastSeen = now
						changed = true
					}
					if !existing.Active {
						existing.Active = true
						changed = true
					}
					if changed {
						companySeen[fp] = existing
					}
					continue
				}
				companySeen[fp] = SeenEntry{
					Title:       job.Title,
					URL:         job.URL,
					FirstSeen:   now,
					LastSeen:    now,
					Active:      true,
					Source:      job.Source,
					ExternalID:  job.ExternalID,
					Location:    job.Location,
					Team:        job.Team,
					PostedAt:    job.PostedAt,
					Description: job.Description,
				}
				newJobs = append(newJobs, job)
				newCount++
			}
			for fp, entry := range companySeen {
				if _, ok := currentSet[fp]; ok {
					continue
				}
				if entry.Active {
					entry.Active = false
					if entry.LastSeen == "" {
						entry.LastSeen = entry.FirstSeen
					}
					companySeen[fp] = entry
				}
			}
			statusLines = append(statusLines, fmt.Sprintf("[OK] %s (%s): %d found, %d new", outcome.Company, outcome.SelectedSource, len(outcome.Jobs), newCount))
		case "blocked":
			blockedOutcomes = append(blockedOutcomes, outcome)
			history := state.Blocked[outcome.Company]
			history = append(history, BlockedEvent{At: utcNow(), AttemptedSources: outcome.AttemptedSources, Message: outcome.Message})
			if len(history) > 30 {
				history = history[len(history)-30:]
			}
			state.Blocked[outcome.Company] = history
			statusLines = append(statusLines, fmt.Sprintf("[BLOCKED] %s (%s): %s", outcome.Company, strings.Join(outcome.AttemptedSources, " -> "), outcome.Message))
		default:
			statusLines = append(statusLines, fmt.Sprintf("[ERROR] %s (%s): %s", outcome.Company, strings.Join(outcome.AttemptedSources, " -> "), outcome.Message))
		}
	}
	if removed := pruneStoredNoiseJobs(state); removed > 0 {
		statusLines = append(statusLines, fmt.Sprintf("[CLEANUP] Removed %d stored non-job row(s)", removed))
	}

	return newJobs, blockedOutcomes, statusLines
}
