package monitor

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFetchGmailMessagesUsesExplicitWindow(t *testing.T) {
	since := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	rawMessage := strings.Join([]string{
		"Message-ID: <msg-1@example.com>",
		"Date: Tue, 11 Mar 2026 10:00:00 +0000",
		"From: Recruiter <recruiter@example.com>",
		"To: Candidate <candidate@example.com>",
		"Subject: Explicit Window Test",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Body text here.",
	}, "\r\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me/messages":
			if got := r.URL.Query().Get("q"); !strings.Contains(got, fmt.Sprintf("after:%d", since.Unix())) {
				t.Fatalf("query %q does not include explicit since %d", got, since.Unix())
			}
			labels := r.URL.Query()["labelIds"]
			if len(labels) != 2 || labels[0] != "INBOX" || labels[1] != "CATEGORY_PERSONAL" {
				t.Fatalf("labelIds = %#v, want [INBOX CATEGORY_PERSONAL]", labels)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{{"id": "msg-1"}},
			})
		case "/users/me/messages/msg-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"threadId":     "thread-1",
				"labelIds":     []string{"INBOX", "UNREAD"},
				"internalDate": "1741687200000",
				"snippet":      "Body text here.",
				"raw":          base64.RawURLEncoding.EncodeToString([]byte(rawMessage)),
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	original := gmailAPIBaseURL
	gmailAPIBaseURL = server.URL
	defer func() { gmailAPIBaseURL = original }()

	messages, err := fetchGmailMessages(
		mailStoredAccount{MailAccount: MailAccount{Provider: string(MailProviderGmail)}},
		providerToken{AccessToken: "token"},
		mailMatchContext{},
		gmailFetchOptions{
			Since:       since,
			MaxMessages: 1,
			Classify:    false,
			InboxOnly:   true,
		},
	)
	if err != nil {
		t.Fatalf("fetchGmailMessages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message := messages[0]
	if message.Subject != "Explicit Window Test" {
		t.Fatalf("subject = %q", message.Subject)
	}
	if message.EventType != "" {
		t.Fatalf("event type = %q, want empty when Classify=false", message.EventType)
	}
	if !strings.Contains(message.BodyText, "Body text here.") {
		t.Fatalf("body text = %q", message.BodyText)
	}
	if !message.IsUnread {
		t.Fatalf("expected unread label to be preserved")
	}
}

func TestExportMailCorpusWritesReviewRows(t *testing.T) {
	statePath := t.TempDir() + "/state.json"
	rawMessage := strings.Join([]string{
		"Message-ID: <export-1@example.com>",
		"Date: Tue, 11 Mar 2026 09:30:00 +0000",
		"From: Recruiter <recruiter@example.com>",
		"To: Candidate <candidate@example.com>",
		"Subject: Export Me",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Please pick an interview slot this week.",
	}, "\r\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{{"id": "gmail-msg-1"}},
			})
		case "/users/me/messages/gmail-msg-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"threadId":     "gmail-thread-1",
				"labelIds":     []string{"INBOX"},
				"internalDate": "1741685400000",
				"snippet":      "Please pick an interview slot this week.",
				"raw":          base64.RawURLEncoding.EncodeToString([]byte(rawMessage)),
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	original := gmailAPIBaseURL
	gmailAPIBaseURL = server.URL
	defer func() { gmailAPIBaseURL = original }()

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider: string(MailProviderGmail),
			Email:    "candidate@gmail.com",
			Status:   "connected",
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{
		{
			Provider:          string(MailProviderGmail),
			ProviderMessageID: "existing-msg",
			InternetMessageID: "<export-1@example.com>",
			Subject:           "Existing copy",
			Sender:            MailAddress{Email: "recruiter@example.com"},
			ReceivedAt:        "2026-03-11T09:30:00Z",
			Snippet:           "Please pick an interview slot this week.",
			BodyText:          "Please pick an interview slot this week.",
			HydrationStatus:   mailHydrationComplete,
			MetadataSource:    mailMetadataSourceLegacy,
			EventType:         mailEventRecruiterReply,
			TriageStatus:      mailTriageImportant,
			Importance:        true,
			Confidence:        0.94,
			DecisionSource:    "rules",
			MatchedCompany:    "Example Corp",
			MatchedJobTitle:   "Software Engineer",
		},
	}); err != nil {
		t.Fatalf("upsertMailMessages error: %v", err)
	}

	response, err := exportMailCorpus(statePath, string(MailProviderGmail), 30, 10)
	if err != nil {
		t.Fatalf("exportMailCorpus error: %v", err)
	}
	if response.Exported != 1 {
		t.Fatalf("exported = %d, want 1", response.Exported)
	}
	if response.ExistingMatches != 1 {
		t.Fatalf("existing matches = %d, want 1", response.ExistingMatches)
	}
	if _, err := os.Stat(response.FilePath); err != nil {
		t.Fatalf("export file stat error: %v", err)
	}

	file, err := os.Open(response.FilePath)
	if err != nil {
		t.Fatalf("open export file error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("expected one exported row")
	}
	var row mailCorpusRecord
	if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
		t.Fatalf("unmarshal export row error: %v", err)
	}
	if row.Subject != "Export Me" {
		t.Fatalf("subject = %q, want Export Me", row.Subject)
	}
	if row.ExistingEventType != mailEventRecruiterReply {
		t.Fatalf("existing event type = %q, want %q", row.ExistingEventType, mailEventRecruiterReply)
	}
	if row.ExistingMatchedCompany != "Example Corp" {
		t.Fatalf("existing matched company = %q", row.ExistingMatchedCompany)
	}
	if row.GoldPrimaryCategory != "" {
		t.Fatalf("gold primary category = %q, want empty", row.GoldPrimaryCategory)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
}

func TestExportMailCorpusSupportsStoredOutlookHistory(t *testing.T) {
	statePath := t.TempDir() + "/state.json"
	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderOutlook),
			Email:       "student@northeastern.edu",
			DisplayName: "Student Outlook",
			Status:      "connected",
			Scopes:      []string{"outlook_web_session"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"kind":"outlook_web","storage_state_path":"state.json"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}
	receivedAt := time.Date(2026, time.March, 11, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{
		{
			Provider:          string(MailProviderOutlook),
			ProviderMessageID: "outlook-msg-1",
			ProviderThreadID:  "outlook-thread-1",
			InternetMessageID: "<outlook-export-1@example.com>",
			Subject:           "Interview scheduled",
			Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@example.com"},
			ReceivedAt:        receivedAt,
			Snippet:           "Please pick a time slot.",
			BodyText:          "Please pick a time slot for your interview.",
			HydrationStatus:   mailHydrationComplete,
			MetadataSource:    mailMetadataSourceOWAService,
			EventType:         mailEventInterviewScheduled,
			TriageStatus:      mailTriageImportant,
			Importance:        true,
			Confidence:        0.93,
			DecisionSource:    "historical",
			MatchedCompany:    "Example Corp",
			MatchedJobTitle:   "Software Engineer",
		},
	}); err != nil {
		t.Fatalf("upsertMailMessages error: %v", err)
	}

	response, err := exportMailCorpus(statePath, string(MailProviderOutlook), 30, 10)
	if err != nil {
		t.Fatalf("exportMailCorpus error: %v", err)
	}
	if response.Exported != 1 {
		t.Fatalf("exported = %d, want 1", response.Exported)
	}
	if response.Provider != string(MailProviderOutlook) {
		t.Fatalf("provider = %q", response.Provider)
	}

	file, err := os.Open(response.FilePath)
	if err != nil {
		t.Fatalf("open export file error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("expected one exported row")
	}
	var row mailCorpusRecord
	if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
		t.Fatalf("unmarshal export row error: %v", err)
	}
	if row.Provider != string(MailProviderOutlook) {
		t.Fatalf("row provider = %q", row.Provider)
	}
	if row.ExistingEventType != mailEventInterviewScheduled {
		t.Fatalf("existing event type = %q", row.ExistingEventType)
	}
	if row.BodyText == "" {
		t.Fatal("expected body text in stored outlook export")
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
}
