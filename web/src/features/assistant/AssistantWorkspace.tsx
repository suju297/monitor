import { startTransition, useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  Bot,
  BrainCircuit,
  BriefcaseBusiness,
  ClipboardList,
  Compass,
  DatabaseZap,
  ExternalLink,
  History,
  MailPlus,
  Play,
  Radar,
  RefreshCw,
  Route,
  Sparkles,
} from 'lucide-react'
import {
  api,
  type AssistantConfigResponse,
  type AssistantLearningSummaryResponse,
  DEFAULT_OBSERVER_BASE_URL,
  loadStoredObserverBaseUrl,
  type AssistantJobsBatchResponse,
  type AssistantQueueResponse,
  type AssistantRunHistoryResponse,
  type AssistantRunsResponse,
  type AssistantRunStatusResponse,
  type AssistantTraceDetailResponse,
  type ObserverRecommendationResponse,
} from '@/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DashboardHero,
  DashboardPage,
  EmptyState,
  FieldGroup,
  InlineAlert,
  MetricCard,
  NativeInput,
  NativeSelect,
  SurfaceCard,
  SurfaceCardContent,
  SurfaceCardDescription,
  SurfaceCardHeader,
  SurfaceCardTitle,
  TabButton,
} from '@/components/dashboard'
import { cn } from '@/lib/utils'

type Loadable<T> = {
  loading: boolean
  error: string
  data: T | null
}

function pillBaseClass() {
  return 'inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium'
}

function formatTimestamp(value?: string | null): string {
  if (!value) return '-'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

function reviewQueueFromRun(run: AssistantRunStatusResponse | null): Array<Record<string, unknown>> {
  const analysis = (run?.result?.analysis as Record<string, unknown> | undefined) || {}
  const reviewQueue = analysis.review_queue
  return Array.isArray(reviewQueue) ? (reviewQueue as Array<Record<string, unknown>>) : []
}

function reviewQueueFromTrace(traceDetail: AssistantTraceDetailResponse | null): Array<Record<string, unknown>> {
  const trace = (traceDetail?.trace || {}) as Record<string, unknown>
  const result = (trace.result || {}) as Record<string, unknown>
  const analysis = (result.analysis || {}) as Record<string, unknown>
  const reviewQueue = analysis.review_queue
  return Array.isArray(reviewQueue) ? (reviewQueue as Array<Record<string, unknown>>) : []
}

function blockersFromRun(run: AssistantRunStatusResponse | null): string[] {
  const submitSafety = (run?.result?.submit_safety as Record<string, unknown> | undefined) || {}
  const blockers = submitSafety.blockers
  return Array.isArray(blockers) ? blockers.map((item) => String(item || '').trim()).filter(Boolean) : []
}

function queuePillClass(status: string): string {
  switch ((status || '').trim().toLowerCase()) {
    case 'completed':
      return `${pillBaseClass()} border-emerald-200 bg-emerald-50 text-emerald-700`
    case 'running':
      return `${pillBaseClass()} border-blue-200 bg-blue-50 text-blue-700`
    case 'failed':
      return `${pillBaseClass()} border-rose-200 bg-rose-50 text-rose-700`
    case 'review':
      return `${pillBaseClass()} border-amber-200 bg-amber-50 text-amber-700`
    case 'ready':
      return `${pillBaseClass()} border-teal-200 bg-teal-50 text-teal-700`
    case 'queued':
      return `${pillBaseClass()} border-amber-200 bg-amber-50 text-amber-700`
    default:
      return `${pillBaseClass()} border-slate-200 bg-slate-100 text-slate-600`
  }
}

function batchModeLabel(value: string): string {
  return value === 'recent' ? 'Recent Jobs' : 'Latest Batch'
}

function formatRate(value?: number | null): string {
  if (typeof value !== 'number' || Number.isNaN(value)) return '-'
  return `${Math.round(value * 100)}%`
}

function tableButtonClass(): string {
  return 'inline-flex items-center rounded-full border border-slate-200 bg-white px-2.5 py-1.5 text-[13px] font-medium text-slate-700 transition hover:bg-slate-50'
}

function subtleCopyClass(): string {
  return 'text-[13px] leading-5 text-slate-500'
}

function learningToneClass(tab: 'summary' | 'overrides' | 'defaults' | 'accepted' | 'candidates'): string {
  switch (tab) {
    case 'overrides':
      return `${pillBaseClass()} border-amber-200 bg-amber-50 text-amber-700`
    case 'accepted':
      return `${pillBaseClass()} border-teal-200 bg-teal-50 text-teal-700`
    case 'defaults':
      return `${pillBaseClass()} border-emerald-200 bg-emerald-50 text-emerald-700`
    case 'candidates':
      return `${pillBaseClass()} border-blue-200 bg-blue-50 text-blue-700`
    default:
      return `${pillBaseClass()} border-slate-200 bg-slate-100 text-slate-600`
  }
}

export default function AssistantPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const activeJobUrl = (searchParams.get('url') || '').trim()
  const [backendBaseUrl, setBackendBaseUrl] = useState<string>(() => loadStoredObserverBaseUrl())
  const [jobUrl, setJobUrl] = useState<string>(() => activeJobUrl)
  const [allowSubmit, setAllowSubmit] = useState(false)
  const [currentRunId, setCurrentRunId] = useState('')
  const [assistantConfig, setAssistantConfig] = useState<AssistantConfigResponse | null>(null)
  const [selectedResumeVariant, setSelectedResumeVariant] = useState('')
  const [workspaceTab, setWorkspaceTab] = useState<'runs' | 'queue' | 'candidates'>('runs')
  const [learningTab, setLearningTab] = useState<'summary' | 'overrides' | 'defaults' | 'accepted' | 'candidates'>('summary')
  const [batchMode, setBatchMode] = useState<'latest_batch' | 'recent'>('latest_batch')
  const [batchLimit, setBatchLimit] = useState(20)
  const [queueNotice, setQueueNotice] = useState('')
  const [queueActionPending, setQueueActionPending] = useState(false)
  const [resumeSelectionPending, setResumeSelectionPending] = useState(false)
  const [runState, setRunState] = useState<Loadable<AssistantRunStatusResponse>>({
    loading: false,
    error: '',
    data: null,
  })
  const [recommendationState, setRecommendationState] = useState<Loadable<ObserverRecommendationResponse>>({
    loading: false,
    error: '',
    data: null,
  })
  const [historyState, setHistoryState] = useState<Loadable<AssistantRunHistoryResponse>>({
    loading: false,
    error: '',
    data: null,
  })
  const [runsState, setRunsState] = useState<Loadable<AssistantRunsResponse>>({
    loading: false,
    error: '',
    data: null,
  })
  const [traceState, setTraceState] = useState<Loadable<AssistantTraceDetailResponse>>({
    loading: false,
    error: '',
    data: null,
  })
  const [queueState, setQueueState] = useState<Loadable<AssistantQueueResponse>>({
    loading: false,
    error: '',
    data: null,
  })
  const [learningState, setLearningState] = useState<Loadable<AssistantLearningSummaryResponse>>({
    loading: false,
    error: '',
    data: null,
  })
  const [jobsBatchState, setJobsBatchState] = useState<Loadable<AssistantJobsBatchResponse>>({
    loading: false,
    error: '',
    data: null,
  })

  useEffect(() => {
    window.localStorage.setItem('greenhouseAssistantBackendBaseUrl', backendBaseUrl)
  }, [backendBaseUrl])

  useEffect(() => {
    if (activeJobUrl !== jobUrl) {
      setJobUrl(activeJobUrl)
    }
  }, [activeJobUrl, jobUrl])

  const loadRecommendationAndHistory = useCallback(
    async (nextUrl: string) => {
      const requestedUrl = nextUrl.trim()
      if (!requestedUrl) {
        setRecommendationState({ loading: false, error: '', data: null })
        setHistoryState({ loading: false, error: '', data: null })
        return
      }
      setRecommendationState((previous) => ({ ...previous, loading: true, error: '' }))
      setHistoryState((previous) => ({ ...previous, loading: true, error: '' }))
      try {
        const [recommendation, history] = await Promise.all([
          api.getObserverRecommendation(backendBaseUrl, requestedUrl),
          api.getAssistantHistory(backendBaseUrl, requestedUrl, 10),
        ])
        startTransition(() => {
          setRecommendationState({ loading: false, error: '', data: recommendation })
          setHistoryState({ loading: false, error: '', data: history })
        })
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        startTransition(() => {
          setRecommendationState({ loading: false, error: message, data: null })
          setHistoryState({ loading: false, error: message, data: null })
        })
      }
    },
    [backendBaseUrl],
  )

  const loadRun = useCallback(
    async (runId: string) => {
      if (!runId) return
      setRunState((previous) => ({ ...previous, loading: true, error: '' }))
      try {
        const payload = await api.getAssistantRun(backendBaseUrl, runId)
        startTransition(() => {
          setRunState({ loading: false, error: '', data: payload })
        })
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        startTransition(() => {
          setRunState({ loading: false, error: message, data: null })
        })
      }
    },
    [backendBaseUrl],
  )

  const loadRuns = useCallback(async () => {
    setRunsState((previous) => ({ ...previous, loading: true, error: '' }))
    try {
      const payload = await api.getAssistantRuns(backendBaseUrl)
      startTransition(() => {
        setRunsState({ loading: false, error: '', data: payload })
      })
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      startTransition(() => {
        setRunsState({ loading: false, error: message, data: null })
      })
    }
  }, [backendBaseUrl])

  const loadTrace = useCallback(
    async (traceId: string) => {
      if (!traceId) return
      setTraceState((previous) => ({ ...previous, loading: true, error: '' }))
      try {
        const payload = await api.getAssistantTrace(backendBaseUrl, traceId)
        startTransition(() => {
          setTraceState({ loading: false, error: '', data: payload })
        })
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        startTransition(() => {
          setTraceState({ loading: false, error: message, data: null })
        })
      }
    },
    [backendBaseUrl],
  )

  const loadQueue = useCallback(async () => {
    setQueueState((previous) => ({ ...previous, loading: true, error: '' }))
    try {
      const payload = await api.getAssistantQueue(backendBaseUrl)
      startTransition(() => {
        setQueueState({ loading: false, error: '', data: payload })
      })
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      startTransition(() => {
        setQueueState({ loading: false, error: message, data: null })
      })
    }
  }, [backendBaseUrl])

  const loadLearningSummary = useCallback(async () => {
    setLearningState((previous) => ({ ...previous, loading: true, error: '' }))
    try {
      const payload = await api.getAssistantLearningSummary(backendBaseUrl, { limit: 6 })
      startTransition(() => {
        setLearningState({ loading: false, error: '', data: payload })
      })
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      startTransition(() => {
        setLearningState({ loading: false, error: message, data: null })
      })
    }
  }, [backendBaseUrl])

  const loadJobsBatch = useCallback(
    async (nextMode = batchMode, nextLimit = batchLimit) => {
      setJobsBatchState((previous) => ({ ...previous, loading: true, error: '' }))
      try {
        const payload = await api.getAssistantJobsBatch(backendBaseUrl, {
          mode: nextMode,
          limit: nextLimit,
          skip_applied: true,
        })
        startTransition(() => {
          setJobsBatchState({ loading: false, error: '', data: payload })
        })
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        startTransition(() => {
          setJobsBatchState({ loading: false, error: message, data: null })
        })
      }
    },
    [backendBaseUrl, batchLimit, batchMode],
  )

  const loadAssistantConfig = useCallback(async () => {
    try {
      const payload = await api.getAssistantConfig(backendBaseUrl)
      startTransition(() => {
        setAssistantConfig(payload)
      })
    } catch {
      startTransition(() => {
        setAssistantConfig(null)
      })
    }
  }, [backendBaseUrl])

  const recommendation = recommendationState.data
  const history = historyState.data
  const queue = queueState.data
  const learning = learningState.data
  const jobsBatch = jobsBatchState.data
  const run = runState.data
  const runs = runsState.data?.runs || []
  const historyRuns = history?.runs || []
  const currentReviewQueue = useMemo(() => reviewQueueFromRun(run), [run])
  const selectedReviewQueue = useMemo(() => reviewQueueFromTrace(traceState.data), [traceState.data])
  const blockers = useMemo(() => blockersFromRun(run), [run])
  const queuedItems = useMemo(() => queue?.items || [], [queue?.items])
  const currentQueueItem = useMemo(
    () => queuedItems.find((item) => item.requested_url === jobUrl.trim()) || null,
    [jobUrl, queuedItems],
  )

  const applyJobContext = useCallback(async () => {
    const trimmedUrl = jobUrl.trim()
    setSearchParams(trimmedUrl ? { url: trimmedUrl } : {})
    await loadRecommendationAndHistory(trimmedUrl)
  }, [jobUrl, loadRecommendationAndHistory, setSearchParams])

  const startAssistant = useCallback(async () => {
    const trimmedUrl = jobUrl.trim()
    if (!trimmedUrl) {
      setRunState({ loading: false, error: 'Enter a Greenhouse job URL or start the next queued job.', data: null })
      return
    }
    setSearchParams({ url: trimmedUrl })
    setRunState((previous) => ({ ...previous, loading: true, error: '' }))
    try {
      const baselineResumeVariant =
        currentQueueItem?.selected_resume_variant || recommendation?.recommended_resume_variant || ''
      const shouldLockResume = Boolean(currentQueueItem || (selectedResumeVariant && selectedResumeVariant !== baselineResumeVariant))
      const payload = await api.startAssistantRun(backendBaseUrl, {
        url: trimmedUrl,
        allow_submit: allowSubmit,
        headless: false,
        resume_variant: shouldLockResume ? selectedResumeVariant || undefined : undefined,
        resume_path: shouldLockResume
          ? assistantConfig?.available_resume_variants.find((option) => option.variant === selectedResumeVariant)?.path || undefined
          : undefined,
        resume_selection_source: shouldLockResume
          ? currentQueueItem?.resume_selection_source || (selectedResumeVariant !== recommendation?.recommended_resume_variant ? 'manual_override' : recommendation?.resume_selection_source)
          : undefined,
      })
      startTransition(() => {
        setCurrentRunId(payload.run_id)
        setRunState({ loading: false, error: '', data: payload })
      })
      void loadRecommendationAndHistory(trimmedUrl)
      void loadQueue()
      void loadRuns()
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      startTransition(() => {
        setRunState({ loading: false, error: message, data: null })
      })
    }
  }, [allowSubmit, assistantConfig?.available_resume_variants, backendBaseUrl, currentQueueItem, jobUrl, loadQueue, loadRecommendationAndHistory, loadRuns, recommendation?.recommended_resume_variant, recommendation?.resume_selection_source, selectedResumeVariant, setSearchParams])

  const enqueueCurrentUrl = useCallback(async () => {
    const trimmedUrl = jobUrl.trim()
    if (!trimmedUrl) {
      setQueueNotice('Enter a Greenhouse URL first.')
      return
    }
    setQueueActionPending(true)
    setQueueNotice('')
    try {
      const baselineResumeVariant =
        currentQueueItem?.selected_resume_variant || recommendation?.recommended_resume_variant || ''
      const shouldLockResume = Boolean(currentQueueItem || (selectedResumeVariant && selectedResumeVariant !== baselineResumeVariant))
      const payload = await api.enqueueAssistantJob(backendBaseUrl, {
        url: trimmedUrl,
        added_by: 'assistant_page_url',
        skip_applied: true,
        selected_resume_variant: shouldLockResume ? selectedResumeVariant || undefined : undefined,
        selected_resume_path: shouldLockResume
          ? assistantConfig?.available_resume_variants.find((option) => option.variant === selectedResumeVariant)?.path || undefined
          : undefined,
        resume_selection_source: shouldLockResume
          ? currentQueueItem?.resume_selection_source || (selectedResumeVariant !== recommendation?.recommended_resume_variant ? 'manual_override' : recommendation?.resume_selection_source)
          : undefined,
      })
      const added = payload.added_count
      const duplicates = payload.duplicate_count
      const skipped = payload.skipped_count
      startTransition(() => {
        setQueueNotice(
          added ? `Queued ${added} job.` : duplicates ? 'That job is already in the assistant queue.' : skipped ? 'That job was skipped.' : 'No queue changes.',
        )
        setQueueState({ loading: false, error: '', data: payload.queue })
      })
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      startTransition(() => {
        setQueueNotice(message)
      })
    } finally {
      setQueueActionPending(false)
    }
  }, [assistantConfig?.available_resume_variants, backendBaseUrl, currentQueueItem, jobUrl, recommendation?.recommended_resume_variant, recommendation?.resume_selection_source, selectedResumeVariant])

  const enqueueJobsBatch = useCallback(async () => {
    setQueueActionPending(true)
    setQueueNotice('')
    try {
      const payload = await api.enqueueAssistantJobsBatch(backendBaseUrl, {
        mode: batchMode,
        limit: batchLimit,
        skip_applied: true,
        added_by: 'assistant_page_batch',
      })
      startTransition(() => {
        setQueueNotice(
          payload.added_count
            ? `Queued ${payload.added_count} ${payload.added_count === 1 ? 'job' : 'jobs'} from ${batchModeLabel(batchMode).toLowerCase()}.`
            : payload.duplicate_count
              ? 'Everything in that selection is already queued.'
              : 'No new jobs were added from the scraped batch.',
        )
        setQueueState({ loading: false, error: '', data: payload.queue })
      })
      await loadJobsBatch(batchMode, batchLimit)
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      startTransition(() => {
        setQueueNotice(message)
      })
    } finally {
      setQueueActionPending(false)
    }
  }, [backendBaseUrl, batchLimit, batchMode, loadJobsBatch])

  const startNextQueuedAssistant = useCallback(async () => {
    setQueueActionPending(true)
    setQueueNotice('')
    setRunState((previous) => ({ ...previous, loading: true, error: '' }))
    try {
      const payload = await api.startNextQueuedAssistantRun(backendBaseUrl, {
        allow_submit: allowSubmit,
        headless: false,
      })
      startTransition(() => {
        setCurrentRunId(payload.run_id)
        setRunState({ loading: false, error: '', data: payload })
        setJobUrl(payload.requested_url)
      })
      setSearchParams(payload.requested_url ? { url: payload.requested_url } : {})
      await Promise.all([loadRecommendationAndHistory(payload.requested_url), loadQueue(), loadJobsBatch(batchMode, batchLimit), loadRuns()])
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      startTransition(() => {
        setRunState({ loading: false, error: message, data: null })
        setQueueNotice(message)
      })
    } finally {
      setQueueActionPending(false)
    }
  }, [allowSubmit, backendBaseUrl, batchLimit, batchMode, loadJobsBatch, loadQueue, loadRecommendationAndHistory, loadRuns, setSearchParams])

  const updateQueuedResumeSelection = useCallback(
    async (queueId: string, requestedUrl: string, nextVariant: string) => {
      setResumeSelectionPending(true)
      try {
        const nextPath =
          assistantConfig?.available_resume_variants.find((option) => option.variant === nextVariant)?.path || ''
        const payload = await api.updateAssistantQueueResume(backendBaseUrl, {
          queue_id: queueId,
          url: requestedUrl,
          selected_resume_variant: nextVariant,
          selected_resume_path: nextPath || undefined,
          resume_selection_source: 'manual_override',
        })
        startTransition(() => {
          setSelectedResumeVariant(nextVariant)
          setQueueState({ loading: false, error: '', data: payload.queue })
          setQueueNotice('Updated queued resume selection.')
        })
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        startTransition(() => {
          setQueueNotice(message)
        })
      } finally {
        setResumeSelectionPending(false)
      }
    },
    [assistantConfig?.available_resume_variants, backendBaseUrl],
  )

  useEffect(() => {
    if (!activeJobUrl) return
    void loadRecommendationAndHistory(activeJobUrl)
  }, [activeJobUrl, loadRecommendationAndHistory])

  useEffect(() => {
    void loadQueue()
    void loadJobsBatch(batchMode, batchLimit)
    void loadRuns()
    void loadLearningSummary()
    void loadAssistantConfig()
  }, [backendBaseUrl, batchLimit, batchMode, loadAssistantConfig, loadJobsBatch, loadLearningSummary, loadQueue, loadRuns])

  useEffect(() => {
    const status = runState.data?.status || ''
    if (!currentRunId || !['queued', 'running'].includes(status)) return
    const timer = window.setInterval(() => {
      startTransition(() => {
        void loadRun(currentRunId)
      })
    }, 2000)
    return () => window.clearInterval(timer)
  }, [currentRunId, loadRun, runState.data?.status])

  useEffect(() => {
    const hasActiveRuns = Boolean(
      runsState.data?.runs?.some((item) => ['queued', 'running'].includes(String(item.status || '').trim().toLowerCase())),
    )
    if (!hasActiveRuns) return
    const timer = window.setInterval(() => {
      startTransition(() => {
        void loadRuns()
      })
    }, 2000)
    return () => window.clearInterval(timer)
  }, [loadRuns, runsState.data?.runs])

  useEffect(() => {
    const status = runState.data?.status || ''
    if (!['completed', 'failed'].includes(status)) return
    const traceId = runState.data?.status === 'completed' ? runState.data?.result_summary?.trace_id || '' : ''
    if (traceId) {
      void loadTrace(traceId)
    }
    void loadQueue()
    void loadJobsBatch(batchMode, batchLimit)
    void loadRuns()
    void loadLearningSummary()
  }, [batchLimit, batchMode, loadJobsBatch, loadLearningSummary, loadQueue, loadRuns, loadTrace, runState.data?.result_summary?.trace_id, runState.data?.status])

  const traceRecord = (traceState.data?.trace || null) as Record<string, unknown> | null
  const focusCompany =
    (run?.result_summary?.company_name || recommendation?.company_name || String(traceRecord?.company_name || '')).trim() || '-'
  const focusJobTitle =
    (run?.result_summary?.job_title || recommendation?.job_title || String(traceRecord?.job_title || '')).trim() || '-'
  const hasFocusState = Boolean(jobUrl.trim() || recommendation || run || historyRuns.length || traceState.data)
  const learningItems = (() => {
    switch (learningTab) {
      case 'overrides':
        return learning?.top_overrides || []
      case 'defaults':
        return learning?.stable_defaults || []
      case 'accepted':
        return learning?.top_acceptances || []
      case 'candidates':
        return learning?.learning_candidates || []
      default:
        return []
    }
  })()

  useEffect(() => {
    const nextVariant =
      currentQueueItem?.selected_resume_variant ||
      recommendation?.recommended_resume_variant ||
      assistantConfig?.default_resume_variant ||
      assistantConfig?.available_resume_variants?.[0]?.variant ||
      ''
    setSelectedResumeVariant((current) => (current === nextVariant ? current : nextVariant))
  }, [
    assistantConfig?.available_resume_variants,
    assistantConfig?.default_resume_variant,
    currentQueueItem?.selected_resume_variant,
    recommendation?.recommended_resume_variant,
  ])

  return (
    <DashboardPage>
      <DashboardHero
        eyebrow="Assistant Ops"
        icon={Bot}
        title="Assistant Traceability"
        subtitle="Queue hosted Greenhouse jobs, launch headed assistant runs, inspect review blockers, and keep the learning set visible without dropping into raw JSON traces."
        actions={
          <>
            <Button asChild variant="outline" className="rounded-full">
              <Link to="/jobs">
                <BriefcaseBusiness className="size-4" />
                Jobs Dashboard
              </Link>
            </Button>
            <Button asChild variant="outline" className="rounded-full">
              <Link to="/monitor">
                <Radar className="size-4" />
                Crawler Monitor
              </Link>
            </Button>
            <Button asChild variant="outline" className="rounded-full">
              <Link to="/mail">
                <MailPlus className="size-4" />
                Mail Analytics
              </Link>
            </Button>
          </>
        }
      >
        <Badge variant="outline" className="max-w-full rounded-full border-slate-200 bg-white/90 px-3 py-1 text-slate-600">
          Observer API: {backendBaseUrl}
        </Badge>
        <Badge variant="outline" className="rounded-full border-blue-200 bg-blue-50 px-3 py-1 text-blue-700">
          {queue?.queue_count ?? 0} queued
        </Badge>
        <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
          {runs.length} tracked runs
        </Badge>
      </DashboardHero>

      <SurfaceCard>
        <SurfaceCardHeader>
          <SurfaceCardTitle>Assistant Controls</SurfaceCardTitle>
          <SurfaceCardDescription>Point the assistant at a Greenhouse role, choose the scraped jobs batch to work from, and control whether submission is allowed.</SurfaceCardDescription>
        </SurfaceCardHeader>
        <SurfaceCardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-[minmax(0,1.3fr)_minmax(0,2.2fr)_minmax(0,1fr)_minmax(0,0.8fr)_minmax(0,1.2fr)]">
            <FieldGroup label="Observer API">
              <NativeInput value={backendBaseUrl} onChange={(event) => setBackendBaseUrl(event.target.value)} placeholder={DEFAULT_OBSERVER_BASE_URL} />
            </FieldGroup>
            <FieldGroup label="Greenhouse Job URL">
              <NativeInput
                value={jobUrl}
                onChange={(event) => setJobUrl(event.target.value)}
                placeholder="https://job-boards.greenhouse.io/company/jobs/12345"
              />
            </FieldGroup>
            <FieldGroup label="Scraped Jobs Mode">
              <NativeSelect value={batchMode} onChange={(event) => setBatchMode(event.target.value as 'latest_batch' | 'recent')}>
                <option value="latest_batch">Latest Batch</option>
                <option value="recent">Recent Jobs</option>
              </NativeSelect>
            </FieldGroup>
            <FieldGroup label="Batch Limit">
              <NativeInput
                type="number"
                min={1}
                max={100}
                value={batchLimit}
                onChange={(event) => setBatchLimit(Math.max(1, Math.min(100, Number(event.target.value) || 1)))}
              />
            </FieldGroup>
            <div className="flex items-end">
              <label className="flex h-10 items-center gap-3 rounded-xl border border-slate-200 bg-white/90 px-3.5 shadow-xs">
                <input type="checkbox" checked={allowSubmit} onChange={(event) => setAllowSubmit(event.target.checked)} className="size-4 accent-slate-950" />
                <span className="text-sm font-medium text-slate-700">Allow submit when eligible</span>
              </label>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            <Button variant="outline" className="rounded-full" onClick={() => void applyJobContext()} disabled={recommendationState.loading || historyState.loading}>
              <RefreshCw className="size-4" />
              Refresh Context
            </Button>
            <Button variant="outline" className="rounded-full" onClick={() => void enqueueCurrentUrl()} disabled={queueActionPending}>
              <ClipboardList className="size-4" />
              Queue URL
            </Button>
            <Button variant="outline" className="rounded-full" onClick={() => void enqueueJobsBatch()} disabled={queueActionPending}>
              <Compass className="size-4" />
              Queue {batchModeLabel(batchMode)}
            </Button>
            <Button className="rounded-full" onClick={() => void startAssistant()} disabled={runState.loading}>
              <Play className="size-4" />
              Start Assistant
            </Button>
            <Button className="rounded-full" onClick={() => void startNextQueuedAssistant()} disabled={queueActionPending || runState.loading}>
              <Route className="size-4" />
              Start Next Queued
            </Button>
          </div>

          {queueNotice ? (
            <div className="rounded-xl border border-teal-200 bg-teal-50 px-3.5 py-2.5 text-sm leading-5 text-teal-700">{queueNotice}</div>
          ) : null}
        </SurfaceCardContent>
      </SurfaceCard>

      <div className="grid gap-4 xl:grid-cols-[1.1fr_0.9fr]">
        <SurfaceCard>
          <SurfaceCardHeader className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
            <div>
              <SurfaceCardTitle>Current Job</SurfaceCardTitle>
              <SurfaceCardDescription>Inspect the selected role, recommended resume, live run state, review queue, and stored trace history.</SurfaceCardDescription>
            </div>
            {hasFocusState ? (
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline" className="rounded-full border-teal-200 bg-teal-50 px-3 py-1 text-teal-700">
                  {focusCompany}
                </Badge>
                <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                  {focusJobTitle}
                </Badge>
                {jobUrl ? (
                  <Button asChild size="sm" variant="outline" className="rounded-full">
                    <a href={jobUrl} target="_blank" rel="noreferrer">
                      <ExternalLink className="size-4" />
                      Open Job
                    </a>
                  </Button>
                ) : null}
              </div>
            ) : null}
          </SurfaceCardHeader>
          <SurfaceCardContent className="space-y-3.5">
            {recommendationState.error ? <InlineAlert>{recommendationState.error}</InlineAlert> : null}
            {runState.error ? <InlineAlert>{runState.error}</InlineAlert> : null}
            {historyState.error ? <InlineAlert>{historyState.error}</InlineAlert> : null}
            {traceState.error ? <InlineAlert>{traceState.error}</InlineAlert> : null}

            {!hasFocusState ? (
              <EmptyState
                title="Pick a job to inspect"
                description="Start from a queued item, choose a scraped candidate, or paste a hosted Greenhouse URL above. Recommendation, run state, review blockers, and trace history will appear here."
              />
            ) : (
              <div className="space-y-4">
                <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                  <MetricCard
                    label="Recommendation"
                    value={recommendation ? 'Ready' : recommendationState.loading ? 'Loading' : 'Pending'}
                    detail={
                      recommendation
                        ? `${recommendation.recommended_resume_variant || '-'} · ${recommendation.recommended_resume_file || '-'}`
                        : 'Load a job context to see resume guidance.'
                    }
                    icon={Sparkles}
                    tone="teal"
                  />
                  <MetricCard
                    label="Live Run"
                    value={run ? run.status : 'Idle'}
                    detail={run?.result_summary ? `${run.result_summary.filled_count} filled · ${run.result_summary.review_pending_count} review` : 'No active run selected.'}
                    icon={Bot}
                    tone="blue"
                  />
                  <MetricCard
                    label="Review Queue"
                    value={currentReviewQueue.length}
                    detail={currentReviewQueue.length ? 'Items from the currently selected run.' : 'Nothing pending in the active run.'}
                    icon={ClipboardList}
                    tone="amber"
                  />
                  <MetricCard
                    label="Recent Runs"
                    value={historyRuns.length}
                    detail={historyRuns.length ? 'Stored traces for this job URL.' : 'No stored traces for this job yet.'}
                    icon={History}
                    tone="slate"
                  />
                </div>

                {recommendation ? (
                  <div className="rounded-[26px] border border-slate-200 bg-slate-50/70 p-5">
                    <div className="mb-4 flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                      <div>
                        <div className="text-lg font-semibold text-slate-950">Recommendation</div>
                        <div className={subtleCopyClass()}>Resume choice, decision buckets, and review posture for the currently selected job.</div>
                      </div>
                      <Badge
                        variant="outline"
                        className={cn(
                          'rounded-full px-3 py-1',
                          recommendation.auto_submit_eligible
                            ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                            : 'border-amber-200 bg-amber-50 text-amber-700',
                        )}
                      >
                        {recommendation.auto_submit_eligible ? 'Auto-submit ready' : 'Needs review'}
                      </Badge>
                    </div>
                    {assistantConfig?.available_resume_variants?.length ? (
                      <div className="mb-4 max-w-sm">
                        <FieldGroup label="Resume for this job">
                          <NativeSelect value={selectedResumeVariant} onChange={(event) => setSelectedResumeVariant(event.target.value)}>
                            {assistantConfig.available_resume_variants.map((option) => (
                              <option key={option.variant} value={option.variant}>
                                {option.label}
                              </option>
                            ))}
                          </NativeSelect>
                        </FieldGroup>
                      </div>
                    ) : null}
                    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                        Recommended variant: {recommendation.recommended_resume_variant || '-'}
                      </div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                        Selected variant: {selectedResumeVariant || recommendation.recommended_resume_variant || '-'}
                      </div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                        Resume file: {recommendation.recommended_resume_file || '-'}
                      </div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                        Recommendation source: {recommendation.resume_selection_source || '-'}
                      </div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                        Queued review items: {(recommendation.review_queue || []).length}
                      </div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                        Decision buckets: {Object.entries(recommendation.decision_counts || {}).map(([key, value]) => `${key} ${value}`).join(' · ') || '-'}
                      </div>
                    </div>
                    {recommendation.recommended_resume_reason ? (
                      <div className="mt-4 text-sm leading-6 text-slate-600">
                        {recommendation.recommended_resume_reason}
                        {typeof recommendation.recommended_resume_confidence === 'number'
                          ? ` (${formatRate(recommendation.recommended_resume_confidence)})`
                          : ''}
                      </div>
                    ) : null}
                  </div>
                ) : null}

                {run ? (
                  <div className="rounded-[26px] border border-slate-200 bg-slate-50/70 p-5">
                    <div className="mb-4 flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                      <div>
                        <div className="text-lg font-semibold text-slate-950">Live Run</div>
                        <div className={subtleCopyClass()}>Run-level timing, resume lock state, submit posture, and any blockers captured during execution.</div>
                      </div>
                      <div className={queuePillClass(run.status)}>{run.status}</div>
                    </div>
                    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">Created: {formatTimestamp(run.created_at)}</div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">Started: {formatTimestamp(run.started_at || undefined)}</div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">Finished: {formatTimestamp(run.finished_at || undefined)}</div>
                      <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">Submit enabled: {run.allow_submit ? 'yes' : 'no'}</div>
                      {run.result_summary?.selected_resume_variant ? (
                        <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                          Locked resume: {run.result_summary.selected_resume_variant}
                        </div>
                      ) : null}
                      {run.result_summary?.selected_resume_file ? (
                        <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                          Resume file: {run.result_summary.selected_resume_file}
                        </div>
                      ) : null}
                      {run.result_summary ? (
                        <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">Filled: {run.result_summary.filled_count}</div>
                      ) : null}
                      {run.result_summary ? (
                        <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">Skipped: {run.result_summary.skipped_count}</div>
                      ) : null}
                      {run.result_summary ? (
                        <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">
                          Review pending: {run.result_summary.review_pending_count}
                        </div>
                      ) : null}
                      {run.result_summary ? (
                        <div className="rounded-2xl border border-white/70 bg-white/80 px-4 py-3 text-sm text-slate-600">Submitted: {run.result_summary.submitted ? 'Yes' : 'No'}</div>
                      ) : null}
                      {run.result_summary?.challenge_detected ? (
                        <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700">Challenge detected: Yes</div>
                      ) : null}
                    </div>
                    {blockers.length ? (
                      <div className="mt-4 space-y-3">
                        <div className="text-sm font-semibold tracking-[0.18em] text-slate-500 uppercase">Blockers</div>
                        {blockers.map((item) => (
                          <div key={item} className="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-700">
                            {item}
                          </div>
                        ))}
                      </div>
                    ) : null}
                  </div>
                ) : null}

                {currentReviewQueue.length ? (
                  <div className="rounded-[26px] border border-slate-200 bg-slate-50/70 p-5">
                    <div className="mb-4 text-lg font-semibold text-slate-950">Current Review Queue</div>
                    <div className="space-y-3">
                      {currentReviewQueue.map((item, index) => (
                        <div key={`${String(item.question_label || index)}-${index}`} className="rounded-2xl border border-white/70 bg-white/80 px-4 py-4">
                          <div className="mb-2 flex flex-wrap items-center gap-2">
                            <span className={String(item.status || '') === 'answered' ? `${pillBaseClass()} border-emerald-200 bg-emerald-50 text-emerald-700` : `${pillBaseClass()} border-amber-200 bg-amber-50 text-amber-700`}>
                              {String(item.status || '-')}
                            </span>
                            <b className="text-slate-900">{String(item.question_label || '-')}</b>
                          </div>
                          <div className={subtleCopyClass()}>{String(item.reason || '-')}</div>
                        </div>
                      ))}
                    </div>
                  </div>
                ) : null}

                {(historyRuns.length || traceState.data) ? (
                  <div className="rounded-[26px] border border-slate-200 bg-slate-50/70 p-5">
                    <div className="mb-4 text-lg font-semibold text-slate-950">Run History</div>
                    {historyRuns.length ? (
                      <div className="space-y-3">
                        {historyRuns.slice(0, 4).map((item) => (
                          <button
                            key={item.trace_id}
                            type="button"
                            className="flex w-full flex-col gap-2 rounded-2xl border border-white/70 bg-white/80 px-4 py-4 text-left transition hover:border-slate-300"
                            onClick={() => void loadTrace(item.trace_id)}
                          >
                            <div className="flex flex-wrap items-center gap-2">
                              <span
                                className={
                                  item.result_summary.submitted
                                    ? `${pillBaseClass()} border-emerald-200 bg-emerald-50 text-emerald-700`
                                    : item.result_summary.review_queue_count
                                      ? `${pillBaseClass()} border-amber-200 bg-amber-50 text-amber-700`
                                      : `${pillBaseClass()} border-slate-200 bg-slate-100 text-slate-600`
                                }
                              >
                                {item.result_summary.submitted ? 'Submitted' : item.result_summary.review_queue_count ? 'Review' : 'Observed'}
                              </span>
                              <b className="text-slate-900">{formatTimestamp(item.captured_at)}</b>
                            </div>
                            <div className={subtleCopyClass()}>
                              filled {item.result_summary.filled_count} · review {item.result_summary.review_queue_count}
                            </div>
                          </button>
                        ))}
                      </div>
                    ) : null}
                    {traceState.loading ? <div className="mt-4 text-sm text-slate-500">Loading trace detail...</div> : null}
                    {traceState.data ? (
                      <div className="mt-4 space-y-4 rounded-2xl border border-white/70 bg-white/80 p-4">
                        <div className="grid gap-3 md:grid-cols-2">
                          <div className="text-sm text-slate-600">Trace ID: {traceState.data.trace_id}</div>
                          <div className="text-sm text-slate-600">Captured: {formatTimestamp(String(traceRecord?.captured_at || ''))}</div>
                        </div>
                        {selectedReviewQueue.length ? (
                          <div className="space-y-3">
                            {selectedReviewQueue.slice(0, 4).map((item, index) => (
                              <div key={`${String(item.question_label || index)}-${index}`} className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-4">
                                <div className="mb-2 flex flex-wrap items-center gap-2">
                                  <span className={String(item.status || '') === 'answered' ? `${pillBaseClass()} border-emerald-200 bg-emerald-50 text-emerald-700` : `${pillBaseClass()} border-amber-200 bg-amber-50 text-amber-700`}>
                                    {String(item.status || '-')}
                                  </span>
                                  <b className="text-slate-900">{String(item.question_label || '-')}</b>
                                </div>
                                <div className={subtleCopyClass()}>{String(item.reason || '-')}</div>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <div className="text-sm text-slate-500">No stored review items in the selected trace.</div>
                        )}
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>
            )}
          </SurfaceCardContent>
        </SurfaceCard>

        <SurfaceCard>
          <SurfaceCardHeader className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
            <div>
              <SurfaceCardTitle>Workspace</SurfaceCardTitle>
              <SurfaceCardDescription>Switch between active processes, queued jobs, and scraped candidates instead of stacking all three at once.</SurfaceCardDescription>
            </div>
            <div className="flex flex-wrap items-center gap-2" role="tablist" aria-label="Workspace sections">
              <TabButton active={workspaceTab === 'runs'} onClick={() => setWorkspaceTab('runs')}>
                Processes
              </TabButton>
              <TabButton active={workspaceTab === 'queue'} onClick={() => setWorkspaceTab('queue')}>
                Queue
              </TabButton>
              <TabButton active={workspaceTab === 'candidates'} onClick={() => setWorkspaceTab('candidates')}>
                Scraped Jobs
              </TabButton>
            </div>
          </SurfaceCardHeader>
          <SurfaceCardContent className="space-y-3.5">
            {workspaceTab === 'runs' ? (
              <>
                {runsState.error ? <InlineAlert>{runsState.error}</InlineAlert> : null}
                <div className="overflow-hidden rounded-[22px] border border-slate-200/80">
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-left text-sm">
                      <thead className="bg-slate-50/80">
                        <tr>
                          <th className="px-3 py-2.5 font-medium text-slate-500">Status</th>
                          <th className="px-3 py-2.5 font-medium text-slate-500">Job</th>
                          <th className="px-3 py-2.5 font-medium text-slate-500">Started</th>
                          <th className="px-3 py-2.5 font-medium text-slate-500">Review</th>
                          <th className="px-3 py-2.5 font-medium text-slate-500">Submitted</th>
                          <th className="px-3 py-2.5 font-medium text-slate-500">Open</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-slate-100">
                        {runs.map((item) => (
                          <tr key={item.run_id} className="bg-white/70">
                            <td className="px-3 py-3">
                              <span className={queuePillClass(item.status)}>{item.status}</span>
                            </td>
                            <td className="px-3 py-3 font-medium text-slate-900">{item.result_summary?.job_title || item.requested_url || '-'}</td>
                            <td className="px-3 py-3 text-slate-600">{formatTimestamp(item.started_at || item.created_at)}</td>
                            <td className="px-3 py-3 text-slate-600">{item.result_summary?.review_pending_count ?? '-'}</td>
                            <td className="px-3 py-3 text-slate-600">{item.result_summary?.submitted ? 'Yes' : 'No'}</td>
                            <td className="px-3 py-3">
                              <button
                                type="button"
                                className={tableButtonClass()}
                                onClick={() => {
                                  setCurrentRunId(item.run_id)
                                  setRunState({ loading: false, error: '', data: item })
                                  if (item.requested_url) {
                                    setJobUrl(item.requested_url)
                                    setSearchParams({ url: item.requested_url })
                                    void loadRecommendationAndHistory(item.requested_url)
                                  }
                                }}
                              >
                                Inspect
                              </button>
                            </td>
                          </tr>
                        ))}
                        {!runs.length ? (
                          <tr>
                            <td colSpan={6} className="px-3 py-8 text-center text-slate-500">
                              {runsState.loading ? 'Loading assistant processes...' : 'No assistant processes have been started in this server session yet.'}
                            </td>
                          </tr>
                        ) : null}
                      </tbody>
                    </table>
                  </div>
                </div>
              </>
            ) : null}

            {workspaceTab === 'queue' ? (
              <>
                {queueState.error ? <InlineAlert>{queueState.error}</InlineAlert> : null}
                {queueState.loading ? (
                  <div className="text-sm text-slate-500">Loading assistant queue...</div>
                ) : queue ? (
                  <div className="space-y-4">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge variant="outline" className="rounded-full border-blue-200 bg-blue-50 px-3 py-1 text-blue-700">
                        {queue.active_count} active
                      </Badge>
                      <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                        {queue.queue_count} total items
                      </Badge>
                      <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                        Updated {formatTimestamp(queue.updated_at || undefined)}
                      </Badge>
                    </div>
                    <div className="space-y-3">
                      {queuedItems.slice(0, 10).map((item) => (
                        <div key={item.queue_id} className="rounded-[24px] border border-slate-200 bg-slate-50/70 p-4">
                          <button
                            type="button"
                            className="flex w-full flex-col gap-2 text-left"
                            onClick={() => {
                              if (!item.requested_url) return
                              setJobUrl(item.requested_url)
                              setSearchParams({ url: item.requested_url })
                              void loadRecommendationAndHistory(item.requested_url)
                            }}
                          >
                            <div className="flex flex-wrap items-center gap-2">
                              <span className={queuePillClass(item.status)}>{item.status}</span>
                              <b className="text-slate-900">{item.company_name || '-'}</b>
                            </div>
                            <div className={subtleCopyClass()}>
                              {item.job_title || '-'} · {formatTimestamp(item.updated_at || item.added_at || undefined)}
                              {item.review_pending_count ? ` · review ${item.review_pending_count}` : ''}
                            </div>
                          </button>
                          {assistantConfig?.available_resume_variants?.length ? (
                            <div className="mt-4 max-w-xs">
                              <FieldGroup label="Resume">
                                <NativeSelect
                                  value={item.selected_resume_variant || assistantConfig.default_resume_variant || ''}
                                  disabled={resumeSelectionPending}
                                  onChange={(event) =>
                                    void updateQueuedResumeSelection(item.queue_id, item.requested_url, event.target.value)
                                  }
                                >
                                  {assistantConfig.available_resume_variants.map((option) => (
                                    <option key={`${item.queue_id}-${option.variant}`} value={option.variant}>
                                      {option.label}
                                    </option>
                                  ))}
                                </NativeSelect>
                              </FieldGroup>
                            </div>
                          ) : null}
                          <div className="mt-3 text-sm text-slate-500">
                            {item.selected_resume_variant ? `Selected resume: ${item.selected_resume_variant}` : 'Resume not selected yet.'}
                          </div>
                        </div>
                      ))}
                      {!queuedItems.length ? <EmptyState title="No assistant jobs queued yet" description="Queued Greenhouse jobs will appear here with status and resume selection controls." /> : null}
                    </div>
                  </div>
                ) : (
                  <EmptyState title="Queue not loaded" description="The assistant queue will appear here once the backend returns queue data." />
                )}
              </>
            ) : null}

            {workspaceTab === 'candidates' ? (
              <>
                {jobsBatchState.error ? <InlineAlert>{jobsBatchState.error}</InlineAlert> : null}
                {jobsBatchState.loading ? (
                  <div className="text-sm text-slate-500">Loading scraped Greenhouse jobs...</div>
                ) : jobsBatch ? (
                  <div className="space-y-4">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge variant="outline" className="rounded-full border-teal-200 bg-teal-50 px-3 py-1 text-teal-700">
                        {batchModeLabel(jobsBatch.mode)}
                      </Badge>
                      <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                        {jobsBatch.candidate_count} queueable
                      </Badge>
                      <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                        {jobsBatch.skipped_count} skipped
                      </Badge>
                    </div>
                    <div className="space-y-3">
                      {jobsBatch.candidates.slice(0, 8).map((item) => (
                        <button
                          key={item.application_key || item.requested_url}
                          type="button"
                          className="flex w-full flex-col gap-2 rounded-[24px] border border-slate-200 bg-slate-50/70 p-4 text-left transition hover:border-slate-300"
                          onClick={() => {
                            setJobUrl(item.requested_url)
                            setSearchParams({ url: item.requested_url })
                            void loadRecommendationAndHistory(item.requested_url)
                          }}
                        >
                          <div className="flex flex-wrap items-center gap-2">
                            <span className={`${pillBaseClass()} border-amber-200 bg-amber-50 text-amber-700`}>Candidate</span>
                            <b className="text-slate-900">{item.company_name || '-'}</b>
                          </div>
                          <div className={subtleCopyClass()}>
                            {item.job_title || '-'} · first seen {formatTimestamp(item.first_seen || item.posted_at || undefined)}
                          </div>
                        </button>
                      ))}
                      {!jobsBatch.candidates.length ? (
                        <EmptyState title="No queueable candidates" description="The current scraped selection does not contain any queueable Greenhouse candidates." />
                      ) : null}
                    </div>
                  </div>
                ) : (
                  <EmptyState title="No scraped jobs loaded" description="Load Greenhouse jobs from the latest batch or the recent feed to inspect queueable candidates." />
                )}
              </>
            ) : null}
          </SurfaceCardContent>
        </SurfaceCard>
      </div>

      <SurfaceCard>
        <SurfaceCardHeader className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <SurfaceCardTitle>Learning</SurfaceCardTitle>
            <SurfaceCardDescription>Keep the global learning set visible, but focus on one slice of signal at a time: summary, overrides, defaults, accepted suggestions, or candidates.</SurfaceCardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-2" role="tablist" aria-label="Learning sections">
            <TabButton active={learningTab === 'summary'} onClick={() => setLearningTab('summary')}>
              Summary
            </TabButton>
            <TabButton active={learningTab === 'overrides'} onClick={() => setLearningTab('overrides')}>
              Overrides
            </TabButton>
            <TabButton active={learningTab === 'defaults'} onClick={() => setLearningTab('defaults')}>
              Defaults
            </TabButton>
            <TabButton active={learningTab === 'accepted'} onClick={() => setLearningTab('accepted')}>
              Accepted
            </TabButton>
            <TabButton active={learningTab === 'candidates'} onClick={() => setLearningTab('candidates')}>
              Candidates
            </TabButton>
          </div>
        </SurfaceCardHeader>
        <SurfaceCardContent className="space-y-4">
          {learningState.error ? <InlineAlert>{learningState.error}</InlineAlert> : null}
          {learningState.loading ? (
            <div className="text-sm text-slate-500">Loading learning summary...</div>
          ) : learning ? (
            learningTab === 'summary' ? (
              <div className="space-y-5">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant="outline" className="rounded-full border-teal-200 bg-teal-50 px-3 py-1 text-teal-700">
                    Global
                  </Badge>
                  <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                    {learning.summary.session_count} sessions
                  </Badge>
                  <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                    {learning.summary.field_event_count} field events
                  </Badge>
                  <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                    {learning.summary.final_answer_count} final answers
                  </Badge>
                </div>
                <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                  <MetricCard label="Overrides" value={learning.summary.override_count} detail="Repeated manual deviations from recommendations." icon={Sparkles} tone="amber" />
                  <MetricCard label="Accepted" value={learning.summary.matched_recommendation_count} detail="Assistant suggestions that matched final answers." icon={BrainCircuit} tone="teal" />
                  <MetricCard label="Manual Only" value={learning.summary.manual_only_answer_count} detail="Answers supplied without a matching recommended value." icon={ClipboardList} tone="slate" />
                  <MetricCard label="Submitted" value={learning.summary.submitted_count} detail="Runs that completed with a submission event." icon={DatabaseZap} tone="emerald" />
                </div>
                <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                  <div className="rounded-2xl border border-slate-200 bg-slate-50/80 px-4 py-3 text-sm text-slate-600">
                    Top overrides tracked: {learning.top_overrides.length}
                  </div>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50/80 px-4 py-3 text-sm text-slate-600">
                    Stable defaults found: {learning.stable_defaults.length}
                  </div>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50/80 px-4 py-3 text-sm text-slate-600">
                    Accepted suggestion families: {learning.top_acceptances.length}
                  </div>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50/80 px-4 py-3 text-sm text-slate-600">
                    Learning candidates proposed: {learning.learning_candidates.length}
                  </div>
                </div>
              </div>
            ) : learningItems.length ? (
              <div className="space-y-3">
                {learningItems.map((item) => (
                  <div key={`${learningTab}-${item.normalized_label}`} className="rounded-[24px] border border-slate-200 bg-slate-50/70 p-4">
                    <div className="mb-2 flex flex-wrap items-center gap-2">
                      <span className={learningToneClass(learningTab)}>
                        {learningTab === 'overrides'
                          ? `${item.override_count} overrides`
                          : learningTab === 'accepted'
                            ? `${item.matched_count} accepted`
                            : learningTab === 'defaults'
                              ? `${item.top_final_answers[0]?.count || 0} matches`
                              : item.candidate?.type || 'candidate'}
                      </span>
                      <b className="text-slate-900">{item.question_label}</b>
                    </div>
                    <div className={subtleCopyClass()}>
                      {learningTab === 'overrides'
                        ? `Top final answer: ${item.top_final_answers[0]?.value || '-'} · acceptance ${formatRate(item.acceptance_rate)} · ${item.company_samples.join(', ') || 'No company sample'}`
                        : learningTab === 'accepted'
                          ? `Suggested answer: ${item.top_recommended_answers[0]?.value || '-'} · acceptance ${formatRate(item.acceptance_rate)}`
                          : learningTab === 'defaults'
                            ? `Candidate answer: ${item.top_final_answers[0]?.value || '-'} · total ${item.total_answers}`
                            : `${item.candidate?.reason || '-'}${item.candidate?.answer ? ` Candidate answer: ${item.candidate.answer}.` : ''}`}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState
                title={
                  learningTab === 'overrides'
                    ? 'No repeated overrides yet'
                    : learningTab === 'accepted'
                      ? 'No accepted suggestions yet'
                      : learningTab === 'defaults'
                        ? 'No stable defaults yet'
                        : 'No learning candidates yet'
                }
                description="As the assistant accumulates more sessions and reviewed answers, this section will fill in with reusable learning signals."
              />
            )
          ) : (
            <EmptyState title="No learning summary available yet" description="Run the assistant on a few jobs to populate global learning signals and candidate defaults." />
          )}
        </SurfaceCardContent>
      </SurfaceCard>
    </DashboardPage>
  )
}
