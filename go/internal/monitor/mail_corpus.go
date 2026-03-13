package monitor

import (
	"bufio"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mailCorpusExistingMatch struct {
	MessageID       int64
	EventType       string
	TriageStatus    string
	Importance      bool
	Confidence      float64
	DecisionSource  string
	MatchedCompany  string
	MatchedJobTitle string
}

type mailCorpusRecord struct {
	ReviewID                string   `json:"review_id"`
	Provider                string   `json:"provider"`
	AccountEmail            string   `json:"account_email"`
	ProviderMessageID       string   `json:"provider_message_id,omitempty"`
	ProviderThreadID        string   `json:"provider_thread_id,omitempty"`
	InternetMessageID       string   `json:"internet_message_id,omitempty"`
	ReceivedAt              string   `json:"received_at,omitempty"`
	SenderName              string   `json:"sender_name,omitempty"`
	SenderEmail             string   `json:"sender_email,omitempty"`
	SenderDomain            string   `json:"sender_domain,omitempty"`
	Subject                 string   `json:"subject"`
	Snippet                 string   `json:"snippet,omitempty"`
	BodyText                string   `json:"body_text,omitempty"`
	Labels                  []string `json:"labels,omitempty"`
	IsUnread                bool     `json:"is_unread"`
	HasInvite               bool     `json:"has_invite"`
	MeetingStart            string   `json:"meeting_start,omitempty"`
	MeetingEnd              string   `json:"meeting_end,omitempty"`
	MeetingOrganizer        string   `json:"meeting_organizer,omitempty"`
	MeetingLocation         string   `json:"meeting_location,omitempty"`
	ExistingMessageID       int64    `json:"existing_message_id,omitempty"`
	ExistingEventType       string   `json:"existing_event_type,omitempty"`
	ExistingTriageStatus    string   `json:"existing_triage_status,omitempty"`
	ExistingImportance      bool     `json:"existing_importance,omitempty"`
	ExistingConfidence      float64  `json:"existing_confidence,omitempty"`
	ExistingDecisionSource  string   `json:"existing_decision_source,omitempty"`
	ExistingMatchedCompany  string   `json:"existing_matched_company,omitempty"`
	ExistingMatchedJobTitle string   `json:"existing_matched_job_title,omitempty"`
	GoldPrimaryCategory     string   `json:"gold_primary_category,omitempty"`
	GoldJobRelated          *bool    `json:"gold_job_related,omitempty"`
	GoldNeedsAttention      *bool    `json:"gold_needs_attention,omitempty"`
	GoldActionRequired      *bool    `json:"gold_action_required,omitempty"`
	GoldAttentionReason     string   `json:"gold_attention_reason,omitempty"`
	ReviewNotes             string   `json:"review_notes,omitempty"`
}

func exportMailCorpus(statePath string, provider string, days int, maxMessages int) (MailCorpusExportResponse, error) {
	provider = normalizeMailProvider(provider)
	if provider == "" {
		provider = string(MailProviderGmail)
	}
	if days <= 0 {
		days = 30
	}
	if maxMessages <= 0 {
		maxMessages = 500
	}
	db, err := openMailDB(statePath)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	if provider == string(MailProviderOutlook) {
		return exportStoredMailCorpus(statePath, db, provider, days, maxMessages)
	}
	if provider != string(MailProviderGmail) {
		return MailCorpusExportResponse{}, fmt.Errorf("unsupported corpus provider")
	}
	account, err := loadStoredMailAccountByProvider(db, provider)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	if strings.TrimSpace(account.TokenJSON) == "" {
		return MailCorpusExportResponse{}, fmt.Errorf("%s account is not connected", provider)
	}
	token, err := ensureFreshMailToken(statePath, &account)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	messages, err := fetchGmailMessages(account, token, mailMatchContext{}, gmailFetchOptions{
		Since:       time.Now().Add(-time.Duration(days) * 24 * time.Hour),
		MaxMessages: maxMessages,
		Classify:    false,
		InboxOnly:   true,
	})
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	rows, matches, err := buildMailCorpusRecords(db, account, messages)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	filePath, err := writeMailCorpusRecords(statePath, provider, rows)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	return MailCorpusExportResponse{
		Ok:              true,
		Provider:        provider,
		AccountEmail:    account.Email,
		Days:            days,
		MaxMessages:     maxMessages,
		Exported:        len(rows),
		ExistingMatches: matches,
		FilePath:        filePath,
		GeneratedAt:     utcNow(),
		Message:         fmt.Sprintf("Exported %d %s corpus rows.", len(rows), provider),
	}, nil
}

func exportStoredMailCorpus(statePath string, db *sql.DB, provider string, days int, maxMessages int) (MailCorpusExportResponse, error) {
	account, err := loadStoredMailAccountByProvider(db, provider)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	messages, err := loadStoredMailCorpusMessages(db, provider, days, maxMessages)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	rows, matches, err := buildMailCorpusRecords(db, account, messages)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	filePath, err := writeMailCorpusRecords(statePath, provider, rows)
	if err != nil {
		return MailCorpusExportResponse{}, err
	}
	return MailCorpusExportResponse{
		Ok:              true,
		Provider:        provider,
		AccountEmail:    account.Email,
		Days:            days,
		MaxMessages:     maxMessages,
		Exported:        len(rows),
		ExistingMatches: matches,
		FilePath:        filePath,
		GeneratedAt:     utcNow(),
		Message:         fmt.Sprintf("Exported %d stored %s corpus rows.", len(rows), provider),
	}, nil
}

func loadStoredMailCorpusMessages(db *sql.DB, provider string, days int, maxMessages int) ([]MailMessage, error) {
	rawLimit := maxMessages * 3
	if rawLimit < maxMessages {
		rawLimit = maxMessages
	}
	if rawLimit > 5000 {
		rawLimit = 5000
	}

	where := []string{"m.provider = ?"}
	args := []any{provider}
	if days > 0 {
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339)
		where = append(where, "m.received_at >= ?")
		args = append(args, cutoff)
	}
	args = append(args, rawLimit)

	rows, err := db.Query(`
		SELECT
			m.id, m.account_id, m.provider, a.email, m.provider_message_id, m.provider_thread_id, m.internet_message_id,
			m.web_link, m.subject, m.sender_name, m.sender_email, m.to_recipients_json, m.cc_recipients_json,
			m.received_at, m.labels_json, m.is_unread, m.snippet, m.body_text, m.body_html,
			m.hydration_status, m.metadata_source, m.hydrated_at, m.has_invite,
			m.meeting_start, m.meeting_end, m.meeting_organizer, m.meeting_location, m.matched_company, m.matched_job_title,
			m.event_type, m.importance, m.confidence, m.decision_source, m.reasons_json, m.triage_status, m.updated_at
		FROM mail_messages m
		JOIN mail_accounts a ON a.id = m.account_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY m.received_at DESC, m.id DESC
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]MailMessage, 0, minInt(rawLimit, 2000))
	for rows.Next() {
		var (
			id                int64
			accountID         int64
			accountEmail      string
			providerMessageID string
			providerThreadID  string
			internetMessageID string
			webLink           string
			subject           string
			senderName        string
			senderEmail       string
			toRecipientsJSON  string
			ccRecipientsJSON  string
			receivedAt        string
			labelsJSON        string
			isUnreadInt       int
			snippet           string
			bodyText          string
			bodyHTML          string
			hydrationStatus   string
			metadataSource    string
			hydratedAt        string
			hasInviteInt      int
			meetingStart      string
			meetingEnd        string
			meetingOrganizer  string
			meetingLocation   string
			matchedCompany    string
			matchedJobTitle   string
			eventType         string
			importanceInt     int
			confidence        float64
			decisionSource    string
			reasonsJSON       string
			triageStatus      string
			updatedAt         string
		)
		if err := rows.Scan(
			&id,
			&accountID,
			&provider,
			&accountEmail,
			&providerMessageID,
			&providerThreadID,
			&internetMessageID,
			&webLink,
			&subject,
			&senderName,
			&senderEmail,
			&toRecipientsJSON,
			&ccRecipientsJSON,
			&receivedAt,
			&labelsJSON,
			&isUnreadInt,
			&snippet,
			&bodyText,
			&bodyHTML,
			&hydrationStatus,
			&metadataSource,
			&hydratedAt,
			&hasInviteInt,
			&meetingStart,
			&meetingEnd,
			&meetingOrganizer,
			&meetingLocation,
			&matchedCompany,
			&matchedJobTitle,
			&eventType,
			&importanceInt,
			&confidence,
			&decisionSource,
			&reasonsJSON,
			&triageStatus,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		messages = append(messages, rowToMailMessage(
			id,
			accountID,
			provider,
			accountEmail,
			providerMessageID,
			providerThreadID,
			internetMessageID,
			webLink,
			subject,
			senderName,
			senderEmail,
			toRecipientsJSON,
			ccRecipientsJSON,
			receivedAt,
			labelsJSON,
			isUnreadInt,
			snippet,
			bodyText,
			bodyHTML,
			hydrationStatus,
			metadataSource,
			hydratedAt,
			hasInviteInt,
			meetingStart,
			meetingEnd,
			meetingOrganizer,
			meetingLocation,
			matchedCompany,
			matchedJobTitle,
			eventType,
			importanceInt,
			confidence,
			decisionSource,
			reasonsJSON,
			triageStatus,
			updatedAt,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	messages = dedupeMailMessages(messages)
	sortMailMessagesByReceivedAt(messages, true)
	if len(messages) > maxMessages {
		messages = messages[:maxMessages]
	}
	return messages, nil
}

func buildMailCorpusRecords(db *sql.DB, account mailStoredAccount, messages []MailMessage) ([]mailCorpusRecord, int, error) {
	byInternetMessageID, byProviderMessageID, err := loadMailCorpusExistingMatches(db)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]mailCorpusRecord, 0, len(messages))
	matches := 0
	for _, message := range messages {
		match, ok := byInternetMessageID[strings.TrimSpace(message.InternetMessageID)]
		if !ok {
			match, ok = byProviderMessageID[strings.TrimSpace(message.ProviderMessageID)]
		}
		if ok {
			matches++
		}
		rows = append(rows, mailCorpusRecord{
			ReviewID:                mailCorpusReviewID(message),
			Provider:                message.Provider,
			AccountEmail:            nonEmpty(strings.TrimSpace(account.Email), strings.TrimSpace(message.AccountEmail)),
			ProviderMessageID:       strings.TrimSpace(message.ProviderMessageID),
			ProviderThreadID:        strings.TrimSpace(message.ProviderThreadID),
			InternetMessageID:       strings.TrimSpace(message.InternetMessageID),
			ReceivedAt:              strings.TrimSpace(message.ReceivedAt),
			SenderName:              strings.TrimSpace(message.Sender.Name),
			SenderEmail:             strings.TrimSpace(message.Sender.Email),
			SenderDomain:            mailSenderDomain(message.Sender.Email),
			Subject:                 strings.TrimSpace(message.Subject),
			Snippet:                 normalizeTextSnippet(message.Snippet, 320),
			BodyText:                normalizeTextSnippet(nonEmpty(message.BodyText, message.Snippet), 20000),
			Labels:                  append([]string{}, message.Labels...),
			IsUnread:                message.IsUnread,
			HasInvite:               message.HasInvite,
			MeetingStart:            strings.TrimSpace(message.MeetingStart),
			MeetingEnd:              strings.TrimSpace(message.MeetingEnd),
			MeetingOrganizer:        strings.TrimSpace(message.MeetingOrganizer),
			MeetingLocation:         strings.TrimSpace(message.MeetingLocation),
			ExistingMessageID:       match.MessageID,
			ExistingEventType:       strings.TrimSpace(match.EventType),
			ExistingTriageStatus:    strings.TrimSpace(match.TriageStatus),
			ExistingImportance:      match.Importance,
			ExistingConfidence:      match.Confidence,
			ExistingDecisionSource:  strings.TrimSpace(match.DecisionSource),
			ExistingMatchedCompany:  strings.TrimSpace(match.MatchedCompany),
			ExistingMatchedJobTitle: strings.TrimSpace(match.MatchedJobTitle),
		})
	}
	return rows, matches, nil
}

func loadMailCorpusExistingMatches(db *sql.DB) (map[string]mailCorpusExistingMatch, map[string]mailCorpusExistingMatch, error) {
	rows, err := db.Query(
		`SELECT id, provider_message_id, internet_message_id, event_type, triage_status, importance, confidence, decision_source, matched_company, matched_job_title
		FROM mail_messages`,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	byInternetMessageID := map[string]mailCorpusExistingMatch{}
	byProviderMessageID := map[string]mailCorpusExistingMatch{}
	for rows.Next() {
		var (
			match             mailCorpusExistingMatch
			providerMessageID string
			internetMessageID string
		)
		if err := rows.Scan(
			&match.MessageID,
			&providerMessageID,
			&internetMessageID,
			&match.EventType,
			&match.TriageStatus,
			&match.Importance,
			&match.Confidence,
			&match.DecisionSource,
			&match.MatchedCompany,
			&match.MatchedJobTitle,
		); err != nil {
			return nil, nil, err
		}
		if key := strings.TrimSpace(internetMessageID); key != "" {
			if _, exists := byInternetMessageID[key]; !exists {
				byInternetMessageID[key] = match
			}
		}
		if key := strings.TrimSpace(providerMessageID); key != "" {
			if _, exists := byProviderMessageID[key]; !exists {
				byProviderMessageID[key] = match
			}
		}
	}
	return byInternetMessageID, byProviderMessageID, rows.Err()
}

func writeMailCorpusRecords(statePath string, provider string, rows []mailCorpusRecord) (string, error) {
	dir := filepath.Join(filepath.Dir(mailDBPath(statePath)), "mail_corpus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-corpus-%s.jsonl", provider, time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return "", err
		}
	}
	if err := writer.Flush(); err != nil {
		return "", err
	}
	if absPath, err := filepath.Abs(path); err == nil {
		path = absPath
	}
	return path, nil
}

func mailCorpusReviewID(message MailMessage) string {
	key := nonEmpty(strings.TrimSpace(message.InternetMessageID), strings.TrimSpace(message.ProviderMessageID))
	if key == "" {
		key = strings.TrimSpace(message.Subject) + "|" + strings.TrimSpace(message.ReceivedAt)
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:])
}

func mailSenderDomain(email string) string {
	email = strings.TrimSpace(email)
	if at := strings.LastIndex(email, "@"); at >= 0 && at+1 < len(email) {
		return strings.ToLower(strings.TrimSpace(email[at+1:]))
	}
	return ""
}
