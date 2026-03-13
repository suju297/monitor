import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import { BrowserRouter, Link, Navigate, Route, Routes } from 'react-router-dom'
import {
  Bot,
  BriefcaseBusiness,
  CirclePlay,
  Clock3,
  FlaskConical,
  Gauge,
  History,
  MailPlus,
  Radar,
  RefreshCw,
  ShieldAlert,
  Sparkles,
  Workflow,
} from 'lucide-react'
import {
  api,
  loadStoredObserverBaseUrl,
  type CrawlScheduleResponse,
  type JobsProgressResponse,
  type JobsResponse,
  type OverviewResponse,
} from '@/api'
import AssistantPage from '@/AssistantPage'
import MailPage from '@/MailPage'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DashboardPage,
  EmptyState,
  FieldGroup,
  InlineAlert,
  MetricCard,
  ModalShell,
  NativeInput,
  NativeSelect,
  ProgressBar,
  SurfaceCard,
  SurfaceCardContent,
  SurfaceCardDescription,
  SurfaceCardHeader,
  SurfaceCardTitle,
  TableShell,
} from '@/components/dashboard'
import { pillClass, subtleTextClass } from '@/components/dashboard-styles'
import { cn } from '@/lib/utils'

type MonitorState = {
  loading: boolean
  error: string
  data: OverviewResponse | null
}

type JobsState = {
  loading: boolean
  error: string
  data: JobsResponse | null
}

type JobRow = JobsResponse['jobs'][number]
type MonitorProgressRow = NonNullable<OverviewResponse['runner']['progress']>[number]
type JobsSortMode = 'best_match' | 'newest' | 'oldest' | 'company' | 'title'
type MonitorSortKey = 'company' | 'phase' | 'result' | 'jobs' | 'started' | 'finished'
type MonitorSortDirection = 'asc' | 'desc'
type ApplicationStatus = '' | 'applied' | 'not_applied'
type JobsLoadOverrides = Partial<{
  q: string
  company: string
  source: string
  postedWithin: string
  everify: string
  sort: JobsSortMode
  limit: number
}>
type JobsLoadOptions = {
  silent?: boolean
}
type AssistantQueueButtonState = 'queued' | 'duplicate' | 'skipped' | 'error'
type AssistantRunButtonState = 'queued' | 'running' | 'completed' | 'failed'

type AssistantBadgeTone = 'done' | 'queued' | 'running' | 'unknown' | 'blocked'

function statusClass(status: string): string {
  switch ((status || '').toLowerCase()) {
    case 'ok':
      return pillClass('emerald')
    case 'blocked':
      return pillClass('amber')
    case 'error':
      return pillClass('rose')
    default:
      return pillClass('slate')
  }
}

function phaseClass(phase: string): string {
  switch ((phase || '').toLowerCase()) {
    case 'running':
      return pillClass('blue')
    case 'done':
      return pillClass('emerald')
    default:
      return pillClass('amber')
  }
}

function eVerifyLabel(status: string): string {
  switch ((status || '').toLowerCase()) {
    case 'enrolled':
      return 'Enrolled'
    case 'not_found':
      return 'No Match'
    case 'not_enrolled':
      return 'Not Enrolled'
    default:
      return 'Unknown'
  }
}

function sourceLabel(value: string): string {
  const normalized = (value || '').trim()
  if (!normalized) return 'All'
  return normalized
    .split(/[_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function normalizeApplicationStatus(value?: string): ApplicationStatus {
  switch ((value || '').trim().toLowerCase()) {
    case 'applied':
      return 'applied'
    case 'not_applied':
    case 'not-applied':
    case 'not applied':
      return 'not_applied'
    default:
      return ''
  }
}

function assistantBadge(job: JobRow): { label: string; tone: AssistantBadgeTone; title: string } | null {
  const source = (job.assistant_last_source || '').trim()
  if (!source) return null

  const outcome = (job.assistant_last_outcome || '').trim().toLowerCase()
  const reviewPendingCount = Math.max(0, Number(job.assistant_last_review_pending_count || 0))
  const currentStatus = normalizeApplicationStatus(job.application_status)
  const isConfirmed = Boolean(job.assistant_last_confirmation_detected)
  const syncAt = (job.assistant_last_sync_at || '').trim()

  let label = 'Observed'
  let tone: AssistantBadgeTone = 'unknown'
  if (outcome === 'submitted' && source === 'playwright_run' && isConfirmed) {
    label = 'Assistant Applied'
    tone = 'done'
  } else if (outcome === 'submitted' && currentStatus === 'applied') {
    label = 'Observed Applied'
    tone = 'done'
  } else if (outcome === 'review_required' || reviewPendingCount > 0) {
    label = 'Under Review'
    tone = 'queued'
  } else if (outcome === 'eligible') {
    label = 'Ready'
    tone = 'running'
  } else if (outcome === 'not_ready') {
    label = 'Needs Attention'
    tone = 'blocked'
  }

  const details: string[] = [sourceLabel(source)]
  if (reviewPendingCount > 0) details.unshift(`${reviewPendingCount} review pending`)
  if (syncAt) details.push(`Last sync ${formatTimestamp(syncAt)}`)
  return { label, tone, title: details.join(' · ') }
}

function assistantBadgeClass(tone: AssistantBadgeTone): string {
  switch (tone) {
    case 'done':
      return pillClass('emerald')
    case 'queued':
      return pillClass('amber')
    case 'running':
      return pillClass('blue')
    case 'blocked':
      return pillClass('rose')
    default:
      return pillClass('slate')
  }
}

function postedWithinLabel(value: string): string {
  switch ((value || '').toLowerCase()) {
    case '24h':
      return 'Latest (24h)'
    case '48h':
      return 'Last 2 Days'
    case '7d':
      return 'Last 7 Days'
    default:
      return 'Any Time'
  }
}

function formatTimestamp(value?: string): string {
  if (!value) return '-'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

function formatPostedDisplay(postedAtLocal?: string, postedAtRaw?: string): string {
  const candidate = (postedAtLocal || postedAtRaw || '').trim()
  if (!candidate) return '-'
  const parsed = new Date(candidate)
  if (Number.isNaN(parsed.getTime())) return candidate

  const now = new Date()
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const startPostedDay = new Date(parsed.getFullYear(), parsed.getMonth(), parsed.getDate())
  const dayDiff = Math.round((startToday.getTime() - startPostedDay.getTime()) / (24 * 60 * 60 * 1000))

  if (dayDiff === 0) {
    const diffMinutes = Math.max(1, Math.floor((now.getTime() - parsed.getTime()) / (60 * 1000)))
    if (diffMinutes < 60) return `${diffMinutes} min ago`
    const diffHours = Math.floor(diffMinutes / 60)
    return `${diffHours} hr${diffHours === 1 ? '' : 's'} ago`
  }

  if (dayDiff === 1) {
    return 'Yesterday'
  }

  const sameYear = parsed.getFullYear() === now.getFullYear()
  return parsed.toLocaleDateString(undefined, sameYear ? { month: 'short', day: 'numeric' } : { year: 'numeric', month: 'short', day: 'numeric' })
}

function firstSeenDisplay(firstSeenLocal?: string, firstSeenRaw?: string): string {
  const localCandidate = (firstSeenLocal || '').trim()
  if (localCandidate && localCandidate !== '-') return localCandidate
  return formatTimestamp(firstSeenRaw)
}

function decisionSourceLabel(value?: string): string {
  switch ((value || '').trim().toLowerCase()) {
    case 'slm':
      return 'SLM reviewed'
    default:
      return 'Deterministic'
  }
}

function roleDecisionLabel(value?: string): string {
  switch ((value || '').trim().toLowerCase()) {
    case 'in':
      return 'Role in-scope'
    case 'out':
      return 'Role out-of-scope'
    default:
      return 'Role unclear'
  }
}

function internshipDecisionLabel(value?: string): string {
  switch ((value || '').trim().toLowerCase()) {
    case 'allowed':
      return 'Internship allowed'
    case 'blocked':
      return 'Internship blocked'
    case 'unknown':
      return 'Internship unclear'
    default:
      return ''
  }
}

function prettifyBoardSlug(slug: string): string {
  const cleaned = (slug || '').trim().replace(/[-_]+/g, ' ')
  if (!cleaned) return ''
  return cleaned
    .split(/\s+/)
    .map((part) => {
      if (!part) return ''
      if (/^\d+$/.test(part)) return part
      if (part.length <= 3) return part.toUpperCase()
      return `${part[0].toUpperCase()}${part.slice(1)}`
    })
    .filter(Boolean)
    .join(' ')
}

function greenhouseEmployerFromURL(rawURL: string): string {
  if (!rawURL) return ''
  try {
    const parsed = new URL(rawURL)
    const host = parsed.hostname.toLowerCase()
    if (!host.includes('greenhouse.io')) return ''
    const parts = parsed.pathname.split('/').filter(Boolean)
    if (parts.length < 3 || parts[1].toLowerCase() !== 'jobs') return ''
    return prettifyBoardSlug(parts[0])
  } catch {
    return ''
  }
}

function isSupportedGreenhouseJobURL(rawURL: string): boolean {
  try {
    const parsed = new URL(rawURL)
    const host = parsed.hostname.toLowerCase()
    if (!['boards.greenhouse.io', 'job-boards.greenhouse.io'].includes(host)) return false
    const parts = parsed.pathname.split('/').filter(Boolean)
    return parts.length >= 3 && parts[1].toLowerCase() === 'jobs'
  } catch {
    return false
  }
}

function jobsColumnClass(columnId: string): string {
  switch (columnId) {
    case 'company':
      return 'align-top'
    case 'title':
      return 'min-w-[17rem] align-top'
    case 'eligibility':
      return 'min-w-[10rem] align-top'
    case 'posted':
      return 'min-w-[7rem] align-top text-[13px] text-slate-600'
    case 'application':
      return 'min-w-[10rem] align-top'
    case 'match':
      return 'min-w-[8rem] align-top text-right'
    default:
      return ''
  }
}

function sortableHeaderButtonClass(active: boolean): string {
  return cn(
    'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[10px] font-semibold tracking-[0.08em] uppercase transition',
    active
      ? 'border-blue-600 bg-blue-600 text-white'
      : 'border-slate-200 bg-white text-slate-500 hover:border-blue-200 hover:text-blue-700',
  )
}

function inlineActionButtonClass(state?: string): string {
  switch ((state || '').trim().toLowerCase()) {
    case 'running':
      return 'border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100'
    case 'queued':
    case 'duplicate':
      return 'border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100'
    case 'completed':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700 hover:bg-emerald-100'
    case 'skipped':
      return 'border-slate-200 bg-slate-100 text-slate-500 hover:bg-slate-200'
    case 'failed':
    case 'error':
      return 'border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100'
    default:
      return 'border-slate-200 bg-white text-slate-700 hover:bg-slate-50'
  }
}

function applicationStatusButtonClass(active: boolean, tone: 'applied' | 'not_applied'): string {
  if (active && tone === 'applied') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-700'
  }
  if (active && tone === 'not_applied') {
    return 'border-blue-600 bg-blue-600 text-white'
  }
  return 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'
}

function eligibilityBadgeClass(status: string): string {
  switch ((status || '').trim().toLowerCase()) {
    case 'friendly':
      return pillClass('emerald')
    case 'blocked':
      return pillClass('rose')
    default:
      return pillClass('amber')
  }
}

function matchBadgeClass(score?: number | null): string {
  if ((score ?? 0) >= 80) return 'border-emerald-200 bg-emerald-50 text-emerald-700'
  if ((score ?? 0) >= 60) return 'border-blue-200 bg-blue-50 text-blue-700'
  return 'border-amber-200 bg-amber-50 text-amber-700'
}

function scheduleIntervalLabel(intervalMinutes: number): string {
  const value = Math.max(5, Math.floor(intervalMinutes || 0))
  if (value % 60 === 0) {
    const hours = value / 60
    return `${hours}h`
  }
  return `${value}m`
}

function sortDateValue(value?: string): number {
  if (!value) return 0
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return 0
  return parsed.getTime()
}

export function MonitorPage() {
  const [state, setState] = useState<MonitorState>({ loading: true, error: '', data: null })
  const [runBusy, setRunBusy] = useState(false)
  const [showHistory, setShowHistory] = useState(false)
  const [schedule, setSchedule] = useState<CrawlScheduleResponse | null>(null)
  const [scheduleBusy, setScheduleBusy] = useState(false)
  const [monitorSort, setMonitorSort] = useState<{ key: MonitorSortKey; direction: MonitorSortDirection }>({
    key: 'phase',
    direction: 'asc',
  })

  const load = useCallback(async () => {
    try {
      const [data, scheduleResponse] = await Promise.all([api.getOverview(), api.getCrawlSchedule().catch(() => null)])
      setState({ loading: false, error: '', data })
      if (scheduleResponse) {
        setSchedule(scheduleResponse)
      }
    } catch (error) {
      setState((prev) => ({
        loading: false,
        data: prev.data,
        error: error instanceof Error ? error.message : String(error),
      }))
    }
  }, [])

  const isRunActive = runBusy || Boolean(state.data?.runner?.running || state.data?.runner?.queued)
  const monitorPollMs = isRunActive ? 2000 : 10000

  useEffect(() => {
    load()
    const timer = window.setInterval(load, monitorPollMs)
    return () => window.clearInterval(timer)
  }, [load, monitorPollMs])

  const run = useCallback(
    async (dryRun: boolean) => {
      if (runBusy) return
      setRunBusy(true)
      setState((prev) => {
        if (!prev.data) return prev
        const runner = prev.data.runner
        const resetProgress = (runner.progress ?? []).map((item) => ({
          ...item,
          phase: 'queued',
          outcome_status: '',
          source: '',
          jobs_found: 0,
          message: '',
          started_at: '',
          finished_at: '',
        }))
        return {
          ...prev,
          error: '',
          data: {
            ...prev.data,
            runner: {
              ...runner,
              running: true,
              last_mode: dryRun ? 'dry-run' : 'live-run',
              last_error: '',
              completed_companies: 0,
              progress: resetProgress,
            },
          },
        }
      })
      try {
        const response = await api.triggerRun(dryRun)
        if (!response.ok) {
          throw new Error(response.message || 'Run did not start')
        }
        if (response.queued) {
          setState((prev) => {
            if (!prev.data) return prev
            return {
              ...prev,
              error: '',
              data: {
                ...prev.data,
                runner: {
                  ...prev.data.runner,
                  running: false,
                  queued: true,
                  queued_mode: dryRun ? 'dry-run' : 'live-run',
                },
              },
            }
          })
        }
        await load()
      } catch (error) {
        setState((prev) => ({
          ...prev,
          error: error instanceof Error ? error.message : String(error),
        }))
        await load()
      } finally {
        setRunBusy(false)
      }
    },
    [load, runBusy],
  )

  const summary = state.data?.summary
  const mailSummary = state.data?.mail
  const companies = state.data?.companies ?? []
  const runner = state.data?.runner
  const scheduleStatus = schedule ?? { enabled: false, interval_minutes: 60 }

  const updateSchedule = useCallback(async (enabled: boolean, intervalMinutes: number) => {
    setScheduleBusy(true)
    try {
      const next = await api.updateCrawlSchedule({
        enabled,
        interval_minutes: intervalMinutes,
      })
      setSchedule(next)
      setState((prev) => ({ ...prev, error: '' }))
    } catch (error) {
      setState((prev) => ({
        ...prev,
        error: error instanceof Error ? error.message : String(error),
      }))
    } finally {
      setScheduleBusy(false)
    }
  }, [])

  const toggleMonitorSort = useCallback((key: MonitorSortKey) => {
    setMonitorSort((previous) => {
      if (previous.key === key) {
        return { key, direction: previous.direction === 'asc' ? 'desc' : 'asc' }
      }
      const defaultDirection: MonitorSortDirection = key === 'jobs' || key === 'started' || key === 'finished' ? 'desc' : 'asc'
      return { key, direction: defaultDirection }
    })
  }, [])

  const progressRows = useMemo(() => {
    const rows = [...(runner?.progress ?? [])]
    const phaseRank: Record<string, number> = { running: 0, queued: 1, done: 2 }
    const resultRank: Record<string, number> = { ok: 0, blocked: 1, error: 2, unknown: 3 }
    const direction = monitorSort.direction === 'asc' ? 1 : -1
    rows.sort((left, right) => {
      let comparison = 0
      switch (monitorSort.key) {
        case 'company':
          comparison = (left.company || '').localeCompare(right.company || '', undefined, { sensitivity: 'base' })
          break
        case 'phase': {
          const leftRank = phaseRank[(left.phase || '').toLowerCase()] ?? 3
          const rightRank = phaseRank[(right.phase || '').toLowerCase()] ?? 3
          comparison = leftRank - rightRank
          break
        }
        case 'result': {
          const leftRank = resultRank[(left.outcome_status || 'unknown').toLowerCase()] ?? 3
          const rightRank = resultRank[(right.outcome_status || 'unknown').toLowerCase()] ?? 3
          comparison = leftRank - rightRank
          break
        }
        case 'jobs':
          comparison = (left.jobs_found ?? 0) - (right.jobs_found ?? 0)
          break
        case 'started':
          comparison = sortDateValue(left.started_at) - sortDateValue(right.started_at)
          break
        case 'finished':
          comparison = sortDateValue(left.finished_at) - sortDateValue(right.finished_at)
          break
        default:
          comparison = 0
      }
      if (comparison === 0) {
        comparison = (left.company || '').localeCompare(right.company || '', undefined, { sensitivity: 'base' })
      }
      return comparison * direction
    })
    return rows
  }, [monitorSort.direction, monitorSort.key, runner?.progress])

  const monitorColumns = useMemo<ColumnDef<MonitorProgressRow>[]>(
    () => [
      {
        id: 'company',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(monitorSort.key === 'company')}
            onClick={() => toggleMonitorSort('company')}
            aria-label="Sort monitor by website"
          >
            Website
            <span className="text-[10px] font-medium tracking-normal opacity-80">
              {monitorSort.key === 'company' ? (monitorSort.direction === 'asc' ? 'A-Z' : 'Z-A') : ''}
            </span>
          </button>
        ),
        cell: ({ row }) => row.original.company || '-',
      },
      {
        id: 'phase',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(monitorSort.key === 'phase')}
            onClick={() => toggleMonitorSort('phase')}
            aria-label="Sort monitor by phase"
          >
            Phase
            <span className="text-[10px] font-medium tracking-normal opacity-80">
              {monitorSort.key === 'phase' ? (monitorSort.direction === 'asc' ? 'Queued-Done' : 'Done-Queued') : ''}
            </span>
          </button>
        ),
        cell: ({ row }) => <span className={phaseClass(row.original.phase)}>{row.original.phase || 'queued'}</span>,
      },
      {
        id: 'result',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(monitorSort.key === 'result')}
            onClick={() => toggleMonitorSort('result')}
            aria-label="Sort monitor by result"
          >
            Result
            <span className="text-[10px] font-medium tracking-normal opacity-80">
              {monitorSort.key === 'result' ? (monitorSort.direction === 'asc' ? 'OK-Error' : 'Error-OK') : ''}
            </span>
          </button>
        ),
        cell: ({ row }) => (
          <span className={statusClass(row.original.outcome_status || 'unknown')}>{row.original.outcome_status || '-'}</span>
        ),
      },
      {
        id: 'source',
        header: 'Source',
        cell: ({ row }) => row.original.source || (row.original.attempted_sources || []).join(' -> ') || '-',
      },
      {
        id: 'jobs',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(monitorSort.key === 'jobs')}
            onClick={() => toggleMonitorSort('jobs')}
            aria-label="Sort monitor by jobs gathered"
          >
            Jobs Gathered
            <span className="text-[10px] font-medium tracking-normal opacity-80">
              {monitorSort.key === 'jobs' ? (monitorSort.direction === 'asc' ? 'Low-High' : 'High-Low') : ''}
            </span>
          </button>
        ),
        cell: ({ row }) => row.original.jobs_found ?? 0,
      },
      {
        id: 'started',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(monitorSort.key === 'started')}
            onClick={() => toggleMonitorSort('started')}
            aria-label="Sort monitor by started time"
          >
            Started
            <span className="text-[10px] font-medium tracking-normal opacity-80">
              {monitorSort.key === 'started' ? (monitorSort.direction === 'asc' ? 'Oldest' : 'Newest') : ''}
            </span>
          </button>
        ),
        cell: ({ row }) => formatTimestamp(row.original.started_at),
      },
      {
        id: 'finished',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(monitorSort.key === 'finished')}
            onClick={() => toggleMonitorSort('finished')}
            aria-label="Sort monitor by finished time"
          >
            Finished
            <span className="text-[10px] font-medium tracking-normal opacity-80">
              {monitorSort.key === 'finished' ? (monitorSort.direction === 'asc' ? 'Oldest' : 'Newest') : ''}
            </span>
          </button>
        ),
        cell: ({ row }) => formatTimestamp(row.original.finished_at),
      },
      {
        id: 'notes',
        header: 'Notes',
        cell: ({ row }) => <span className={cn(subtleTextClass(), 'line-clamp-3 block max-w-sm')}>{(row.original.message || '').slice(0, 180) || '-'}</span>,
      },
    ],
    [monitorSort.direction, monitorSort.key, toggleMonitorSort],
  )

  const monitorTable = useReactTable({
    data: progressRows,
    columns: monitorColumns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row, index) => `${(row.company || '').trim()}-${index}`,
  })

  const runnerTotalCompanies = runner?.total_companies ?? 0
  const totalTargets = runnerTotalCompanies > 0 ? runnerTotalCompanies : (summary?.companies_total ?? companies.length)
  const completedTargets =
    runnerTotalCompanies > 0
      ? (runner?.completed_companies ?? 0)
      : progressRows.filter((row) => (row.phase || '').toLowerCase() === 'done').length
  const runningTargets = progressRows.filter((row) => (row.phase || '').toLowerCase() === 'running').length
  const jobsGathered = progressRows.reduce(
    (count, row) =>
      count + ((row.outcome_status || '').toLowerCase() === 'ok' && (row.phase || '').toLowerCase() === 'done' ? row.jobs_found || 0 : 0),
    0,
  )
  const blockedThisRun = progressRows.filter(
    (row) => (row.phase || '').toLowerCase() === 'done' && (row.outcome_status || '').toLowerCase() === 'blocked',
  ).length
  const scoring = runner?.scoring
  const scoringScheduled = scoring?.scheduled_jobs ?? 0
  const scoringCompleted = scoring?.completed_jobs ?? 0
  const scoringQueued = scoring?.queued_jobs ?? 0
  const scoringSuccess = scoring?.success_jobs ?? 0
  const scoringFailed = scoring?.failed_jobs ?? 0
  const scoringRunning = Boolean(scoring?.running)
  const scoringProgress = scoringScheduled > 0 ? `${scoringCompleted}/${scoringScheduled}` : '0/0'
  const scoringPercent = scoringScheduled > 0 ? Math.min(100, Math.round((scoringCompleted / scoringScheduled) * 100)) : 0
  const schedulerEnabled = Boolean(scheduleStatus.enabled)
  const scheduleIntervalMinutes = Math.max(5, scheduleStatus.interval_minutes || 60)
  const scheduleNext = scheduleStatus.next_run_at ? formatTimestamp(scheduleStatus.next_run_at) : '-'
  const scheduleLastTrigger = scheduleStatus.last_trigger_at ? formatTimestamp(scheduleStatus.last_trigger_at) : '-'
  const scheduleResult = scheduleStatus.last_trigger_result || '-'
  const statusLine = runner?.running
    ? `Run in progress: ${completedTargets}/${totalTargets} completed`
    : runner?.queued
      ? `Run queued: waiting for the current background task to finish`
      : `Last run: ${summary?.last_run_local || '-'} | Updated: ${summary?.generated_at || '-'}`

  return (
    <DashboardPage>
      <section className="relative overflow-hidden rounded-[22px] border border-slate-200/80 bg-white/90 px-4 py-3.5 shadow-[0_18px_60px_-42px_rgba(15,23,42,0.3)] backdrop-blur md:px-5 md:py-4">
        <div className="pointer-events-none absolute inset-y-0 right-0 w-52 bg-[radial-gradient(circle_at_top_right,_rgba(37,99,235,0.1),_transparent_60%)]" />
        <div className="relative space-y-3">
          <div className="flex items-start gap-3">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-blue-200/80 bg-blue-600 text-white shadow-sm">
              <Radar className="size-4" />
            </div>
            <div className="min-w-0 space-y-1">
              <h1 className="text-[1.7rem] leading-tight font-semibold tracking-tight text-slate-950 md:text-[1.95rem]">Crawler Monitor</h1>
              <p className="max-w-3xl text-[13px] leading-5 text-slate-600">
                Run the crawl, watch target-by-target telemetry, and keep jobs and mail in sync without dropping into raw logs.
              </p>
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-200/70 pt-3">
            <div className="flex flex-wrap items-center gap-1.5">
              <div className="inline-flex items-center rounded-full border border-blue-200/80 bg-blue-50 px-2.5 py-0.5 text-[10px] font-semibold tracking-[0.18em] text-blue-700 uppercase">
                Operations
              </div>
              <Badge variant="outline" className="rounded-full border-slate-200 bg-white/90 px-2.5 py-0.5 text-[11px] text-slate-600">
                {statusLine}
              </Badge>
              <Badge variant="outline" className="rounded-full border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-[11px] text-emerald-700">
                Auto refresh {Math.round(monitorPollMs / 1000)}s
              </Badge>
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              <Button asChild size="sm" variant="outline" className="rounded-full">
                <Link to="/assistant">
                  <Bot className="size-4" />
                  Assistant
                </Link>
              </Button>
              <Button asChild size="sm" variant="outline" className="rounded-full">
                <Link to="/mail">
                  <MailPlus className="size-4" />
                  Mail
                </Link>
              </Button>
              <Button asChild size="sm" variant="outline" className="rounded-full">
                <Link to="/jobs">
                  <BriefcaseBusiness className="size-4" />
                  Jobs
                </Link>
              </Button>
              <Button size="sm" className="rounded-full" onClick={() => run(false)} disabled={runBusy}>
                <CirclePlay className="size-4" />
                Start Crawl
              </Button>
              <Button size="sm" variant="secondary" className="rounded-full" onClick={() => run(true)} disabled={runBusy}>
                <FlaskConical className="size-4" />
                Dry Run
              </Button>
              <Button size="sm" variant="outline" className="rounded-full" onClick={load} disabled={runBusy}>
                <RefreshCw className="size-4" />
                Refresh
              </Button>
              <Button size="sm" variant="outline" className="rounded-full" onClick={() => setShowHistory(true)}>
                <History className="size-4" />
                History
              </Button>
            </div>
          </div>
        </div>
      </section>

      {state.error ? <InlineAlert>{state.error}</InlineAlert> : null}

      {scoringRunning ? (
        <SurfaceCard aria-live="polite">
          <SurfaceCardHeader className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline" className="rounded-full border-blue-200 bg-blue-50 px-3 py-1 text-blue-700">
                  <Sparkles className="size-3.5" />
                  SLM Scoring
                </Badge>
                <span className="text-sm text-slate-500">trigger: {scoring?.trigger || '-'}</span>
              </div>
              <SurfaceCardTitle>Scoring progress is active during this crawl.</SurfaceCardTitle>
              <SurfaceCardDescription>Queued scoring jobs run in the background after eligible roles are collected.</SurfaceCardDescription>
            </div>
            <div className="text-right">
              <div className="text-2xl font-semibold tracking-tight text-slate-950">{scoringProgress}</div>
              <div className="text-sm text-slate-500">{scoringPercent}% complete</div>
            </div>
          </SurfaceCardHeader>
          <SurfaceCardContent className="space-y-3.5">
            <ProgressBar value={scoringPercent} indicatorClassName="bg-blue-600" />
            <div className="grid gap-2.5 md:grid-cols-3 xl:grid-cols-6">
              <div className="rounded-xl border border-slate-200 bg-slate-50/80 px-3 py-2.5 text-[13px] text-slate-600">Queued: {scoringQueued}</div>
              <div className="rounded-xl border border-slate-200 bg-slate-50/80 px-3 py-2.5 text-[13px] text-slate-600">Success: {scoringSuccess}</div>
              <div className="rounded-xl border border-slate-200 bg-slate-50/80 px-3 py-2.5 text-[13px] text-slate-600">Failed: {scoringFailed}</div>
              <div className="rounded-xl border border-slate-200 bg-slate-50/80 px-3 py-2.5 text-[13px] text-slate-600">
                Eligible: {scoring?.eligible_jobs ?? 0}
              </div>
              <div className="rounded-xl border border-slate-200 bg-slate-50/80 px-3 py-2.5 text-[13px] text-slate-600">
                Started: {formatTimestamp(scoring?.started_at)}
              </div>
              <div className="rounded-xl border border-slate-200 bg-slate-50/80 px-3 py-2.5 text-[13px] text-slate-600">
                Updated: {formatTimestamp(scoring?.updated_at)}
              </div>
            </div>
            {scoring?.last_error ? <InlineAlert>Last error: {scoring.last_error}</InlineAlert> : null}
          </SurfaceCardContent>
        </SurfaceCard>
      ) : null}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-5">
        <MetricCard label="Targets" value={totalTargets} detail="Configured companies." icon={Radar} tone="slate" />
        <MetricCard label="Running Now" value={runningTargets} detail="Active crawls." icon={Workflow} tone="blue" />
        <MetricCard
          label="Completed"
          value={`${completedTargets}/${totalTargets}`}
          detail="Finished this run."
          icon={Gauge}
          tone="emerald"
        />
        <MetricCard label="Jobs Gathered" value={jobsGathered} detail="Successful openings." icon={BriefcaseBusiness} tone="teal" />
        <MetricCard label="Blocked This Run" value={blockedThisRun} detail="Blocked outcomes." icon={ShieldAlert} tone="amber" />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.05fr_0.95fr] xl:items-stretch">
        <SurfaceCard className="h-full">
          <SurfaceCardHeader className="xl:min-h-[72px]">
            <SurfaceCardTitle>Recurring Crawl</SurfaceCardTitle>
            <SurfaceCardDescription>Keep the job feed current without manually starting the crawler each time.</SurfaceCardDescription>
          </SurfaceCardHeader>
          <SurfaceCardContent className="space-y-3">
            <div className="flex flex-wrap items-center gap-2.5">
              <Badge
                variant="outline"
                className={cn(
                  'rounded-full px-3 py-1',
                  schedulerEnabled ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-100 text-slate-600',
                )}
              >
                <Clock3 className="size-3.5" />
                {schedulerEnabled ? 'Scheduler ON' : 'Scheduler OFF'}
              </Badge>
              <span className="text-[13px] text-slate-600">
                {schedulerEnabled
                  ? `Automatic crawl every ${scheduleIntervalLabel(scheduleIntervalMinutes)}.`
                  : 'Automatic crawling is paused.'}
              </span>
            </div>

            <div className="grid gap-3 md:grid-cols-[220px_auto] md:items-end">
              <FieldGroup label="Interval">
                <NativeSelect
                  value={scheduleIntervalMinutes}
                  onChange={(event) => void updateSchedule(scheduleStatus.enabled, Number(event.target.value))}
                  disabled={scheduleBusy}
                >
                  {[15, 30, 60, 120, 240, 360, 720, 1440].map((interval) => (
                    <option key={interval} value={interval}>
                      {scheduleIntervalLabel(interval)}
                    </option>
                  ))}
                </NativeSelect>
              </FieldGroup>

              <Button
                size="sm"
                className="rounded-full"
                variant={schedulerEnabled ? 'outline' : 'default'}
                onClick={() => void updateSchedule(!schedulerEnabled, scheduleIntervalMinutes)}
                disabled={scheduleBusy}
              >
                {schedulerEnabled ? 'Pause Scheduler' : 'Enable Scheduler'}
              </Button>
            </div>

            <div className="grid gap-2 sm:grid-cols-3">
              <div className="rounded-lg border border-slate-200 bg-slate-50/80 px-3 py-2 text-[12px] leading-4.5 text-slate-600">
                <span className="mr-1 font-semibold text-slate-700">Next:</span>
                {schedulerEnabled ? scheduleNext : 'Scheduler off'}
              </div>
              <div className="rounded-lg border border-slate-200 bg-slate-50/80 px-3 py-2 text-[12px] leading-4.5 text-slate-600">
                <span className="mr-1 font-semibold text-slate-700">Last:</span>
                {scheduleLastTrigger}
              </div>
              <div className="rounded-lg border border-slate-200 bg-slate-50/80 px-3 py-2 text-[12px] leading-4.5 text-slate-600">
                <span className="mr-1 font-semibold text-slate-700">Result:</span>
                {scheduleResult}
              </div>
            </div>

            {scheduleStatus.last_error ? <InlineAlert>Last scheduler error: {scheduleStatus.last_error}</InlineAlert> : null}
          </SurfaceCardContent>
        </SurfaceCard>

        <SurfaceCard className="h-full">
          <SurfaceCardHeader className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between xl:min-h-[72px]">
            <div>
              <SurfaceCardTitle>Mail Signals</SurfaceCardTitle>
              <SurfaceCardDescription>Applications, recruiter replies, interviews, and rejections pulled from the mail analytics layer.</SurfaceCardDescription>
            </div>
            <Button asChild size="sm" variant="outline" className="rounded-full">
              <Link to="/mail">Open Mail</Link>
            </Button>
          </SurfaceCardHeader>
          <SurfaceCardContent className="space-y-3">
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge variant="outline" className="rounded-full border-emerald-200 bg-emerald-50 px-3 py-1 text-emerald-700">
                Important {mailSummary?.unread_important_count ?? 0}
              </Badge>
              <Badge variant="outline" className="rounded-full border-blue-200 bg-blue-50 px-3 py-1 text-blue-700">
                New {mailSummary?.new_message_count ?? 0}
              </Badge>
              <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                Accounts {mailSummary?.connected_accounts_count ?? 0}
              </Badge>
            </div>
            <div className="grid gap-2">
              <div className="flex items-center gap-3 rounded-lg border border-blue-200/70 bg-blue-50/70 px-3 py-2.5">
                <Badge variant="outline" className="shrink-0 rounded-full border-blue-200 bg-white/80 text-blue-700">
                  Interview
                </Badge>
                <div className="min-w-0 truncate text-sm font-medium text-slate-900">{mailSummary?.latest_interview?.subject || 'No interview signal yet'}</div>
              </div>
              <div className="flex items-center gap-3 rounded-lg border border-rose-200/70 bg-rose-50/70 px-3 py-2.5">
                <Badge variant="outline" className="shrink-0 rounded-full border-rose-200 bg-white/80 text-rose-700">
                  Rejection
                </Badge>
                <div className="min-w-0 truncate text-sm font-medium text-slate-900">{mailSummary?.latest_rejection?.subject || 'No rejection signal yet'}</div>
              </div>
              <div className="flex items-center gap-3 rounded-lg border border-emerald-200/70 bg-emerald-50/70 px-3 py-2.5">
                <Badge variant="outline" className="shrink-0 rounded-full border-emerald-200 bg-white/80 text-emerald-700">
                  Reply
                </Badge>
                <div className="min-w-0 truncate text-sm font-medium text-slate-900">
                  {mailSummary?.latest_recruiter_reply?.subject || 'No recruiter reply signal yet'}
                </div>
              </div>
            </div>
          </SurfaceCardContent>
        </SurfaceCard>
      </div>

      <SurfaceCard>
        <SurfaceCardHeader className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <SurfaceCardTitle>Crawl Observability</SurfaceCardTitle>
            <SurfaceCardDescription>Target-by-target phase, result, source, and volume telemetry for the current background activity.</SurfaceCardDescription>
          </div>
          <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
            {progressRows.length} targets
          </Badge>
        </SurfaceCardHeader>
        <SurfaceCardContent>
          {progressRows.length ? (
            <div className="overflow-hidden rounded-[22px] border border-slate-200/80">
              <div className="overflow-x-auto">
                <table className="min-w-full text-left text-sm">
                  <thead className="bg-slate-50/80">
                    {monitorTable.getHeaderGroups().map((headerGroup) => (
                      <tr key={headerGroup.id}>
                        {headerGroup.headers.map((header) => (
                          <th key={header.id} className="px-3 py-2.5 font-medium text-slate-500">
                            {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                          </th>
                        ))}
                      </tr>
                    ))}
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {monitorTable.getRowModel().rows.map((row) => (
                      <tr key={row.id} className="bg-white/70">
                        {row.getVisibleCells().map((cell) => (
                          <td key={cell.id} className="px-3 py-3 align-top text-slate-700">
                            {flexRender(cell.column.columnDef.cell, cell.getContext())}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            <EmptyState
              title={runner?.running ? 'Collecting crawl telemetry' : runner?.queued ? 'Crawler run queued' : 'Crawler observability is ready'}
              description={
                runner?.running
                  ? 'Targets are being crawled right now. This panel refreshes automatically as outcomes arrive.'
                  : runner?.queued
                    ? 'A crawl is queued behind another background task and will start as soon as the current work finishes.'
                    : 'Start a crawl from the controls above to populate live progress, source outcomes, and gathered job counts.'
              }
              meta={
                <>
                  <span>Configured targets: {totalTargets}</span>
                  <span>Auto refresh: {Math.round(monitorPollMs / 1000)}s</span>
                </>
              }
            />
          )}
        </SurfaceCardContent>
      </SurfaceCard>

      <ModalShell
        open={showHistory}
        onClose={() => setShowHistory(false)}
        title="Persisted Company History"
        description="Last known status, source, and notes stored for each company target."
        maxWidthClassName="max-w-6xl"
        align="top"
        ariaLabel="Persisted company status history"
        headerAction={
          <Button variant="outline" className="rounded-full" onClick={() => setShowHistory(false)}>
            Close
          </Button>
        }
        contentClassName="overflow-hidden"
      >
        <TableShell viewportClassName="overflow-x-auto">
          <table className="min-w-full text-left text-sm">
            <thead className="bg-slate-50/80">
              <tr>
                <th className="px-3 py-2.5 font-medium text-slate-500">Company</th>
                <th className="px-3 py-2.5 font-medium text-slate-500">Status</th>
                <th className="px-3 py-2.5 font-medium text-slate-500">Source</th>
                <th className="px-3 py-2.5 font-medium text-slate-500">Seen</th>
                <th className="px-3 py-2.5 font-medium text-slate-500">Blocked</th>
                <th className="px-3 py-2.5 font-medium text-slate-500">Updated</th>
                <th className="px-3 py-2.5 font-medium text-slate-500">Notes</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {companies.map((company) => (
                <tr key={company.name} className="bg-white/70">
                  <td className="px-3 py-3 font-medium text-slate-900">{company.name}</td>
                  <td className="px-3 py-3">
                    <span className={statusClass(company.status)}>{company.status}</span>
                  </td>
                  <td className="px-3 py-3 text-slate-600">{company.selected_source || '-'}</td>
                  <td className="px-3 py-3 text-slate-600">{company.seen_jobs}</td>
                  <td className="px-3 py-3 text-slate-600">{company.blocked_events}</td>
                  <td className="px-3 py-3 text-slate-600">{company.updated_at_local || '-'}</td>
                  <td className="px-3 py-3 text-sm leading-5 text-slate-500">{(company.message || '').slice(0, 140)}</td>
                </tr>
              ))}
              {!companies.length ? (
                <tr>
                  <td colSpan={7} className="px-3 py-8 text-center text-slate-500">
                    {state.loading ? 'Loading...' : 'No company data yet.'}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </TableShell>
      </ModalShell>
    </DashboardPage>
  )
}

export function JobsPage() {
  const [state, setState] = useState<JobsState>({ loading: true, error: '', data: null })
  const [q, setQ] = useState('')
  const [company, setCompany] = useState('')
  const [source, setSource] = useState('')
  const [postedWithin, setPostedWithin] = useState('')
  const [everify, setEverify] = useState('')
  const [sort, setSort] = useState<JobsSortMode>('best_match')
  const [limit, setLimit] = useState(500)
  const [filtersExpanded, setFiltersExpanded] = useState(false)
  const [openMatchReasonKey, setOpenMatchReasonKey] = useState('')
  const [pendingApplicationUpdates, setPendingApplicationUpdates] = useState<Record<string, boolean>>({})
  const [pendingAssistantQueueUpdates, setPendingAssistantQueueUpdates] = useState<Record<string, boolean>>({})
  const [assistantQueueStates, setAssistantQueueStates] = useState<Record<string, AssistantQueueButtonState>>({})
  const [pendingAssistantRunStarts, setPendingAssistantRunStarts] = useState<Record<string, boolean>>({})
  const [assistantRunStates, setAssistantRunStates] = useState<Record<string, AssistantRunButtonState>>({})
  const [jobsProgress, setJobsProgress] = useState<JobsProgressResponse | null>(null)
  const jobsRequestRef = useRef(0)
  const jobsProgressPollRef = useRef<number | null>(null)
  const hasJobsDataRef = useRef(false)

  const companies = state.data?.filters.companies ?? []
  const sourceOptions = state.data?.filters.source_options ?? []
  const postedOptions = state.data?.filters.posted_options ?? ['all', '24h', '48h', '7d']
  const eVerifyOptions = state.data?.filters.everify_options ?? ['all', 'enrolled', 'unknown', 'not_found', 'not_enrolled']

  const stopJobsProgressPolling = useCallback(() => {
    if (jobsProgressPollRef.current === null) return
    window.clearInterval(jobsProgressPollRef.current)
    jobsProgressPollRef.current = null
  }, [])

  const pollJobsProgress = useCallback(async (requestID: number) => {
    try {
      const progress = await api.getJobsProgress()
      if (jobsRequestRef.current !== requestID) return
      setJobsProgress(progress)
    } catch {
      // Progress polling is best-effort; job data request still drives final state.
    }
  }, [])

  const loadJobs = useCallback(
    async (overrides?: JobsLoadOverrides, options?: JobsLoadOptions) => {
      const effectiveQ = overrides?.q ?? q
      const effectiveCompany = overrides?.company ?? company
      const effectiveSource = overrides?.source ?? source
      const effectivePostedWithin = overrides?.postedWithin ?? postedWithin
      const effectiveEverify = overrides?.everify ?? everify
      const effectiveSort = overrides?.sort ?? sort
      const effectiveLimit = overrides?.limit ?? limit
      const useSilentUI = Boolean(options?.silent) && hasJobsDataRef.current

      const requestID = Date.now()
      jobsRequestRef.current = requestID
      stopJobsProgressPolling()
      if (useSilentUI) {
        setState((prev) => ({ ...prev, error: '' }))
      } else {
        setState((prev) => ({ loading: true, error: '', data: prev.data }))
        setJobsProgress((previous) => ({
          run_id: previous?.run_id,
          running: true,
          phase: previous?.phase || 'starting',
          message: previous?.message || 'Preparing jobs feed',
          progress_percent: previous?.progress_percent ?? 0,
          total_jobs: previous?.total_jobs ?? 0,
          filtered_jobs: previous?.filtered_jobs ?? 0,
          scoring: previous?.scoring ?? {
            running: false,
            scheduled_jobs: 0,
            queued_jobs: 0,
            completed_jobs: 0,
            success_jobs: 0,
            failed_jobs: 0,
          },
        }))
        void pollJobsProgress(requestID)
        jobsProgressPollRef.current = window.setInterval(() => {
          void pollJobsProgress(requestID)
        }, 350)
      }

      try {
        const data = await api.getJobs({
          q: effectiveQ,
          company: effectiveCompany,
          source: effectiveSource,
          postedWithin: effectivePostedWithin,
          everify: effectiveEverify,
          sort: effectiveSort,
          limit: effectiveLimit,
        })
        if (jobsRequestRef.current !== requestID) return
        setState({ loading: false, error: '', data })
        if (!useSilentUI) {
          await pollJobsProgress(requestID)
        }
      } catch (error) {
        if (jobsRequestRef.current !== requestID) return
        setState((prev) => ({
          loading: false,
          data: prev.data,
          error: error instanceof Error ? error.message : String(error),
        }))
      } finally {
        if (jobsRequestRef.current === requestID) {
          stopJobsProgressPolling()
          if (!useSilentUI) {
            setJobsProgress((previous) => {
              if (!previous) return previous
              return {
                ...previous,
                running: false,
              }
            })
          }
        }
      }
    },
    [company, everify, limit, pollJobsProgress, postedWithin, q, sort, source, stopJobsProgressPolling],
  )

  const applyHeaderSort = useCallback(
    (target: 'best_match' | 'company' | 'title' | 'posted') => {
      let nextSort: JobsSortMode = sort
      if (target === 'posted') {
        nextSort = sort === 'newest' ? 'oldest' : 'newest'
      } else if (target === 'best_match') {
        nextSort = 'best_match'
      } else {
        nextSort = target
      }
      if (nextSort === sort && target !== 'posted') return
      setSort(nextSort)
      void loadJobs({ sort: nextSort }, { silent: true })
    },
    [loadJobs, sort],
  )

  const renderPostedCell = useCallback((job: JobsResponse['jobs'][number]) => {
    const postedLabel = formatPostedDisplay(job.posted_at_local, job.posted_at)
    if (postedLabel !== '-') {
      return <span className="text-[13px] font-medium text-slate-700">{postedLabel}</span>
    }
    return (
      <div className="space-y-1">
        <div>Unknown</div>
        <div className="text-xs text-slate-500">Discovered {firstSeenDisplay(job.first_seen_local, job.first_seen)}</div>
      </div>
    )
  }, [])

  useEffect(() => {
    loadJobs()
    const timer = window.setInterval(loadJobs, 20000)
    return () => {
      window.clearInterval(timer)
      stopJobsProgressPolling()
      jobsRequestRef.current = jobsRequestRef.current + 1
    }
  }, [loadJobs, stopJobsProgressPolling])

  useEffect(() => {
    setOpenMatchReasonKey('')
  }, [state.data?.summary.generated_at])

  useEffect(() => {
    hasJobsDataRef.current = Boolean(state.data)
  }, [state.data])

  const summary = state.data?.summary
  const jobs = state.data?.jobs ?? []
  const loadProgressPercent = Math.max(0, Math.min(100, Math.round(jobsProgress?.progress_percent ?? 0)))
  const loadPhase = (jobsProgress?.phase || 'starting').replace(/_/g, ' ')
  const scoring = jobsProgress?.scoring

  const updateApplicationStatus = useCallback(async (job: JobRow, nextStatus: ApplicationStatus) => {
    const fingerprint = (job.fingerprint || '').trim()
    if (!fingerprint) {
      setState((prev) => ({
        ...prev,
        error: 'This job cannot be updated because it is missing a database fingerprint.',
      }))
      return
    }

    const previousStatus = normalizeApplicationStatus(job.application_status)
    const previousUpdatedAt = (job.application_updated_at || '').trim()
    const requestedStatus: ApplicationStatus = previousStatus === nextStatus ? '' : nextStatus

    setPendingApplicationUpdates((prev) => ({ ...prev, [fingerprint]: true }))
    setState((prev) => {
      if (!prev.data) return prev
      return {
        ...prev,
        error: '',
        data: {
          ...prev.data,
          jobs: prev.data.jobs.map((item) =>
            item.fingerprint === fingerprint
              ? {
                  ...item,
                  application_status: requestedStatus,
                }
              : item,
          ),
        },
      }
    })

    try {
      const response = await api.updateJobApplicationStatus({ fingerprint, status: requestedStatus })
      setState((prev) => {
        if (!prev.data) return prev
        return {
          ...prev,
          data: {
            ...prev.data,
            jobs: prev.data.jobs.map((item) =>
              item.fingerprint === response.fingerprint
                ? {
                    ...item,
                    application_status: normalizeApplicationStatus(response.status),
                    application_updated_at: response.updated_at || '',
                  }
                : item,
            ),
          },
        }
      })
    } catch (error) {
      setState((prev) => {
        if (!prev.data) {
          return {
            loading: false,
            data: null,
            error: error instanceof Error ? error.message : String(error),
          }
        }
        return {
          ...prev,
          error: error instanceof Error ? error.message : String(error),
          data: {
            ...prev.data,
            jobs: prev.data.jobs.map((item) =>
              item.fingerprint === fingerprint
                ? {
                    ...item,
                    application_status: previousStatus,
                    application_updated_at: previousUpdatedAt,
                  }
                : item,
            ),
          },
        }
      })
    } finally {
      setPendingApplicationUpdates((prev) => {
        const next = { ...prev }
        delete next[fingerprint]
        return next
      })
    }
  }, [])

  const queueAssistantJob = useCallback(async (job: JobRow) => {
    const fingerprint = (job.fingerprint || '').trim()
    if (!fingerprint) {
      setState((prev) => ({
        ...prev,
        error: 'This job cannot be queued because it is missing a database fingerprint.',
      }))
      return
    }
    if (!isSupportedGreenhouseJobURL(job.url || '')) {
      setState((prev) => ({
        ...prev,
        error: 'Only hosted Greenhouse job pages can be queued right now.',
      }))
      return
    }

    setPendingAssistantQueueUpdates((prev) => ({ ...prev, [fingerprint]: true }))
    try {
      const response = await api.enqueueAssistantJob(loadStoredObserverBaseUrl(), {
        url: job.url || '',
        company_name: job.company || '',
        job_title: job.title || '',
        fingerprint,
        first_seen: job.first_seen || '',
        source: job.source || '',
        application_status: normalizeApplicationStatus(job.application_status),
        assistant_last_outcome: job.assistant_last_outcome || '',
        assistant_last_source: job.assistant_last_source || '',
        assistant_last_review_pending_count: job.assistant_last_review_pending_count || 0,
        added_by: 'jobs_ui',
        skip_applied: true,
      })
      setAssistantQueueStates((prev) => ({
        ...prev,
        [fingerprint]:
          response.added_count > 0
            ? 'queued'
            : response.duplicate_count > 0
              ? 'duplicate'
              : response.skipped_count > 0
                ? 'skipped'
                : 'error',
      }))
      setState((prev) => ({ ...prev, error: '' }))
    } catch (error) {
      setAssistantQueueStates((prev) => ({ ...prev, [fingerprint]: 'error' }))
      setState((prev) => ({
        ...prev,
        error: error instanceof Error ? error.message : String(error),
      }))
    } finally {
      setPendingAssistantQueueUpdates((prev) => {
        const next = { ...prev }
        delete next[fingerprint]
        return next
      })
    }
  }, [])

  const startAssistantForJob = useCallback(async (job: JobRow) => {
    const fingerprint = (job.fingerprint || '').trim()
    if (!fingerprint) {
      setState((prev) => ({
        ...prev,
        error: 'This job cannot be started because it is missing a database fingerprint.',
      }))
      return
    }
    if (!isSupportedGreenhouseJobURL(job.url || '')) {
      setState((prev) => ({
        ...prev,
        error: 'Only hosted Greenhouse job pages can be started from the jobs page right now.',
      }))
      return
    }

    setPendingAssistantRunStarts((prev) => ({ ...prev, [fingerprint]: true }))
    try {
      const response = await api.startAssistantRun(loadStoredObserverBaseUrl(), {
        url: job.url || '',
        allow_submit: false,
        headless: false,
      })
      setAssistantRunStates((prev) => ({
        ...prev,
        [fingerprint]:
          response.status === 'running'
            ? 'running'
            : response.status === 'queued'
              ? 'queued'
              : response.status === 'failed'
                ? 'failed'
                : 'completed',
      }))
      setState((prev) => ({ ...prev, error: '' }))
    } catch (error) {
      setAssistantRunStates((prev) => ({ ...prev, [fingerprint]: 'failed' }))
      setState((prev) => ({
        ...prev,
        error: error instanceof Error ? error.message : String(error),
      }))
    } finally {
      setPendingAssistantRunStarts((prev) => {
        const next = { ...prev }
        delete next[fingerprint]
        return next
      })
    }
  }, [])

  const jobColumns = useMemo<ColumnDef<JobRow>[]>(
    () => [
      {
        id: 'company',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(sort === 'company')}
            onClick={() => applyHeaderSort('company')}
            aria-label="Sort by company"
          >
            Company
            <span className="text-[10px] font-medium tracking-normal opacity-80">{sort === 'company' ? 'A-Z' : ''}</span>
          </button>
        ),
        cell: ({ row }) => {
          const job = row.original
          const isMyGreenhouse = (job.company || '').trim().toLowerCase() === 'my greenhouse'
          const employer = (job.team || '').trim() || greenhouseEmployerFromURL(job.url || '')
          const companyDisplay = isMyGreenhouse && employer ? employer : job.company || '-'
          return (
            <div className="space-y-1">
              <div className="font-semibold text-slate-900">{companyDisplay}</div>
              {isMyGreenhouse && employer ? <div className="text-xs text-slate-500">via My Greenhouse</div> : null}
            </div>
          )
        },
      },
      {
        id: 'title',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(sort === 'title')}
            onClick={() => applyHeaderSort('title')}
            aria-label="Sort by title"
          >
            Title
            <span className="text-[10px] font-medium tracking-normal opacity-80">{sort === 'title' ? 'A-Z' : ''}</span>
          </button>
        ),
        cell: ({ row }) => {
          const job = row.original
          const fingerprint = (job.fingerprint || '').trim()
          const queueState = fingerprint ? assistantQueueStates[fingerprint] : undefined
          const queuePending = Boolean(fingerprint && pendingAssistantQueueUpdates[fingerprint])
          const runState = fingerprint ? assistantRunStates[fingerprint] : undefined
          const runPending = Boolean(fingerprint && pendingAssistantRunStarts[fingerprint])
          const canQueue = isSupportedGreenhouseJobURL(job.url || '')
          const workAuthStatus = (job.work_auth_status || 'unknown').toLowerCase()
          const applied = normalizeApplicationStatus(job.application_status) === 'applied'
          const showAssistantActions = canQueue && !applied && workAuthStatus !== 'blocked'
          const queueLabel = queuePending
            ? 'Queueing...'
            : queueState === 'queued'
              ? 'Queued'
              : queueState === 'duplicate'
                ? 'In Queue'
                : queueState === 'skipped'
                  ? 'Applied'
                  : queueState === 'error'
                    ? 'Retry Queue'
                    : 'Queue'
          const runLabel = runPending
            ? 'Starting...'
            : runState === 'running'
              ? 'Running'
              : runState === 'queued'
                ? 'Queued'
                : runState === 'failed'
                  ? 'Retry Run'
                  : runState === 'completed'
                    ? 'Run Again'
                    : 'Run'
          return (
            <div className="space-y-2">
              <a
                className="inline-flex text-sm leading-5 font-semibold text-slate-900 underline-offset-4 hover:text-blue-700 hover:underline"
                href={job.url || '#'}
                target="_blank"
                rel="noreferrer"
              >
                {job.title || job.url || '-'}
              </a>
              <div className="flex flex-wrap items-center gap-1.5">
                {job.url ? (
                  <Button asChild size="sm" variant="outline" className="rounded-full">
                    <Link to={`/assistant?url=${encodeURIComponent(job.url)}`}>Assistant</Link>
                  </Button>
                ) : null}
                {showAssistantActions ? (
                  <button
                    type="button"
                    className={cn(
                      'inline-flex h-[30px] items-center rounded-full border px-3 text-sm font-medium transition disabled:pointer-events-none disabled:opacity-60',
                      inlineActionButtonClass(runState),
                    )}
                    disabled={runPending || runState === 'running' || runState === 'queued'}
                    onClick={() => void startAssistantForJob(job)}
                  >
                    {runLabel}
                  </button>
                ) : null}
                {showAssistantActions ? (
                  <button
                    type="button"
                    className={cn(
                      'inline-flex h-[30px] items-center rounded-full border px-3 text-sm font-medium transition disabled:pointer-events-none disabled:opacity-60',
                      inlineActionButtonClass(queueState),
                    )}
                    disabled={queuePending || applied || queueState === 'queued' || queueState === 'duplicate' || queueState === 'skipped'}
                    onClick={() => void queueAssistantJob(job)}
                  >
                    {queueLabel}
                  </button>
                ) : null}
              </div>
              <div className="text-[12px] leading-5 text-slate-500">
                {job.recommended_resume ? `SLM resume: ${job.recommended_resume}` : 'SLM chooses the resume automatically when you queue or run the job.'}
              </div>
            </div>
          )
        },
      },
      {
        id: 'eligibility',
        header: 'Your Eligibility',
        cell: ({ row }) => {
          const job = row.original
          const workAuthStatus = (job.work_auth_status || 'unknown').toLowerCase()
          const workAuthLabel =
            workAuthStatus === 'blocked'
              ? 'Not Eligible'
              : workAuthStatus === 'friendly'
                ? 'Eligible'
                : 'Needs Review'
          const notes = [...(job.work_auth_notes?.slice(0, 2) ?? [])]
          return (
            <div className="space-y-1.5">
              <div className={eligibilityBadgeClass(workAuthStatus)}>{workAuthLabel}</div>
              {notes.length ? <div className={subtleTextClass()}>{notes.slice(0, 2).join(' · ')}</div> : null}
            </div>
          )
        },
      },
      {
        id: 'posted',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(sort === 'newest' || sort === 'oldest')}
            onClick={() => applyHeaderSort('posted')}
            aria-label="Sort by posted time"
          >
            Posted
            <span className="text-[10px] font-medium tracking-normal opacity-80">
              {sort === 'newest' || sort === 'oldest' ? (sort === 'oldest' ? 'Oldest' : 'Newest') : ''}
            </span>
          </button>
        ),
        cell: ({ row }) => renderPostedCell(row.original),
      },
      {
        id: 'location',
        header: 'Location',
        cell: ({ row }) => row.original.location || '-',
      },
      {
        id: 'application',
        header: 'Applied?',
        cell: ({ row }) => {
          const job = row.original
          const fingerprint = (job.fingerprint || '').trim()
          const currentStatus = normalizeApplicationStatus(job.application_status)
          const assistantState = assistantBadge(job)
          const isPending = Boolean(fingerprint && pendingApplicationUpdates[fingerprint])
          const disabled = !fingerprint || isPending
          const updatedAt = (job.application_updated_at || '').trim()
          return (
            <div className="space-y-2">
              {assistantState ? (
                <div className={assistantBadgeClass(assistantState.tone)} title={assistantState.title}>
                  {assistantState.label}
                </div>
              ) : null}
              <div className="flex flex-wrap gap-1.5">
                <button
                  type="button"
                  className={cn(
                    'inline-flex h-[30px] items-center rounded-full border px-3 text-sm font-medium transition disabled:pointer-events-none disabled:opacity-60',
                    applicationStatusButtonClass(currentStatus === 'applied', 'applied'),
                  )}
                  disabled={disabled}
                  onClick={() => void updateApplicationStatus(job, 'applied')}
                >
                  Applied
                </button>
                <button
                  type="button"
                  className={cn(
                    'inline-flex h-[30px] items-center rounded-full border px-3 text-sm font-medium transition disabled:pointer-events-none disabled:opacity-60',
                    applicationStatusButtonClass(currentStatus === 'not_applied', 'not_applied'),
                  )}
                  disabled={disabled}
                  onClick={() => void updateApplicationStatus(job, 'not_applied')}
                >
                  Not Applied
                </button>
              </div>
              <div className={subtleTextClass()}>
                {isPending ? 'Saving...' : updatedAt ? `Saved ${formatTimestamp(updatedAt)}` : 'Not set'}
              </div>
            </div>
          )
        },
      },
      {
        id: 'match',
        header: () => (
          <button
            type="button"
            className={sortableHeaderButtonClass(sort === 'best_match')}
            onClick={() => applyHeaderSort('best_match')}
            aria-label="Sort by best match"
          >
            Match
            <span className="text-[10px] font-medium tracking-normal opacity-80">{sort === 'best_match' ? 'Best' : ''}</span>
          </button>
        ),
        cell: ({ row }) => {
          const job = row.original
          const hasMatchReasons = Boolean(job.match_reasons?.length)
          const showMatchReason = hasMatchReasons && openMatchReasonKey === row.id
          const decisionBits = [
            decisionSourceLabel(job.decision_source),
            roleDecisionLabel(job.role_decision),
            internshipDecisionLabel(job.internship_decision),
          ].filter(Boolean)
          return (
            <div className="relative flex flex-col items-end gap-1.5">
              <button
                type="button"
                className={cn(
                  'inline-flex min-w-16 items-center justify-center rounded-full border px-2.5 py-1 text-sm font-semibold transition disabled:pointer-events-none disabled:opacity-60',
                  matchBadgeClass(job.match_score),
                )}
                onClick={() =>
                  setOpenMatchReasonKey((current) => {
                    if (!hasMatchReasons) return ''
                    return current === row.id ? '' : row.id
                  })
                }
                disabled={!hasMatchReasons}
                aria-expanded={showMatchReason}
                aria-label={showMatchReason ? 'Hide match reasons' : 'Show match reasons'}
              >
                {Math.max(0, Math.min(100, Math.round(job.match_score ?? 0)))}%
              </button>
              {job.recommended_resume ? <div className="text-xs font-medium text-slate-700">{job.recommended_resume}</div> : null}
              {decisionBits.length ? <div className="max-w-[14rem] text-right text-[11px] leading-4 text-slate-500">{decisionBits.join(' · ')}</div> : null}
              {showMatchReason ? (
                <div
                  className="absolute top-[calc(100%+0.5rem)] right-0 z-10 w-72 rounded-xl border border-slate-200 bg-white p-3.5 text-left shadow-[0_24px_60px_-32px_rgba(15,23,42,0.5)]"
                  role="note"
                >
                  {job.match_reasons?.slice(0, 4).map((reason, reasonIndex) => (
                    <div className="text-sm leading-5 text-slate-600" key={`${row.id}-reason-${reasonIndex}`}>
                      {reason}
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          )
        },
      },
    ],
    [
      applyHeaderSort,
      assistantQueueStates,
      assistantRunStates,
      openMatchReasonKey,
      pendingApplicationUpdates,
      pendingAssistantQueueUpdates,
      pendingAssistantRunStarts,
      queueAssistantJob,
      renderPostedCell,
      sort,
      startAssistantForJob,
      updateApplicationStatus,
    ],
  )

  const jobsTable = useReactTable({
    data: jobs,
    columns: jobColumns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row, index) => (row.fingerprint || '').trim() || `${(row.company || '').trim()}-${(row.url || '').trim()}-${index}`,
  })

  const headerText = useMemo(() => {
    if (state.loading) {
      const message = jobsProgress?.message?.trim() || `Processing jobs feed (${loadPhase})`
      return `${message} (${loadProgressPercent}%)`
    }
    if (!summary) return 'Loading jobs...'
    return `${summary.filtered_jobs} visible of ${summary.total_jobs} active jobs · Updated ${summary.generated_at || '-'}`
  }, [jobsProgress?.message, loadPhase, loadProgressPercent, state.loading, summary])

  return (
    <DashboardPage className="min-h-[100dvh] md:h-[100dvh] md:overflow-hidden md:px-3 md:py-3" contentClassName="max-w-none gap-3 md:h-full md:min-h-0">
      <div className="relative z-20 shrink-0">
        <SurfaceCard className="rounded-[18px]">
          <SurfaceCardContent className="flex flex-wrap items-center justify-between gap-2 px-3 py-3">
            <div className="flex min-w-0 flex-wrap items-center gap-2.5">
              <div className="flex size-8 items-center justify-center rounded-xl border border-blue-200/80 bg-blue-600 text-white shadow-sm">
                <BriefcaseBusiness className="size-4" />
              </div>
              <div className="text-[1.1rem] leading-none font-semibold tracking-tight text-slate-950">Jobs</div>
              <Badge variant="outline" className="rounded-full border-slate-200 bg-white px-3 py-1 text-slate-600">
                {headerText}
              </Badge>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button asChild size="sm" variant="outline" className="rounded-full">
                <Link to="/assistant">
                  <Bot className="size-4" />
                  Assistant
                </Link>
              </Button>
              <Button asChild size="sm" variant="outline" className="rounded-full">
                <Link to="/mail">
                  <MailPlus className="size-4" />
                  Mail Analytics
                </Link>
              </Button>
              <Button asChild size="sm" variant="outline" className="rounded-full">
                <Link to="/monitor">
                  <Radar className="size-4" />
                  Crawler Monitor
                </Link>
              </Button>
              <Button size="sm" variant="outline" className="rounded-full" onClick={() => setFiltersExpanded((current) => !current)}>
                {filtersExpanded ? 'Hide Filters' : 'Filters'}
              </Button>
              <Button size="sm" variant="outline" className="rounded-full" onClick={() => void loadJobs()} disabled={state.loading}>
                <RefreshCw className="size-4" />
                Refresh
              </Button>
            </div>
          </SurfaceCardContent>
        </SurfaceCard>

        {filtersExpanded ? (
          <div className="absolute top-[calc(100%+0.5rem)] right-0 left-0 z-30">
            <SurfaceCard className="rounded-[18px] shadow-[0_32px_80px_-40px_rgba(15,23,42,0.45)]">
              <SurfaceCardContent className="space-y-3 px-3 py-3">
                <div className="grid gap-2.5 md:grid-cols-2 xl:grid-cols-[minmax(0,2fr)_repeat(6,minmax(120px,1fr))]">
                  <FieldGroup label="Search">
                    <NativeInput
                      className="h-9"
                      placeholder="Search company, title, description, URL, or location..."
                      value={q}
                      onChange={(event) => setQ(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter') {
                          void loadJobs()
                          setFiltersExpanded(false)
                        }
                      }}
                    />
                  </FieldGroup>
                  <FieldGroup label="Company">
                    <NativeSelect className="h-9" value={company} onChange={(event) => setCompany(event.target.value)}>
                      <option value="">All Companies</option>
                      {companies.map((item) => (
                        <option value={item} key={item}>
                          {item}
                        </option>
                      ))}
                    </NativeSelect>
                  </FieldGroup>
                  <FieldGroup label="Source">
                    <NativeSelect className="h-9" value={source} onChange={(event) => setSource(event.target.value)}>
                      <option value="">Source: All</option>
                      {sourceOptions.map((option) => (
                        <option key={option} value={option}>
                          Source: {sourceLabel(option)}
                        </option>
                      ))}
                    </NativeSelect>
                  </FieldGroup>
                  <FieldGroup label="Posted">
                    <NativeSelect
                      className="h-9"
                      value={postedWithin || 'all'}
                      onChange={(event) => setPostedWithin(event.target.value === 'all' ? '' : event.target.value)}
                    >
                      {postedOptions.map((option) => (
                        <option key={option} value={option}>
                          Posted: {postedWithinLabel(option)}
                        </option>
                      ))}
                    </NativeSelect>
                  </FieldGroup>
                  <FieldGroup label="E-Verify">
                    <NativeSelect className="h-9" value={everify || 'all'} onChange={(event) => setEverify(event.target.value === 'all' ? '' : event.target.value)}>
                      {eVerifyOptions.map((option) => (
                        <option key={option} value={option}>
                          E-Verify: {option === 'all' ? 'All' : eVerifyLabel(option)}
                        </option>
                      ))}
                    </NativeSelect>
                  </FieldGroup>
                  <FieldGroup label="Sort">
                    <NativeSelect className="h-9" value={sort} onChange={(event) => setSort(event.target.value as typeof sort)}>
                      <option value="best_match">Best Match</option>
                      <option value="newest">Newest First</option>
                      <option value="oldest">Oldest First</option>
                      <option value="company">Company A-Z</option>
                      <option value="title">Title A-Z</option>
                    </NativeSelect>
                  </FieldGroup>
                  <FieldGroup label="Limit">
                    <NativeSelect className="h-9" value={limit} onChange={(event) => setLimit(Number(event.target.value))}>
                      <option value={200}>200</option>
                      <option value={500}>500</option>
                      <option value={1000}>1000</option>
                    </NativeSelect>
                  </FieldGroup>
                </div>
                <div className="flex flex-wrap items-center gap-2.5">
                  <Button size="sm" className="rounded-full" onClick={() => {
                    void loadJobs()
                    setFiltersExpanded(false)
                  }} disabled={state.loading}>
                    Apply Filters
                  </Button>
                  <Badge variant="outline" className="rounded-full border-blue-200 bg-blue-50 px-3 py-1 text-blue-700">
                    SLM picks the resume automatically
                  </Badge>
                  <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                    {summary?.filtered_jobs ?? 0} visible jobs
                  </Badge>
                  <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                    {summary?.total_jobs ?? 0} active jobs
                  </Badge>
                  <Badge variant="outline" className="rounded-full border-slate-200 bg-slate-100 px-3 py-1 text-slate-600">
                    {summary?.companies_count ?? 0} companies
                  </Badge>
                </div>
              </SurfaceCardContent>
            </SurfaceCard>
          </div>
        ) : null}
      </div>

      {state.error ? <InlineAlert>{state.error}</InlineAlert> : null}

      {state.loading ? (
        <SurfaceCard aria-live="polite" className="shrink-0 rounded-[16px]">
          <SurfaceCardContent className="space-y-2 px-3 py-3">
            <div className="flex flex-wrap items-center justify-between gap-3 text-sm text-slate-600">
              <div className="inline-flex items-center gap-2">
                <Workflow className="size-4 text-blue-600" />
                <span>{jobsProgress?.message?.trim() || 'Processing jobs feed'}</span>
              </div>
              <div className="inline-flex items-center gap-3">
                <span>{loadPhase}</span>
                <span className="font-semibold text-slate-950">{loadProgressPercent}%</span>
              </div>
            </div>
            <ProgressBar value={loadProgressPercent} indicatorClassName="bg-blue-600" />
            <div className="flex flex-wrap items-center gap-2 text-[12px] text-slate-500">
              <span>Jobs {jobsProgress?.filtered_jobs ?? 0}/{jobsProgress?.total_jobs ?? 0}</span>
              <span>{scoring?.scheduled_jobs ? `SLM ${scoring.completed_jobs}/${scoring.scheduled_jobs}` : 'SLM idle'}</span>
            </div>
          </SurfaceCardContent>
        </SurfaceCard>
      ) : null}

      <SurfaceCard className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-[18px]">
        <SurfaceCardContent className="flex min-h-0 flex-1 flex-col space-y-0 px-2 py-2">
          <TableShell className="min-h-0 flex-1 rounded-[16px]" viewportClassName="h-full">
              <table className="min-w-full text-left text-sm">
                <thead className="sticky top-0 z-10 bg-slate-50/95 backdrop-blur">
                  {jobsTable.getHeaderGroups().map((headerGroup) => (
                    <tr key={headerGroup.id}>
                      {headerGroup.headers.map((header) => (
                        <th
                          key={header.id}
                          className={cn(
                            'px-3 py-2 font-medium text-slate-500',
                            header.column.id === 'match' ? 'text-right' : '',
                          )}
                        >
                          {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                        </th>
                      ))}
                    </tr>
                  ))}
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {jobsTable.getRowModel().rows.map((row) => (
                    <tr key={row.id} className="bg-white/70 align-top">
                      {row.getVisibleCells().map((cell) => (
                        <td key={cell.id} className={cn('px-3 py-2.5 text-slate-700', jobsColumnClass(cell.column.id))}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      ))}
                    </tr>
                  ))}
                  {!jobsTable.getRowModel().rows.length ? (
                    <tr>
                      <td colSpan={jobColumns.length} className="px-3 py-10 text-center text-slate-500">
                        {state.loading ? 'Loading...' : 'No jobs match current filters.'}
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
          </TableShell>
        </SurfaceCardContent>
      </SurfaceCard>
    </DashboardPage>
  )
}

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Navigate to="/monitor" replace />} />
        <Route path="/monitor" element={<MonitorPage />} />
        <Route path="/jobs" element={<JobsPage />} />
        <Route path="/mail" element={<MailPage />} />
        <Route path="/assistant" element={<AssistantPage />} />
        <Route path="*" element={<Navigate to="/monitor" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
