package monitor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type mailOAuthSession struct {
	Provider     string
	State        string
	CodeVerifier string
	RedirectURI  string
	CreatedAt    time.Time
}

var mailOAuthSessions = struct {
	mu       sync.Mutex
	sessions map[string]mailOAuthSession
}{
	sessions: map[string]mailOAuthSession{},
}

var gmailAPIBaseURL = "https://gmail.googleapis.com/gmail/v1"

type MailSyncRunner struct {
	mu          sync.Mutex
	cfg         MailServiceConfig
	status      MailRunStatus
	coordinator *BackgroundTaskCoordinator
}

func NewMailSyncRunner(cfg MailServiceConfig) *MailSyncRunner {
	return &MailSyncRunner{
		cfg:    cfg,
		status: MailRunStatus{},
	}
}

func (m *MailSyncRunner) SetCoordinator(coordinator *BackgroundTaskCoordinator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.coordinator = coordinator
}

func (m *MailSyncRunner) Snapshot() MailRunStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := m.status
	if len(m.status.Progress) > 0 {
		snapshot.Progress = make([]MailRunAccountProgress, len(m.status.Progress))
		copy(snapshot.Progress, m.status.Progress)
	}
	return snapshot
}

func (m *MailSyncRunner) Trigger() taskTriggerResult {
	if m.coordinator == nil {
		if m.startRun() {
			return taskTriggerResult{Accepted: true, Started: true, Message: "Mail sync started."}
		}
		return taskTriggerResult{Message: "Mail sync already in progress."}
	}
	result := m.coordinator.RequestMail()
	if result.Queued {
		m.markQueued()
	}
	return result
}

func (m *MailSyncRunner) startRun() bool {
	m.mu.Lock()
	if m.status.Running {
		m.mu.Unlock()
		return false
	}
	m.status = MailRunStatus{
		Running:   true,
		Queued:    false,
		LastStart: utcNow(),
		Progress:  []MailRunAccountProgress{},
	}
	m.mu.Unlock()

	go m.run()
	return true
}

func (m *MailSyncRunner) markQueued() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Queued = true
}

func (m *MailSyncRunner) run() {
	summary, err := syncAllMailAccounts(m.cfg, m.markAccountStart, m.markAccountProgress, m.markAccountDone)
	m.mu.Lock()
	m.status.Running = false
	m.status.LastEnd = utcNow()
	if err != nil {
		m.status.LastError = err.Error()
		m.mu.Unlock()
		if m.coordinator != nil {
			m.coordinator.Finish(backgroundTaskMail)
		}
		return
	}
	m.status.LastError = ""
	m.status.MessagesFetched = summary.Fetched
	m.status.MessagesStored = summary.Stored
	m.status.MessagesDiscovered = summary.Discovered
	m.status.MessagesHydrated = summary.Hydrated
	m.status.ImportantMessages = summary.Important
	m.status.CutoffReached = summary.CutoffReached
	m.status.DegradedMode = summary.DegradedMode
	m.status.AccountsCompleted = len(summary.Accounts)
	m.mu.Unlock()
	if m.coordinator != nil {
		m.coordinator.Finish(backgroundTaskMail)
	}
}

func (m *MailSyncRunner) ensureAccountProgressLocked(account mailStoredAccount) int {
	for i := range m.status.Progress {
		if m.status.Progress[i].AccountID == account.ID {
			return i
		}
	}
	m.status.Progress = append(m.status.Progress, MailRunAccountProgress{
		AccountID:    account.ID,
		Provider:     account.Provider,
		AccountEmail: account.Email,
		Phase:        "queued",
	})
	m.status.AccountsTotal = len(m.status.Progress)
	return len(m.status.Progress) - 1
}

func (m *MailSyncRunner) markAccountStart(account mailStoredAccount) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.ensureAccountProgressLocked(account)
	entry := m.status.Progress[index]
	entry.Phase = "running"
	entry.AccountEmail = nonEmpty(strings.TrimSpace(entry.AccountEmail), strings.TrimSpace(account.Email))
	entry.Fetched = 0
	entry.Stored = 0
	entry.Discovered = 0
	entry.Hydrated = 0
	entry.Important = 0
	entry.CutoffReached = false
	entry.DegradedMode = false
	entry.StartedAt = utcNow()
	entry.Message = ""
	m.status.Progress[index] = entry
	m.refreshStatusTotalsLocked()
}

func (m *MailSyncRunner) markAccountProgress(account mailStoredAccount, summary MailSyncAccountSummary, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.ensureAccountProgressLocked(account)
	entry := m.status.Progress[index]
	entry.Phase = "running"
	entry.AccountEmail = nonEmpty(strings.TrimSpace(summary.AccountEmail), nonEmpty(strings.TrimSpace(entry.AccountEmail), strings.TrimSpace(account.Email)))
	entry.Fetched = summary.Fetched
	entry.Stored = summary.Stored
	entry.Discovered = summary.Discovered
	entry.Hydrated = summary.Hydrated
	entry.Important = summary.ImportantCount
	entry.CutoffReached = summary.CutoffReached
	entry.DegradedMode = summary.DegradedMode
	if phase := strings.TrimSpace(summary.Phase); phase != "" {
		entry.Phase = phase
	}
	if entry.StartedAt == "" {
		entry.StartedAt = utcNow()
	}
	if strings.TrimSpace(message) != "" {
		entry.Message = strings.TrimSpace(message)
	}
	m.status.Progress[index] = entry
	m.refreshStatusTotalsLocked()
}

func (m *MailSyncRunner) markAccountDone(account mailStoredAccount, summary MailSyncAccountSummary, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.ensureAccountProgressLocked(account)
	entry := m.status.Progress[index]
	entry.Phase = "done"
	entry.AccountEmail = nonEmpty(strings.TrimSpace(summary.AccountEmail), nonEmpty(strings.TrimSpace(entry.AccountEmail), strings.TrimSpace(account.Email)))
	entry.Fetched = summary.Fetched
	entry.Stored = summary.Stored
	entry.Discovered = summary.Discovered
	entry.Hydrated = summary.Hydrated
	entry.Important = summary.ImportantCount
	entry.CutoffReached = summary.CutoffReached
	entry.DegradedMode = summary.DegradedMode
	entry.FinishedAt = utcNow()
	if err != nil {
		entry.Message = err.Error()
		if strings.TrimSpace(m.status.LastError) == "" {
			m.status.LastError = err.Error()
		}
	} else if strings.TrimSpace(entry.Message) == "" {
		entry.Message = "Sync completed."
	}
	m.status.Progress[index] = entry
	m.refreshStatusTotalsLocked()
	completed := 0
	for _, item := range m.status.Progress {
		if item.Phase == "done" {
			completed++
		}
	}
	m.status.AccountsCompleted = completed
}

func (m *MailSyncRunner) refreshStatusTotalsLocked() {
	fetched := 0
	stored := 0
	discovered := 0
	hydrated := 0
	important := 0
	cutoffReached := false
	degradedMode := false
	for _, item := range m.status.Progress {
		fetched += item.Fetched
		stored += item.Stored
		discovered += item.Discovered
		hydrated += item.Hydrated
		important += item.Important
		cutoffReached = cutoffReached || item.CutoffReached
		degradedMode = degradedMode || item.DegradedMode
	}
	m.status.AccountsTotal = len(m.status.Progress)
	m.status.MessagesFetched = fetched
	m.status.MessagesStored = stored
	m.status.MessagesDiscovered = discovered
	m.status.MessagesHydrated = hydrated
	m.status.ImportantMessages = important
	m.status.CutoffReached = cutoffReached
	m.status.DegradedMode = degradedMode
}

func mailScheduleEnabledDefault() bool {
	return parseBoolEnv("MAIL_SCHEDULE_ENABLED", false)
}

func resolveMailSchedulePath(statePath string) string {
	if configured := strings.TrimSpace(os.Getenv("MAIL_SCHEDULE_PATH")); configured != "" {
		return configured
	}
	root := strings.TrimSpace(filepath.Dir(strings.TrimSpace(statePath)))
	if root == "" || root == "." {
		return ".state/mail_schedule.json"
	}
	return filepath.Join(root, "mail_schedule.json")
}

type MailScheduler struct {
	mu       sync.Mutex
	runner   *MailSyncRunner
	filePath string
	status   CrawlScheduleStatus
	stopCh   chan struct{}
}

func NewMailScheduler(runner *MailSyncRunner, statePath string) *MailScheduler {
	scheduler := &MailScheduler{
		runner:   runner,
		filePath: resolveMailSchedulePath(statePath),
		status: CrawlScheduleStatus{
			Enabled:         mailScheduleEnabledDefault(),
			IntervalMinutes: mailPollIntervalMinutes(),
		},
	}
	scheduler.mu.Lock()
	if err := scheduler.loadLocked(); err != nil {
		scheduler.status.LastError = err.Error()
	}
	if scheduler.status.Enabled {
		scheduler.startLoopLocked()
	}
	scheduler.mu.Unlock()
	return scheduler
}

func (s *MailScheduler) Snapshot() CrawlScheduleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *MailScheduler) Update(enabled bool, intervalMinutes int) (CrawlScheduleStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if intervalMinutes < 5 {
		intervalMinutes = 5
	}
	if intervalMinutes > 24*60 {
		intervalMinutes = 24 * 60
	}
	s.status.Enabled = enabled
	s.status.IntervalMinutes = intervalMinutes
	s.status.LastError = ""
	if enabled {
		s.startLoopLocked()
	} else {
		s.stopLoopLocked()
		s.status.NextRunAt = ""
	}
	err := s.persistLocked()
	if err != nil {
		s.status.LastError = err.Error()
	}
	return s.status, err
}

func (s *MailScheduler) startLoopLocked() {
	s.stopLoopLocked()
	interval := time.Duration(s.status.IntervalMinutes) * time.Minute
	s.status.NextRunAt = time.Now().Add(interval).UTC().Format(time.RFC3339)
	stopCh := make(chan struct{})
	s.stopCh = stopCh
	go s.loop(stopCh, interval)
}

func (s *MailScheduler) stopLoopLocked() {
	if s.stopCh == nil {
		return
	}
	close(s.stopCh)
	s.stopCh = nil
}

func (s *MailScheduler) loop(stopCh <-chan struct{}, interval time.Duration) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-timer.C:
			s.triggerScheduledRun()
			timer.Reset(interval)
		}
	}
}

func (s *MailScheduler) triggerScheduledRun() {
	result := s.runner.Trigger()
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.status.Enabled {
		s.status.NextRunAt = ""
		return
	}
	s.status.LastTriggerAt = now.UTC().Format(time.RFC3339)
	if result.Accepted {
		if result.Queued {
			s.status.LastTriggerResult = "queued"
		} else {
			s.status.LastTriggerResult = "started"
		}
		s.status.LastError = ""
	} else {
		s.status.LastTriggerResult = "skipped"
		s.status.LastError = strings.TrimSpace(result.Message)
	}
	s.status.NextRunAt = now.Add(time.Duration(s.status.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
	_ = s.persistLocked()
}

func (s *MailScheduler) loadLocked() error {
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	store := crawlScheduleStore{}
	if err := json.Unmarshal(raw, &store); err != nil {
		return err
	}
	s.status.Enabled = store.Enabled
	if store.IntervalMinutes > 0 {
		s.status.IntervalMinutes = store.IntervalMinutes
	}
	s.status.LastTriggerAt = strings.TrimSpace(store.LastTriggerAt)
	s.status.LastTriggerResult = strings.TrimSpace(store.LastTriggerResult)
	return nil
}

func (s *MailScheduler) persistLocked() error {
	store := crawlScheduleStore{
		Enabled:           s.status.Enabled,
		IntervalMinutes:   s.status.IntervalMinutes,
		LastTriggerAt:     s.status.LastTriggerAt,
		LastTriggerResult: s.status.LastTriggerResult,
	}
	body, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tempPath := s.filePath + ".tmp"
	if err := os.WriteFile(tempPath, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, s.filePath)
}

func syncAllMailAccounts(
	cfg MailServiceConfig,
	onStart func(account mailStoredAccount),
	onProgress func(account mailStoredAccount, summary MailSyncAccountSummary, message string),
	onDone func(account mailStoredAccount, summary MailSyncAccountSummary, err error),
) (MailSyncSummary, error) {
	summary := MailSyncSummary{Accounts: []MailSyncAccountSummary{}}
	accounts, err := loadStoredMailAccounts(cfg.StatePath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "mail db disabled") {
			return summary, nil
		}
		return summary, err
	}
	context := loadMailMatchContext(cfg)
	sort.Slice(accounts, func(i, j int) bool {
		return strings.ToLower(accounts[i].Provider) < strings.ToLower(accounts[j].Provider)
	})
	for _, account := range accounts {
		if normalizeMailProvider(account.Provider) == "" || strings.TrimSpace(account.TokenJSON) == "" || strings.TrimSpace(account.Status) != "connected" {
			continue
		}
		if onStart != nil {
			onStart(account)
		}
		accountSummary, accountErr := syncMailAccount(cfg.StatePath, account, context, onProgress)
		if onDone != nil {
			onDone(account, accountSummary, accountErr)
		}
		summary.Accounts = append(summary.Accounts, accountSummary)
		summary.Fetched += accountSummary.Fetched
		summary.Stored += accountSummary.Stored
		summary.Discovered += accountSummary.Discovered
		summary.Hydrated += accountSummary.Hydrated
		summary.Important += accountSummary.ImportantCount
		summary.CutoffReached = summary.CutoffReached || accountSummary.CutoffReached
		summary.DegradedMode = summary.DegradedMode || accountSummary.DegradedMode
		if accountErr != nil {
			_ = updateMailAccountSyncState(cfg.StatePath, account.ID, account.LastSyncAt, accountErr.Error())
			continue
		}
		_ = updateMailAccountSyncState(cfg.StatePath, account.ID, utcNow(), "")
	}
	return summary, nil
}

func syncMailAccount(
	statePath string,
	account mailStoredAccount,
	context mailMatchContext,
	onProgress func(account mailStoredAccount, summary MailSyncAccountSummary, message string),
) (MailSyncAccountSummary, error) {
	summary := MailSyncAccountSummary{
		AccountID:    account.ID,
		Provider:     account.Provider,
		AccountEmail: account.Email,
	}
	token, err := loadProviderToken(account.TokenJSON)
	if err != nil {
		return summary, err
	}
	var messages []MailMessage
	switch normalizeMailProvider(account.Provider) {
	case string(MailProviderGmail):
		token, err = ensureFreshMailToken(statePath, &account)
		if err != nil {
			return summary, err
		}
		messages, err = syncGmailMessages(account, token, context)
	default:
		err = fmt.Errorf("unsupported provider")
	}
	if err != nil {
		return summary, err
	}
	summary.Fetched = len(messages)
	stored, important, err := upsertMailMessages(statePath, account, messages)
	if err != nil {
		return summary, err
	}
	summary.Stored = stored
	summary.ImportantCount = important
	return summary, nil
}

func mailHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func mailSyncMaxMessages() int {
	limit := 100
	if raw := strings.TrimSpace(os.Getenv("MAIL_SYNC_MAX_MESSAGES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 10 {
		return 10
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func mailSyncWindow(lastSyncAt string) time.Time {
	if parsed := parseISOTime(lastSyncAt); !parsed.IsZero() {
		return parsed.Add(-2 * time.Hour)
	}
	return time.Now().Add(-time.Duration(mailBackfillDays()) * 24 * time.Hour)
}

func normalizeUnixMillisString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return ""
	}
	if parsed < 1_000_000_000_000 {
		parsed = parsed * 1000
	}
	return time.UnixMilli(parsed).UTC().Format(time.RFC3339)
}

func randomURLToken(byteCount int) (string, error) {
	data := make([]byte, byteCount)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func mailCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func mailRequestBaseURL(r *http.Request) string {
	scheme := "http"
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "127.0.0.1:8765"
	}
	return scheme + "://" + host
}

func gmailOAuthClientID() string {
	id, _ := gmailOAuthCredentials()
	return id
}

func gmailOAuthClientSecret() string {
	_, secret := gmailOAuthCredentials()
	return secret
}

func gmailOAuthCredentials() (string, string) {
	clientID := strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"))
	if clientID != "" || clientSecret != "" {
		return clientID, clientSecret
	}
	for _, candidate := range gmailOAuthClientJSONCandidates() {
		id, secret := gmailOAuthCredentialsFromJSON(candidate)
		if id != "" || secret != "" {
			return id, secret
		}
	}
	return "", ""
}

func gmailOAuthClientJSONCandidates() []string {
	candidates := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}
	add(os.Getenv("GOOGLE_OAUTH_CLIENT_JSON"))
	searchDirs := []string{
		filepath.Join(workspaceRoot(), ".local"),
		workspaceRoot(),
	}
	wd, err := os.Getwd()
	if err == nil {
		searchDirs = append(searchDirs, wd)
		parent := filepath.Dir(wd)
		if parent != wd {
			searchDirs = append(searchDirs, parent)
		}
	}
	for _, dir := range searchDirs {
		matches, matchErr := filepath.Glob(filepath.Join(dir, "client_secret_*.json"))
		if matchErr != nil {
			continue
		}
		sort.Strings(matches)
		for _, match := range matches {
			add(match)
		}
	}
	return candidates
}

func gmailOAuthCredentialsFromJSON(path string) (string, string) {
	body, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return "", ""
	}
	payload := map[string]map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	root := payload["installed"]
	if len(root) == 0 {
		root = payload["web"]
	}
	return strings.TrimSpace(asString(root["client_id"])), strings.TrimSpace(asString(root["client_secret"]))
}

func providerScopes(provider string) []string {
	switch normalizeMailProvider(provider) {
	case string(MailProviderGmail):
		return []string{"https://www.googleapis.com/auth/gmail.readonly"}
	default:
		return nil
	}
}

func providerAuthEndpoint(provider string) string {
	switch normalizeMailProvider(provider) {
	case string(MailProviderGmail):
		return "https://accounts.google.com/o/oauth2/v2/auth"
	default:
		return ""
	}
}

func providerTokenEndpoint(provider string) string {
	switch normalizeMailProvider(provider) {
	case string(MailProviderGmail):
		return "https://oauth2.googleapis.com/token"
	default:
		return ""
	}
}

func providerClientID(provider string) string {
	switch normalizeMailProvider(provider) {
	case string(MailProviderGmail):
		return gmailOAuthClientID()
	default:
		return ""
	}
}

func providerClientSecret(provider string) string {
	switch normalizeMailProvider(provider) {
	case string(MailProviderGmail):
		return gmailOAuthClientSecret()
	default:
		return ""
	}
}

func loadProviderToken(raw string) (providerToken, error) {
	token := providerToken{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &token); err != nil {
		return token, err
	}
	return token, nil
}

func startMailConnect(statePath string, provider string, baseURL string) (MailConnectResponse, error) {
	normalized := normalizeMailProvider(provider)
	if normalized != string(MailProviderGmail) {
		return MailConnectResponse{Ok: false}, fmt.Errorf("unsupported provider")
	}
	return startGmailDesktopConnect(statePath)
}

func startGmailDesktopConnect(statePath string) (MailConnectResponse, error) {
	response := MailConnectResponse{Ok: false, Provider: string(MailProviderGmail)}
	clientID := strings.TrimSpace(gmailOAuthClientID())
	clientSecret := strings.TrimSpace(gmailOAuthClientSecret())
	if clientID == "" {
		return response, fmt.Errorf("missing GOOGLE_OAUTH_CLIENT_ID or Google desktop client JSON")
	}
	if clientSecret == "" {
		return response, fmt.Errorf("missing GOOGLE_OAUTH_CLIENT_SECRET or Google desktop client JSON")
	}
	state, err := randomURLToken(24)
	if err != nil {
		return response, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return response, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return response, fmt.Errorf("failed to start Gmail loopback listener: %w", err)
	}
	redirectURI := "http://" + listener.Addr().String()
	session := mailOAuthSession{
		Provider:     string(MailProviderGmail),
		State:        state,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		CreatedAt:    time.Now(),
	}
	server := &http.Server{}
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		query := r.URL.Query()
		if query.Get("state") != state {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h1>Mail connect failed</h1><p>OAuth state did not match.</p></body></html>"))
			go shutdown()
			return
		}
		if errText := strings.TrimSpace(query.Get("error")); errText != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h1>Mail connect failed</h1><p>" + html.EscapeString(errText) + "</p></body></html>"))
			go shutdown()
			return
		}
		code := strings.TrimSpace(query.Get("code"))
		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h1>Mail connect failed</h1><p>Missing OAuth code.</p></body></html>"))
			go shutdown()
			return
		}
		token, exchangeErr := exchangeMailOAuthCode(string(MailProviderGmail), session, code)
		if exchangeErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h1>Mail connect failed</h1><p>" + html.EscapeString(exchangeErr.Error()) + "</p></body></html>"))
			go shutdown()
			return
		}
		identity, identityErr := fetchMailIdentity(string(MailProviderGmail), token)
		if identityErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h1>Mail connect failed</h1><p>" + html.EscapeString(identityErr.Error()) + "</p></body></html>"))
			go shutdown()
			return
		}
		tokenBody, _ := json.Marshal(token)
		identity.Provider = string(MailProviderGmail)
		identity.Status = "connected"
		identity.ConnectedAt = utcNow()
		stored, storeErr := upsertMailAccount(statePath, mailStoredAccount{
			MailAccount: identity,
			TokenJSON:   string(tokenBody),
		})
		if storeErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("<html><body><h1>Mail connect failed</h1><p>" + html.EscapeString(storeErr.Error()) + "</p></body></html>"))
			go shutdown()
			return
		}
		link := strings.TrimRight(frontendBaseURL(), "/") + "/mail"
		_, _ = w.Write([]byte("<html><body><h1>Mailbox connected</h1><p>gmail connected for " + html.EscapeString(stored.Email) + ".</p><p><a href=\"" + html.EscapeString(link) + "\">Return to Mail Monitor</a></p></body></html>"))
		go shutdown()
	})
	go func() {
		_ = server.Serve(listener)
	}()
	time.AfterFunc(5*time.Minute, shutdown)

	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("scope", strings.Join(providerScopes(string(MailProviderGmail)), " "))
	values.Set("state", state)
	values.Set("code_challenge", mailCodeChallenge(verifier))
	values.Set("code_challenge_method", "S256")
	values.Set("access_type", "offline")
	values.Set("include_granted_scopes", "true")
	values.Set("prompt", "consent")
	response.Ok = true
	response.AuthURL = providerAuthEndpoint(string(MailProviderGmail)) + "?" + values.Encode()
	response.Message = "Complete Gmail sign-in in the new tab. When Google finishes, the local callback page will confirm the mailbox is connected."
	return response, nil
}

func startMailOAuth(provider string, baseURL string) (MailConnectResponse, error) {
	response := MailConnectResponse{Ok: false, Provider: normalizeMailProvider(provider)}
	if response.Provider == "" {
		return response, fmt.Errorf("unsupported provider")
	}
	clientID := providerClientID(response.Provider)
	if clientID == "" {
		return response, fmt.Errorf("missing OAuth client ID for %s", response.Provider)
	}
	if response.Provider == string(MailProviderGmail) && providerClientSecret(response.Provider) == "" {
		return response, fmt.Errorf("missing GOOGLE_OAUTH_CLIENT_SECRET")
	}
	state, err := randomURLToken(24)
	if err != nil {
		return response, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return response, err
	}
	redirectURI := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/mail/auth/" + response.Provider + "/callback"
	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("scope", strings.Join(providerScopes(response.Provider), " "))
	values.Set("state", state)
	values.Set("code_challenge", mailCodeChallenge(verifier))
	values.Set("code_challenge_method", "S256")
	if response.Provider == string(MailProviderGmail) {
		values.Set("access_type", "offline")
		values.Set("include_granted_scopes", "true")
		values.Set("prompt", "consent")
	}
	mailOAuthSessions.mu.Lock()
	mailOAuthSessions.sessions[state] = mailOAuthSession{
		Provider:     response.Provider,
		State:        state,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		CreatedAt:    time.Now(),
	}
	mailOAuthSessions.mu.Unlock()
	response.Ok = true
	response.AuthURL = providerAuthEndpoint(response.Provider) + "?" + values.Encode()
	return response, nil
}

func takeMailOAuthSession(state string) (mailOAuthSession, bool) {
	mailOAuthSessions.mu.Lock()
	defer mailOAuthSessions.mu.Unlock()
	session, ok := mailOAuthSessions.sessions[state]
	if ok {
		delete(mailOAuthSessions.sessions, state)
	}
	for key, candidate := range mailOAuthSessions.sessions {
		if time.Since(candidate.CreatedAt) > 20*time.Minute {
			delete(mailOAuthSessions.sessions, key)
		}
	}
	return session, ok
}

func exchangeMailOAuthCode(provider string, session mailOAuthSession, code string) (providerToken, error) {
	form := url.Values{}
	form.Set("client_id", providerClientID(provider))
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", session.RedirectURI)
	form.Set("code_verifier", session.CodeVerifier)
	if secret := providerClientSecret(provider); secret != "" {
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequest(http.MethodPost, providerTokenEndpoint(provider), strings.NewReader(form.Encode()))
	if err != nil {
		return providerToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := mailHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return providerToken{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return providerToken{}, err
	}
	if resp.StatusCode >= 400 {
		return providerToken{}, fmt.Errorf("oauth token exchange failed: %s", normalizeTextSnippet(string(body), 300))
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return providerToken{}, err
	}
	token := providerToken{
		AccessToken:  strings.TrimSpace(asString(payload["access_token"])),
		TokenType:    strings.TrimSpace(asString(payload["token_type"])),
		RefreshToken: strings.TrimSpace(asString(payload["refresh_token"])),
		Scope:        strings.TrimSpace(asString(payload["scope"])),
	}
	expiresIn := parseIntDefault(payload["expires_in"], 0)
	if expiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if token.AccessToken == "" {
		return providerToken{}, fmt.Errorf("oauth exchange returned no access token")
	}
	return token, nil
}

func refreshMailOAuthToken(provider string, existing providerToken) (providerToken, error) {
	if strings.TrimSpace(existing.RefreshToken) == "" {
		return existing, nil
	}
	form := url.Values{}
	form.Set("client_id", providerClientID(provider))
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", existing.RefreshToken)
	if secret := providerClientSecret(provider); secret != "" {
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequest(http.MethodPost, providerTokenEndpoint(provider), strings.NewReader(form.Encode()))
	if err != nil {
		return providerToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := mailHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return providerToken{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return providerToken{}, err
	}
	if resp.StatusCode >= 400 {
		return providerToken{}, fmt.Errorf("oauth refresh failed: %s", normalizeTextSnippet(string(body), 300))
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return providerToken{}, err
	}
	token := existing
	if accessToken := strings.TrimSpace(asString(payload["access_token"])); accessToken != "" {
		token.AccessToken = accessToken
	}
	if tokenType := strings.TrimSpace(asString(payload["token_type"])); tokenType != "" {
		token.TokenType = tokenType
	}
	if refreshToken := strings.TrimSpace(asString(payload["refresh_token"])); refreshToken != "" {
		token.RefreshToken = refreshToken
	}
	if scope := strings.TrimSpace(asString(payload["scope"])); scope != "" {
		token.Scope = scope
	}
	expiresIn := parseIntDefault(payload["expires_in"], 0)
	if expiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return token, nil
}

func tokenExpiredSoon(token providerToken) bool {
	expiry := parseISOTime(token.Expiry)
	if expiry.IsZero() {
		return false
	}
	return time.Until(expiry) < 90*time.Second
}

func persistMailAccountToken(statePath string, accountID int64, token providerToken) error {
	db, err := openMailDB(statePath)
	if err != nil {
		return err
	}
	body, err := json.Marshal(token)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE mail_accounts SET token_json = ?, updated_at = ? WHERE id = ?`, string(body), utcNow(), accountID)
	return err
}

func ensureFreshMailToken(statePath string, account *mailStoredAccount) (providerToken, error) {
	token, err := loadProviderToken(account.TokenJSON)
	if err != nil {
		return token, err
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return token, fmt.Errorf("missing access token")
	}
	if !tokenExpiredSoon(token) {
		return token, nil
	}
	refreshed, err := refreshMailOAuthToken(account.Provider, token)
	if err != nil {
		return token, err
	}
	if err := persistMailAccountToken(statePath, account.ID, refreshed); err != nil {
		return refreshed, err
	}
	account.TokenJSON = string(mustJSON(refreshed))
	return refreshed, nil
}

func mustJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}

func authJSONRequest(token providerToken, method string, target string, headers map[string]string, body io.Reader) (map[string]any, error) {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := mailHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed: %s", normalizeTextSnippet(string(raw), 300))
	}
	payload := map[string]any{}
	if len(raw) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func authBytesRequest(token providerToken, method string, target string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := mailHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed: %s", normalizeTextSnippet(string(body), 300))
	}
	return body, nil
}

func fetchMailIdentity(provider string, token providerToken) (MailAccount, error) {
	account := MailAccount{Provider: provider, Status: "connected"}
	switch normalizeMailProvider(provider) {
	case string(MailProviderGmail):
		payload, err := authJSONRequest(token, http.MethodGet, "https://gmail.googleapis.com/gmail/v1/users/me/profile", nil, nil)
		if err != nil {
			return account, err
		}
		account.Email = strings.TrimSpace(asString(payload["emailAddress"]))
		account.DisplayName = account.Email
	default:
		return account, fmt.Errorf("unsupported provider")
	}
	if account.Email == "" {
		return account, fmt.Errorf("provider did not return account email")
	}
	account.Scopes = providerScopes(provider)
	return account, nil
}

func handleMailOAuthCallback(statePath string, provider string, r *http.Request) (MailAccount, error) {
	if normalizeMailProvider(provider) != string(MailProviderGmail) {
		return MailAccount{}, fmt.Errorf("unsupported provider")
	}
	sessionState := strings.TrimSpace(r.URL.Query().Get("state"))
	session, ok := takeMailOAuthSession(sessionState)
	if !ok || session.Provider != normalizeMailProvider(provider) {
		return MailAccount{}, fmt.Errorf("oauth session not found or expired")
	}
	if errText := strings.TrimSpace(r.URL.Query().Get("error")); errText != "" {
		return MailAccount{}, fmt.Errorf("oauth denied: %s", errText)
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		return MailAccount{}, fmt.Errorf("missing oauth code")
	}
	token, err := exchangeMailOAuthCode(provider, session, code)
	if err != nil {
		return MailAccount{}, err
	}
	identity, err := fetchMailIdentity(provider, token)
	if err != nil {
		return MailAccount{}, err
	}
	tokenBody, _ := json.Marshal(token)
	identity.Provider = normalizeMailProvider(provider)
	identity.Status = "connected"
	identity.ConnectedAt = utcNow()
	return upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: identity,
		TokenJSON:   string(tokenBody),
	})
}

func decodeGmailRawMessage(rawValue string) ([]byte, error) {
	normalized := strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t', ' ':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(rawValue))
	if normalized == "" {
		return nil, fmt.Errorf("missing Gmail raw payload")
	}

	candidates := []struct {
		name   string
		decode func(string) ([]byte, error)
		value  string
	}{
		{name: "raw-url", decode: base64.RawURLEncoding.DecodeString, value: normalized},
		{name: "url", decode: base64.URLEncoding.DecodeString, value: normalized},
		{name: "raw-std", decode: base64.RawStdEncoding.DecodeString, value: normalized},
		{name: "std", decode: base64.StdEncoding.DecodeString, value: normalized},
	}
	if remainder := len(normalized) % 4; remainder != 0 {
		padded := normalized + strings.Repeat("=", 4-remainder)
		candidates = append(candidates,
			struct {
				name   string
				decode func(string) ([]byte, error)
				value  string
			}{name: "url-padded", decode: base64.URLEncoding.DecodeString, value: padded},
			struct {
				name   string
				decode func(string) ([]byte, error)
				value  string
			}{name: "std-padded", decode: base64.StdEncoding.DecodeString, value: padded},
		)
	}

	var lastErr error
	for _, candidate := range candidates {
		decoded, err := candidate.decode(candidate.value)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("failed to decode Gmail raw payload")
	}
	return nil, lastErr
}

func syncGmailMessages(account mailStoredAccount, token providerToken, context mailMatchContext) ([]MailMessage, error) {
	return fetchGmailMessages(account, token, context, gmailFetchOptions{
		Since:       mailSyncWindow(account.LastSyncAt),
		MaxMessages: mailSyncMaxMessages(),
		Classify:    true,
		InboxOnly:   true,
	})
}

type gmailFetchOptions struct {
	Since       time.Time
	MaxMessages int
	Classify    bool
	InboxOnly   bool
}

func fetchGmailMessages(account mailStoredAccount, token providerToken, context mailMatchContext, options gmailFetchOptions) ([]MailMessage, error) {
	since := options.Since
	if since.IsZero() {
		since = mailSyncWindow(account.LastSyncAt)
	}
	maxMessages := options.MaxMessages
	if maxMessages <= 0 {
		maxMessages = mailSyncMaxMessages()
	}
	query := url.Values{}
	query.Set("maxResults", "50")
	if options.InboxOnly {
		query.Add("labelIds", "INBOX")
		query.Add("labelIds", "CATEGORY_PERSONAL")
	}
	query.Set("q", fmt.Sprintf("after:%d -label:spam -label:trash -label:sent -label:drafts", since.Unix()))
	target := strings.TrimRight(gmailAPIBaseURL, "/") + "/users/me/messages?" + query.Encode()
	results := make([]MailMessage, 0)
	for target != "" && len(results) < maxMessages {
		payload, err := authJSONRequest(token, http.MethodGet, target, nil, nil)
		if err != nil {
			return results, err
		}
		for _, row := range asSlice(payload["messages"]) {
			item := asMap(row)
			id := strings.TrimSpace(asString(item["id"]))
			if id == "" {
				continue
			}
			messagePayload, err := authJSONRequest(token, http.MethodGet, strings.TrimRight(gmailAPIBaseURL, "/")+"/users/me/messages/"+url.PathEscape(id)+"?format=raw", nil, nil)
			if err != nil {
				return results, err
			}
			rawValue := strings.TrimSpace(asString(messagePayload["raw"]))
			rawBytes, err := decodeGmailRawMessage(rawValue)
			if err != nil {
				return results, err
			}
			parsed, err := parseInternetMessage(rawBytes)
			if err != nil {
				return results, err
			}
			labels := make([]string, 0)
			isUnread := false
			for _, label := range asSlice(messagePayload["labelIds"]) {
				value := strings.TrimSpace(asString(label))
				if value == "" {
					continue
				}
				labels = append(labels, value)
				if strings.EqualFold(value, "UNREAD") {
					isUnread = true
				}
			}
			message := MailMessage{
				Provider:          string(MailProviderGmail),
				ProviderMessageID: id,
				ProviderThreadID:  strings.TrimSpace(asString(messagePayload["threadId"])),
				InternetMessageID: parsed.MessageID,
				WebLink:           "https://mail.google.com/mail/u/0/#inbox/" + id,
				Subject:           nonEmpty(parsed.Subject, asString(messagePayload["snippet"])),
				Sender:            parsed.From,
				ToRecipients:      parsed.To,
				CcRecipients:      parsed.Cc,
				ReceivedAt:        normalizeUnixMillisString(asString(messagePayload["internalDate"])),
				Labels:            labels,
				IsUnread:          isUnread,
				Snippet:           normalizeTextSnippet(asString(messagePayload["snippet"]), 320),
				BodyText:          truncateRunes(strings.TrimSpace(parsed.TextBody), 120000),
				BodyHTML:          truncateRunes(strings.TrimSpace(parsed.HTMLBody), 120000),
				HydrationStatus:   mailHydrationComplete,
				MetadataSource:    mailMetadataSourceLegacy,
				HydratedAt:        utcNow(),
			}
			if message.ReceivedAt == "" {
				message.ReceivedAt = parsed.Date
			}
			extractInviteMetadata(&message, parsed.Calendar)
			if options.Classify {
				classifyMailMessage(&message, context)
			}
			results = append(results, message)
			if len(results) >= maxMessages {
				break
			}
		}
		nextToken := strings.TrimSpace(asString(payload["nextPageToken"]))
		if nextToken == "" {
			break
		}
		target = strings.TrimRight(gmailAPIBaseURL, "/") + "/users/me/messages?" + query.Encode() + "&pageToken=" + url.QueryEscape(nextToken)
	}
	return results, nil
}

func stableMailFingerprint(message MailMessage) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.ToLower(strings.TrimSpace(message.Subject)),
		strings.ToLower(strings.TrimSpace(message.Sender.Email)),
		strings.ToLower(strings.TrimSpace(message.Sender.Name)),
		strings.TrimSpace(message.ReceivedAt),
		normalizeTextSnippet(message.Snippet, 240),
	}, "\n")))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
