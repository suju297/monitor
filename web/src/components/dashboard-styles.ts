import { cn } from '@/lib/utils'

type SurfaceTone = 'teal' | 'amber' | 'blue' | 'slate' | 'rose' | 'emerald'
type PillSize = 'compact' | 'status' | 'label'

const tonePillStyles: Record<SurfaceTone, string> = {
  teal: 'border-teal-200 bg-teal-50 text-teal-700',
  amber: 'border-amber-200 bg-amber-50 text-amber-700',
  blue: 'border-blue-200 bg-blue-50 text-blue-700',
  slate: 'border-slate-200 bg-slate-100 text-slate-600',
  rose: 'border-rose-200 bg-rose-50 text-rose-700',
  emerald: 'border-emerald-200 bg-emerald-50 text-emerald-700',
}

const pillSizeStyles: Record<PillSize, string> = {
  compact: 'px-1.5 py-0.5 text-[10px] leading-none whitespace-nowrap',
  status: 'px-2.5 py-0.5 text-[11px]',
  label: 'px-3 py-1 text-[11px]',
}

export function pillClass(tone: SurfaceTone = 'slate', size: PillSize = 'status', className?: string) {
  return cn('inline-flex items-center rounded-full border font-medium', pillSizeStyles[size], tonePillStyles[tone], className)
}

export function subtleTextClass(className?: string) {
  return cn('text-[13px] leading-5 text-slate-500', className)
}
