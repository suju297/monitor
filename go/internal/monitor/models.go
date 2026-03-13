package monitor

import "time"

type Job struct {
	Company     string `json:"company"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	ExternalID  string `json:"external_id,omitempty"`
	Location    string `json:"location,omitempty"`
	Team        string `json:"team,omitempty"`
	PostedAt    string `json:"posted_at,omitempty"`
	Description string `json:"description,omitempty"`
}

type CrawlOutcome struct {
	Company          string   `json:"company"`
	AttemptedSources []string `json:"attempted_sources"`
	Status           string   `json:"status"`
	SelectedSource   string   `json:"selected_source,omitempty"`
	Jobs             []Job    `json:"jobs,omitempty"`
	Message          string   `json:"message,omitempty"`
}

type Company struct {
	Name            string            `yaml:"name"`
	Source          string            `yaml:"source"`
	CareersURL      string            `yaml:"careers_url"`
	FallbackSources []string          `yaml:"fallback_sources"`
	TimeoutSeconds  int               `yaml:"timeout_seconds"`
	MaxLinks        int               `yaml:"max_links"`
	GreenhouseBoard string            `yaml:"greenhouse_board"`
	LeverSite       string            `yaml:"lever_site"`
	Template        map[string]any    `yaml:"template"`
	Command         any               `yaml:"command"`
	CommandEnv      map[string]string `yaml:"command_env"`
	MyGreenhouseCmd any               `yaml:"my_greenhouse_command"`
	Orchestration   map[string]any    `yaml:"orchestration"`
	Disabled        bool              `yaml:"disabled"`
}

type SeenEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	FirstSeen   string `json:"first_seen"`
	LastSeen    string `json:"last_seen,omitempty"`
	Active      bool   `json:"active,omitempty"`
	Source      string `json:"source,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
	Location    string `json:"location,omitempty"`
	Team        string `json:"team,omitempty"`
	PostedAt    string `json:"posted_at,omitempty"`
	Description string `json:"description,omitempty"`
}

type CompanyStatus struct {
	Status           string   `json:"status"`
	SelectedSource   string   `json:"selected_source,omitempty"`
	AttemptedSources []string `json:"attempted_sources,omitempty"`
	Message          string   `json:"message,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

type BlockedEvent struct {
	At               string   `json:"at"`
	AttemptedSources []string `json:"attempted_sources,omitempty"`
	Message          string   `json:"message,omitempty"`
}

type MonitorState struct {
	Seen         map[string]map[string]SeenEntry `json:"seen"`
	CompanyState map[string]CompanyStatus        `json:"company_status"`
	Blocked      map[string][]BlockedEvent       `json:"blocked_events"`
	LastRun      string                          `json:"last_run"`
}

type ReportSummary struct {
	CompaniesTotal int `json:"companies_total"`
	NewJobsCount   int `json:"new_jobs_count"`
	BlockedCount   int `json:"blocked_count"`
	OKCount        int `json:"ok_count"`
	ErrorCount     int `json:"error_count"`
}

type URLVerificationSummary struct {
	Enabled      bool   `json:"enabled"`
	TotalURLs    int    `json:"total_urls"`
	CheckedURLs  int    `json:"checked_urls"`
	SkippedURLs  int    `json:"skipped_urls"`
	OKCount      int    `json:"ok_count"`
	BlockedCount int    `json:"blocked_count"`
	ErrorCount   int    `json:"error_count"`
	DurationMs   int64  `json:"duration_ms"`
	ArtifactPath string `json:"artifact_path,omitempty"`
}

type RunReport struct {
	GeneratedAt     string                 `json:"generated_at"`
	DryRun          bool                   `json:"dry_run"`
	Baseline        bool                   `json:"baseline"`
	Summary         ReportSummary          `json:"summary"`
	URLVerification URLVerificationSummary `json:"url_verification"`
	Outcomes        []CrawlOutcome         `json:"outcomes"`
	NewJobs         []Job                  `json:"new_jobs"`
}

type RunnerStatus struct {
	Running            bool                    `json:"running"`
	Queued             bool                    `json:"queued"`
	LastStart          string                  `json:"last_start,omitempty"`
	LastEnd            string                  `json:"last_end,omitempty"`
	LastExitCode       int                     `json:"last_exit_code"`
	LastMode           string                  `json:"last_mode,omitempty"`
	QueuedMode         string                  `json:"queued_mode,omitempty"`
	LastStdout         string                  `json:"last_stdout,omitempty"`
	LastStderr         string                  `json:"last_stderr,omitempty"`
	LastError          string                  `json:"last_error,omitempty"`
	TotalCompanies     int                     `json:"total_companies"`
	CompletedCompanies int                     `json:"completed_companies"`
	Progress           []RunnerCompanyProgress `json:"progress,omitempty"`
	Scoring            SLMScoringStatus        `json:"scoring"`
}

type SLMScoringStatus struct {
	Running       bool   `json:"running"`
	Trigger       string `json:"trigger,omitempty"`
	Model         string `json:"model,omitempty"`
	Phase         string `json:"phase,omitempty"`
	EligibleJobs  int    `json:"eligible_jobs"`
	ScheduledJobs int    `json:"scheduled_jobs"`
	QueuedJobs    int    `json:"queued_jobs"`
	CompletedJobs int    `json:"completed_jobs"`
	SuccessJobs   int    `json:"success_jobs"`
	FailedJobs    int    `json:"failed_jobs"`
	StartedAt     string `json:"started_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type RunnerCompanyProgress struct {
	Company          string   `json:"company"`
	Phase            string   `json:"phase"` // queued | running | done
	OutcomeStatus    string   `json:"outcome_status,omitempty"`
	Source           string   `json:"source,omitempty"`
	JobsFound        int      `json:"jobs_found"`
	AttemptedSources []string `json:"attempted_sources,omitempty"`
	Message          string   `json:"message,omitempty"`
	StartedAt        string   `json:"started_at,omitempty"`
	FinishedAt       string   `json:"finished_at,omitempty"`
}

type DashboardSummary struct {
	GeneratedAt       string         `json:"generated_at"`
	LastRun           string         `json:"last_run,omitempty"`
	LastRunLocal      string         `json:"last_run_local"`
	CompaniesTotal    int            `json:"companies_total"`
	StatusCounts      map[string]int `json:"status_counts"`
	TotalSeenJobs     int            `json:"total_seen_jobs"`
	NewJobsLastReport int            `json:"new_jobs_last_report"`
	BlockedLastReport int            `json:"blocked_last_report"`
}

type DashboardCompany struct {
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	SelectedSource  string   `json:"selected_source"`
	AttemptedSource []string `json:"attempted_sources"`
	Message         string   `json:"message"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	UpdatedAtLocal  string   `json:"updated_at_local"`
	SeenJobs        int      `json:"seen_jobs"`
	BlockedEvents   int      `json:"blocked_events"`
}

type DashboardBlocked struct {
	Company          string   `json:"company"`
	At               string   `json:"at,omitempty"`
	AtLocal          string   `json:"at_local"`
	Message          string   `json:"message"`
	AttemptedSources []string `json:"attempted_sources"`
}

type DashboardJob struct {
	Company                       string   `json:"company"`
	Fingerprint                   string   `json:"fingerprint,omitempty"`
	Title                         string   `json:"title"`
	URL                           string   `json:"url"`
	FirstSeen                     string   `json:"first_seen,omitempty"`
	FirstSeenLocal                string   `json:"first_seen_local"`
	LastSeen                      string   `json:"last_seen,omitempty"`
	LastSeenLocal                 string   `json:"last_seen_local"`
	Active                        bool     `json:"active"`
	Source                        string   `json:"source,omitempty"`
	Location                      string   `json:"location,omitempty"`
	Team                          string   `json:"team,omitempty"`
	PostedAt                      string   `json:"posted_at,omitempty"`
	PostedAtLocal                 string   `json:"posted_at_local,omitempty"`
	Description                   string   `json:"description,omitempty"`
	ApplicationStatus             string   `json:"application_status,omitempty"`
	ApplicationUpdatedAt          string   `json:"application_updated_at,omitempty"`
	AssistantLastSyncAt           string   `json:"assistant_last_sync_at,omitempty"`
	AssistantLastSource           string   `json:"assistant_last_source,omitempty"`
	AssistantLastOutcome          string   `json:"assistant_last_outcome,omitempty"`
	AssistantLastAutoSubmit       bool     `json:"assistant_last_auto_submit_eligible,omitempty"`
	AssistantLastReviewPending    int      `json:"assistant_last_review_pending_count,omitempty"`
	AssistantLastConfirmed        bool     `json:"assistant_last_confirmation_detected,omitempty"`
	MatchScore                    int      `json:"match_score"`
	MatchReasons                  []string `json:"match_reasons,omitempty"`
	RecommendedResume             string   `json:"recommended_resume,omitempty"`
	RoleDecision                  string   `json:"role_decision,omitempty"`
	InternshipDecision            string   `json:"internship_decision,omitempty"`
	DecisionSource                string   `json:"decision_source,omitempty"`
	WorkAuthStatus                string   `json:"work_auth_status,omitempty"`
	WorkAuthNotes                 []string `json:"work_auth_notes,omitempty"`
	EVerifyStatus                 string   `json:"everify_status,omitempty"`
	EVerifySource                 string   `json:"everify_source,omitempty"`
	EVerifyChecked                string   `json:"everify_checked_at,omitempty"`
	EVerifyNote                   string   `json:"everify_note,omitempty"`
	HeuristicCached               bool     `json:"-"`
	HeuristicContextHash          string   `json:"-"`
	FeedRelevantCached            bool     `json:"-"`
	DeterministicRoleDecision     string   `json:"-"`
	DeterministicRoleReasons      []string `json:"-"`
	DeterministicInternshipStatus string   `json:"-"`
	NeedsRoleSLM                  bool     `json:"-"`
	NeedsInternshipSLM            bool     `json:"-"`
}

type DashboardJobsSummary struct {
	GeneratedAt          string `json:"generated_at"`
	TotalJobs            int    `json:"total_jobs"`
	FilteredJobs         int    `json:"filtered_jobs"`
	CompaniesCount       int    `json:"companies_count"`
	LatestFirstSeen      string `json:"latest_first_seen,omitempty"`
	LatestFirstSeenLocal string `json:"latest_first_seen_local"`
	PostedDatedJobs      int    `json:"posted_dated_jobs"`
	MissingPostedDate    int    `json:"missing_posted_date"`
	EVerifyEnrolled      int    `json:"everify_enrolled"`
	EVerifyUnknown       int    `json:"everify_unknown"`
	EVerifyNotFound      int    `json:"everify_not_found"`
	EVerifyNotEnrolled   int    `json:"everify_not_enrolled"`
}

type DashboardJobsFilters struct {
	Query           string   `json:"query"`
	Company         string   `json:"company"`
	Source          string   `json:"source"`
	SLMModel        string   `json:"slm_model"`
	SLMModelOptions []string `json:"slm_model_options"`
	Sort            string   `json:"sort"`
	Limit           int      `json:"limit"`
	PostedWithin    string   `json:"posted_within"`
	EVerify         string   `json:"everify"`
	Companies       []string `json:"companies"`
	SourceOptions   []string `json:"source_options"`
	PostedOptions   []string `json:"posted_options"`
	EVerifyOptions  []string `json:"everify_options"`
}

type DashboardJobsResponse struct {
	Summary DashboardJobsSummary `json:"summary"`
	Filters DashboardJobsFilters `json:"filters"`
	Jobs    []DashboardJob       `json:"jobs"`
}

type JobApplicationStatusUpdate struct {
	OK          bool   `json:"ok"`
	Fingerprint string `json:"fingerprint"`
	Status      string `json:"status"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type JobsFeedScoringProgress struct {
	Running       bool `json:"running"`
	ScheduledJobs int  `json:"scheduled_jobs"`
	QueuedJobs    int  `json:"queued_jobs"`
	CompletedJobs int  `json:"completed_jobs"`
	SuccessJobs   int  `json:"success_jobs"`
	FailedJobs    int  `json:"failed_jobs"`
}

type JobsFeedProgress struct {
	RunID           string                  `json:"run_id,omitempty"`
	Running         bool                    `json:"running"`
	SLMModel        string                  `json:"slm_model,omitempty"`
	Phase           string                  `json:"phase"`
	Message         string                  `json:"message,omitempty"`
	ProgressPercent int                     `json:"progress_percent"`
	TotalJobs       int                     `json:"total_jobs"`
	FilteredJobs    int                     `json:"filtered_jobs"`
	StartedAt       string                  `json:"started_at,omitempty"`
	UpdatedAt       string                  `json:"updated_at,omitempty"`
	FinishedAt      string                  `json:"finished_at,omitempty"`
	Scoring         JobsFeedScoringProgress `json:"scoring"`
}

type CrawlScheduleStatus struct {
	Enabled           bool   `json:"enabled"`
	IntervalMinutes   int    `json:"interval_minutes"`
	NextRunAt         string `json:"next_run_at,omitempty"`
	LastTriggerAt     string `json:"last_trigger_at,omitempty"`
	LastTriggerResult string `json:"last_trigger_result,omitempty"`
	LastError         string `json:"last_error,omitempty"`
}

type DashboardOverview struct {
	Summary       DashboardSummary    `json:"summary"`
	Companies     []DashboardCompany  `json:"companies"`
	BlockedRecent []DashboardBlocked  `json:"blocked_recent"`
	NewJobs       []DashboardJob      `json:"new_jobs"`
	Mail          MailOverviewSummary `json:"mail"`
	Runner        RunnerStatus        `json:"runner"`
	Paths         map[string]string   `json:"paths"`
}

type EVerifyCompanyRecord struct {
	Company   string `json:"company"`
	Status    string `json:"status"`
	Source    string `json:"source,omitempty"`
	CheckedAt string `json:"checked_at,omitempty"`
	Note      string `json:"note,omitempty"`
}

type RunOptions struct {
	ConfigPath     string
	StatePath      string
	ReportPath     string
	DotenvPath     string
	Workers        int
	Baseline       bool
	DryRun         bool
	AlertOnBlocked bool
	Verbose        bool
	OnCompanyStart func(company string)
	OnCompanyDone  func(outcome CrawlOutcome)
}

type TrackerResult struct {
	Outcomes        []CrawlOutcome
	NewJobs         []Job
	BlockedOutcomes []CrawlOutcome
	StatusLines     []string
	URLVerification URLVerificationSummary
	Report          RunReport
}

type MonitorRunnerConfig struct {
	CWD            string
	ConfigPath     string
	StatePath      string
	ReportPath     string
	SchedulePath   string
	DotenvPath     string
	Workers        int
	AlertOnBlocked bool
	BinaryPath     string
}

func localNow() time.Time {
	return time.Now().Local()
}
