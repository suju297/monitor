import * as React from 'react'
import type { LucideIcon } from 'lucide-react'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

type DashboardPageProps = React.ComponentProps<'main'> & {
  contentClassName?: string
}

type DashboardHeroProps = {
  title: string
  subtitle?: string
  eyebrow?: string
  icon?: LucideIcon
  actions?: React.ReactNode
  children?: React.ReactNode
  compact?: boolean
  className?: string
}

type MetricCardProps = {
  label: string
  value: React.ReactNode
  detail?: React.ReactNode
  icon?: LucideIcon
  tone?: 'teal' | 'amber' | 'blue' | 'slate' | 'rose' | 'emerald'
  className?: string
}

type ModalShellProps = {
  open: boolean
  title: React.ReactNode
  description?: React.ReactNode
  children: React.ReactNode
  onClose: () => void
  headerAction?: React.ReactNode
  className?: string
  panelClassName?: string
  contentClassName?: string
  align?: 'center' | 'top'
  maxWidthClassName?: string
  ariaLabel?: string
}

type FieldGroupProps = React.ComponentProps<'label'> & {
  label: string
  hint?: string
}

const metricToneStyles: Record<NonNullable<MetricCardProps['tone']>, string> = {
  teal: 'border-emerald-200/70 bg-linear-to-br from-emerald-50 via-sky-50/35 to-white',
  amber: 'border-amber-200/80 bg-linear-to-br from-amber-50 via-white to-white',
  blue: 'border-blue-200/80 bg-linear-to-br from-blue-50 via-white to-white',
  slate: 'border-slate-200/80 bg-linear-to-br from-slate-50 via-blue-50/30 to-white',
  rose: 'border-rose-200/80 bg-linear-to-br from-rose-50 via-white to-white',
  emerald: 'border-emerald-200/80 bg-linear-to-br from-emerald-50 via-white to-white',
}

export function DashboardPage({ className, contentClassName, children, ...props }: DashboardPageProps) {
  return (
    <main
      className={cn(
        'min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(37,99,235,0.12),_transparent_28%),radial-gradient(circle_at_top_right,_rgba(16,185,129,0.08),_transparent_22%),linear-gradient(180deg,#f8fafc_0%,#eef6ff_100%)] px-4 py-4 md:px-6 md:py-6',
        className,
      )}
      {...props}
    >
      <div className={cn('mx-auto flex w-full max-w-[1720px] flex-col gap-4 md:gap-5', contentClassName)}>{children}</div>
    </main>
  )
}

export function DashboardHero({
  title,
  subtitle,
  eyebrow,
  icon: Icon,
  actions,
  children,
  compact = false,
  className,
}: DashboardHeroProps) {
  return (
    <section
      className={cn(
        'relative overflow-hidden border border-slate-200/80 bg-white/88 shadow-[0_24px_80px_-44px_rgba(15,23,42,0.42)] backdrop-blur',
        compact ? 'rounded-[20px] px-4 py-4 md:px-5 md:py-4' : 'rounded-[24px] px-5 py-5 md:px-6 md:py-5',
        className,
      )}
    >
      <div className="pointer-events-none absolute inset-y-0 right-0 w-64 bg-[radial-gradient(circle_at_top_right,_rgba(37,99,235,0.1),_transparent_60%)]" />
      <div className={cn('relative grid xl:items-start', compact ? 'gap-3 xl:grid-cols-[minmax(0,1fr)_auto]' : 'gap-4 xl:grid-cols-[minmax(0,1.25fr)_auto]')}>
        <div className={cn('min-w-0', compact ? 'space-y-2.5' : 'space-y-3')}>
          <div className="flex flex-wrap items-center gap-2">
            {Icon ? (
              <div
                className={cn(
                  'flex items-center justify-center border border-blue-200/80 bg-blue-600 text-white shadow-sm',
                  compact ? 'size-8 rounded-lg' : 'size-10 rounded-xl',
                )}
              >
                <Icon className={cn(compact ? 'size-4' : 'size-[18px]')} />
              </div>
            ) : null}
            {eyebrow ? (
              <div
                className={cn(
                  'inline-flex items-center rounded-full border border-blue-200/80 bg-blue-50 font-semibold tracking-[0.18em] text-blue-700 uppercase',
                  compact ? 'px-2 py-0.5 text-[9px]' : 'px-2.5 py-0.5 text-[10px]',
                )}
              >
                {eyebrow}
              </div>
            ) : null}
          </div>
          <div className={cn(compact ? 'space-y-1' : 'space-y-1.5')}>
            <h1 className={cn('leading-tight font-semibold tracking-tight text-slate-950', compact ? 'text-[1.35rem] md:text-[1.6rem]' : 'text-[1.9rem] md:text-[2.35rem]')}>
              {title}
            </h1>
            {subtitle ? <p className={cn('max-w-3xl text-slate-600', compact ? 'text-[13px] leading-5' : 'text-sm leading-5')}>{subtitle}</p> : null}
          </div>
          {children ? (
            <div className={cn('flex flex-wrap items-center gap-2.5 text-slate-500', compact ? 'text-[11px]' : 'text-xs md:text-sm')}>{children}</div>
          ) : null}
        </div>
        {actions ? <div className={cn('flex flex-wrap items-center gap-2.5 xl:justify-end', compact ? 'xl:max-w-[44rem]' : 'xl:max-w-[38rem]')}>{actions}</div> : null}
      </div>
    </section>
  )
}

export function SurfaceCard({ className, ...props }: React.ComponentProps<typeof Card>) {
  return <Card className={cn('border-slate-200/80 bg-white/88 shadow-[0_22px_70px_-50px_rgba(15,23,42,0.38)] backdrop-blur', className)} {...props} />
}

export function SurfaceCardHeader({ className, ...props }: React.ComponentProps<typeof CardHeader>) {
  return <CardHeader className={cn('gap-1.5 border-b border-slate-200/70 pb-4', className)} {...props} />
}

export function SurfaceCardTitle({ className, ...props }: React.ComponentProps<typeof CardTitle>) {
  return <CardTitle className={cn('text-base font-semibold text-slate-950 md:text-lg', className)} {...props} />
}

export function SurfaceCardDescription({ className, ...props }: React.ComponentProps<typeof CardDescription>) {
  return <CardDescription className={cn('max-w-3xl text-sm leading-5 text-slate-500', className)} {...props} />
}

export function SurfaceCardContent({ className, ...props }: React.ComponentProps<typeof CardContent>) {
  return <CardContent className={cn('space-y-3.5', className)} {...props} />
}

export function MetricCard({ label, value, detail, icon: Icon, tone = 'slate', className }: MetricCardProps) {
  return (
    <SurfaceCard className={cn('gap-3 p-4', metricToneStyles[tone], className)}>
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1.5">
          <div className="text-[10px] font-semibold tracking-[0.18em] text-slate-500 uppercase">{label}</div>
          <div className="text-[2rem] leading-none font-semibold tracking-tight text-slate-950">{value}</div>
        </div>
        {Icon ? (
          <div className="flex size-9 items-center justify-center rounded-xl border border-white/70 bg-white/80 text-slate-700 shadow-sm">
            <Icon className="size-4" />
          </div>
        ) : null}
      </div>
      {detail ? <div className="line-clamp-2 text-[13px] leading-5 text-slate-600">{detail}</div> : null}
    </SurfaceCard>
  )
}

export function InlineAlert({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn(
        'rounded-xl border border-rose-200/80 bg-rose-50/90 px-3.5 py-2.5 text-sm leading-5 text-rose-700 shadow-sm',
        className,
      )}
      {...props}
    />
  )
}

export function FieldGroup({ label, hint, className, children, ...props }: FieldGroupProps) {
  return (
    <label className={cn('flex flex-col gap-2', className)} {...props}>
      <span className="text-[10px] font-semibold tracking-[0.18em] text-slate-500 uppercase">{label}</span>
      {children}
      {hint ? <span className="text-[11px] text-slate-500">{hint}</span> : null}
    </label>
  )
}

export function NativeInput({ className, ...props }: React.ComponentProps<'input'>) {
  return (
    <input
      className={cn(
        'h-10 w-full rounded-xl border border-slate-200 bg-white/90 px-3.5 text-sm text-slate-900 shadow-xs outline-none transition focus:border-blue-300 focus:ring-[3px] focus:ring-blue-100 disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
      {...props}
    />
  )
}

export function NativeSelect({ className, children, ...props }: React.ComponentProps<'select'>) {
  return (
    <select
      className={cn(
        'h-10 w-full rounded-xl border border-slate-200 bg-white/90 px-3.5 text-sm text-slate-900 shadow-xs outline-none transition focus:border-blue-300 focus:ring-[3px] focus:ring-blue-100 disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
      {...props}
    >
      {children}
    </select>
  )
}

export function TabButton({
  active,
  className,
  ...props
}: React.ComponentProps<'button'> & {
  active?: boolean
}) {
  return (
    <button
      type="button"
      className={cn(
        'inline-flex items-center justify-center rounded-full border px-3.5 py-1.5 text-[13px] font-medium transition',
        active
          ? 'border-blue-600 bg-blue-600 text-white shadow-sm'
          : 'border-slate-200 bg-white/80 text-slate-600 hover:border-blue-200 hover:text-blue-700',
        className,
      )}
      {...props}
    />
  )
}

export function ProgressBar({
  value,
  className,
  indicatorClassName,
}: {
  value: number
  className?: string
  indicatorClassName?: string
}) {
  const safeValue = Math.max(0, Math.min(100, Math.round(value)))
  return (
    <div className={cn('h-2.5 overflow-hidden rounded-full bg-blue-100/70', className)}>
      <div className={cn('h-full rounded-full bg-blue-600 transition-[width]', indicatorClassName)} style={{ width: `${safeValue}%` }} />
    </div>
  )
}

export function EmptyState({
  title,
  description,
  meta,
  className,
}: {
  title: string
  description: string
  meta?: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn('rounded-[22px] border border-dashed border-slate-200 bg-slate-50/80 px-5 py-7 text-center', className)}>
      <div className="space-y-2.5">
        <div className="text-base font-semibold text-slate-900 md:text-lg">{title}</div>
        <div className="mx-auto max-w-2xl text-sm leading-5 text-slate-500">{description}</div>
        {meta ? <div className="flex flex-wrap items-center justify-center gap-2.5 text-[11px] text-slate-500">{meta}</div> : null}
      </div>
    </div>
  )
}

export function TableShell({
  className,
  viewportClassName,
  children,
}: React.PropsWithChildren<{
  className?: string
  viewportClassName?: string
}>) {
  return (
    <div className={cn('overflow-hidden rounded-[20px] border border-slate-200/80 bg-white/80', className)}>
      <div className={cn('min-h-0 flex-1 overflow-auto', viewportClassName)}>{children}</div>
    </div>
  )
}

export function ModalShell({
  open,
  title,
  description,
  children,
  onClose,
  headerAction,
  className,
  panelClassName,
  contentClassName,
  align = 'center',
  maxWidthClassName = 'max-w-3xl',
  ariaLabel,
}: ModalShellProps) {
  if (!open) return null

  return (
    <div
      className={cn(
        'fixed inset-0 z-50 bg-slate-950/50 backdrop-blur-sm',
        align === 'top' ? 'flex items-start justify-center px-4 py-8' : 'flex items-end justify-center p-3 sm:items-center sm:p-6',
        className,
      )}
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={ariaLabel}
    >
      <SurfaceCard
        className={cn('flex max-h-[90vh] w-full flex-col overflow-hidden bg-white shadow-[0_40px_120px_-42px_rgba(15,23,42,0.5)]', maxWidthClassName, panelClassName)}
        onClick={(event) => event.stopPropagation()}
      >
        <SurfaceCardHeader className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div className="space-y-1">
            <SurfaceCardTitle>{title}</SurfaceCardTitle>
            {description ? <SurfaceCardDescription>{description}</SurfaceCardDescription> : null}
          </div>
          {headerAction}
        </SurfaceCardHeader>
        <SurfaceCardContent className={cn('min-h-0 flex-1 overflow-y-auto', contentClassName)}>{children}</SurfaceCardContent>
      </SurfaceCard>
    </div>
  )
}
