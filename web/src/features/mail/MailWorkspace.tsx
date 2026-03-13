import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import { Link } from 'react-router-dom'
import {
  Bot,
  BriefcaseBusiness,
  Building2,
  CalendarClock,
  ExternalLink,
  MessageSquareReply,
  ShieldCheck,
  RefreshCw,
  Rows3,
  ShieldAlert,
} from 'lucide-react'

import {
  api,
  type CrawlScheduleResponse,
  type MailAccount,
  type MailAnalyticsResponse,
  type MailLifecycleApplicationItem,
  type MailLifecycleRejectionItem,
  type MailMessageDetailResponse,
  type MailMeetingItem,
  type MailMessagesResponse,
  type MailOpenActionItem,
  type MailRunStatusResponse,
} from '@/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'

type MailMessagesState = {
  loading: boolean
  data: MailMessagesResponse | null
}

type MailAnalyticsState = {
  loading: boolean
  error: string
  data: MailAnalyticsResponse | null
}

type MailMessage = MailMessagesResponse['messages'][number]
type LifecyclePreviewKind = 'open_applications' | 'open_actions' | 'upcoming_meetings' | 'unresolved_rejections'
type DetailPanelKind =
  | 'mailbox_state'
  | 'lifecycle_preview'
  | 'company_pulse'
  | 'open_applications'
  | 'resolved_rejections'
  | 'open_actions'
  | 'unresolved_rejections'
  | 'upcoming_meetings'
type OverlayState =
  | { kind: 'panel'; panel: DetailPanelKind }
  | { kind: 'message'; messageId: number; preview?: MailMessage | null }
  | null
type MessageDetailState = {
  loading: boolean
  error: string
  data: MailMessageDetailResponse['message'] | null
}
type MailProviderKey = 'gmail'
type CompanyPulseRow = {
  company: string
  total: number
  applications: number
  rejections: number
  openActions: number
  lastReceivedAt?: string
}
type Tone = 'slate' | 'emerald' | 'rose' | 'amber' | 'blue'

const MESSAGE_LIMIT = 250
const SCHEDULE_OPTIONS = [10, 30, 60]

const toneStyles: Record<
  Tone,
  {
    card: string
    icon: string
    number: string
    badge: string
    soft: string
  }
> = {
  slate: {
    card: 'border-slate-200/80 bg-white/92',
    icon: 'border-slate-200 bg-slate-50 text-slate-700',
    number: 'text-slate-950',
    badge: 'border-slate-200 bg-slate-50 text-slate-700',
    soft: 'border-slate-200/80 bg-slate-50/85',
  },
  emerald: {
    card: 'border-emerald-200/80 bg-linear-to-br from-emerald-50/90 via-white to-white',
    icon: 'border-emerald-200 bg-emerald-50 text-emerald-700',
    number: 'text-emerald-800',
    badge: 'border-emerald-200 bg-emerald-50 text-emerald-700',
    soft: 'border-emerald-200/80 bg-emerald-50/70',
  },
  rose: {
    card: 'border-rose-200/80 bg-linear-to-br from-rose-50/90 via-white to-white',
    icon: 'border-rose-200 bg-rose-50 text-rose-700',
    number: 'text-rose-800',
    badge: 'border-rose-200 bg-rose-50 text-rose-700',
    soft: 'border-rose-200/80 bg-rose-50/70',
  },
  amber: {
    card: 'border-amber-200/80 bg-linear-to-br from-amber-50/90 via-white to-white',
    icon: 'border-amber-200 bg-amber-50 text-amber-700',
    number: 'text-amber-800',
    badge: 'border-amber-200 bg-amber-50 text-amber-700',
    soft: 'border-amber-200/80 bg-amber-50/70',
  },
  blue: {
    card: 'border-blue-200/80 bg-linear-to-br from-blue-50/90 via-white to-white',
    icon: 'border-blue-200 bg-blue-50 text-blue-700',
    number: 'text-blue-800',
    badge: 'border-blue-200 bg-blue-50 text-blue-700',
    soft: 'border-blue-200/80 bg-blue-50/70',
  },
}

function parseDate(value?: string): Date | null {
  if (!value) return null
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return null
  return parsed
}

function formatTimestamp(value?: string): string {
  if (!value) return '-'
  const parsed = parseDate(value)
  if (!parsed) return value
  return parsed.toLocaleString()
}

function formatRelativeTimestamp(value?: string): string {
  if (!value) return '-'
  const parsed = parseDate(value)
  if (!parsed) return value

  const now = new Date()
  const isToday =
    parsed.getFullYear() === now.getFullYear() &&
    parsed.getMonth() === now.getMonth() &&
    parsed.getDate() === now.getDate()
  if (isToday) {
    return `Today at ${parsed.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}`
  }

  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
  const isYesterday =
    parsed.getFullYear() === yesterday.getFullYear() &&
    parsed.getMonth() === yesterday.getMonth() &&
    parsed.getDate() === yesterday.getDate()
  if (isYesterday) {
    return `Yesterday at ${parsed.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}`
  }

  return parsed.toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

function formatMeetingWindow(values: { meeting_start?: string; meeting_end?: string }): string {
  const start = parseDate(values.meeting_start)
  const end = parseDate(values.meeting_end)
  if (!start && !end) return 'Invite attached'
  const startText = start
    ? start.toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
    : '-'
  if (!end) return startText
  return `${startText} to ${end.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}`
}

function subjectLabel(value?: string): string {
  const normalized = (value || '').replace(/\s+/g, ' ').trim()
  if (!normalized) return '(No subject)'
  return normalized.replace(/\s*Summarize\s*$/i, '').trim() || '(No subject)'
}

function senderFallback(value?: string): string {
  const trimmed = (value || '').trim()
  if (!trimmed) return 'Unknown sender'
  const domain = trimmed.includes('@') ? trimmed.split('@')[1] || trimmed : trimmed
  const root = domain.split('.')[0] || domain
  return root
    .split(/[-_]+/)
    .filter(Boolean)
    .map((part) => (part.length <= 3 ? part.toUpperCase() : `${part[0].toUpperCase()}${part.slice(1)}`))
    .join(' ')
}

function companyLabel(message: MailMessage): string {
  return (message.matched_company || '').trim() || (message.sender.name || '').trim() || senderFallback(message.sender.email)
}

function eventLabel(value?: string): string {
  switch ((value || '').trim().toLowerCase()) {
    case 'application_acknowledged':
      return 'Application Logged'
    case 'recruiter_reply':
      return 'Recruiter Reply'
    case 'recruiter_outreach':
      return 'Recruiter Outreach'
    case 'india_job_market':
      return 'India Market'
    case 'job_board_invite':
      return 'Job Invite'
    case 'interview_scheduled':
      return 'Interview Scheduled'
    case 'interview_updated':
      return 'Interview Updated'
    case 'rejection':
      return 'Rejection'
    default:
      return 'Signal'
  }
}

function messageTone(message: MailMessage): Tone {
  switch ((message.event_type || '').trim().toLowerCase()) {
    case 'application_acknowledged':
      return 'emerald'
    case 'rejection':
      return 'rose'
    case 'india_job_market':
    case 'job_board_invite':
      return 'slate'
    case 'interview_scheduled':
    case 'interview_updated':
      return 'blue'
    case 'recruiter_reply':
    case 'recruiter_outreach':
      return 'amber'
    default:
      return 'slate'
  }
}

function isSignalMessage(message: MailMessage): boolean {
  switch ((message.event_type || '').trim().toLowerCase()) {
    case 'application_acknowledged':
    case 'recruiter_reply':
    case 'recruiter_outreach':
    case 'interview_scheduled':
    case 'interview_updated':
    case 'rejection':
      return true
    default:
      return false
  }
}

function isOpenActionMessage(message: MailMessage): boolean {
  const triage = (message.triage_status || '').trim().toLowerCase()
  if (triage === 'ignored' || triage === 'reviewed') return false
  if (triage === 'follow_up') return true
  const eventType = (message.event_type || '').trim().toLowerCase()
  return (eventType === 'recruiter_reply' || eventType === 'recruiter_outreach' || eventType === 'interview_scheduled' || eventType === 'interview_updated') &&
    (triage === 'new' || triage === 'important')
}

function visibleMailReasons(message?: Pick<MailMessage, 'event_type' | 'reasons'> | null): string[] {
  const reasons = Array.isArray(message?.reasons) ? message.reasons.filter(Boolean) : []
  const eventType = (message?.event_type || '').trim().toLowerCase()
  if (eventType === 'recruiter_reply' || eventType === 'recruiter_outreach') {
    return reasons
  }
  return reasons.filter((reason) => {
    const normalized = reason.trim().toLowerCase()
    return normalized !== 'recruiter reply or outreach signal detected' &&
      normalized !== 'direct recruiter reply signal detected' &&
      normalized !== 'direct recruiter outreach signal detected'
  })
}

function isWithinLast7Days(value?: string): boolean {
  const parsed = parseDate(value)
  if (!parsed) return false
  const now = new Date()
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const startDay = new Date(parsed.getFullYear(), parsed.getMonth(), parsed.getDate())
  const diffDays = Math.floor((startToday.getTime() - startDay.getTime()) / (24 * 60 * 60 * 1000))
  return diffDays >= 0 && diffDays <= 6
}

function scheduleIntervalLabel(intervalMinutes: number): string {
  const value = Math.max(5, Math.floor(intervalMinutes || 0))
  if (value % 60 === 0) return `${value / 60}h`
  return `${value}m`
}

function providerLabel(provider: MailProviderKey): string {
  switch (provider) {
    case 'gmail':
    default:
      return 'Gmail'
  }
}

function providerBadgeTone(input: { connected: boolean; syncing: boolean; hasError: boolean }): Tone {
  if (input.syncing) return 'blue'
  if (input.hasError) return 'rose'
  if (input.connected) return 'emerald'
  return 'slate'
}

function runPhaseLabel(phase?: string): string {
  switch ((phase || '').trim().toLowerCase()) {
    case 'discovering':
      return 'Discovering'
    case 'hydrating':
      return 'Hydrating'
    case 'queued':
      return 'Queued'
    case 'running':
      return 'Syncing'
    default:
      return 'Syncing'
  }
}

function progressCountsText(progress?: NonNullable<MailRunStatusResponse['progress']>[number] | null): string {
  if (!progress) return 'Waiting for first batch'
  const parts: string[] = []
  if (progress.discovered > 0) parts.push(`${progress.discovered} discovered`)
  if (progress.hydrated > 0) parts.push(`${progress.hydrated} hydrated`)
  if (progress.stored > 0) parts.push(`${progress.stored} stored`)
  if (parts.length === 0 && (progress.fetched > 0 || progress.stored > 0)) {
    parts.push(`${progress.fetched} fetched`)
    if (progress.stored > 0) parts.push(`${progress.stored} stored`)
  }
  if (progress.cutoff_reached) parts.push('7d covered')
  if (progress.degraded_mode) parts.push('fallback mode')
  return parts.length ? parts.join(' · ') : progress.message || 'Waiting for first batch'
}

function hydrationStatusLabel(status?: string): string {
  switch ((status || '').trim().toLowerCase()) {
    case 'pending':
      return 'Body syncing'
    case 'failed':
      return 'Body sync failed'
    default:
      return ''
  }
}

function ProviderStatusPill({
  provider,
  status,
  detail,
  tone,
}: {
  provider: MailProviderKey
  status: string
  detail: string
  tone: Tone
}) {
  return (
    <div className={cn('min-w-[136px] rounded-2xl border px-3 py-2', toneStyles[tone].soft)}>
      <div className="flex items-center gap-2">
        <span className={cn('size-2 rounded-full', tone === 'emerald' ? 'bg-emerald-500' : tone === 'rose' ? 'bg-rose-500' : tone === 'blue' ? 'bg-blue-500' : 'bg-slate-400')} />
        <div className="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase">{providerLabel(provider)}</div>
        <Badge variant="outline" className={compactBadgeClass(tone)}>
          {status}
        </Badge>
      </div>
      <div className="mt-1 truncate text-xs text-slate-600">{detail}</div>
    </div>
  )
}

function ScopeSelect({
  label,
  value,
  onValueChange,
  options,
}: {
  label: string
  value: string
  onValueChange: (value: string) => void
  options: Array<{ value: string; label: string }>
}) {
  return (
    <div className="space-y-1">
      <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">{label}</div>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger className="h-8 rounded-xl border-slate-200 bg-white/90 text-sm shadow-none">
          <SelectValue placeholder={label} />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function CompactMetricTile({
  label,
  value,
  detail,
  tone = 'slate',
  onClick,
}: {
  label: string
  value: ReactNode
  detail?: ReactNode
  tone?: Tone
  onClick?: () => void
}) {
  const styles = toneStyles[tone]
  const body = (
    <div
      className={cn(
        'min-w-[148px] rounded-2xl border px-3 py-2 transition',
        styles.soft,
        onClick ? 'hover:-translate-y-0.5 hover:shadow-sm' : '',
      )}
    >
      <div className="text-[10px] font-semibold tracking-[0.18em] text-slate-500 uppercase">{label}</div>
      <div className={cn('mt-1 text-[0.98rem] font-semibold leading-tight tracking-tight', styles.number)}>{value}</div>
      {detail ? <div className="mt-1 truncate text-xs text-slate-500">{detail}</div> : null}
    </div>
  )

  if (!onClick) return body
  return (
    <button type="button" onClick={onClick} className="text-left">
      {body}
    </button>
  )
}

function compactBadgeClass(tone: Tone): string {
  return cn('rounded-full px-1.5 py-0.5 text-[10px] font-medium leading-none whitespace-nowrap', toneStyles[tone].badge)
}

function EventGlyph({ eventType, className }: { eventType?: string; className?: string }) {
  switch ((eventType || '').trim().toLowerCase()) {
    case 'application_acknowledged':
      return <BriefcaseBusiness className={className} />
    case 'recruiter_reply':
    case 'recruiter_outreach':
      return <MessageSquareReply className={className} />
    case 'india_job_market':
      return <Building2 className={className} />
    case 'job_board_invite':
      return <BriefcaseBusiness className={className} />
    case 'interview_scheduled':
    case 'interview_updated':
      return <CalendarClock className={className} />
    case 'rejection':
      return <ShieldAlert className={className} />
    default:
      return <Rows3 className={className} />
  }
}

function EventChip({ eventType }: { eventType?: string }) {
  const normalized = (eventType || '').trim().toLowerCase()
  const tone = (() => {
    switch (normalized) {
      case 'application_acknowledged':
        return 'emerald' as const
      case 'recruiter_reply':
      case 'recruiter_outreach':
        return 'amber' as const
      case 'india_job_market':
      case 'job_board_invite':
        return 'slate' as const
      case 'interview_scheduled':
      case 'interview_updated':
        return 'blue' as const
      case 'rejection':
        return 'rose' as const
      default:
        return 'slate' as const
    }
  })()
  return (
    <Badge variant="outline" className={compactBadgeClass(tone)}>
      <EventGlyph eventType={eventType} className="size-3" />
      {eventLabel(eventType)}
    </Badge>
  )
}

type ScrollableDataTableProps<TData extends object> = {
  data: TData[]
  columns: ColumnDef<TData>[]
  emptyTitle: string
  emptyDescription: string
  maxHeightClass?: string
  getRowId?: (row: TData, index: number) => string
  onRowClick?: (row: TData) => void
}

function ScrollableDataTable<TData extends object>({
  data,
  columns,
  emptyTitle,
  emptyDescription,
  maxHeightClass = 'max-h-[380px]',
  getRowId,
  onRowClick,
}: ScrollableDataTableProps<TData>) {
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId,
  })

  const columnCount = Math.max(columns.length, table.getAllLeafColumns().length)

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-[20px] border border-slate-200/80 bg-white/80">
      <div className={cn('min-h-0 flex-1 overflow-auto', maxHeightClass)}>
        <table className="min-w-full text-left text-[13px] leading-5">
          <thead className="sticky top-0 z-10 bg-slate-50/95 backdrop-blur">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th key={header.id} className="px-3 py-2 text-[10px] font-semibold tracking-[0.16em] text-slate-500 uppercase">
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody className="divide-y divide-slate-100">
            {table.getRowModel().rows.length ? (
              table.getRowModel().rows.map((row) => {
                const clickable = Boolean(onRowClick)
                return (
                  <tr
                    key={row.id}
                    className={cn(
                      'bg-white/80 transition',
                      clickable ? 'cursor-pointer hover:bg-slate-50/90' : '',
                    )}
                    onClick={clickable ? () => onRowClick?.(row.original) : undefined}
                    onKeyDown={
                      clickable
                        ? (event) => {
                            if (event.key === 'Enter' || event.key === ' ') {
                              event.preventDefault()
                              onRowClick?.(row.original)
                            }
                          }
                        : undefined
                    }
                    role={clickable ? 'button' : undefined}
                    tabIndex={clickable ? 0 : undefined}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td key={cell.id} className="px-3 py-2.5 align-middle text-slate-700">
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                )
              })
            ) : (
              <tr>
                <td colSpan={columnCount} className="px-4 py-10">
                  <EmptyPanel title={emptyTitle} description={emptyDescription} />
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function DetailModal({
  open,
  title,
  description,
  onClose,
  children,
}: {
  open: boolean
  title: string
  description?: string
  onClose: () => void
  children: React.ReactNode
}) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-950/50 p-3 backdrop-blur-sm sm:items-center sm:p-6" onClick={onClose}>
      <div
        className="flex max-h-[88vh] w-full max-w-3xl flex-col overflow-hidden rounded-[28px] border border-slate-200 bg-white shadow-[0_40px_120px_-42px_rgba(15,23,42,0.5)]"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-4">
          <div className="space-y-1">
            <div className="text-lg font-semibold text-slate-950">{title}</div>
            {description ? <div className="text-sm leading-6 text-slate-500">{description}</div> : null}
          </div>
          <Button type="button" variant="outline" size="sm" onClick={onClose}>
            Close
          </Button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>
  )
}

function EmptyPanel({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-3xl border border-dashed border-slate-200 bg-slate-50/80 px-5 py-8 text-center">
      <div className="space-y-2">
        <div className="text-base font-semibold text-slate-900">{title}</div>
        <div className="text-sm leading-6 text-slate-500">{description}</div>
      </div>
    </div>
  )
}

function previewPaneTitle(kind: LifecyclePreviewKind): string {
  switch (kind) {
    case 'open_applications':
      return 'Open Applications'
    case 'open_actions':
      return 'Open Actions'
    case 'upcoming_meetings':
      return 'Upcoming Meetings'
    case 'unresolved_rejections':
      return 'Unresolved Rejections'
    default:
      return 'Lifecycle Preview'
  }
}

export default function MailPage() {
  const [messagesState, setMessagesState] = useState<MailMessagesState>({ loading: true, data: null })
  const [analyticsState, setAnalyticsState] = useState<MailAnalyticsState>({ loading: true, error: '', data: null })
  const [error, setError] = useState('')
  const [accountsGeneratedAt, setAccountsGeneratedAt] = useState('')
  const [accounts, setAccounts] = useState<MailAccount[]>([])
  const [runStatus, setRunStatus] = useState<MailRunStatusResponse | null>(null)
  const [schedule, setSchedule] = useState<CrawlScheduleResponse | null>(null)
  const [notice, setNotice] = useState('')
  const [scheduleBusy, setScheduleBusy] = useState(false)
  const [runBusy, setRunBusy] = useState(false)
  const [connectBusy, setConnectBusy] = useState<'gmail' | ''>('')
  const [provider, setProvider] = useState('')
  const [accountId, setAccountId] = useState(0)
  const [company, setCompany] = useState('')
  const [showFilters, setShowFilters] = useState(false)
  const [previewPane, setPreviewPane] = useState<LifecyclePreviewKind>('open_applications')
  const [overlay, setOverlay] = useState<OverlayState>(null)
  const [messageDetail, setMessageDetail] = useState<MessageDetailState>({ loading: false, error: '', data: null })
  const loadSequenceRef = useRef(0)
  const lastRunStatusRef = useRef<MailRunStatusResponse | null>(null)

  const mailQuery = useMemo(
    () => ({
      provider,
      accountId: accountId || undefined,
      company,
    }),
    [accountId, company, provider],
  )

  const loadRunMeta = useCallback((requestId: number) => {
    void api
      .getMailRunStatus()
      .then((nextRunStatus) => {
        if (requestId !== loadSequenceRef.current) return
        setRunStatus(nextRunStatus)
        lastRunStatusRef.current = nextRunStatus
      })
      .catch(() => null)

    void api
      .getMailSchedule()
      .then((nextSchedule) => {
        if (requestId !== loadSequenceRef.current) return
        setSchedule(nextSchedule)
      })
      .catch(() => null)
  }, [])

  const loadMailView = useCallback(async () => {
    const requestId = loadSequenceRef.current + 1
    loadSequenceRef.current = requestId
    setError('')
    setMessagesState((previous) => ({ ...previous, loading: true }))
    setAnalyticsState((previous) => ({ ...previous, loading: true, error: '' }))
    loadRunMeta(requestId)

    let messages: MailMessagesResponse
    try {
      messages = await api.getMailMessages({
        ...mailQuery,
        limit: MESSAGE_LIMIT,
      })
    } catch (nextError) {
      if (requestId !== loadSequenceRef.current) return
      setMessagesState((previous) => ({ ...previous, loading: false }))
      setAnalyticsState((previous) => ({ ...previous, loading: false }))
      setError(nextError instanceof Error ? nextError.message : String(nextError))
      return
    }

    if (requestId !== loadSequenceRef.current) return
    setMessagesState({ loading: false, data: messages })

    const nextAccounts = await api.getMailAccounts().catch(() => null)
    if (requestId !== loadSequenceRef.current) return
    if (nextAccounts) {
      setAccounts(nextAccounts.accounts)
      setAccountsGeneratedAt(nextAccounts.generated_at || '')
    }

    try {
      const analytics = await api.getMailAnalytics(mailQuery)
      if (requestId !== loadSequenceRef.current) return
      setAnalyticsState({ loading: false, error: '', data: analytics })
    } catch (nextError) {
      if (requestId !== loadSequenceRef.current) return
      setAnalyticsState((previous) => ({
        ...previous,
        loading: false,
        error: nextError instanceof Error ? nextError.message : String(nextError),
      }))
    }
  }, [loadRunMeta, mailQuery])

  const refreshRunStatus = useCallback(async () => {
    try {
      const nextRunStatus = await api.getMailRunStatus()
      const previous = lastRunStatusRef.current
      setRunStatus(nextRunStatus)
      lastRunStatusRef.current = nextRunStatus
      const syncCompleted = Boolean(previous?.running) && !nextRunStatus.running
      const runFinished = !nextRunStatus.running && Boolean(nextRunStatus.last_end) && nextRunStatus.last_end !== previous?.last_end
      if (syncCompleted || runFinished) {
        void loadMailView()
      }
    } catch {
      // Keep the current status visible if polling fails.
    }
  }, [loadMailView])

  useEffect(() => {
    void loadMailView()
  }, [loadMailView])

  useEffect(() => {
    const pollMs = runStatus?.running ? 3000 : 15000
    const timer = window.setInterval(() => {
      void refreshRunStatus()
    }, pollMs)
    return () => window.clearInterval(timer)
  }, [refreshRunStatus, runStatus?.running])

  useEffect(() => {
    if (!overlay) return undefined
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOverlay(null)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [overlay])

  useEffect(() => {
    if (!overlay || overlay.kind !== 'message') {
      setMessageDetail({ loading: false, error: '', data: null })
      return
    }
    let cancelled = false
    setMessageDetail({ loading: true, error: '', data: overlay.preview ?? null })
    api
      .getMailMessageDetail(overlay.messageId)
      .then((response) => {
        if (cancelled) return
        setMessageDetail({ loading: false, error: '', data: response.message })
      })
      .catch((error) => {
        if (cancelled) return
        setMessageDetail((previous) => ({
          loading: false,
          data: previous.data,
          error: error instanceof Error ? error.message : String(error),
        }))
      })
    return () => {
      cancelled = true
    }
  }, [overlay])

  const updateSchedule = useCallback(async (enabled: boolean, intervalMinutes: number) => {
    setScheduleBusy(true)
    try {
      const next = await api.updateMailSchedule({ enabled, interval_minutes: intervalMinutes })
      setSchedule(next)
      setError('')
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error))
    } finally {
      setScheduleBusy(false)
    }
  }, [])

  const runSync = useCallback(async () => {
    setRunBusy(true)
    try {
      const response = await api.triggerMailRun()
      if (!response.ok) throw new Error(response.message || 'Mail sync did not start')
      setError('')
      await refreshRunStatus()
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error))
    } finally {
      setRunBusy(false)
    }
  }, [refreshRunStatus])

  const startConnect = useCallback(
    async (nextProvider: 'gmail') => {
      setConnectBusy(nextProvider)
      setNotice('')
      try {
        const response = await api.startMailConnect(nextProvider)
        if (!response.ok || !response.auth_url) {
          if (response.ok && response.message) {
            setNotice(response.message)
            setError('')
            await loadMailView()
            return
          }
          throw new Error(response.message || `Could not start ${nextProvider} connect flow`)
        }

        const opened = window.open(response.auth_url, '_blank', 'noopener,noreferrer')
        if (!opened) {
          window.location.href = response.auth_url
          return
        }
        setNotice(response.message || '')
        setError('')
      } catch (error) {
        setError(error instanceof Error ? error.message : String(error))
      } finally {
        setConnectBusy('')
      }
    },
    [loadMailView],
  )

  const scheduleStatus = schedule ?? { enabled: false, interval_minutes: 10 }
  const analytics = analyticsState.data
  const messages = messagesState.data?.messages

  const visibleAccounts = useMemo(() => {
    const fromMessages = messagesState.data?.filters.account_options ?? []
    const source = fromMessages.length ? fromMessages : accounts
    return source.filter((item) => item.provider === 'gmail' && (item.status || '').toLowerCase() === 'connected')
  }, [accounts, messagesState.data?.filters.account_options])

  const accountByProvider = useMemo(() => {
    const next = new Map<string, MailAccount>()
    accounts.forEach((item) => {
      if (!item.provider) return
      if (!next.has(item.provider)) next.set(item.provider, item)
    })
    return next
  }, [accounts])

  const runProgressByProvider = useMemo(() => {
    const next = new Map<string, NonNullable<MailRunStatusResponse['progress']>[number]>()
    ;(runStatus?.progress ?? []).forEach((item) => {
      if (!item.provider) return
      if (!next.has(item.provider)) next.set(item.provider, item)
    })
    return next
  }, [runStatus?.progress])

  const providerIndicators = useMemo(
    () =>
      (['gmail'] as MailProviderKey[]).map((providerName) => {
        const account = accountByProvider.get(providerName)
        const progress = runProgressByProvider.get(providerName)
        const connected = (account?.status || '').toLowerCase() === 'connected'
        const phase = (progress?.phase || '').trim().toLowerCase()
        const syncing = Boolean(runStatus?.running) && phase !== '' && phase !== 'done'
        const hasError = Boolean(account?.last_error)
        const tone = providerBadgeTone({ connected, syncing, hasError })
        const status = syncing ? runPhaseLabel(progress?.phase) : connected ? 'Connected' : 'Not Connected'
        const syncCounts = progressCountsText(progress)
        const detail = syncing
          ? `${account?.email || progress?.account_email || providerLabel(providerName)} · ${syncCounts}`
          : hasError
            ? account?.last_error || 'Connection error'
            : connected
              ? account?.last_sync_at
                ? `${account?.email || providerLabel(providerName)} · Last sync ${formatRelativeTimestamp(account.last_sync_at)}`
                : `${account?.email || providerLabel(providerName)} · Waiting for first sync`
              : 'No account connected'
        return {
          provider: providerName,
          connected,
          hasError,
          status,
          detail,
          tone,
        }
      }),
    [accountByProvider, runProgressByProvider, runStatus?.running],
  )

  const providerOptions = useMemo(() => {
    const hasGmail = visibleAccounts.length > 0 || accounts.some((item) => item.provider === 'gmail')
    return hasGmail ? ['gmail'] : []
  }, [accounts, visibleAccounts])

  const accountActionBusy = connectBusy !== ''
  const gmailConnected = providerIndicators[0]?.connected ?? false
  const gmailActionLabel = gmailConnected ? 'Reconnect Gmail' : 'Connect Gmail'

  useEffect(() => {
    if (provider && provider !== 'gmail') {
      setProvider('')
    }
  }, [provider])

  const companyOptions = messagesState.data?.filters.company_options ?? []

  const recentSignals = useMemo(
    () => (messages ?? []).filter(isSignalMessage).sort((left, right) => (parseDate(right.received_at)?.getTime() || 0) - (parseDate(left.received_at)?.getTime() || 0)),
    [messages],
  )

  const latestSignal = recentSignals[0] ?? null
  const companyPulse = useMemo(() => {
    const rows = new Map<string, CompanyPulseRow>()
    recentSignals
      .filter((message) => isWithinLast7Days(message.received_at))
      .forEach((message) => {
        const key = companyLabel(message)
        const row = rows.get(key) ?? {
          company: key,
          total: 0,
          applications: 0,
          rejections: 0,
          openActions: 0,
          lastReceivedAt: '',
        }
        row.total += 1
        if (message.event_type === 'application_acknowledged') row.applications += 1
        if (message.event_type === 'rejection') row.rejections += 1
        if (isOpenActionMessage(message)) row.openActions += 1
        if (!row.lastReceivedAt || (parseDate(message.received_at)?.getTime() || 0) > (parseDate(row.lastReceivedAt)?.getTime() || 0)) {
          row.lastReceivedAt = message.received_at
        }
        rows.set(key, row)
      })
    return Array.from(rows.values())
      .sort((left, right) => {
        if (right.total !== left.total) return right.total - left.total
        return (parseDate(right.lastReceivedAt)?.getTime() || 0) - (parseDate(left.lastReceivedAt)?.getTime() || 0)
      })
      .slice(0, 8)
  }, [recentSignals])

  const recentSignalColumns = useMemo<ColumnDef<MailMessage>[]>(
    () => [
      {
        id: 'signal',
        header: 'Signal',
        cell: ({ row }) => {
          const message = row.original
          const tone = messageTone(message)
          return (
            <div className="flex min-w-[9.5rem] items-center gap-2">
              <div className={cn('flex size-6 items-center justify-center rounded-lg border', toneStyles[tone].icon)}>
                <EventGlyph eventType={message.event_type} className="size-3.5" />
              </div>
              <div className="flex flex-wrap gap-1">
                <EventChip eventType={message.event_type} />
                {message.is_unread ? (
                  <Badge variant="outline" className={compactBadgeClass('slate')}>
                    New
                  </Badge>
                ) : null}
              </div>
            </div>
          )
        },
      },
      {
        id: 'company',
        header: 'Company',
        cell: ({ row }) => {
          const message = row.original
          return (
            <div className="min-w-[8.5rem]">
              <div className="font-medium text-slate-950">{companyLabel(message)}</div>
              <div className="truncate text-xs text-slate-500">{message.sender.name || message.sender.email || '-'}</div>
            </div>
          )
        },
      },
      {
        id: 'message',
        header: 'Message',
        cell: ({ row }) => {
          const message = row.original
          return (
            <div className="min-w-[18rem]">
              <div className="truncate font-medium text-slate-950">{subjectLabel(message.subject)}</div>
              <div className="truncate text-xs text-slate-500">{message.matched_job_title || 'Job title not matched yet'}</div>
            </div>
          )
        },
      },
      {
        id: 'flags',
        header: 'Flags',
        cell: ({ row }) => {
          const message = row.original
          return (
            <div className="flex min-w-[10rem] flex-wrap gap-1">
              {isOpenActionMessage(message) ? (
                <Badge variant="outline" className={compactBadgeClass('amber')}>
                  Action
                </Badge>
              ) : null}
              {message.has_invite ? (
                <Badge variant="outline" className={compactBadgeClass('blue')}>
                  Invite
                </Badge>
              ) : null}
              {message.importance ? (
                <Badge variant="outline" className={compactBadgeClass('slate')}>
                  Important
                </Badge>
              ) : null}
            </div>
          )
        },
      },
      {
        id: 'when',
        header: 'When',
        cell: ({ row }) => <div className="whitespace-nowrap text-xs text-slate-500">{formatRelativeTimestamp(row.original.received_at)}</div>,
      },
    ],
    [],
  )

  const openApplicationColumns = useMemo<ColumnDef<MailLifecycleApplicationItem>[]>(
    () => [
      {
        id: 'state',
        header: 'State',
        cell: ({ row }) => (
          <div className="flex min-w-[8.5rem] items-center gap-2">
            <div className={cn('flex size-6 items-center justify-center rounded-lg border', toneStyles.emerald.icon)}>
              <BriefcaseBusiness className="size-3.5" />
            </div>
            <div className="flex flex-wrap gap-1">
              <Badge variant="outline" className={compactBadgeClass('emerald')}>
                Active
              </Badge>
              {row.original.low_confidence ? (
                <Badge variant="outline" className={compactBadgeClass('amber')}>
                  Review
                </Badge>
              ) : null}
            </div>
          </div>
        ),
      },
      {
        id: 'company',
        header: 'Company',
        cell: ({ row }) => (
          <div className="min-w-[8rem]">
            <div className="font-medium text-slate-950">{row.original.company || 'Unmatched company'}</div>
            <div className="truncate text-xs text-slate-500">{row.original.sender || 'Sender unavailable'}</div>
          </div>
        ),
      },
      {
        id: 'job',
        header: 'Job',
        cell: ({ row }) => (
          <div className="min-w-[16rem]">
            <div className="truncate font-medium text-slate-950">{row.original.job_title || subjectLabel(row.original.subject)}</div>
            <div className="truncate text-xs text-slate-500">{subjectLabel(row.original.subject)}</div>
          </div>
        ),
      },
      {
        id: 'confidence',
        header: 'Confidence',
        cell: ({ row }) => (
          <Badge variant="outline" className={compactBadgeClass(row.original.low_confidence ? 'amber' : 'emerald')}>
            {Math.round((row.original.confidence || 0) * 100)}%
          </Badge>
        ),
      },
      {
        id: 'when',
        header: 'When',
        cell: ({ row }) => <div className="whitespace-nowrap text-xs text-slate-500">{formatRelativeTimestamp(row.original.received_at)}</div>,
      },
    ],
    [],
  )

  const resolvedRejectionColumns = useMemo<ColumnDef<MailLifecycleRejectionItem>[]>(
    () => [
      {
        id: 'state',
        header: 'State',
        cell: () => (
          <div className="flex min-w-[8.5rem] items-center gap-2">
            <div className={cn('flex size-6 items-center justify-center rounded-lg border', toneStyles.blue.icon)}>
              <ShieldCheck className="size-3.5" />
            </div>
            <Badge variant="outline" className={compactBadgeClass('blue')}>
              Resolved
            </Badge>
          </div>
        ),
      },
      {
        id: 'company',
        header: 'Company',
        cell: ({ row }) => (
          <div className="min-w-[8rem]">
            <div className="font-medium text-slate-950">{row.original.company || 'Matched rejection'}</div>
            <div className="truncate text-xs text-slate-500">{row.original.sender || 'Sender unavailable'}</div>
          </div>
        ),
      },
      {
        id: 'job',
        header: 'Closed Job',
        cell: ({ row }) => (
          <div className="min-w-[16rem]">
            <div className="truncate font-medium text-slate-950">{row.original.job_title || subjectLabel(row.original.subject)}</div>
            <div className="truncate text-xs text-slate-500">
              {row.original.matched_application || 'Matched by exact company and title'}
            </div>
          </div>
        ),
      },
      {
        id: 'match',
        header: 'Match',
        cell: ({ row }) => (
          <Badge variant="outline" className={compactBadgeClass('blue')}>
            {Math.round((row.original.confidence || 0) * 100)}%
          </Badge>
        ),
      },
      {
        id: 'when',
        header: 'When',
        cell: ({ row }) => <div className="whitespace-nowrap text-xs text-slate-500">{formatRelativeTimestamp(row.original.received_at)}</div>,
      },
    ],
    [],
  )

  const unresolvedRejectionColumns = useMemo<ColumnDef<MailLifecycleRejectionItem>[]>(
    () => [
      {
        id: 'state',
        header: 'State',
        cell: ({ row }) => (
          <div className="flex min-w-[9rem] items-center gap-2">
            <div className={cn('flex size-6 items-center justify-center rounded-lg border', toneStyles.rose.icon)}>
              <ShieldAlert className="size-3.5" />
            </div>
            <div className="flex flex-wrap gap-1">
              <Badge variant="outline" className={compactBadgeClass('rose')}>
                Rejection
              </Badge>
              {row.original.company_only_match ? (
                <Badge variant="outline" className={compactBadgeClass('amber')}>
                  Company only
                </Badge>
              ) : null}
            </div>
          </div>
        ),
      },
      {
        id: 'company',
        header: 'Company',
        cell: ({ row }) => (
          <div className="min-w-[8rem]">
            <div className="font-medium text-slate-950">{row.original.company || 'Unmatched rejection'}</div>
            <div className="truncate text-xs text-slate-500">{row.original.sender || 'Sender unavailable'}</div>
          </div>
        ),
      },
      {
        id: 'job',
        header: 'Job',
        cell: ({ row }) => (
          <div className="min-w-[16rem]">
            <div className="truncate font-medium text-slate-950">{row.original.job_title || subjectLabel(row.original.subject)}</div>
            <div className="truncate text-xs text-slate-500">
              {row.original.matched_application ? `Possible match: ${row.original.matched_application}` : 'No exact application title match found'}
            </div>
          </div>
        ),
      },
      {
        id: 'match',
        header: 'Match',
        cell: ({ row }) => (
          <Badge variant="outline" className={compactBadgeClass(row.original.company_only_match ? 'amber' : 'rose')}>
            {Math.round((row.original.confidence || 0) * 100)}%
          </Badge>
        ),
      },
      {
        id: 'when',
        header: 'When',
        cell: ({ row }) => <div className="whitespace-nowrap text-xs text-slate-500">{formatRelativeTimestamp(row.original.received_at)}</div>,
      },
    ],
    [],
  )

  const openActionColumns = useMemo<ColumnDef<MailOpenActionItem>[]>(
    () => [
      {
        id: 'signal',
        header: 'Signal',
        cell: ({ row }) => (
          <div className="flex min-w-[9rem] flex-wrap gap-1">
            <EventChip eventType={row.original.event_type} />
            <Badge variant="outline" className={compactBadgeClass('slate')}>
              {row.original.triage_status || 'new'}
            </Badge>
          </div>
        ),
      },
      {
        id: 'company',
        header: 'Company',
        cell: ({ row }) => (
          <div className="min-w-[8rem]">
            <div className="font-medium text-slate-950">{row.original.company || 'Open action'}</div>
            <div className="truncate text-xs text-slate-500">{row.original.sender || 'Sender unavailable'}</div>
          </div>
        ),
      },
      {
        id: 'message',
        header: 'Message',
        cell: ({ row }) => (
          <div className="min-w-[16rem]">
            <div className="truncate font-medium text-slate-950">{row.original.job_title || subjectLabel(row.original.subject)}</div>
            <div className="truncate text-xs text-slate-500">{subjectLabel(row.original.subject)}</div>
          </div>
        ),
      },
      {
        id: 'reason',
        header: 'Reason',
        cell: ({ row }) => (
          <div className="min-w-[10rem] truncate text-xs text-slate-500">{visibleMailReasons(row.original)[0] || 'Follow-up signal'}</div>
        ),
      },
      {
        id: 'when',
        header: 'When',
        cell: ({ row }) => <div className="whitespace-nowrap text-xs text-slate-500">{formatRelativeTimestamp(row.original.received_at)}</div>,
      },
    ],
    [],
  )

  const upcomingMeetingColumns = useMemo<ColumnDef<MailMeetingItem>[]>(
    () => [
      {
        id: 'meeting',
        header: 'Meeting',
        cell: () => (
          <div className="flex min-w-[8rem] items-center gap-2">
            <div className={cn('flex size-6 items-center justify-center rounded-lg border', toneStyles.blue.icon)}>
              <CalendarClock className="size-3.5" />
            </div>
            <Badge variant="outline" className={compactBadgeClass('blue')}>
              Invite
            </Badge>
          </div>
        ),
      },
      {
        id: 'company',
        header: 'Company',
        cell: ({ row }) => (
          <div className="min-w-[8rem]">
            <div className="font-medium text-slate-950">{row.original.company || 'Meeting invite'}</div>
            <div className="truncate text-xs text-slate-500">{row.original.sender || 'Sender unavailable'}</div>
          </div>
        ),
      },
      {
        id: 'subject',
        header: 'Subject',
        cell: ({ row }) => (
          <div className="min-w-[16rem]">
            <div className="truncate font-medium text-slate-950">{subjectLabel(row.original.subject)}</div>
            <div className="truncate text-xs text-slate-500">{row.original.meeting_organizer || 'Organizer unavailable'}</div>
          </div>
        ),
      },
      {
        id: 'schedule',
        header: 'Schedule',
        cell: ({ row }) => (
          <div className="min-w-[11rem] text-xs text-slate-500">
            <div className="whitespace-nowrap">{formatMeetingWindow(row.original)}</div>
            {row.original.meeting_location ? <div className="truncate">{row.original.meeting_location}</div> : null}
          </div>
        ),
      },
      {
        id: 'received',
        header: 'Received',
        cell: ({ row }) => <div className="whitespace-nowrap text-xs text-slate-500">{formatRelativeTimestamp(row.original.received_at)}</div>,
      },
    ],
    [],
  )

  const companyPulseColumns = useMemo<ColumnDef<CompanyPulseRow>[]>(
    () => [
      {
        id: 'company',
        header: 'Company',
        cell: ({ row }) => (
          <div className="min-w-[10rem]">
            <div className="font-medium text-slate-950">{row.original.company}</div>
            <div className="text-xs text-slate-500">{row.original.total} visible signals</div>
          </div>
        ),
      },
      {
        id: 'mix',
        header: 'Mix',
        cell: ({ row }) => (
          <div className="flex min-w-[13rem] flex-wrap gap-1">
            <Badge variant="outline" className={compactBadgeClass('emerald')}>
              {row.original.applications} apps
            </Badge>
            <Badge variant="outline" className={compactBadgeClass('rose')}>
              {row.original.rejections} rejects
            </Badge>
            <Badge variant="outline" className={compactBadgeClass('amber')}>
              {row.original.openActions} actions
            </Badge>
          </div>
        ),
      },
      {
        id: 'last',
        header: 'Last Signal',
        cell: ({ row }) => <div className="whitespace-nowrap text-xs text-slate-500">{formatRelativeTimestamp(row.original.lastReceivedAt)}</div>,
      },
    ],
    [],
  )

  const previewCount = useMemo(() => {
    if (!analytics) return 0
    switch (previewPane) {
      case 'open_applications':
        return analytics.details.open_applications.length
      case 'open_actions':
        return analytics.details.open_actions.length
      case 'upcoming_meetings':
        return analytics.details.upcoming_meetings.length
      case 'unresolved_rejections':
        return analytics.details.unresolved_rejections.length
      default:
        return 0
    }
  }, [analytics, previewPane])

  const latestMeeting = analytics?.details.upcoming_meetings[0] ?? null
  const trackedSignalCount = recentSignals.length
  const generatedAt = analytics?.summary.generated_at || messagesState.data?.summary.generated_at || accountsGeneratedAt || '-'
  const isRefreshing = messagesState.loading || analyticsState.loading
  const runCountsText =
    runStatus &&
    (runStatus.messages_discovered > 0 ||
      runStatus.messages_hydrated > 0 ||
      runStatus.messages_stored > 0 ||
      runStatus.messages_fetched > 0)
      ? [
          runStatus.messages_discovered > 0 ? `${runStatus.messages_discovered} discovered` : '',
          runStatus.messages_hydrated > 0 ? `${runStatus.messages_hydrated} hydrated` : '',
          runStatus.messages_stored > 0 ? `${runStatus.messages_stored} stored` : '',
          runStatus.cutoff_reached ? '7d covered' : '',
          runStatus.degraded_mode ? 'fallback mode' : '',
        ]
          .filter(Boolean)
          .join(' · ')
      : 'Waiting for first batch'
  const headerText =
    isRefreshing && trackedSignalCount > 0
      ? `Refreshing ${trackedSignalCount} tracked signals`
      : `Updated ${generatedAt}${runStatus?.running ? ` · Syncing ${runCountsText}` : ''}`

  const appliedTodayCount = analytics?.summary.today.applications ?? 0
  const rejectedTodayCount = analytics?.summary.today.rejections ?? 0
  const hasScopeFilters = provider !== '' || accountId > 0 || company !== ''
  const hiddenScopeSummary = [
    provider ? `Provider: ${providerLabel(provider as MailProviderKey)}` : 'Gmail',
    accountId > 0 ? `Account: ${visibleAccounts.find((item) => item.id === accountId)?.email || 'Selected account'}` : 'All accounts',
    company ? `Company: ${company}` : 'All companies',
  ].join(' · ')

  const openOverlayMessage = useCallback((messageId: number, preview?: MailMessage | null) => {
    setOverlay({ kind: 'message', messageId, preview })
  }, [])

  const renderLifecyclePreviewTabs = () => (
    <div className="flex flex-wrap gap-2">
      {(['open_applications', 'open_actions', 'upcoming_meetings', 'unresolved_rejections'] as LifecyclePreviewKind[]).map((kind) => (
        <button
          key={kind}
          type="button"
          onClick={() => setPreviewPane(kind)}
          className={cn(
            'inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-medium transition',
            previewPane === kind
              ? 'border-slate-950 bg-slate-950 text-white'
              : 'border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-950',
          )}
        >
          {previewPaneTitle(kind)}
        </button>
      ))}
    </div>
  )

  const renderLifecyclePreview = (maxHeightClass = 'max-h-[360px] xl:h-full') => {
    if (!analytics) {
      return <EmptyPanel title="Waiting for analytics" description="Sync a mailbox or refresh this view to build lifecycle details." />
    }

    switch (previewPane) {
      case 'open_applications':
        return (
          <ScrollableDataTable
            data={analytics.details.open_applications}
            columns={openApplicationColumns}
            emptyTitle="No open applications"
            emptyDescription="Every tracked application is either still pending classification or already resolved."
            maxHeightClass={maxHeightClass}
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      case 'open_actions':
        return (
          <ScrollableDataTable
            data={analytics.details.open_actions}
            columns={openActionColumns}
            emptyTitle="No open actions"
            emptyDescription="Recruiter replies, interview updates, and follow-up items will show up here."
            maxHeightClass={maxHeightClass}
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      case 'upcoming_meetings':
        return (
          <ScrollableDataTable
            data={analytics.details.upcoming_meetings}
            columns={upcomingMeetingColumns}
            emptyTitle="No meetings queued"
            emptyDescription="Interview invites pulled from synced mail will show here."
            maxHeightClass={maxHeightClass}
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      case 'unresolved_rejections':
        return (
          <ScrollableDataTable
            data={analytics.details.unresolved_rejections}
            columns={unresolvedRejectionColumns}
            emptyTitle="No unresolved rejections"
            emptyDescription="Company-only or weak-title rejections will surface here for manual inspection."
            maxHeightClass={maxHeightClass}
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      default:
        return null
    }
  }

  const renderPanelContent = () => {
    if (!analytics && overlay?.kind === 'panel' && overlay.panel !== 'mailbox_state') {
      return <EmptyPanel title="Waiting for analytics" description="Refresh this view after a sync completes." />
    }

    switch (overlay?.kind === 'panel' ? overlay.panel : null) {
      case 'mailbox_state':
        return visibleAccounts.length ? (
          <div className="space-y-3">
            {visibleAccounts.map((item) => (
              <div key={`${item.provider}-${item.id}`} className="rounded-3xl border border-slate-200 bg-slate-50/80 p-4 shadow-sm">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <div className="text-base font-semibold text-slate-950">{item.email || item.display_name || item.provider}</div>
                    <div className="text-sm text-slate-500">{item.provider} account</div>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="outline" className={cn('rounded-full', toneStyles.slate.badge)}>
                      {item.status || 'connected'}
                    </Badge>
                    <Badge variant="outline" className={cn('rounded-full', toneStyles.blue.badge)}>
                      {item.last_sync_at ? `Last sync ${formatRelativeTimestamp(item.last_sync_at)}` : 'Waiting for first sync'}
                    </Badge>
                  </div>
                </div>
                {item.last_error ? <div className="mt-3 text-sm text-rose-700">{item.last_error}</div> : null}
              </div>
            ))}
            <div className="rounded-3xl border border-slate-200 bg-slate-50/70 p-4 text-sm leading-6 text-slate-600">
              Scheduler {scheduleStatus.enabled ? `enabled every ${scheduleIntervalLabel(scheduleStatus.interval_minutes || 10)}` : 'disabled'}.
              {scheduleStatus.next_run_at ? ` Next run ${formatTimestamp(scheduleStatus.next_run_at)}.` : ''}
            </div>
          </div>
        ) : (
          <EmptyPanel title="No connected mailboxes" description="Connect Gmail to start tracking applications, replies, interviews, and lifecycle changes." />
        )
      case 'lifecycle_preview':
        return (
          <div className="space-y-4">
            <div className="grid gap-2 sm:grid-cols-4">
              <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-3">
                <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">Open Apps</div>
                <div className={cn('mt-1 text-lg font-semibold', toneStyles.emerald.number)}>{analytics?.summary.open_applications_count ?? 0}</div>
              </div>
              <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-3">
                <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">Open Actions</div>
                <div className={cn('mt-1 text-lg font-semibold', toneStyles.amber.number)}>{analytics?.summary.open_actions_count ?? 0}</div>
              </div>
              <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-3">
                <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">Meetings</div>
                <div className={cn('mt-1 text-lg font-semibold', toneStyles.blue.number)}>{analytics?.summary.upcoming_meetings_count ?? 0}</div>
              </div>
              <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-3">
                <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">Unresolved</div>
                <div className={cn('mt-1 text-lg font-semibold', toneStyles.rose.number)}>{analytics?.summary.unresolved_rejections_count ?? 0}</div>
              </div>
            </div>
            {renderLifecyclePreviewTabs()}
            {renderLifecyclePreview('max-h-[60vh]')}
          </div>
        )
      case 'company_pulse':
        return (
          <div className="space-y-4">
            <div className="grid gap-2 sm:grid-cols-3">
              <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-3">
                <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">Yesterday</div>
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-slate-700">
                  <span className={cn('font-semibold', toneStyles.emerald.number)}>{analytics?.summary.yesterday.applications ?? 0} apps</span>
                  <span className={cn('font-semibold', toneStyles.rose.number)}>{analytics?.summary.yesterday.rejections ?? 0} rejects</span>
                </div>
              </div>
              <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-3">
                <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">Last 7 Days</div>
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-slate-700">
                  <span className={cn('font-semibold', toneStyles.emerald.number)}>{analytics?.summary.last_7_days.applications ?? 0} apps</span>
                  <span className={cn('font-semibold', toneStyles.rose.number)}>{analytics?.summary.last_7_days.rejections ?? 0} rejects</span>
                </div>
              </div>
              <div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-3">
                <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">All Time</div>
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-slate-700">
                  <span className={cn('font-semibold', toneStyles.emerald.number)}>{analytics?.summary.all_time.applications ?? 0} apps</span>
                  <span className={cn('font-semibold', toneStyles.rose.number)}>{analytics?.summary.all_time.rejections ?? 0} rejects</span>
                </div>
              </div>
            </div>
            <ScrollableDataTable
              data={companyPulse}
              columns={companyPulseColumns}
              emptyTitle="No company activity"
              emptyDescription="This table counts visible signal mail from the last 7 days in the current scope."
              maxHeightClass="max-h-[60vh]"
              getRowId={(item) => item.company}
            />
          </div>
        )
      case 'open_applications':
        return (
          <ScrollableDataTable
            data={analytics?.details.open_applications ?? []}
            columns={openApplicationColumns}
            emptyTitle="No open applications"
            emptyDescription="Every tracked acknowledgement is either still being synced or already resolved by a matched rejection."
            maxHeightClass="max-h-[60vh]"
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      case 'resolved_rejections':
        return (
          <ScrollableDataTable
            data={analytics?.details.resolved_rejections ?? []}
            columns={resolvedRejectionColumns}
            emptyTitle="No resolved rejections"
            emptyDescription="Exact company-and-title rejection matches will show here once the application lifecycle closes."
            maxHeightClass="max-h-[60vh]"
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      case 'open_actions':
        return (
          <ScrollableDataTable
            data={analytics?.details.open_actions ?? []}
            columns={openActionColumns}
            emptyTitle="No open actions"
            emptyDescription="Plain acknowledgements, verification codes, and rejections stay out of this list."
            maxHeightClass="max-h-[60vh]"
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      case 'unresolved_rejections':
        return (
          <ScrollableDataTable
            data={analytics?.details.unresolved_rejections ?? []}
            columns={unresolvedRejectionColumns}
            emptyTitle="No unresolved rejections"
            emptyDescription="Ambiguous rejections stay here instead of auto-closing an application."
            maxHeightClass="max-h-[60vh]"
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      case 'upcoming_meetings':
        return (
          <ScrollableDataTable
            data={analytics?.details.upcoming_meetings ?? []}
            columns={upcomingMeetingColumns}
            emptyTitle="No meetings queued"
            emptyDescription="Interview invite metadata appears here when a future meeting is detected in synced mail."
            maxHeightClass="max-h-[60vh]"
            getRowId={(item) => String(item.id)}
            onRowClick={(item) => openOverlayMessage(item.id)}
          />
        )
      default:
        return null
    }
  }

  const renderMessageContent = () => {
    const detail = messageDetail.data
    if (messageDetail.loading && !detail) {
      return <div className="text-sm text-slate-500">Loading message details…</div>
    }
    if (messageDetail.error && !detail) {
      return <div className="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{messageDetail.error}</div>
    }
    if (!detail) {
      return <EmptyPanel title="Message not available" description="This detail view could not be loaded from the mail store." />
    }

    const tone = messageTone(detail)
    const hydrationLabel = hydrationStatusLabel(detail.hydration_status)
    return (
      <div className="space-y-4">
        <div className={cn('rounded-3xl border p-4 shadow-sm', toneStyles[tone].soft)}>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline" className={cn('rounded-full text-xs', toneStyles[tone].badge)}>
                {eventLabel(detail.event_type)}
              </Badge>
              <Badge variant="outline" className={cn('rounded-full text-xs', toneStyles.slate.badge)}>
                {detail.triage_status || 'new'}
              </Badge>
              {detail.has_invite ? (
                <Badge variant="outline" className={cn('rounded-full text-xs', toneStyles.blue.badge)}>
                  Invite metadata
                </Badge>
              ) : null}
              {hydrationLabel ? (
                <Badge variant="outline" className={cn('rounded-full text-xs', detail.hydration_status === 'failed' ? toneStyles.rose.badge : toneStyles.amber.badge)}>
                  {hydrationLabel}
                </Badge>
              ) : null}
            </div>
            <div className="text-sm text-slate-500">{formatRelativeTimestamp(detail.received_at)}</div>
          </div>
          <div className="mt-3 text-xl font-semibold tracking-tight text-slate-950">{subjectLabel(detail.subject)}</div>
          <div className="mt-3 grid gap-3 text-sm text-slate-600 sm:grid-cols-2">
            <div>
              <div className="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase">Company</div>
              <div className="mt-1">{companyLabel(detail)}</div>
            </div>
            <div>
              <div className="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase">Matched job</div>
              <div className="mt-1">{detail.matched_job_title || 'Not matched yet'}</div>
            </div>
            <div>
              <div className="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase">Sender</div>
              <div className="mt-1">{detail.sender.name || detail.sender.email || '-'}</div>
            </div>
            <div>
              <div className="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase">Decision</div>
              <div className="mt-1">
                {detail.decision_source || 'rules'} · {Math.round((detail.confidence || 0) * 100)}% confidence
              </div>
            </div>
            <div>
              <div className="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase">Received</div>
              <div className="mt-1">{formatTimestamp(detail.received_at_local || detail.received_at)}</div>
            </div>
            <div>
              <div className="text-xs font-semibold tracking-[0.14em] text-slate-500 uppercase">Account</div>
              <div className="mt-1">{detail.account_email || detail.provider || '-'}</div>
            </div>
          </div>
          {detail.has_invite ? (
            <div className="mt-4 rounded-2xl border border-blue-200/80 bg-blue-50/70 p-3 text-sm text-slate-700">
              <div className="font-semibold text-slate-950">{formatMeetingWindow(detail)}</div>
              <div className="mt-1">
                {detail.meeting_organizer || detail.sender.email || 'Organizer unavailable'}
                {detail.meeting_location ? ` · ${detail.meeting_location}` : ''}
              </div>
            </div>
          ) : null}
          {visibleMailReasons(detail).length ? (
            <div className="mt-4 flex flex-wrap gap-2">
              {visibleMailReasons(detail).map((reason) => (
                <Badge key={reason} variant="outline" className={cn('rounded-full text-xs', toneStyles.slate.badge)}>
                  {reason}
                </Badge>
              ))}
            </div>
          ) : null}
          {detail.web_link ? (
            <div className="mt-4">
              <a
                href={detail.web_link}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-full border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition hover:border-slate-300 hover:text-slate-950"
              >
                Open original message
                <ExternalLink className="size-4" />
              </a>
            </div>
          ) : null}
        </div>
        {detail.body_text || detail.snippet ? (
          <div className="rounded-3xl border border-slate-200 bg-slate-50/80 p-4 shadow-sm">
            <div className="text-xs font-semibold tracking-[0.18em] text-slate-500 uppercase">Message context</div>
            {detail.hydration_status === 'pending' ? (
              <div className="mt-3 rounded-2xl border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
                Full body is still syncing from Gmail. Showing the current snippet until hydration completes.
              </div>
            ) : null}
            {detail.hydration_status === 'failed' ? (
              <div className="mt-3 rounded-2xl border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
                Full body sync failed for this message. The stored preview is still available.
              </div>
            ) : null}
            <div className="mt-3 whitespace-pre-wrap text-sm leading-6 text-slate-700">{detail.body_text || detail.snippet}</div>
          </div>
        ) : null}
        {messageDetail.error ? <div className="text-sm text-rose-700">{messageDetail.error}</div> : null}
      </div>
    )
  }

  const overlayTitle =
    overlay?.kind === 'panel'
      ? {
          mailbox_state: 'Mailbox State',
          lifecycle_preview: 'Lifecycle Preview',
          company_pulse: 'Company Pulse',
          open_applications: 'Open Applications',
          resolved_rejections: 'Resolved by Rejection',
          open_actions: 'Open Actions',
          unresolved_rejections: 'Unresolved Rejections',
          upcoming_meetings: 'Upcoming Meetings',
        }[overlay.panel]
      : messageDetail.data
        ? subjectLabel(messageDetail.data.subject)
        : 'Signal Detail'

  const overlayDescription =
    overlay?.kind === 'panel'
      ? {
          mailbox_state: 'Connected accounts, sync state, and scheduler status.',
          lifecycle_preview: 'Open applications, actions, meetings, and unresolved rejections in one drill-in.',
          company_pulse: 'Company-level clustering for visible signal traffic in the current scope.',
          open_applications: 'Application acknowledgements that remain open because no exact rejection has closed them yet.',
          resolved_rejections: 'Rejections that matched an earlier application by company and job title.',
          open_actions: 'Recruiter replies, interview updates, and follow-up items that still deserve attention.',
          unresolved_rejections: 'Rejections with a company match but no exact job-title match, or other ambiguous closure cases.',
          upcoming_meetings: 'Calendar invites and interview meetings extracted from synced mail.',
        }[overlay.panel]
      : 'Message-level context from the synced mail store.'

  return (
    <main className="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(16,185,129,0.1),_transparent_26%),radial-gradient(circle_at_top_right,_rgba(59,130,246,0.1),_transparent_24%),linear-gradient(180deg,#f8fafc_0%,#edf2f7_100%)] px-4 py-4 lg:h-screen lg:overflow-hidden lg:py-3">
      <div className="flex h-full min-h-0 w-full flex-col gap-3">
        <section className="overflow-hidden rounded-[28px] border border-slate-200/80 bg-white/92 shadow-[0_24px_80px_-54px_rgba(15,23,42,0.45)]">
          <div className="flex flex-col gap-3 px-4 py-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline" className={compactBadgeClass('emerald')}>
                  Mail
                </Badge>
                <span className="rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-xs text-slate-600">{headerText}</span>
              </div>
              <div className="space-y-1">
                <h1 className="text-2xl font-semibold tracking-tight text-slate-950 sm:text-[2rem]">Mail Analytics</h1>
                <p className="text-sm text-slate-600">Signals only. Newsletter noise and non-job digests stay out of the lifecycle views.</p>
              </div>
            </div>
            <div className="flex flex-col gap-2 xl:items-end">
              <div className="flex flex-wrap gap-2 xl:justify-end">
                <Button asChild variant="outline" size="sm" className="h-8">
                  <Link to="/monitor">Monitor</Link>
                </Button>
                <Button asChild variant="outline" size="sm" className="h-8">
                  <Link to="/jobs">Jobs</Link>
                </Button>
                <Button asChild variant="outline" size="sm" className="h-8">
                  <Link to="/assistant">
                    <Bot className="size-4" />
                    Assistant
                  </Link>
                </Button>
                <Button type="button" size="sm" className="h-8" onClick={() => void runSync()} disabled={runBusy || Boolean(runStatus?.running)}>
                  <RefreshCw className={cn('size-4', runBusy || runStatus?.running ? 'animate-spin' : '')} />
                  Sync Mail
                </Button>
                <Button type="button" variant="outline" size="sm" className="h-8" onClick={() => void loadMailView()} disabled={isRefreshing}>
                  <RefreshCw className={cn('size-4', isRefreshing ? 'animate-spin' : '')} />
                  Refresh
                </Button>
              </div>
              <div className="flex flex-wrap items-end gap-2 xl:justify-end">
                <div className="space-y-1">
                  <div className="text-[11px] font-semibold tracking-[0.18em] text-slate-500 uppercase">Scheduler</div>
                  <Select
                    value={String(scheduleStatus.interval_minutes || 10)}
                    onValueChange={(value) => void updateSchedule(scheduleStatus.enabled, Number(value))}
                    disabled={scheduleBusy}
                  >
                    <SelectTrigger className="h-8 w-[108px] rounded-xl border-slate-200 bg-white/90 text-sm shadow-none">
                      <SelectValue placeholder="Interval" />
                    </SelectTrigger>
                    <SelectContent>
                      {SCHEDULE_OPTIONS.map((option) => (
                        <SelectItem key={option} value={String(option)}>
                          {scheduleIntervalLabel(option)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-8"
                  onClick={() => void updateSchedule(!scheduleStatus.enabled, scheduleStatus.interval_minutes || 10)}
                  disabled={scheduleBusy}
                >
                  {scheduleStatus.enabled ? 'Disable Scheduler' : 'Enable Scheduler'}
                </Button>
                <Button type="button" variant="outline" size="sm" className="h-8" onClick={() => void startConnect('gmail')} disabled={accountActionBusy}>
                  {gmailActionLabel}
                </Button>
              </div>
            </div>
          </div>

          {notice ? <div className="border-t border-emerald-100 bg-emerald-50 px-4 py-2.5 text-sm text-emerald-700">{notice}</div> : null}
          {error ? <div className="border-t border-rose-100 bg-rose-50 px-4 py-2.5 text-sm text-rose-700">{error}</div> : null}
          {analyticsState.error ? (
            <div className="border-t border-amber-100 bg-amber-50 px-4 py-2.5 text-sm text-amber-800">
              Analytics refresh is still catching up. Showing the previous lifecycle view until it finishes.
            </div>
          ) : null}

          <div className="border-t border-slate-200/70 px-4 py-3">
            <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
              <div className="flex flex-wrap gap-2">
                <CompactMetricTile
                  label="Lifecycle"
                  value={previewCount}
                  detail={`${previewPaneTitle(previewPane)} in focus`}
                  tone="blue"
                  onClick={() => setOverlay({ kind: 'panel', panel: 'lifecycle_preview' })}
                />
                <CompactMetricTile
                  label="Company Pulse"
                  value={companyPulse.length}
                  detail={`${analytics?.summary.last_7_days.applications ?? 0} apps · ${analytics?.summary.last_7_days.rejections ?? 0} rejects`}
                  tone="slate"
                  onClick={() => setOverlay({ kind: 'panel', panel: 'company_pulse' })}
                />
              </div>
              <div className="flex flex-wrap gap-2 xl:justify-end">
                {providerIndicators.map((item) => (
                  <ProviderStatusPill key={item.provider} provider={item.provider} status={item.status} detail={item.detail} tone={item.tone} />
                ))}
                <Button type="button" variant="outline" size="sm" className="h-8" onClick={() => setShowFilters((current) => !current)}>
                  {showFilters ? 'Hide Filters' : 'Show Filters'}
                </Button>
              </div>
            </div>
            {showFilters ? (
              <div className="mt-3 grid gap-2 sm:grid-cols-3">
                <ScopeSelect
                  label="Provider"
                  value={provider || 'all'}
                  onValueChange={(value) => setProvider(value === 'all' ? '' : value)}
                  options={[{ value: 'all', label: 'All Providers' }, ...providerOptions.map((option) => ({ value: option, label: option }))]}
                />
                <ScopeSelect
                  label="Account"
                  value={String(accountId || 0)}
                  onValueChange={(value) => setAccountId(Number(value) || 0)}
                  options={[
                    { value: '0', label: 'All Accounts' },
                    ...visibleAccounts.map((option) => ({ value: String(option.id), label: option.email || option.provider })),
                  ]}
                />
                <ScopeSelect
                  label="Company"
                  value={company || 'all'}
                  onValueChange={(value) => setCompany(value === 'all' ? '' : value)}
                  options={[{ value: 'all', label: 'All Companies' }, ...companyOptions.map((option) => ({ value: option, label: option }))]}
                />
              </div>
            ) : (
              <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-slate-500">
                <span className="rounded-full border border-slate-200 bg-slate-50 px-3 py-1">
                  {hasScopeFilters ? `Filters hidden · ${hiddenScopeSummary}` : 'Filters hidden · full mail scope'}
                </span>
              </div>
            )}
          </div>
        </section>

        <section className="overflow-x-auto">
          <div className="flex min-w-max gap-2 rounded-[28px] border border-slate-200/80 bg-white/88 px-3 py-3 shadow-[0_18px_60px_-48px_rgba(15,23,42,0.42)]">
            <CompactMetricTile
              label="Applied Today"
              value={appliedTodayCount}
              detail={`${rejectedTodayCount} rejects today`}
              tone="emerald"
            />
            <CompactMetricTile
              label="Latest Signal"
              value={latestSignal ? formatRelativeTimestamp(latestSignal.received_at) : 'No signal yet'}
              detail={latestSignal ? subjectLabel(latestSignal.subject) : 'Sync mail to populate'}
              tone={latestSignal ? messageTone(latestSignal) : 'slate'}
              onClick={latestSignal ? () => openOverlayMessage(latestSignal.id, latestSignal) : undefined}
            />
            <CompactMetricTile
              label="Next Meeting"
              value={latestMeeting ? formatMeetingWindow(latestMeeting) : 'No meetings queued'}
              detail={latestMeeting ? latestMeeting.company || latestMeeting.subject : 'Interview invites appear here'}
              tone="blue"
              onClick={() => setOverlay({ kind: 'panel', panel: 'upcoming_meetings' })}
            />
            <CompactMetricTile
              label="Open Apps"
              value={analytics?.summary.open_applications_count ?? 0}
              detail="Active acknowledgements"
              tone="emerald"
              onClick={() => setOverlay({ kind: 'panel', panel: 'open_applications' })}
            />
            <CompactMetricTile
              label="Open Actions"
              value={analytics?.summary.open_actions_count ?? 0}
              detail="Replies and follow-ups"
              tone="amber"
              onClick={() => setOverlay({ kind: 'panel', panel: 'open_actions' })}
            />
            <CompactMetricTile
              label="Resolved"
              value={analytics?.summary.resolved_rejections_count ?? 0}
              detail="Closed by rejection"
              tone="blue"
              onClick={() => setOverlay({ kind: 'panel', panel: 'resolved_rejections' })}
            />
            <CompactMetricTile
              label="Unresolved"
              value={analytics?.summary.unresolved_rejections_count ?? 0}
              detail="Needs manual review"
              tone="rose"
              onClick={() => setOverlay({ kind: 'panel', panel: 'unresolved_rejections' })}
            />
            <CompactMetricTile
              label="Tracked Signals"
              value={trackedSignalCount}
              detail={`${visibleAccounts.length} Gmail account${visibleAccounts.length === 1 ? '' : 's'}`}
              tone="slate"
            />
            <CompactMetricTile
              label="Last 7 Days"
              value={`${analytics?.summary.last_7_days.applications ?? 0} / ${analytics?.summary.last_7_days.rejections ?? 0}`}
              detail="apps / rejects"
              tone="slate"
            />
          </div>
        </section>

        <section className="min-h-0 flex-1">
          <Card className="h-full min-h-0 gap-0 overflow-hidden border-slate-200/80 bg-white/92 py-0 shadow-[0_24px_80px_-54px_rgba(15,23,42,0.45)] xl:flex xl:flex-col">
            <CardHeader className="gap-2 border-b border-slate-200/70 px-4 py-3 sm:px-5">
              <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
                <div className="space-y-1">
                  <CardTitle className="text-lg font-semibold text-slate-950">Recent Signals</CardTitle>
                  <CardDescription>Table-first view of tracked job mail. Noise and ignored mail stay out of this feed.</CardDescription>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Badge variant="outline" className={compactBadgeClass('slate')}>
                    {trackedSignalCount} tracked
                  </Badge>
                  <Badge variant="outline" className={compactBadgeClass('emerald')}>
                    {analytics?.summary.all_time.applications ?? 0} apps
                  </Badge>
                  <Badge variant="outline" className={compactBadgeClass('rose')}>
                    {analytics?.summary.all_time.rejections ?? 0} rejects
                  </Badge>
                </div>
              </div>
            </CardHeader>
            <CardContent className="min-h-0 flex-1 p-0">
              <ScrollableDataTable
                data={recentSignals}
                columns={recentSignalColumns}
                emptyTitle="No recent signals"
                emptyDescription="Sync mail or widen the scope by removing company and account filters."
                maxHeightClass="h-full"
                getRowId={(item) => String(item.id)}
                onRowClick={(item) => openOverlayMessage(item.id, item)}
              />
            </CardContent>
          </Card>
        </section>
      </div>

      <DetailModal open={overlay !== null} title={overlayTitle} description={overlayDescription} onClose={() => setOverlay(null)}>
        {overlay?.kind === 'panel' ? renderPanelContent() : renderMessageContent()}
      </DetailModal>
    </main>
  )
}
