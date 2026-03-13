package monitor

type MailProvider string

const (
	MailProviderGmail   MailProvider = "gmail"
	MailProviderOutlook MailProvider = "outlook"
)

const (
	mailEventRecruiterReply          = "recruiter_reply"
	mailEventRecruiterOutreach       = "recruiter_outreach"
	mailEventIndiaJobMarket          = "india_job_market"
	mailEventJobBoardInvite          = "job_board_invite"
	mailEventInterviewScheduled      = "interview_scheduled"
	mailEventInterviewUpdated        = "interview_updated"
	mailEventApplicationAcknowledged = "application_acknowledged"
	mailEventRejection               = "rejection"
	mailEventOtherJobRelated         = "other_job_related"
	mailEventIgnored                 = "ignored"
)

const (
	mailTriageNew       = "new"
	mailTriageReviewed  = "reviewed"
	mailTriageImportant = "important"
	mailTriageIgnored   = "ignored"
	mailTriageFollowUp  = "follow_up"
)

const (
	mailHydrationPending  = "pending"
	mailHydrationComplete = "complete"
	mailHydrationFailed   = "failed"
)

const (
	mailMetadataSourceGraph      = "graph"
	mailMetadataSourceOWAService = "owa_service"
	mailMetadataSourceDOMRow     = "dom_row"
	mailMetadataSourceDOMOpen    = "dom_open"
	mailMetadataSourceLegacy     = "legacy"
)

type MailAddress struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

type MailAccount struct {
	ID          int64    `json:"id"`
	Provider    string   `json:"provider"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name,omitempty"`
	Status      string   `json:"status"`
	Scopes      []string `json:"scopes,omitempty"`
	ConnectedAt string   `json:"connected_at,omitempty"`
	LastSyncAt  string   `json:"last_sync_at,omitempty"`
	LastError   string   `json:"last_error,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

type MailAccountsResponse struct {
	GeneratedAt string        `json:"generated_at"`
	Accounts    []MailAccount `json:"accounts"`
}

type MailMessage struct {
	ID                int64         `json:"id"`
	AccountID         int64         `json:"account_id"`
	Provider          string        `json:"provider"`
	AccountEmail      string        `json:"account_email,omitempty"`
	ProviderMessageID string        `json:"provider_message_id,omitempty"`
	ProviderThreadID  string        `json:"provider_thread_id,omitempty"`
	InternetMessageID string        `json:"internet_message_id,omitempty"`
	WebLink           string        `json:"web_link,omitempty"`
	Subject           string        `json:"subject"`
	Sender            MailAddress   `json:"sender"`
	ToRecipients      []MailAddress `json:"to_recipients,omitempty"`
	CcRecipients      []MailAddress `json:"cc_recipients,omitempty"`
	ReceivedAt        string        `json:"received_at,omitempty"`
	ReceivedAtLocal   string        `json:"received_at_local,omitempty"`
	UpdatedAt         string        `json:"updated_at,omitempty"`
	UpdatedAtLocal    string        `json:"updated_at_local,omitempty"`
	Labels            []string      `json:"labels,omitempty"`
	IsUnread          bool          `json:"is_unread"`
	Snippet           string        `json:"snippet,omitempty"`
	BodyText          string        `json:"body_text,omitempty"`
	BodyHTML          string        `json:"body_html,omitempty"`
	HydrationStatus   string        `json:"hydration_status,omitempty"`
	MetadataSource    string        `json:"metadata_source,omitempty"`
	HydratedAt        string        `json:"hydrated_at,omitempty"`
	HasInvite         bool          `json:"has_invite"`
	MeetingStart      string        `json:"meeting_start,omitempty"`
	MeetingEnd        string        `json:"meeting_end,omitempty"`
	MeetingOrganizer  string        `json:"meeting_organizer,omitempty"`
	MeetingLocation   string        `json:"meeting_location,omitempty"`
	MatchedCompany    string        `json:"matched_company,omitempty"`
	MatchedJobTitle   string        `json:"matched_job_title,omitempty"`
	EventType         string        `json:"event_type"`
	Importance        bool          `json:"importance"`
	Confidence        float64       `json:"confidence"`
	DecisionSource    string        `json:"decision_source"`
	Reasons           []string      `json:"reasons,omitempty"`
	TriageStatus      string        `json:"triage_status"`
}

type MailMessageFilters struct {
	AccountID     int64
	Provider      string
	EventType     string
	TriageStatus  string
	Company       string
	UnreadOnly    bool
	ImportantOnly bool
	Limit         int
}

type MailMessagesSummary struct {
	GeneratedAt       string `json:"generated_at"`
	TotalMessages     int    `json:"total_messages"`
	FilteredMessages  int    `json:"filtered_messages"`
	NewMessages       int    `json:"new_messages"`
	ImportantUnread   int    `json:"important_unread"`
	ConnectedAccounts int    `json:"connected_accounts"`
}

type MailMessagesFilters struct {
	AccountIDOptions []MailAccount `json:"account_options"`
	Provider         string        `json:"provider"`
	AccountID        int64         `json:"account_id"`
	EventType        string        `json:"event_type"`
	TriageStatus     string        `json:"triage_status"`
	Company          string        `json:"company"`
	UnreadOnly       bool          `json:"unread_only"`
	ImportantOnly    bool          `json:"important_only"`
	Limit            int           `json:"limit"`
	CompanyOptions   []string      `json:"company_options"`
	EventOptions     []string      `json:"event_options"`
	TriageOptions    []string      `json:"triage_options"`
}

type MailMessagesResponse struct {
	Summary  MailMessagesSummary `json:"summary"`
	Filters  MailMessagesFilters `json:"filters"`
	Messages []MailMessage       `json:"messages"`
}

type MailMessageDetailResponse struct {
	Ok      bool        `json:"ok"`
	Message MailMessage `json:"message"`
}

type MailTriageUpdateResponse struct {
	Ok           bool   `json:"ok"`
	ID           int64  `json:"id"`
	TriageStatus string `json:"triage_status"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

type MailConnectResponse struct {
	Ok       bool   `json:"ok"`
	Provider string `json:"provider"`
	AuthURL  string `json:"auth_url,omitempty"`
	Message  string `json:"message,omitempty"`
}

type MailAccountActionResponse struct {
	Ok      bool        `json:"ok"`
	Account MailAccount `json:"account,omitempty"`
	Message string      `json:"message,omitempty"`
}

type MailCorpusExportResponse struct {
	Ok              bool   `json:"ok"`
	Provider        string `json:"provider,omitempty"`
	AccountEmail    string `json:"account_email,omitempty"`
	Days            int    `json:"days,omitempty"`
	MaxMessages     int    `json:"max_messages,omitempty"`
	Exported        int    `json:"exported,omitempty"`
	ExistingMatches int    `json:"existing_matches,omitempty"`
	FilePath        string `json:"file_path,omitempty"`
	GeneratedAt     string `json:"generated_at,omitempty"`
	Message         string `json:"message,omitempty"`
}

type MailOverviewHighlight struct {
	ID              int64  `json:"id"`
	Subject         string `json:"subject"`
	Company         string `json:"company,omitempty"`
	Sender          string `json:"sender,omitempty"`
	ReceivedAt      string `json:"received_at,omitempty"`
	ReceivedAtLocal string `json:"received_at_local,omitempty"`
	EventType       string `json:"event_type"`
}

type MailOverviewSummary struct {
	GeneratedAt            string                 `json:"generated_at"`
	UnreadImportantCount   int                    `json:"unread_important_count"`
	NewMessageCount        int                    `json:"new_message_count"`
	ConnectedAccountsCount int                    `json:"connected_accounts_count"`
	EventCounts            map[string]int         `json:"event_counts"`
	LatestInterview        *MailOverviewHighlight `json:"latest_interview,omitempty"`
	LatestRejection        *MailOverviewHighlight `json:"latest_rejection,omitempty"`
	LatestRecruiterReply   *MailOverviewHighlight `json:"latest_recruiter_reply,omitempty"`
}

type MailOverviewResponse struct {
	Summary  MailOverviewSummary `json:"summary"`
	Accounts []MailAccount       `json:"accounts"`
}

type MailCountPair struct {
	Applications int `json:"applications"`
	Rejections   int `json:"rejections"`
}

type MailDailyBucket struct {
	DayKey       string `json:"day_key"`
	Label        string `json:"label"`
	Applications int    `json:"applications"`
	Rejections   int    `json:"rejections"`
}

type MailLifecycleApplicationItem struct {
	ID              int64   `json:"id"`
	Company         string  `json:"company,omitempty"`
	JobTitle        string  `json:"job_title,omitempty"`
	Subject         string  `json:"subject"`
	ReceivedAt      string  `json:"received_at,omitempty"`
	ReceivedAtLocal string  `json:"received_at_local,omitempty"`
	Sender          string  `json:"sender,omitempty"`
	LowConfidence   bool    `json:"low_confidence"`
	Confidence      float64 `json:"confidence"`
}

type MailLifecycleRejectionItem struct {
	ID                 int64   `json:"id"`
	Company            string  `json:"company,omitempty"`
	JobTitle           string  `json:"job_title,omitempty"`
	Subject            string  `json:"subject"`
	ReceivedAt         string  `json:"received_at,omitempty"`
	ReceivedAtLocal    string  `json:"received_at_local,omitempty"`
	Sender             string  `json:"sender,omitempty"`
	Confidence         float64 `json:"confidence"`
	CompanyOnlyMatch   bool    `json:"company_only_match,omitempty"`
	MatchedApplication string  `json:"matched_application,omitempty"`
}

type MailMeetingItem struct {
	ID               int64  `json:"id"`
	Company          string `json:"company,omitempty"`
	Subject          string `json:"subject"`
	ReceivedAt       string `json:"received_at,omitempty"`
	ReceivedAtLocal  string `json:"received_at_local,omitempty"`
	MeetingStart     string `json:"meeting_start,omitempty"`
	MeetingEnd       string `json:"meeting_end,omitempty"`
	MeetingOrganizer string `json:"meeting_organizer,omitempty"`
	MeetingLocation  string `json:"meeting_location,omitempty"`
	Sender           string `json:"sender,omitempty"`
}

type MailOpenActionItem struct {
	ID              int64    `json:"id"`
	Company         string   `json:"company,omitempty"`
	JobTitle        string   `json:"job_title,omitempty"`
	Subject         string   `json:"subject"`
	EventType       string   `json:"event_type"`
	TriageStatus    string   `json:"triage_status"`
	ReceivedAt      string   `json:"received_at,omitempty"`
	ReceivedAtLocal string   `json:"received_at_local,omitempty"`
	Sender          string   `json:"sender,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
}

type MailAnalyticsSummary struct {
	GeneratedAt               string        `json:"generated_at"`
	ConnectedAccountsCount    int           `json:"connected_accounts_count"`
	Today                     MailCountPair `json:"today"`
	Yesterday                 MailCountPair `json:"yesterday"`
	Last7Days                 MailCountPair `json:"last_7_days"`
	AllTime                   MailCountPair `json:"all_time"`
	OpenApplicationsCount     int           `json:"open_applications_count"`
	ResolvedRejectionsCount   int           `json:"resolved_rejections_count"`
	UnresolvedRejectionsCount int           `json:"unresolved_rejections_count"`
	OpenActionsCount          int           `json:"open_actions_count"`
	UpcomingMeetingsCount     int           `json:"upcoming_meetings_count"`
}

type MailAnalyticsDetails struct {
	OpenApplications     []MailLifecycleApplicationItem `json:"open_applications"`
	ResolvedRejections   []MailLifecycleRejectionItem   `json:"resolved_rejections"`
	UnresolvedRejections []MailLifecycleRejectionItem   `json:"unresolved_rejections"`
	UpcomingMeetings     []MailMeetingItem              `json:"upcoming_meetings"`
	OpenActions          []MailOpenActionItem           `json:"open_actions"`
}

type MailAnalyticsResponse struct {
	Summary      MailAnalyticsSummary `json:"summary"`
	DailyBuckets []MailDailyBucket    `json:"daily_buckets"`
	Details      MailAnalyticsDetails `json:"details"`
}

type MailRunAccountProgress struct {
	AccountID     int64  `json:"account_id"`
	Provider      string `json:"provider"`
	AccountEmail  string `json:"account_email,omitempty"`
	Phase         string `json:"phase"`
	Fetched       int    `json:"fetched"`
	Stored        int    `json:"stored"`
	Discovered    int    `json:"discovered"`
	Hydrated      int    `json:"hydrated"`
	Important     int    `json:"important"`
	CutoffReached bool   `json:"cutoff_reached"`
	DegradedMode  bool   `json:"degraded_mode"`
	Message       string `json:"message,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

type MailRunStatus struct {
	Running            bool                     `json:"running"`
	Queued             bool                     `json:"queued"`
	LastStart          string                   `json:"last_start,omitempty"`
	LastEnd            string                   `json:"last_end,omitempty"`
	LastError          string                   `json:"last_error,omitempty"`
	AccountsTotal      int                      `json:"accounts_total"`
	AccountsCompleted  int                      `json:"accounts_completed"`
	MessagesFetched    int                      `json:"messages_fetched"`
	MessagesStored     int                      `json:"messages_stored"`
	MessagesDiscovered int                      `json:"messages_discovered"`
	MessagesHydrated   int                      `json:"messages_hydrated"`
	ImportantMessages  int                      `json:"important_messages"`
	CutoffReached      bool                     `json:"cutoff_reached"`
	DegradedMode       bool                     `json:"degraded_mode"`
	Progress           []MailRunAccountProgress `json:"progress,omitempty"`
}

type MailServiceConfig struct {
	StatePath  string
	ConfigPath string
}

type MailSyncAccountSummary struct {
	AccountID      int64
	Provider       string
	AccountEmail   string
	Phase          string
	Fetched        int
	Stored         int
	Discovered     int
	Hydrated       int
	ImportantCount int
	CutoffReached  bool
	DegradedMode   bool
}

type MailSyncSummary struct {
	Accounts      []MailSyncAccountSummary
	Fetched       int
	Stored        int
	Discovered    int
	Hydrated      int
	Important     int
	CutoffReached bool
	DegradedMode  bool
}

type providerToken struct {
	Kind             string `json:"kind,omitempty"`
	AccessToken      string `json:"access_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Expiry           string `json:"expiry,omitempty"`
	StorageStatePath string `json:"storage_state_path,omitempty"`
}

type mailStoredAccount struct {
	MailAccount
	TokenJSON string
}
