export type OverviewResponse = {
  summary: {
    generated_at: string
    last_run?: string
    last_run_local: string
    companies_total: number
    status_counts: { ok: number; blocked: number; error: number; unknown: number }
    total_seen_jobs: number
    new_jobs_last_report: number
    blocked_last_report: number
  }
  mail: {
    generated_at: string
    unread_important_count: number
    new_message_count: number
    connected_accounts_count: number
    event_counts: Record<string, number>
    latest_interview?: {
      id: number
      subject: string
      company?: string
      sender?: string
      received_at?: string
      received_at_local?: string
      event_type: string
    } | null
    latest_rejection?: {
      id: number
      subject: string
      company?: string
      sender?: string
      received_at?: string
      received_at_local?: string
      event_type: string
    } | null
    latest_recruiter_reply?: {
      id: number
      subject: string
      company?: string
      sender?: string
      received_at?: string
      received_at_local?: string
      event_type: string
    } | null
  }
  companies: Array<{
    name: string
    status: string
    selected_source: string
    attempted_sources: string[]
    message: string
    updated_at?: string
    updated_at_local: string
    seen_jobs: number
    blocked_events: number
  }>
  blocked_recent: Array<{
    company: string
    at?: string
    at_local: string
    message: string
    attempted_sources: string[]
  }>
  new_jobs: Array<{
    company: string
    title: string
    url: string
    first_seen?: string
    first_seen_local: string
    source?: string
    location?: string
    team?: string
    posted_at?: string
    posted_at_local?: string
    description?: string
    match_score?: number
    match_reasons?: string[]
    recommended_resume?: string
    work_auth_status?: string
    work_auth_notes?: string[]
  }>
  runner: {
    running: boolean
    queued?: boolean
    last_start?: string
    last_end?: string
    last_exit_code: number
    last_mode?: string
    queued_mode?: string
    last_stdout?: string
    last_stderr?: string
    last_error?: string
    total_companies?: number
    completed_companies?: number
    progress?: Array<{
      company: string
      phase: 'queued' | 'running' | 'done' | string
      outcome_status?: string
      source?: string
      jobs_found: number
      attempted_sources?: string[]
      message?: string
      started_at?: string
      finished_at?: string
    }>
    scoring?: {
      running: boolean
      trigger?: string
      phase?: string
      eligible_jobs: number
      scheduled_jobs: number
      queued_jobs: number
      completed_jobs: number
      success_jobs: number
      failed_jobs: number
      started_at?: string
      updated_at?: string
      finished_at?: string
      duration_ms?: number
      last_error?: string
    }
  }
}

export type JobsResponse = {
  summary: {
    generated_at: string
    total_jobs: number
    filtered_jobs: number
    companies_count: number
    latest_first_seen?: string
    latest_first_seen_local: string
    posted_dated_jobs: number
    missing_posted_date: number
    everify_enrolled: number
    everify_unknown: number
    everify_not_found: number
    everify_not_enrolled: number
  }
  filters: {
    query: string
    company: string
    source: string
    slm_model: string
    slm_model_options: string[]
    sort: string
    limit: number
    posted_within: string
    everify: string
    companies: string[]
    source_options: string[]
    posted_options: string[]
    everify_options: string[]
  }
  jobs: Array<{
    company: string
    fingerprint?: string
    title: string
    url: string
    first_seen?: string
    first_seen_local: string
    source?: string
    location?: string
    team?: string
    posted_at?: string
    posted_at_local?: string
    description?: string
    application_status?: string
    application_updated_at?: string
    assistant_last_sync_at?: string
    assistant_last_source?: string
    assistant_last_outcome?: string
    assistant_last_auto_submit_eligible?: boolean
    assistant_last_review_pending_count?: number
    assistant_last_confirmation_detected?: boolean
    match_score?: number
    match_reasons?: string[]
    recommended_resume?: string
    role_decision?: string
    internship_decision?: string
    decision_source?: string
    work_auth_status?: string
    work_auth_notes?: string[]
    everify_status?: string
    everify_source?: string
    everify_checked_at?: string
    everify_note?: string
  }>
}

export type JobsProgressResponse = {
  run_id?: string
  running: boolean
  slm_model?: string
  phase: string
  message?: string
  progress_percent: number
  total_jobs: number
  filtered_jobs: number
  started_at?: string
  updated_at?: string
  finished_at?: string
  scoring: {
    running: boolean
    scheduled_jobs: number
    queued_jobs: number
    completed_jobs: number
    success_jobs: number
    failed_jobs: number
  }
}

export type JobApplicationStatusUpdateResponse = {
  ok: boolean
  fingerprint: string
  status: string
  updated_at?: string
}

export type CrawlScheduleResponse = {
  enabled: boolean
  interval_minutes: number
  next_run_at?: string
  last_trigger_at?: string
  last_trigger_result?: string
  last_error?: string
}

export type MailAccount = {
  id: number
  provider: string
  email: string
  display_name?: string
  status: string
  scopes?: string[]
  connected_at?: string
  last_sync_at?: string
  last_error?: string
  updated_at?: string
}

export type MailAccountActionResponse = {
  ok: boolean
  account?: MailAccount
  message?: string
}

export type MailOverviewResponse = {
  summary: {
    generated_at: string
    unread_important_count: number
    new_message_count: number
    connected_accounts_count: number
    event_counts: Record<string, number>
    latest_interview?: {
      id: number
      subject: string
      company?: string
      sender?: string
      received_at?: string
      received_at_local?: string
      event_type: string
    } | null
    latest_rejection?: {
      id: number
      subject: string
      company?: string
      sender?: string
      received_at?: string
      received_at_local?: string
      event_type: string
    } | null
    latest_recruiter_reply?: {
      id: number
      subject: string
      company?: string
      sender?: string
      received_at?: string
      received_at_local?: string
      event_type: string
    } | null
  }
  accounts: MailAccount[]
}

export type MailMessagesResponse = {
  summary: {
    generated_at: string
    total_messages: number
    filtered_messages: number
    new_messages: number
    important_unread: number
    connected_accounts: number
  }
  filters: {
    account_options: MailAccount[]
    provider: string
    account_id: number
    event_type: string
    triage_status: string
    company: string
    unread_only: boolean
    important_only: boolean
    limit: number
    company_options: string[]
    event_options: string[]
    triage_options: string[]
  }
  messages: Array<{
    id: number
    account_id: number
    provider: string
    account_email?: string
    provider_message_id?: string
    provider_thread_id?: string
    internet_message_id?: string
    web_link?: string
    subject: string
    sender: {
      name?: string
      email: string
    }
    to_recipients?: Array<{ name?: string; email: string }>
    cc_recipients?: Array<{ name?: string; email: string }>
    received_at?: string
    received_at_local?: string
    updated_at?: string
    updated_at_local?: string
    labels?: string[]
    is_unread: boolean
    snippet?: string
    body_text?: string
    body_html?: string
    hydration_status?: string
    metadata_source?: string
    hydrated_at?: string
    has_invite: boolean
    meeting_start?: string
    meeting_end?: string
    meeting_organizer?: string
    meeting_location?: string
    matched_company?: string
    matched_job_title?: string
    event_type: string
    importance: boolean
    confidence: number
    decision_source: string
    reasons?: string[]
    triage_status: string
  }>
}

export type MailCountPair = {
  applications: number
  rejections: number
}

export type MailDailyBucket = {
  day_key: string
  label: string
  applications: number
  rejections: number
}

export type MailLifecycleApplicationItem = {
  id: number
  company?: string
  job_title?: string
  subject: string
  received_at?: string
  received_at_local?: string
  sender?: string
  low_confidence: boolean
  confidence: number
}

export type MailLifecycleRejectionItem = {
  id: number
  company?: string
  job_title?: string
  subject: string
  received_at?: string
  received_at_local?: string
  sender?: string
  confidence: number
  company_only_match?: boolean
  matched_application?: string
}

export type MailMeetingItem = {
  id: number
  company?: string
  subject: string
  received_at?: string
  received_at_local?: string
  meeting_start?: string
  meeting_end?: string
  meeting_organizer?: string
  meeting_location?: string
  sender?: string
}

export type MailOpenActionItem = {
  id: number
  company?: string
  job_title?: string
  subject: string
  event_type: string
  triage_status: string
  received_at?: string
  received_at_local?: string
  sender?: string
  reasons?: string[]
}

export type MailAnalyticsResponse = {
  summary: {
    generated_at: string
    connected_accounts_count: number
    today: MailCountPair
    yesterday: MailCountPair
    last_7_days: MailCountPair
    all_time: MailCountPair
    open_applications_count: number
    resolved_rejections_count: number
    unresolved_rejections_count: number
    open_actions_count: number
    upcoming_meetings_count: number
  }
  daily_buckets: MailDailyBucket[]
  details: {
    open_applications: MailLifecycleApplicationItem[]
    resolved_rejections: MailLifecycleRejectionItem[]
    unresolved_rejections: MailLifecycleRejectionItem[]
    upcoming_meetings: MailMeetingItem[]
    open_actions: MailOpenActionItem[]
  }
}

export type MailMessageDetailResponse = {
  ok: boolean
  message: MailMessagesResponse['messages'][number]
}

export type MailRunStatusResponse = {
  running: boolean
  queued?: boolean
  last_start?: string
  last_end?: string
  last_error?: string
  accounts_total: number
  accounts_completed: number
  messages_fetched: number
  messages_stored: number
  messages_discovered: number
  messages_hydrated: number
  important_messages: number
  cutoff_reached: boolean
  degraded_mode: boolean
  progress?: Array<{
    account_id: number
    provider: string
    account_email?: string
    phase: string
    fetched: number
    stored: number
    discovered: number
    hydrated: number
    important: number
    cutoff_reached: boolean
    degraded_mode: boolean
    message?: string
    started_at?: string
    finished_at?: string
  }>
}

export type MailConnectResponse = {
  ok: boolean
  provider: string
  auth_url?: string
  message?: string
}

export type TaskTriggerResponse = {
  ok: boolean
  queued?: boolean
  started?: boolean
  message?: string
}

export type MailTriageUpdateResponse = {
  ok: boolean
  id: number
  triage_status: string
  updated_at?: string
}

export type ObserverRecommendationResponse = {
  ok: boolean
  requested_url: string
  company_name?: string
  job_title?: string
  decision_counts?: Record<string, number>
  auto_submit_eligible?: boolean
  available_resume_variants?: Array<{
    variant: string
    label: string
    path: string
    file: string
  }>
  recommended_resume_file?: string
  recommended_resume_path?: string
  recommended_resume_variant?: string
  recommended_resume_reason?: string
  recommended_resume_confidence?: number | null
  resume_selection_source?: string
  analysis?: Record<string, unknown>
  review_queue?: Array<{
    question_label: string
    decision: string
    status: string
    reason: string
    suggested_answer?: string
    suggested_answer_source?: string
  }>
}

export type AssistantRunStatusResponse = {
  ok: boolean
  run_id: string
  status: 'queued' | 'running' | 'completed' | 'failed' | string
  requested_url: string
  allow_submit: boolean
  headless: boolean
  selected_resume_variant?: string | null
  selected_resume_path?: string | null
  resume_selection_source?: string | null
  created_at: string
  started_at?: string | null
  finished_at?: string | null
  error?: string | null
  reused_existing_run?: boolean
  result_summary?: {
    company_name?: string | null
    job_title?: string | null
    filled_count: number
    skipped_count: number
    error_count: number
    browser_validation_error_count: number
    review_pending_count: number
    auto_submit_eligible: boolean
    eligible_after_browser_validation: boolean
    submitted: boolean
    confirmation_detected: boolean
    challenge_detected?: boolean
    challenge_blocker_count?: number
    selected_resume_variant?: string | null
    selected_resume_file?: string | null
    resume_selection_source?: string | null
    trace_id?: string | null
    manual_session_id?: string | null
    manual_event_count?: number
    jobs_db_stored: boolean
  } | null
  result?: Record<string, unknown> | null
}

export type AssistantRunsResponse = {
  ok: boolean
  run_count: number
  runs: AssistantRunStatusResponse[]
}

export type AssistantRunHistoryResponse = {
  ok: boolean
  requested_url: string
  application_key?: string | null
  limit: number
  run_count: number
  runs: Array<{
    trace_id: string
    captured_at: string
    application_key?: string
    company_name?: string
    job_title?: string
    requested_url?: string
    public_url?: string
    page_url?: string
    schema_source?: string
    run_resume_selection?: {
      selected_resume_variant?: string | null
      selected_resume_path?: string | null
      selected_resume_file?: string | null
      resume_selection_source?: string | null
    } | null
    result_summary: {
      filled_count: number
      skipped_count: number
      error_count: number
      review_queue_count: number
      review_queue_status_counts?: Record<string, number>
      auto_submit_eligible: boolean
      eligible_after_browser_validation: boolean
      submission_requested: boolean
      submission_attempted: boolean
      submitted: boolean
      confirmation_detected: boolean
    }
  }>
}

export type AssistantTraceDetailResponse = {
  ok: boolean
  trace_id: string
  trace: Record<string, unknown>
}

export type AssistantLearningFamily = {
  normalized_label: string
  question_label: string
  question_group?: string | null
  total_answers: number
  recommended_count: number
  matched_count: number
  override_count: number
  manual_only_count: number
  acceptance_rate: number
  top_final_answers: Array<{
    value: string
    count: number
  }>
  top_recommended_answers: Array<{
    value: string
    count: number
  }>
  company_samples: string[]
  last_seen_at?: string | null
  candidate?: {
    type: string
    answer?: string | null
    support: number
    confidence: number
    reason: string
  } | null
}

export type AssistantLearningSummaryResponse = {
  ok: boolean
  requested_url?: string | null
  application_key?: string | null
  summary: {
    session_count: number
    submitted_count: number
    assistant_review_session_count: number
    field_event_count: number
    final_answer_count: number
    matched_recommendation_count: number
    override_count: number
    manual_only_answer_count: number
  }
  top_overrides: AssistantLearningFamily[]
  top_acceptances: AssistantLearningFamily[]
  stable_defaults: AssistantLearningFamily[]
  learning_candidates: AssistantLearningFamily[]
}

export type AssistantQueueItem = {
  queue_id: string
  status: string
  requested_url: string
  application_key?: string | null
  company_name?: string | null
  job_title?: string | null
  fingerprint?: string | null
  first_seen?: string | null
  source?: string | null
  application_status?: string | null
  assistant_last_outcome?: string | null
  assistant_last_source?: string | null
  assistant_last_review_pending_count?: number
  added_by?: string | null
  added_at?: string | null
  updated_at?: string | null
  run_id?: string | null
  last_trace_id?: string | null
  last_error?: string | null
  review_pending_count?: number
  submitted?: boolean
  confirmation_detected?: boolean
  skip_reason?: string | null
  selected_resume_variant?: string | null
  selected_resume_path?: string | null
  resume_selection_source?: string | null
  resume_selection_updated_at?: string | null
}

export type AssistantConfigResponse = {
  ok: boolean
  available_resume_variants: Array<{
    variant: string
    label: string
    path: string
    file: string
  }>
  default_resume_variant?: string | null
}

export type AssistantQueueResponse = {
  ok: boolean
  updated_at?: string | null
  queue_count: number
  active_count: number
  counts: Record<string, number>
  items: AssistantQueueItem[]
}

export type AssistantJobsCandidate = {
  fingerprint?: string | null
  company_name?: string | null
  job_title?: string | null
  requested_url: string
  source?: string | null
  first_seen?: string | null
  posted_at?: string | null
  application_status?: string | null
  assistant_last_outcome?: string | null
  assistant_last_source?: string | null
  assistant_last_review_pending_count?: number
  assistant_last_confirmation_detected?: boolean
  application_key?: string | null
}

export type AssistantJobsBatchResponse = {
  ok: boolean
  mode: string
  limit: number
  skip_applied: boolean
  jobs_db_file?: string
  candidate_count: number
  skipped_count: number
  candidates: AssistantJobsCandidate[]
  skipped: Array<{
    requested_url: string
    company_name?: string | null
    job_title?: string | null
    reason: string
  }>
}

export type AssistantQueueMutationResponse = {
  ok: boolean
  mode?: string
  limit?: number
  skip_applied?: boolean
  candidate_count?: number
  added_count: number
  duplicate_count: number
  skipped_count: number
  added: AssistantQueueItem[]
  duplicates: AssistantQueueItem[]
  skipped: Array<{
    requested_url: string
    company_name?: string | null
    job_title?: string | null
    reason: string
  }>
  queue: AssistantQueueResponse
}

const API_BASE = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.trim() ?? ''
export const DEFAULT_OBSERVER_BASE_URL = 'http://127.0.0.1:8776'
export const OBSERVER_BASE_URL_STORAGE_KEY = 'greenhouseAssistantBackendBaseUrl'

function toURL(path: string): string {
  if (!API_BASE) return path
  const base = API_BASE.endsWith('/') ? API_BASE.slice(0, -1) : API_BASE
  return `${base}${path}`
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const target = toURL(path)
  let response: Response
  try {
    response = await fetch(target, {
      headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
      ...init,
      cache: 'no-store',
    })
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    throw new Error(
      `Load failed for ${path}: ${message}. Check that the dashboard API is running (expected: http://127.0.0.1:8765).`,
    )
  }
  if (!response.ok) {
    let detail = ''
    try {
      const payload = (await response.json()) as { message?: string }
      detail = String(payload?.message || '').trim()
    } catch {
      try {
        detail = (await response.text()).trim()
      } catch {
        detail = ''
      }
    }
    const suffix = detail ? `: ${detail}` : ''
    throw new Error(`Request failed ${response.status} for ${path}${suffix}`)
  }
  try {
    return (await response.json()) as T
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    throw new Error(`Invalid JSON response for ${path}: ${message}`)
  }
}

async function requestAbsoluteJSON<T>(target: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(target, {
      headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
      ...init,
      cache: 'no-store',
    })
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    throw new Error(
      `Load failed for ${target}: ${message}. Check that the observer API is running (expected: http://127.0.0.1:8776).`,
    )
  }
  if (!response.ok) {
    throw new Error(`Request failed ${response.status} for ${target}`)
  }
  try {
    return (await response.json()) as T
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    throw new Error(`Invalid JSON response for ${target}: ${message}`)
  }
}

function buildObserverURL(baseUrl: string, path: string, params?: Record<string, string | number | boolean | undefined>): string {
  const base = String(baseUrl || '').trim().replace(/\/+$/, '')
  const url = new URL(`${base}${path}`)
  Object.entries(params || {}).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    url.searchParams.set(key, String(value))
  })
  return url.toString()
}

export function loadStoredObserverBaseUrl(): string {
  if (typeof window === 'undefined') return DEFAULT_OBSERVER_BASE_URL
  const stored = window.localStorage.getItem(OBSERVER_BASE_URL_STORAGE_KEY)
  return (stored || DEFAULT_OBSERVER_BASE_URL).trim() || DEFAULT_OBSERVER_BASE_URL
}

export const api = {
  getOverview: () => requestJSON<OverviewResponse>('/api/overview'),
  getJobs: (params: { q?: string; company?: string; source?: string; postedWithin?: string; everify?: string; slmModel?: string; sort?: string; limit?: number }) => {
    const query = new URLSearchParams()
    if (params.q) query.set('q', params.q)
    if (params.company) query.set('company', params.company)
    if (params.source) query.set('source', params.source)
    if (params.postedWithin) query.set('posted_within', params.postedWithin)
    if (params.everify) query.set('everify', params.everify)
    if (params.slmModel) query.set('slm_model', params.slmModel)
    if (params.sort) query.set('sort', params.sort)
    if (params.limit) query.set('limit', String(params.limit))
    const suffix = query.toString() ? `?${query.toString()}` : ''
    return requestJSON<JobsResponse>(`/api/jobs${suffix}`)
  },
  getJobsProgress: () => requestJSON<JobsProgressResponse>('/api/jobs-progress'),
  getCrawlSchedule: () => requestJSON<CrawlScheduleResponse>('/api/crawl-schedule'),
  updateCrawlSchedule: (payload: { enabled: boolean; interval_minutes: number }) =>
    requestJSON<CrawlScheduleResponse>('/api/crawl-schedule', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  getMailOverview: () => requestJSON<MailOverviewResponse>('/api/mail/overview'),
  getMailAnalytics: (params: {
    accountId?: number
    provider?: string
    company?: string
  }) => {
    const query = new URLSearchParams()
    if (params.accountId) query.set('account_id', String(params.accountId))
    if (params.provider) query.set('provider', params.provider)
    if (params.company) query.set('company', params.company)
    const suffix = query.toString() ? `?${query.toString()}` : ''
    return requestJSON<MailAnalyticsResponse>(`/api/mail/analytics${suffix}`)
  },
  getMailAccounts: () => requestJSON<{ generated_at: string; accounts: MailAccount[] }>('/api/mail/accounts'),
  getMailMessages: (params: {
    accountId?: number
    provider?: string
    eventType?: string
    triageStatus?: string
    company?: string
    unreadOnly?: boolean
    importantOnly?: boolean
    limit?: number
  }) => {
    const query = new URLSearchParams()
    if (params.accountId) query.set('account_id', String(params.accountId))
    if (params.provider) query.set('provider', params.provider)
    if (params.eventType) query.set('event_type', params.eventType)
    if (params.triageStatus) query.set('triage_status', params.triageStatus)
    if (params.company) query.set('company', params.company)
    if (params.unreadOnly) query.set('unread_only', 'true')
    if (params.importantOnly) query.set('important_only', 'true')
    if (params.limit) query.set('limit', String(params.limit))
    const suffix = query.toString() ? `?${query.toString()}` : ''
    return requestJSON<MailMessagesResponse>(`/api/mail/messages${suffix}`)
  },
  getMailMessageDetail: (id: number) => requestJSON<MailMessageDetailResponse>(`/api/mail/messages/${id}`),
  updateMailTriage: (id: number, payload: { triage_status: string }) =>
    requestJSON<MailTriageUpdateResponse>(`/api/mail/messages/${id}/triage`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  startMailConnect: (provider: 'gmail') =>
    requestJSON<MailConnectResponse>(`/api/mail/accounts/${provider}/connect/start`, {
      method: 'POST',
    }),
  disconnectMailAccount: (provider: 'gmail') =>
    requestJSON<MailAccountActionResponse>(`/api/mail/accounts/${provider}/disconnect`, {
      method: 'POST',
    }),
  triggerMailRun: () =>
    requestJSON<TaskTriggerResponse>('/api/mail/run', {
      method: 'POST',
      body: JSON.stringify({}),
    }),
  getMailRunStatus: () => requestJSON<MailRunStatusResponse>('/api/mail/run-status'),
  getMailSchedule: () => requestJSON<CrawlScheduleResponse>('/api/mail-schedule'),
  updateMailSchedule: (payload: { enabled: boolean; interval_minutes: number }) =>
    requestJSON<CrawlScheduleResponse>('/api/mail-schedule', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  triggerRun: (dryRun: boolean) =>
    requestJSON<TaskTriggerResponse>('/api/run', {
      method: 'POST',
      body: JSON.stringify({ dry_run: dryRun }),
    }),
  updateJobApplicationStatus: (payload: { fingerprint: string; status: string }) =>
    requestJSON<JobApplicationStatusUpdateResponse>('/api/jobs/application-status', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  getObserverRecommendation: (baseUrl: string, url: string) =>
    requestAbsoluteJSON<ObserverRecommendationResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/recommendation', { url }),
    ),
  startAssistantRun: (
    baseUrl: string,
    payload: {
      url: string
      allow_submit: boolean
      headless?: boolean
      resume_variant?: string
      resume_path?: string
      resume_selection_source?: string
    },
  ) =>
    requestAbsoluteJSON<AssistantRunStatusResponse>(buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/start'), {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  getAssistantConfig: (baseUrl: string) =>
    requestAbsoluteJSON<AssistantConfigResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/config'),
    ),
  getAssistantRun: (baseUrl: string, runId: string) =>
    requestAbsoluteJSON<AssistantRunStatusResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/run', { run_id: runId }),
    ),
  getAssistantRuns: (baseUrl: string) =>
    requestAbsoluteJSON<AssistantRunsResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/runs'),
    ),
  getAssistantHistory: (baseUrl: string, url: string, limit = 10) =>
    requestAbsoluteJSON<AssistantRunHistoryResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/history', { url, limit }),
    ),
  getAssistantTrace: (baseUrl: string, traceId: string) =>
    requestAbsoluteJSON<AssistantTraceDetailResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/trace', { trace_id: traceId }),
    ),
  getAssistantLearningSummary: (baseUrl: string, params?: { url?: string; limit?: number }) =>
    requestAbsoluteJSON<AssistantLearningSummaryResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/learning', {
        url: params?.url,
        limit: params?.limit,
      }),
    ),
  getAssistantQueue: (baseUrl: string) =>
    requestAbsoluteJSON<AssistantQueueResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/queue'),
    ),
  getAssistantJobsBatch: (baseUrl: string, params: { mode: string; limit: number; skip_applied?: boolean }) =>
    requestAbsoluteJSON<AssistantJobsBatchResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/jobs', {
        mode: params.mode,
        limit: params.limit,
        skip_applied: params.skip_applied ?? true,
      }),
    ),
  enqueueAssistantJob: (
    baseUrl: string,
    payload: {
      url: string
      company_name?: string
      job_title?: string
      fingerprint?: string
      first_seen?: string
      source?: string
      application_status?: string
      assistant_last_outcome?: string
      assistant_last_source?: string
      assistant_last_review_pending_count?: number
      added_by?: string
      skip_applied?: boolean
      selected_resume_variant?: string
      selected_resume_path?: string
      resume_selection_source?: string
    },
  ) =>
    requestAbsoluteJSON<AssistantQueueMutationResponse>(buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/queue'), {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  enqueueAssistantJobsBatch: (
    baseUrl: string,
    payload: { mode: string; limit: number; skip_applied?: boolean; added_by?: string },
  ) =>
    requestAbsoluteJSON<AssistantQueueMutationResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/queue/batch'),
      {
        method: 'POST',
        body: JSON.stringify(payload),
      },
    ),
  startNextQueuedAssistantRun: (baseUrl: string, payload: { allow_submit: boolean; headless?: boolean }) =>
    requestAbsoluteJSON<AssistantRunStatusResponse>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/queue/start-next'),
      {
        method: 'POST',
        body: JSON.stringify(payload),
      },
    ),
  updateAssistantQueueResume: (
    baseUrl: string,
    payload: {
      queue_id?: string
      url?: string
      selected_resume_variant?: string
      selected_resume_path?: string
      resume_selection_source?: string
    },
  ) =>
    requestAbsoluteJSON<{ ok: boolean; updated: AssistantQueueItem[]; queue: AssistantQueueResponse }>(
      buildObserverURL(baseUrl, '/api/greenhouse-observer/assistant/queue/resume'),
      {
        method: 'POST',
        body: JSON.stringify(payload),
      },
    ),
}
