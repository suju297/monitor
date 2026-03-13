package monitor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

var mailDBRegistry = struct {
	mu  sync.Mutex
	dbs map[string]*sql.DB
}{
	dbs: map[string]*sql.DB{},
}

func mailDBEnabled() bool {
	return parseBoolEnv("MAIL_DB_ENABLED", true)
}

func mailDBPath(statePath string) string {
	if configured := strings.TrimSpace(os.Getenv("MAIL_DB_PATH")); configured != "" {
		return configured
	}
	root := strings.TrimSpace(filepath.Dir(strings.TrimSpace(statePath)))
	if root == "" || root == "." {
		return ".state/mail.db"
	}
	return filepath.Join(root, "mail.db")
}

func mailDBBusyTimeoutMS() int {
	value := 5000
	if raw := strings.TrimSpace(os.Getenv("MAIL_DB_BUSY_TIMEOUT_MS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	if value < 1000 {
		return 1000
	}
	if value > 30000 {
		return 30000
	}
	return value
}

func openMailDB(statePath string) (*sql.DB, error) {
	if !mailDBEnabled() {
		return nil, fmt.Errorf("mail db disabled")
	}
	path := mailDBPath(statePath)
	if absPath, err := filepath.Abs(path); err == nil {
		path = absPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	mailDBRegistry.mu.Lock()
	if existing := mailDBRegistry.dbs[path]; existing != nil {
		mailDBRegistry.mu.Unlock()
		if err := archiveHistoricalOutlookAccounts(existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	mailDBRegistry.mu.Unlock()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA temp_store=MEMORY`,
		`PRAGMA foreign_keys=ON`,
		fmt.Sprintf(`PRAGMA busy_timeout=%d`, mailDBBusyTimeoutMS()),
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := ensureMailDBSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := archiveHistoricalOutlookAccounts(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	mailDBRegistry.mu.Lock()
	if existing := mailDBRegistry.dbs[path]; existing != nil {
		mailDBRegistry.mu.Unlock()
		_ = db.Close()
		return existing, nil
	}
	mailDBRegistry.dbs[path] = db
	mailDBRegistry.mu.Unlock()
	return db, nil
}

func ensureMailDBSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS mail_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL UNIQUE,
			email TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'disconnected',
			token_json TEXT NOT NULL DEFAULT '',
			scopes_json TEXT NOT NULL DEFAULT '[]',
			connected_at TEXT,
			last_sync_at TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mail_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id INTEGER NOT NULL,
			provider TEXT NOT NULL,
			provider_message_id TEXT NOT NULL,
			provider_thread_id TEXT NOT NULL DEFAULT '',
			internet_message_id TEXT NOT NULL DEFAULT '',
			web_link TEXT NOT NULL DEFAULT '',
			subject TEXT NOT NULL DEFAULT '',
			sender_name TEXT NOT NULL DEFAULT '',
			sender_email TEXT NOT NULL DEFAULT '',
			to_recipients_json TEXT NOT NULL DEFAULT '[]',
			cc_recipients_json TEXT NOT NULL DEFAULT '[]',
			received_at TEXT NOT NULL DEFAULT '',
			labels_json TEXT NOT NULL DEFAULT '[]',
			is_unread INTEGER NOT NULL DEFAULT 0,
			snippet TEXT NOT NULL DEFAULT '',
			body_text TEXT NOT NULL DEFAULT '',
			body_html TEXT NOT NULL DEFAULT '',
			hydration_status TEXT NOT NULL DEFAULT 'complete',
			metadata_source TEXT NOT NULL DEFAULT 'legacy',
			hydrated_at TEXT NOT NULL DEFAULT '',
			has_invite INTEGER NOT NULL DEFAULT 0,
			meeting_start TEXT NOT NULL DEFAULT '',
			meeting_end TEXT NOT NULL DEFAULT '',
			meeting_organizer TEXT NOT NULL DEFAULT '',
			meeting_location TEXT NOT NULL DEFAULT '',
			matched_company TEXT NOT NULL DEFAULT '',
			matched_job_title TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL DEFAULT 'ignored',
			importance INTEGER NOT NULL DEFAULT 0,
			confidence REAL NOT NULL DEFAULT 0,
			decision_source TEXT NOT NULL DEFAULT 'rules',
			reasons_json TEXT NOT NULL DEFAULT '[]',
			triage_status TEXT NOT NULL DEFAULT 'new',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(account_id) REFERENCES mail_accounts(id) ON DELETE CASCADE,
			UNIQUE(account_id, provider_message_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_messages_received_at ON mail_messages(received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_messages_event_type ON mail_messages(event_type, received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_messages_triage ON mail_messages(triage_status, received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_messages_company ON mail_messages(matched_company, received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_messages_unread_important ON mail_messages(is_unread, importance, received_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	for _, migration := range []struct {
		table  string
		column string
		def    string
	}{
		{table: "mail_messages", column: "hydration_status", def: `TEXT NOT NULL DEFAULT 'complete'`},
		{table: "mail_messages", column: "metadata_source", def: `TEXT NOT NULL DEFAULT 'legacy'`},
		{table: "mail_messages", column: "hydrated_at", def: `TEXT NOT NULL DEFAULT ''`},
	} {
		if err := ensureSQLiteColumn(db, migration.table, migration.column, migration.def); err != nil {
			return err
		}
	}
	return nil
}

func ensureSQLiteColumn(db *sql.DB, table string, column string, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(column)) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func normalizeMailProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(MailProviderGmail):
		return string(MailProviderGmail)
	case string(MailProviderOutlook), "graph", "microsoft":
		return string(MailProviderOutlook)
	default:
		return ""
	}
}

func liveMailProvider() string {
	return string(MailProviderGmail)
}

func archiveHistoricalOutlookAccounts(db *sql.DB) error {
	_, err := db.Exec(
		`UPDATE mail_accounts
		SET status = ?, token_json = '', scopes_json = '[]', last_error = '', updated_at = ?
		WHERE provider = ?`,
		"disconnected",
		utcNow(),
		string(MailProviderOutlook),
	)
	return err
}

func appendLiveMailProvider(where []string, args []any, alias string) ([]string, []any) {
	columnPrefix := ""
	if alias = strings.TrimSpace(alias); alias != "" {
		columnPrefix = alias + "."
	}
	where = append(where, columnPrefix+"provider = ?")
	args = append(args, liveMailProvider())
	return where, args
}

func normalizeMailEventType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case mailEventRecruiterReply,
		mailEventRecruiterOutreach,
		mailEventIndiaJobMarket,
		mailEventJobBoardInvite,
		mailEventInterviewScheduled,
		mailEventInterviewUpdated,
		mailEventApplicationAcknowledged,
		mailEventRejection,
		mailEventOtherJobRelated,
		mailEventIgnored:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return mailEventIgnored
	}
}

func normalizeMailTriageStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case mailTriageNew, mailTriageReviewed, mailTriageImportant, mailTriageIgnored, mailTriageFollowUp:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return mailTriageNew
	}
}

func normalizeMailHydrationStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case mailHydrationPending, mailHydrationComplete, mailHydrationFailed:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return mailHydrationComplete
	}
}

func normalizeMailMetadataSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case mailMetadataSourceGraph,
		mailMetadataSourceOWAService,
		mailMetadataSourceDOMRow,
		mailMetadataSourceDOMOpen,
		mailMetadataSourceLegacy:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return mailMetadataSourceLegacy
	}
}

func normalizeMailConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func buildMailWhere(filters MailMessageFilters, alias string, includeEvent bool, includeTriage bool, includeUnread bool, includeImportant bool) ([]string, []any) {
	columnPrefix := ""
	if alias = strings.TrimSpace(alias); alias != "" {
		columnPrefix = alias + "."
	}
	where := []string{"1=1"}
	args := make([]any, 0, 8)
	if filters.AccountID > 0 {
		where = append(where, columnPrefix+"account_id = ?")
		args = append(args, filters.AccountID)
	}
	if provider := normalizeMailProvider(filters.Provider); provider != "" {
		where = append(where, columnPrefix+"provider = ?")
		args = append(args, provider)
	}
	if includeEvent {
		if eventType := strings.TrimSpace(filters.EventType); eventType != "" {
			where = append(where, columnPrefix+"event_type = ?")
			args = append(args, normalizeMailEventType(eventType))
		}
	}
	if includeTriage {
		if triageStatus := strings.TrimSpace(filters.TriageStatus); triageStatus != "" {
			where = append(where, columnPrefix+"triage_status = ?")
			args = append(args, normalizeMailTriageStatus(triageStatus))
		}
	}
	if company := companyFilterKey(filters.Company); company != "" {
		where = append(where, "lower(replace("+columnPrefix+"matched_company, ' ', '')) = ?")
		args = append(args, company)
	}
	if includeUnread && filters.UnreadOnly {
		where = append(where, columnPrefix+"is_unread = 1")
	}
	if includeImportant && filters.ImportantOnly {
		where = append(where, columnPrefix+"importance = 1")
	}
	return where, args
}

func parseMailTime(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}
	}
	return parsed.In(time.Local)
}

func mailDailyBucketKey(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func mailLifecycleTitleKey(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ""
	}
	var builder strings.Builder
	lastSpace := false
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			builder.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func mailDisplaySender(message MailMessage) string {
	if value := strings.TrimSpace(message.Sender.Name); value != "" {
		return value
	}
	return strings.TrimSpace(message.Sender.Email)
}

func mailDisplayCompany(message MailMessage) string {
	if value := strings.TrimSpace(message.MatchedCompany); value != "" {
		return value
	}
	if value := strings.TrimSpace(message.Sender.Name); value != "" {
		return value
	}
	return strings.TrimSpace(message.Sender.Email)
}

func mailDisplaySubject(value string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if normalized == "" {
		return "(No subject)"
	}
	normalized = strings.TrimSpace(strings.TrimSuffix(normalized, "Summarize"))
	if normalized == "" {
		return "(No subject)"
	}
	return normalized
}

func mailLogicalMessageKey(message MailMessage) string {
	if key := strings.ToLower(strings.TrimSpace(message.InternetMessageID)); key != "" {
		return key
	}
	return ""
}

func mailMessageMetadataRank(message MailMessage) int {
	switch normalizeMailMetadataSource(message.MetadataSource) {
	case mailMetadataSourceGraph:
		return 5
	case mailMetadataSourceLegacy:
		return 4
	case mailMetadataSourceOWAService:
		return 3
	case mailMetadataSourceDOMOpen:
		return 2
	case mailMetadataSourceDOMRow:
		return 1
	default:
		return 0
	}
}

func mailMessageProviderRank(message MailMessage) int {
	switch normalizeMailProvider(message.Provider) {
	case string(MailProviderGmail):
		return 2
	case string(MailProviderOutlook):
		return 1
	default:
		return 0
	}
}

func mailMessageQualityScore(message MailMessage) int {
	score := 0
	if normalizeMailHydrationStatus(message.HydrationStatus) == mailHydrationComplete {
		score += 1000
	}
	if strings.TrimSpace(message.BodyText) != "" {
		score += 300
	}
	if strings.TrimSpace(message.BodyHTML) != "" {
		score += 200
	}
	if strings.TrimSpace(message.Snippet) != "" {
		score += 40
	}
	if strings.TrimSpace(message.MeetingStart) != "" || strings.TrimSpace(message.MeetingEnd) != "" {
		score += 30
	}
	if message.HasInvite {
		score += 20
	}
	if strings.TrimSpace(message.MatchedCompany) != "" {
		score += 10
	}
	if strings.TrimSpace(message.MatchedJobTitle) != "" {
		score += 10
	}
	score += mailMessageMetadataRank(message) * 20
	score += mailMessageProviderRank(message) * 10
	return score
}

func shouldPreferMailMessage(candidate MailMessage, existing MailMessage) bool {
	candidateScore := mailMessageQualityScore(candidate)
	existingScore := mailMessageQualityScore(existing)
	if candidateScore != existingScore {
		return candidateScore > existingScore
	}
	candidateReceivedAt := parseMailTime(candidate.ReceivedAt)
	existingReceivedAt := parseMailTime(existing.ReceivedAt)
	if !candidateReceivedAt.Equal(existingReceivedAt) {
		return candidateReceivedAt.After(existingReceivedAt)
	}
	candidateUpdatedAt := parseMailTime(candidate.UpdatedAt)
	existingUpdatedAt := parseMailTime(existing.UpdatedAt)
	if !candidateUpdatedAt.Equal(existingUpdatedAt) {
		return candidateUpdatedAt.After(existingUpdatedAt)
	}
	return candidate.ID > existing.ID
}

func dedupeMailMessages(messages []MailMessage) []MailMessage {
	if len(messages) == 0 {
		return messages
	}
	indexByKey := make(map[string]int, len(messages))
	out := make([]MailMessage, 0, len(messages))
	for _, message := range messages {
		key := mailLogicalMessageKey(message)
		if key == "" {
			out = append(out, message)
			continue
		}
		if index, ok := indexByKey[key]; ok {
			if shouldPreferMailMessage(message, out[index]) {
				out[index] = message
			}
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, message)
	}
	return out
}

func sortMailMessagesByReceivedAt(messages []MailMessage, descending bool) {
	sort.Slice(messages, func(i, j int) bool {
		left := parseMailTime(messages[i].ReceivedAt)
		right := parseMailTime(messages[j].ReceivedAt)
		if !left.Equal(right) {
			if descending {
				return left.After(right)
			}
			return left.Before(right)
		}
		if messages[i].ID != messages[j].ID {
			if descending {
				return messages[i].ID > messages[j].ID
			}
			return messages[i].ID < messages[j].ID
		}
		if descending {
			return strings.Compare(messages[i].ProviderMessageID, messages[j].ProviderMessageID) > 0
		}
		return strings.Compare(messages[i].ProviderMessageID, messages[j].ProviderMessageID) < 0
	})
}

func mailIsUpcomingMeetingMessage(message MailMessage, now time.Time) bool {
	if !message.HasInvite {
		return false
	}
	meetingEnd := parseMailTime(message.MeetingEnd)
	meetingStart := parseMailTime(message.MeetingStart)
	cutoff := meetingEnd
	if cutoff.IsZero() {
		cutoff = meetingStart
	}
	if cutoff.IsZero() {
		return false
	}
	return !cutoff.Before(now.Add(-15 * time.Minute))
}

func mailIsOpenActionMessage(message MailMessage) bool {
	status := normalizeMailTriageStatus(message.TriageStatus)
	if status == mailTriageIgnored || status == mailTriageReviewed {
		return false
	}
	if status == mailTriageFollowUp {
		return true
	}
	switch normalizeMailEventType(message.EventType) {
	case mailEventRecruiterReply, mailEventInterviewScheduled, mailEventInterviewUpdated:
		return status == mailTriageNew || status == mailTriageImportant
	default:
		return false
	}
}

func mailPollIntervalMinutes() int {
	value := 10
	if raw := strings.TrimSpace(os.Getenv("MAIL_POLL_INTERVAL_MINUTES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	if value < 5 {
		return 5
	}
	if value > 24*60 {
		return 24 * 60
	}
	return value
}

func mailBackfillDays() int {
	value := 7
	if raw := strings.TrimSpace(os.Getenv("MAIL_BACKFILL_DAYS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	if value < 1 {
		return 1
	}
	if value > 365 {
		return 365
	}
	return value
}

func upsertMailAccount(statePath string, account mailStoredAccount) (MailAccount, error) {
	empty := MailAccount{}
	db, err := openMailDB(statePath)
	if err != nil {
		return empty, err
	}

	now := utcNow()
	provider := normalizeMailProvider(account.Provider)
	if provider == "" {
		return empty, fmt.Errorf("unsupported provider")
	}
	scopesJSON, err := json.Marshal(account.Scopes)
	if err != nil {
		return empty, err
	}
	connectedAt := strings.TrimSpace(account.ConnectedAt)
	if connectedAt == "" {
		connectedAt = now
	}
	_, err = db.Exec(
		`INSERT INTO mail_accounts (
			provider, email, display_name, status, token_json, scopes_json, connected_at, last_sync_at, last_error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider) DO UPDATE SET
			email=excluded.email,
			display_name=excluded.display_name,
			status=excluded.status,
			token_json=excluded.token_json,
			scopes_json=excluded.scopes_json,
			connected_at=excluded.connected_at,
			last_error=excluded.last_error,
			updated_at=excluded.updated_at`,
		provider,
		strings.TrimSpace(account.Email),
		strings.TrimSpace(account.DisplayName),
		nonEmpty(strings.TrimSpace(account.Status), "connected"),
		strings.TrimSpace(account.TokenJSON),
		string(scopesJSON),
		connectedAt,
		strings.TrimSpace(account.LastSyncAt),
		strings.TrimSpace(account.LastError),
		now,
		now,
	)
	if err != nil {
		return empty, err
	}
	stored, err := loadStoredMailAccountByProvider(db, provider)
	if err != nil {
		return empty, err
	}
	return stored.MailAccount, nil
}

func loadStoredMailAccountByProvider(db *sql.DB, provider string) (mailStoredAccount, error) {
	account := mailStoredAccount{}
	var scopesJSON string
	err := db.QueryRow(
		`SELECT id, provider, email, display_name, status, scopes_json, connected_at, last_sync_at, last_error, updated_at, token_json
		 FROM mail_accounts WHERE provider = ?`,
		provider,
	).Scan(
		&account.ID,
		&account.Provider,
		&account.Email,
		&account.DisplayName,
		&account.Status,
		&scopesJSON,
		&account.ConnectedAt,
		&account.LastSyncAt,
		&account.LastError,
		&account.UpdatedAt,
		&account.TokenJSON,
	)
	if err != nil {
		return account, err
	}
	_ = json.Unmarshal([]byte(scopesJSON), &account.Scopes)
	return account, nil
}

func loadStoredMailAccounts(statePath string) ([]mailStoredAccount, error) {
	db, err := openMailDB(statePath)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(
		`SELECT id, provider, email, display_name, status, scopes_json, connected_at, last_sync_at, last_error, updated_at, token_json
		 FROM mail_accounts ORDER BY provider ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]mailStoredAccount, 0)
	for rows.Next() {
		account := mailStoredAccount{}
		var scopesJSON string
		if err := rows.Scan(
			&account.ID,
			&account.Provider,
			&account.Email,
			&account.DisplayName,
			&account.Status,
			&scopesJSON,
			&account.ConnectedAt,
			&account.LastSyncAt,
			&account.LastError,
			&account.UpdatedAt,
			&account.TokenJSON,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopesJSON), &account.Scopes)
		out = append(out, account)
	}
	return out, rows.Err()
}

func loadMailAccounts(statePath string) ([]MailAccount, error) {
	stored, err := loadStoredMailAccounts(statePath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "mail db disabled") {
			return []MailAccount{}, nil
		}
		return nil, err
	}
	out := make([]MailAccount, 0, len(stored))
	for _, account := range stored {
		if normalizeMailProvider(account.Provider) != liveMailProvider() {
			continue
		}
		out = append(out, account.MailAccount)
	}
	return out, nil
}

func updateMailAccountSyncState(statePath string, accountID int64, lastSyncAt string, lastError string) error {
	db, err := openMailDB(statePath)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`UPDATE mail_accounts SET last_sync_at = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(lastSyncAt),
		strings.TrimSpace(lastError),
		utcNow(),
		accountID,
	)
	return err
}

func disconnectMailAccount(statePath string, provider string) (MailAccount, error) {
	empty := MailAccount{}
	normalizedProvider := normalizeMailProvider(provider)
	if normalizedProvider != liveMailProvider() {
		return empty, fmt.Errorf("unsupported provider")
	}
	db, err := openMailDB(statePath)
	if err != nil {
		return empty, err
	}
	result, err := db.Exec(
		`UPDATE mail_accounts
		SET status = ?, token_json = '', scopes_json = '[]', last_error = '', updated_at = ?
		WHERE provider = ?`,
		"disconnected",
		utcNow(),
		normalizedProvider,
	)
	if err != nil {
		return empty, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return empty, err
	}
	if affected == 0 {
		return empty, sql.ErrNoRows
	}
	stored, err := loadStoredMailAccountByProvider(db, normalizedProvider)
	if err != nil {
		return empty, err
	}
	return stored.MailAccount, nil
}

func encodeMailAddresses(addresses []MailAddress) string {
	if len(addresses) == 0 {
		return "[]"
	}
	body, err := json.Marshal(addresses)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func decodeMailAddresses(raw string) []MailAddress {
	out := make([]MailAddress, 0)
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &out)
	return out
}

func decodeStringSlice(raw string) []string {
	out := make([]string, 0)
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &out)
	return out
}

func encodeStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	body, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func rowToMailMessage(
	id int64,
	accountID int64,
	provider string,
	accountEmail string,
	providerMessageID string,
	providerThreadID string,
	internetMessageID string,
	webLink string,
	subject string,
	senderName string,
	senderEmail string,
	toRecipientsJSON string,
	ccRecipientsJSON string,
	receivedAt string,
	labelsJSON string,
	isUnreadInt int,
	snippet string,
	bodyText string,
	bodyHTML string,
	hydrationStatus string,
	metadataSource string,
	hydratedAt string,
	hasInviteInt int,
	meetingStart string,
	meetingEnd string,
	meetingOrganizer string,
	meetingLocation string,
	matchedCompany string,
	matchedJobTitle string,
	eventType string,
	importanceInt int,
	confidence float64,
	decisionSource string,
	reasonsJSON string,
	triageStatus string,
	updatedAt string,
) MailMessage {
	message := MailMessage{
		ID:                id,
		AccountID:         accountID,
		Provider:          normalizeMailProvider(provider),
		AccountEmail:      strings.TrimSpace(accountEmail),
		ProviderMessageID: strings.TrimSpace(providerMessageID),
		ProviderThreadID:  strings.TrimSpace(providerThreadID),
		InternetMessageID: strings.TrimSpace(internetMessageID),
		WebLink:           strings.TrimSpace(webLink),
		Subject:           strings.TrimSpace(subject),
		Sender: MailAddress{
			Name:  strings.TrimSpace(senderName),
			Email: strings.TrimSpace(senderEmail),
		},
		ToRecipients:     decodeMailAddresses(toRecipientsJSON),
		CcRecipients:     decodeMailAddresses(ccRecipientsJSON),
		ReceivedAt:       strings.TrimSpace(receivedAt),
		ReceivedAtLocal:  isoToLocal(receivedAt),
		UpdatedAt:        strings.TrimSpace(updatedAt),
		UpdatedAtLocal:   isoToLocal(updatedAt),
		Labels:           decodeStringSlice(labelsJSON),
		IsUnread:         isUnreadInt != 0,
		Snippet:          strings.TrimSpace(snippet),
		BodyText:         strings.TrimSpace(bodyText),
		BodyHTML:         strings.TrimSpace(bodyHTML),
		HydrationStatus:  normalizeMailHydrationStatus(hydrationStatus),
		MetadataSource:   normalizeMailMetadataSource(metadataSource),
		HydratedAt:       strings.TrimSpace(hydratedAt),
		HasInvite:        hasInviteInt != 0,
		MeetingStart:     strings.TrimSpace(meetingStart),
		MeetingEnd:       strings.TrimSpace(meetingEnd),
		MeetingOrganizer: strings.TrimSpace(meetingOrganizer),
		MeetingLocation:  strings.TrimSpace(meetingLocation),
		MatchedCompany:   strings.TrimSpace(matchedCompany),
		MatchedJobTitle:  strings.TrimSpace(matchedJobTitle),
		EventType:        normalizeMailEventType(eventType),
		Importance:       importanceInt != 0,
		Confidence:       normalizeMailConfidence(confidence),
		DecisionSource:   strings.TrimSpace(decisionSource),
		Reasons:          decodeStringSlice(reasonsJSON),
		TriageStatus:     normalizeMailTriageStatus(triageStatus),
	}
	normalizeReadMailMessage(&message)
	return message
}

func upsertMailMessages(statePath string, account mailStoredAccount, messages []MailMessage) (int, int, error) {
	if len(messages) == 0 {
		return 0, 0, nil
	}
	db, err := openMailDB(statePath)
	if err != nil {
		return 0, 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	statement, err := tx.Prepare(fmt.Sprintf(`
		INSERT INTO mail_messages (
			account_id, provider, provider_message_id, provider_thread_id, internet_message_id, web_link,
			subject, sender_name, sender_email, to_recipients_json, cc_recipients_json,
			received_at, labels_json, is_unread, snippet, body_text, body_html,
			hydration_status, metadata_source, hydrated_at,
			has_invite, meeting_start, meeting_end, meeting_organizer, meeting_location,
			matched_company, matched_job_title, event_type, importance, confidence,
			decision_source, reasons_json, triage_status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, provider_message_id) DO UPDATE SET
			provider_thread_id=CASE WHEN excluded.provider_thread_id != '' THEN excluded.provider_thread_id ELSE mail_messages.provider_thread_id END,
			internet_message_id=CASE WHEN excluded.internet_message_id != '' THEN excluded.internet_message_id ELSE mail_messages.internet_message_id END,
			web_link=CASE WHEN excluded.web_link != '' THEN excluded.web_link ELSE mail_messages.web_link END,
			subject=CASE WHEN excluded.subject != '' THEN excluded.subject ELSE mail_messages.subject END,
			sender_name=CASE WHEN excluded.sender_name != '' THEN excluded.sender_name ELSE mail_messages.sender_name END,
			sender_email=CASE WHEN excluded.sender_email != '' THEN excluded.sender_email ELSE mail_messages.sender_email END,
			to_recipients_json=CASE WHEN excluded.to_recipients_json != '[]' THEN excluded.to_recipients_json ELSE mail_messages.to_recipients_json END,
			cc_recipients_json=CASE WHEN excluded.cc_recipients_json != '[]' THEN excluded.cc_recipients_json ELSE mail_messages.cc_recipients_json END,
			received_at=CASE WHEN excluded.received_at != '' THEN excluded.received_at ELSE mail_messages.received_at END,
			labels_json=CASE WHEN excluded.labels_json != '[]' THEN excluded.labels_json ELSE mail_messages.labels_json END,
			is_unread=excluded.is_unread,
			snippet=CASE WHEN excluded.snippet != '' THEN excluded.snippet ELSE mail_messages.snippet END,
			body_text=CASE WHEN excluded.body_text != '' THEN excluded.body_text ELSE mail_messages.body_text END,
			body_html=CASE WHEN excluded.body_html != '' THEN excluded.body_html ELSE mail_messages.body_html END,
			hydration_status=CASE
				WHEN excluded.hydration_status = '%s' THEN excluded.hydration_status
				WHEN mail_messages.hydration_status = '%s' THEN mail_messages.hydration_status
				WHEN excluded.hydration_status != '' THEN excluded.hydration_status
				ELSE mail_messages.hydration_status
			END,
			metadata_source=CASE
				WHEN excluded.hydration_status = '%s' AND mail_messages.hydration_status = '%s' THEN mail_messages.metadata_source
				WHEN excluded.metadata_source != '' THEN excluded.metadata_source
				ELSE mail_messages.metadata_source
			END,
			hydrated_at=CASE WHEN excluded.hydrated_at != '' THEN excluded.hydrated_at ELSE mail_messages.hydrated_at END,
			has_invite=CASE WHEN excluded.has_invite != 0 THEN excluded.has_invite ELSE mail_messages.has_invite END,
			meeting_start=CASE WHEN excluded.meeting_start != '' THEN excluded.meeting_start ELSE mail_messages.meeting_start END,
			meeting_end=CASE WHEN excluded.meeting_end != '' THEN excluded.meeting_end ELSE mail_messages.meeting_end END,
			meeting_organizer=CASE WHEN excluded.meeting_organizer != '' THEN excluded.meeting_organizer ELSE mail_messages.meeting_organizer END,
			meeting_location=CASE WHEN excluded.meeting_location != '' THEN excluded.meeting_location ELSE mail_messages.meeting_location END,
			matched_company=CASE WHEN excluded.matched_company != '' THEN excluded.matched_company ELSE mail_messages.matched_company END,
			matched_job_title=CASE WHEN excluded.matched_job_title != '' THEN excluded.matched_job_title ELSE mail_messages.matched_job_title END,
			event_type=excluded.event_type,
			importance=excluded.importance,
			confidence=excluded.confidence,
			decision_source=excluded.decision_source,
			reasons_json=excluded.reasons_json,
			updated_at=excluded.updated_at`,
		mailHydrationComplete,
		mailHydrationComplete,
		mailHydrationPending,
		mailHydrationComplete,
	))
	if err != nil {
		return 0, 0, err
	}
	defer statement.Close()

	now := utcNow()
	stored := 0
	important := 0
	for _, message := range messages {
		if strings.TrimSpace(message.ProviderMessageID) == "" {
			continue
		}
		reasonsJSON := encodeStringSlice(message.Reasons)
		if _, err := statement.Exec(
			account.ID,
			normalizeMailProvider(message.Provider),
			strings.TrimSpace(message.ProviderMessageID),
			strings.TrimSpace(message.ProviderThreadID),
			strings.TrimSpace(message.InternetMessageID),
			strings.TrimSpace(message.WebLink),
			normalizeTextSnippet(message.Subject, 500),
			normalizeTextSnippet(message.Sender.Name, 240),
			strings.ToLower(strings.TrimSpace(message.Sender.Email)),
			encodeMailAddresses(message.ToRecipients),
			encodeMailAddresses(message.CcRecipients),
			nonEmpty(strings.TrimSpace(message.ReceivedAt), now),
			encodeStringSlice(message.Labels),
			boolToInt(message.IsUnread),
			normalizeTextSnippet(message.Snippet, 500),
			strings.TrimSpace(message.BodyText),
			strings.TrimSpace(message.BodyHTML),
			normalizeMailHydrationStatus(message.HydrationStatus),
			normalizeMailMetadataSource(message.MetadataSource),
			strings.TrimSpace(message.HydratedAt),
			boolToInt(message.HasInvite),
			strings.TrimSpace(message.MeetingStart),
			strings.TrimSpace(message.MeetingEnd),
			strings.TrimSpace(message.MeetingOrganizer),
			strings.TrimSpace(message.MeetingLocation),
			strings.TrimSpace(message.MatchedCompany),
			strings.TrimSpace(message.MatchedJobTitle),
			normalizeMailEventType(message.EventType),
			boolToInt(message.Importance),
			normalizeMailConfidence(message.Confidence),
			nonEmpty(strings.TrimSpace(message.DecisionSource), "rules"),
			reasonsJSON,
			normalizeMailTriageStatus(message.TriageStatus),
			now,
			now,
		); err != nil {
			return stored, important, err
		}
		stored++
		if message.Importance {
			important++
		}
	}
	if err := tx.Commit(); err != nil {
		return stored, important, err
	}
	return stored, important, nil
}

func mailEventOptions() []string {
	return []string{
		"all",
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
	}
}

func mailTriageOptions() []string {
	return []string{"all", mailTriageNew, mailTriageReviewed, mailTriageImportant, mailTriageIgnored, mailTriageFollowUp}
}

func loadMailAnalytics(statePath string, filters MailMessageFilters) (MailAnalyticsResponse, error) {
	return loadMailAnalyticsWithConfig(MailServiceConfig{StatePath: statePath}, filters)
}

func loadMailAnalyticsWithConfig(cfg MailServiceConfig, filters MailMessageFilters) (MailAnalyticsResponse, error) {
	response := MailAnalyticsResponse{
		Summary: MailAnalyticsSummary{GeneratedAt: utcNow()},
		Details: MailAnalyticsDetails{
			OpenApplications:     []MailLifecycleApplicationItem{},
			ResolvedRejections:   []MailLifecycleRejectionItem{},
			UnresolvedRejections: []MailLifecycleRejectionItem{},
			UpcomingMeetings:     []MailMeetingItem{},
			OpenActions:          []MailOpenActionItem{},
		},
		DailyBuckets: []MailDailyBucket{},
	}
	context := loadMailMatchContext(cfg)

	accounts, err := loadMailAccounts(cfg.StatePath)
	if err != nil {
		return response, err
	}
	response.Summary.ConnectedAccountsCount = len(accounts)

	now := time.Now().In(time.Local)
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	bucketIndex := map[string]int{}
	for offset := 6; offset >= 0; offset-- {
		day := startToday.AddDate(0, 0, -offset)
		key := mailDailyBucketKey(day)
		bucketIndex[key] = len(response.DailyBuckets)
		response.DailyBuckets = append(response.DailyBuckets, MailDailyBucket{
			DayKey: key,
			Label:  strings.ToUpper(day.Format("Mon")),
		})
	}

	db, err := openMailDB(cfg.StatePath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "mail db disabled") {
			return response, nil
		}
		return response, err
	}

	where, args := buildMailWhere(filters, "m", false, false, false, false)
	where, args = appendLiveMailProvider(where, args, "m")
	rows, err := db.Query(`
		SELECT
			m.id, m.account_id, m.provider, a.email, m.provider_message_id, m.provider_thread_id, m.internet_message_id,
			m.subject, m.sender_name, m.sender_email, m.received_at, m.is_unread, m.snippet,
			m.hydration_status, m.metadata_source, m.hydrated_at, m.has_invite, m.meeting_start, m.meeting_end,
			m.meeting_organizer, m.meeting_location, m.matched_company, m.matched_job_title,
			m.event_type, m.importance, m.confidence, m.decision_source, m.reasons_json, m.triage_status, m.updated_at
		FROM mail_messages m
		JOIN mail_accounts a ON a.id = m.account_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY m.received_at ASC, m.id ASC`,
		args...,
	)
	if err != nil {
		return response, err
	}
	defer rows.Close()

	type lifecycleApplicationState struct {
		item       MailLifecycleApplicationItem
		companyKey string
		titleKey   string
		openedAt   time.Time
		open       bool
	}

	messages := make([]MailMessage, 0, 256)
	applicationStates := make([]*lifecycleApplicationState, 0, 64)
	applicationsByExact := map[string][]*lifecycleApplicationState{}
	applicationsByCompany := map[string][]*lifecycleApplicationState{}

	for rows.Next() {
		var (
			id                int64
			accountID         int64
			provider          string
			accountEmail      string
			providerMessageID string
			providerThreadID  string
			internetMessageID string
			subject           string
			senderName        string
			senderEmail       string
			receivedAt        string
			isUnreadInt       int
			snippet           string
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
			&subject,
			&senderName,
			&senderEmail,
			&receivedAt,
			&isUnreadInt,
			&snippet,
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
			return response, err
		}

		message := rowToMailMessage(
			id,
			accountID,
			provider,
			accountEmail,
			providerMessageID,
			providerThreadID,
			internetMessageID,
			"",
			subject,
			senderName,
			senderEmail,
			"[]",
			"[]",
			receivedAt,
			"[]",
			isUnreadInt,
			snippet,
			"",
			"",
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
		)
		normalizeReadMailMessageWithContext(&message, context)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return response, err
	}

	messages = dedupeMailMessages(messages)
	sortMailMessagesByReceivedAt(messages, false)

	for _, message := range messages {
		eventType := normalizeMailEventType(message.EventType)
		receivedAtTime := parseMailTime(message.ReceivedAt)
		if eventType == mailEventApplicationAcknowledged {
			response.Summary.AllTime.Applications++
		}
		if eventType == mailEventRejection {
			response.Summary.AllTime.Rejections++
		}
		if !receivedAtTime.IsZero() {
			receivedDay := time.Date(receivedAtTime.Year(), receivedAtTime.Month(), receivedAtTime.Day(), 0, 0, 0, 0, time.Local)
			diffDays := int(startToday.Sub(receivedDay).Hours() / 24)
			if diffDays == 0 {
				if eventType == mailEventApplicationAcknowledged {
					response.Summary.Today.Applications++
				}
				if eventType == mailEventRejection {
					response.Summary.Today.Rejections++
				}
			}
			if diffDays == 1 {
				if eventType == mailEventApplicationAcknowledged {
					response.Summary.Yesterday.Applications++
				}
				if eventType == mailEventRejection {
					response.Summary.Yesterday.Rejections++
				}
			}
			if diffDays >= 0 && diffDays <= 6 {
				index := bucketIndex[mailDailyBucketKey(receivedDay)]
				if eventType == mailEventApplicationAcknowledged {
					response.Summary.Last7Days.Applications++
					response.DailyBuckets[index].Applications++
				}
				if eventType == mailEventRejection {
					response.Summary.Last7Days.Rejections++
					response.DailyBuckets[index].Rejections++
				}
			}
		}

		if mailIsUpcomingMeetingMessage(message, now) {
			response.Details.UpcomingMeetings = append(response.Details.UpcomingMeetings, MailMeetingItem{
				ID:               message.ID,
				Company:          mailDisplayCompany(message),
				Subject:          mailDisplaySubject(message.Subject),
				ReceivedAt:       message.ReceivedAt,
				ReceivedAtLocal:  message.ReceivedAtLocal,
				MeetingStart:     message.MeetingStart,
				MeetingEnd:       message.MeetingEnd,
				MeetingOrganizer: message.MeetingOrganizer,
				MeetingLocation:  message.MeetingLocation,
				Sender:           mailDisplaySender(message),
			})
		}

		if mailIsOpenActionMessage(message) {
			response.Details.OpenActions = append(response.Details.OpenActions, MailOpenActionItem{
				ID:              message.ID,
				Company:         mailDisplayCompany(message),
				JobTitle:        strings.TrimSpace(message.MatchedJobTitle),
				Subject:         mailDisplaySubject(message.Subject),
				EventType:       message.EventType,
				TriageStatus:    message.TriageStatus,
				ReceivedAt:      message.ReceivedAt,
				ReceivedAtLocal: message.ReceivedAtLocal,
				Sender:          mailDisplaySender(message),
				Reasons:         append([]string(nil), message.Reasons...),
			})
		}

		switch eventType {
		case mailEventApplicationAcknowledged:
			companyKey := companyFilterKey(message.MatchedCompany)
			titleKey := mailLifecycleTitleKey(message.MatchedJobTitle)
			state := &lifecycleApplicationState{
				item: MailLifecycleApplicationItem{
					ID:              message.ID,
					Company:         mailDisplayCompany(message),
					JobTitle:        strings.TrimSpace(message.MatchedJobTitle),
					Subject:         mailDisplaySubject(message.Subject),
					ReceivedAt:      message.ReceivedAt,
					ReceivedAtLocal: message.ReceivedAtLocal,
					Sender:          mailDisplaySender(message),
					LowConfidence:   companyKey == "" || titleKey == "",
					Confidence:      message.Confidence,
				},
				companyKey: companyKey,
				titleKey:   titleKey,
				openedAt:   receivedAtTime,
				open:       true,
			}
			applicationStates = append(applicationStates, state)
			if companyKey != "" {
				applicationsByCompany[companyKey] = append(applicationsByCompany[companyKey], state)
			}
			if companyKey != "" && titleKey != "" {
				exactKey := companyKey + "|" + titleKey
				applicationsByExact[exactKey] = append(applicationsByExact[exactKey], state)
			}
		case mailEventRejection:
			rejection := MailLifecycleRejectionItem{
				ID:              message.ID,
				Company:         mailDisplayCompany(message),
				JobTitle:        strings.TrimSpace(message.MatchedJobTitle),
				Subject:         mailDisplaySubject(message.Subject),
				ReceivedAt:      message.ReceivedAt,
				ReceivedAtLocal: message.ReceivedAtLocal,
				Sender:          mailDisplaySender(message),
				Confidence:      message.Confidence,
			}
			companyKey := companyFilterKey(message.MatchedCompany)
			titleKey := mailLifecycleTitleKey(message.MatchedJobTitle)
			matched := false
			if companyKey != "" && titleKey != "" {
				exactKey := companyKey + "|" + titleKey
				for _, candidate := range applicationsByExact[exactKey] {
					if !candidate.open {
						continue
					}
					if !candidate.openedAt.IsZero() && !receivedAtTime.IsZero() && candidate.openedAt.After(receivedAtTime) {
						continue
					}
					candidate.open = false
					rejection.MatchedApplication = nonEmpty(strings.TrimSpace(candidate.item.JobTitle), candidate.item.Subject)
					response.Details.ResolvedRejections = append(response.Details.ResolvedRejections, rejection)
					matched = true
					break
				}
			}
			if matched || companyKey == "" {
				continue
			}
			for _, candidate := range applicationsByCompany[companyKey] {
				if !candidate.open {
					continue
				}
				if !candidate.openedAt.IsZero() && !receivedAtTime.IsZero() && candidate.openedAt.After(receivedAtTime) {
					continue
				}
				rejection.CompanyOnlyMatch = true
				if rejection.MatchedApplication == "" {
					rejection.MatchedApplication = nonEmpty(strings.TrimSpace(candidate.item.JobTitle), candidate.item.Subject)
				}
			}
			response.Details.UnresolvedRejections = append(response.Details.UnresolvedRejections, rejection)
		}
	}

	for _, state := range applicationStates {
		if !state.open {
			continue
		}
		response.Details.OpenApplications = append(response.Details.OpenApplications, state.item)
	}

	sort.Slice(response.Details.OpenApplications, func(i, j int) bool {
		return parseMailTime(response.Details.OpenApplications[i].ReceivedAt).After(parseMailTime(response.Details.OpenApplications[j].ReceivedAt))
	})
	sort.Slice(response.Details.ResolvedRejections, func(i, j int) bool {
		return parseMailTime(response.Details.ResolvedRejections[i].ReceivedAt).After(parseMailTime(response.Details.ResolvedRejections[j].ReceivedAt))
	})
	sort.Slice(response.Details.UnresolvedRejections, func(i, j int) bool {
		return parseMailTime(response.Details.UnresolvedRejections[i].ReceivedAt).After(parseMailTime(response.Details.UnresolvedRejections[j].ReceivedAt))
	})
	sort.Slice(response.Details.OpenActions, func(i, j int) bool {
		return parseMailTime(response.Details.OpenActions[i].ReceivedAt).After(parseMailTime(response.Details.OpenActions[j].ReceivedAt))
	})
	sort.Slice(response.Details.UpcomingMeetings, func(i, j int) bool {
		left := parseMailTime(response.Details.UpcomingMeetings[i].MeetingStart)
		right := parseMailTime(response.Details.UpcomingMeetings[j].MeetingStart)
		if left.IsZero() || right.IsZero() {
			return parseMailTime(response.Details.UpcomingMeetings[i].ReceivedAt).Before(parseMailTime(response.Details.UpcomingMeetings[j].ReceivedAt))
		}
		return left.Before(right)
	})

	response.Summary.OpenApplicationsCount = len(response.Details.OpenApplications)
	response.Summary.ResolvedRejectionsCount = len(response.Details.ResolvedRejections)
	response.Summary.UnresolvedRejectionsCount = len(response.Details.UnresolvedRejections)
	response.Summary.OpenActionsCount = len(response.Details.OpenActions)
	response.Summary.UpcomingMeetingsCount = len(response.Details.UpcomingMeetings)

	return response, nil
}

func loadMailMessages(statePath string, filters MailMessageFilters) (MailMessagesResponse, error) {
	return loadMailMessagesWithConfig(MailServiceConfig{StatePath: statePath}, filters)
}

func loadMailMessagesWithConfig(cfg MailServiceConfig, filters MailMessageFilters) (MailMessagesResponse, error) {
	response := MailMessagesResponse{
		Summary: MailMessagesSummary{GeneratedAt: utcNow()},
		Filters: MailMessagesFilters{
			Provider:      normalizeMailProvider(filters.Provider),
			AccountID:     filters.AccountID,
			EventType:     normalizeMailEventType(filters.EventType),
			TriageStatus:  normalizeMailTriageStatus(filters.TriageStatus),
			Company:       strings.TrimSpace(filters.Company),
			UnreadOnly:    filters.UnreadOnly,
			ImportantOnly: filters.ImportantOnly,
			Limit:         filters.Limit,
			EventOptions:  mailEventOptions(),
			TriageOptions: mailTriageOptions(),
		},
		Messages: []MailMessage{},
	}
	context := loadMailMatchContext(cfg)
	if response.Filters.EventType == mailEventIgnored && strings.TrimSpace(filters.EventType) == "" {
		response.Filters.EventType = ""
	}
	if response.Filters.TriageStatus == mailTriageNew && strings.TrimSpace(filters.TriageStatus) == "" {
		response.Filters.TriageStatus = ""
	}
	accounts, err := loadMailAccounts(cfg.StatePath)
	if err != nil {
		return response, err
	}
	response.Filters.AccountIDOptions = accounts
	response.Summary.ConnectedAccounts = len(accounts)
	db, err := openMailDB(cfg.StatePath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "mail db disabled") {
			return response, nil
		}
		return response, err
	}

	_ = db.QueryRow(`SELECT COUNT(*) FROM mail_messages WHERE provider = ?`, liveMailProvider()).Scan(&response.Summary.TotalMessages)
	_ = db.QueryRow(`SELECT COUNT(*) FROM mail_messages WHERE provider = ? AND triage_status = ?`, liveMailProvider(), mailTriageNew).Scan(&response.Summary.NewMessages)
	_ = db.QueryRow(`SELECT COUNT(*) FROM mail_messages WHERE provider = ? AND importance = 1 AND is_unread = 1 AND triage_status != ?`, liveMailProvider(), mailTriageIgnored).Scan(&response.Summary.ImportantUnread)

	where, args := buildMailWhere(filters, "m", true, true, true, true)
	where, args = appendLiveMailProvider(where, args, "m")
	limit := filters.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	response.Filters.Limit = limit
	rawLimit := limit * 4
	if rawLimit < limit {
		rawLimit = limit
	}
	if rawLimit > 4000 {
		rawLimit = 4000
	}

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
		append(args, rawLimit)...,
	)
	if err != nil {
		return response, err
	}
	defer rows.Close()

	rawMessages := make([]MailMessage, 0, minInt(limit*2, 2000))
	companyLabels := map[string]string{}
	for rows.Next() {
		var (
			id                int64
			accountID         int64
			provider          string
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
			return response, err
		}
		message := rowToMailMessage(
			id, accountID, provider, accountEmail, providerMessageID, providerThreadID, internetMessageID, webLink,
			subject, senderName, senderEmail, toRecipientsJSON, ccRecipientsJSON, receivedAt, labelsJSON, isUnreadInt,
			snippet, bodyText, bodyHTML, hydrationStatus, metadataSource, hydratedAt, hasInviteInt, meetingStart, meetingEnd, meetingOrganizer, meetingLocation,
			matchedCompany, matchedJobTitle, eventType, importanceInt, confidence, decisionSource, reasonsJSON, triageStatus, updatedAt,
		)
		normalizeReadMailMessageWithContext(&message, context)
		rawMessages = append(rawMessages, message)
	}
	if err := rows.Err(); err != nil {
		return response, err
	}

	dedupedMessages := dedupeMailMessages(rawMessages)
	sortMailMessagesByReceivedAt(dedupedMessages, true)
	response.Summary.FilteredMessages = len(dedupedMessages)
	for _, message := range dedupedMessages {
		companyLabels[companyFilterKey(message.MatchedCompany)] = choosePreferredCompanyLabel(companyLabels[companyFilterKey(message.MatchedCompany)], message.MatchedCompany)
		message.BodyText = ""
		message.BodyHTML = ""
		response.Messages = append(response.Messages, message)
		if len(response.Messages) >= limit {
			break
		}
	}

	companyOptions := make([]string, 0, len(companyLabels))
	for _, label := range companyLabels {
		if label != "" {
			companyOptions = append(companyOptions, label)
		}
	}
	sort.Slice(companyOptions, func(i, j int) bool {
		return strings.ToLower(companyOptions[i]) < strings.ToLower(companyOptions[j])
	})
	response.Filters.CompanyOptions = companyOptions
	return response, nil
}

func loadMailMessageDetail(statePath string, id int64) (MailMessage, error) {
	return loadMailMessageDetailWithConfig(MailServiceConfig{StatePath: statePath}, id)
}

func loadMailMessageDetailWithConfig(cfg MailServiceConfig, id int64) (MailMessage, error) {
	context := loadMailMatchContext(cfg)
	db, err := openMailDB(cfg.StatePath)
	if err != nil {
		return MailMessage{}, err
	}
	var (
		accountID         int64
		provider          string
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
	err = db.QueryRow(`
		SELECT
			m.account_id, m.provider, a.email, m.provider_message_id, m.provider_thread_id, m.internet_message_id,
			m.web_link, m.subject, m.sender_name, m.sender_email, m.to_recipients_json, m.cc_recipients_json,
			m.received_at, m.labels_json, m.is_unread, m.snippet, m.body_text, m.body_html,
			m.hydration_status, m.metadata_source, m.hydrated_at, m.has_invite,
			m.meeting_start, m.meeting_end, m.meeting_organizer, m.meeting_location, m.matched_company, m.matched_job_title,
			m.event_type, m.importance, m.confidence, m.decision_source, m.reasons_json, m.triage_status, m.updated_at
		FROM mail_messages m
		JOIN mail_accounts a ON a.id = m.account_id
		WHERE m.id = ?`,
		id,
	).Scan(
		&accountID, &provider, &accountEmail, &providerMessageID, &providerThreadID, &internetMessageID,
		&webLink, &subject, &senderName, &senderEmail, &toRecipientsJSON, &ccRecipientsJSON,
		&receivedAt, &labelsJSON, &isUnreadInt, &snippet, &bodyText, &bodyHTML, &hydrationStatus, &metadataSource, &hydratedAt, &hasInviteInt,
		&meetingStart, &meetingEnd, &meetingOrganizer, &meetingLocation, &matchedCompany, &matchedJobTitle,
		&eventType, &importanceInt, &confidence, &decisionSource, &reasonsJSON, &triageStatus, &updatedAt,
	)
	if err != nil {
		return MailMessage{}, err
	}
	message := rowToMailMessage(
		id, accountID, provider, accountEmail, providerMessageID, providerThreadID, internetMessageID, webLink,
		subject, senderName, senderEmail, toRecipientsJSON, ccRecipientsJSON, receivedAt, labelsJSON, isUnreadInt,
		snippet, bodyText, bodyHTML, hydrationStatus, metadataSource, hydratedAt, hasInviteInt, meetingStart, meetingEnd, meetingOrganizer, meetingLocation,
		matchedCompany, matchedJobTitle, eventType, importanceInt, confidence, decisionSource, reasonsJSON, triageStatus, updatedAt,
	)
	normalizeReadMailMessageWithContext(&message, context)
	return message, nil
}

func updateMailMessageTriage(statePath string, id int64, triageStatus string) (MailTriageUpdateResponse, error) {
	response := MailTriageUpdateResponse{Ok: false}
	db, err := openMailDB(statePath)
	if err != nil {
		return response, err
	}

	status := normalizeMailTriageStatus(triageStatus)
	updatedAt := utcNow()
	result, err := db.Exec(`UPDATE mail_messages SET triage_status = ?, updated_at = ? WHERE id = ?`, status, updatedAt, id)
	if err != nil {
		return response, err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return response, sql.ErrNoRows
	}
	return MailTriageUpdateResponse{
		Ok:           true,
		ID:           id,
		TriageStatus: status,
		UpdatedAt:    updatedAt,
	}, nil
}

func loadMailOverview(statePath string) (MailOverviewResponse, error) {
	response := MailOverviewResponse{
		Summary: MailOverviewSummary{
			GeneratedAt: utcNow(),
			EventCounts: map[string]int{},
		},
		Accounts: []MailAccount{},
	}
	accounts, err := loadMailAccounts(statePath)
	if err != nil {
		return response, err
	}
	response.Accounts = accounts
	response.Summary.ConnectedAccountsCount = len(accounts)
	db, err := openMailDB(statePath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "mail db disabled") {
			return response, nil
		}
		return response, err
	}

	rows, err := db.Query(`
		SELECT
			m.id, m.account_id, m.provider, a.email, m.provider_message_id, m.provider_thread_id, m.internet_message_id,
			m.subject, m.sender_name, m.sender_email, m.received_at, m.is_unread, m.snippet,
			m.hydration_status, m.metadata_source, m.has_invite, m.meeting_start, m.meeting_end,
			m.meeting_organizer, m.meeting_location, m.matched_company, m.matched_job_title,
			m.event_type, m.importance, m.confidence, m.decision_source, m.reasons_json, m.triage_status, m.updated_at
		FROM mail_messages m
		JOIN mail_accounts a ON a.id = m.account_id
		WHERE m.provider = ?
		ORDER BY m.received_at DESC, m.id DESC`,
		liveMailProvider(),
	)
	if err != nil {
		return response, err
	}
	defer rows.Close()

	messages := make([]MailMessage, 0, 256)
	for rows.Next() {
		var (
			id                int64
			accountID         int64
			provider          string
			accountEmail      string
			providerMessageID string
			providerThreadID  string
			internetMessageID string
			subject           string
			senderName        string
			senderEmail       string
			receivedAt        string
			isUnreadInt       int
			snippet           string
			hydrationStatus   string
			metadataSource    string
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
			&subject,
			&senderName,
			&senderEmail,
			&receivedAt,
			&isUnreadInt,
			&snippet,
			&hydrationStatus,
			&metadataSource,
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
			return response, err
		}
		message := rowToMailMessage(
			id, accountID, provider, accountEmail, providerMessageID, providerThreadID, internetMessageID, "",
			subject, senderName, senderEmail, "[]", "[]", receivedAt, "[]", isUnreadInt, snippet, "", "",
			hydrationStatus, metadataSource, "", hasInviteInt, meetingStart, meetingEnd, meetingOrganizer, meetingLocation,
			matchedCompany, matchedJobTitle, eventType, importanceInt, confidence, decisionSource, reasonsJSON, triageStatus, updatedAt,
		)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return response, err
	}

	messages = dedupeMailMessages(messages)
	sortMailMessagesByReceivedAt(messages, true)

	buildHighlight := func(message MailMessage) *MailOverviewHighlight {
		sender := strings.TrimSpace(message.Sender.Name)
		if sender == "" {
			sender = strings.TrimSpace(message.Sender.Email)
		}
		return &MailOverviewHighlight{
			ID:              message.ID,
			Subject:         strings.TrimSpace(message.Subject),
			Company:         strings.TrimSpace(message.MatchedCompany),
			Sender:          sender,
			ReceivedAt:      strings.TrimSpace(message.ReceivedAt),
			ReceivedAtLocal: isoToLocal(message.ReceivedAt),
			EventType:       normalizeMailEventType(message.EventType),
		}
	}

	for _, message := range messages {
		if message.Importance && message.IsUnread && normalizeMailTriageStatus(message.TriageStatus) != mailTriageIgnored {
			response.Summary.UnreadImportantCount++
		}
		if normalizeMailTriageStatus(message.TriageStatus) == mailTriageNew {
			response.Summary.NewMessageCount++
		}
		eventType := normalizeMailEventType(message.EventType)
		response.Summary.EventCounts[eventType]++
		if response.Summary.LatestInterview == nil && (eventType == mailEventInterviewScheduled || eventType == mailEventInterviewUpdated) {
			response.Summary.LatestInterview = buildHighlight(message)
		}
		if response.Summary.LatestRejection == nil && eventType == mailEventRejection {
			response.Summary.LatestRejection = buildHighlight(message)
		}
		if response.Summary.LatestRecruiterReply == nil && eventType == mailEventRecruiterReply {
			response.Summary.LatestRecruiterReply = buildHighlight(message)
		}
	}
	return response, nil
}
