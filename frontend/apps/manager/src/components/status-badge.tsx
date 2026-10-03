import { cn } from '@workspace/ui/lib/utils'
import type { ProjectStatus } from '@/lib/types'

const styles: Record<ProjectStatus, { dot: string; text: string; label: string }> = {
  running: { dot: 'bg-brand', text: 'text-brand border-brand/30 bg-brand/10', label: 'Running' },
  stopped: { dot: 'bg-muted-foreground', text: 'text-muted-foreground border-border bg-muted/50', label: 'Stopped' },
  starting: { dot: 'bg-amber-400 animate-pulse', text: 'text-amber-400 border-amber-400/30 bg-amber-400/10', label: 'Starting' },
  stopping: { dot: 'bg-amber-400 animate-pulse', text: 'text-amber-400 border-amber-400/30 bg-amber-400/10', label: 'Stopping' },
  error: { dot: 'bg-destructive', text: 'text-destructive border-destructive/30 bg-destructive/10', label: 'Unhealthy' },
  unknown: { dot: 'bg-muted-foreground', text: 'text-muted-foreground border-border bg-muted/50', label: 'Unknown' },
}

export function StatusBadge({ status, className }: { status: ProjectStatus; className?: string }) {
  const s = styles[status] ?? styles.unknown
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium',
        s.text,
        className,
      )}
    >
      <span className={cn('size-1.5 rounded-full', s.dot)} />
      {s.label}
    </span>
  )
}
