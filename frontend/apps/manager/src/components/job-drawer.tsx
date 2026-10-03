import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  ArrowDownToLine,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Loader2,
  Maximize2,
  Minimize2,
  Terminal,
  X,
  XCircle,
} from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@workspace/ui/components/tooltip'
import { openStream } from '@/lib/api'
import { cn } from '@workspace/ui/lib/utils'

interface JobState {
  id: number
  title: string
  lines: string[]
  status: 'running' | 'succeeded' | 'failed'
}

type PanelMode = 'open' | 'collapsed' | 'hidden'

interface JobContextValue {
  openJob: (id: number, title: string) => void
  job: JobState | null
  mode: PanelMode
  setMode: (m: PanelMode) => void
  height: number
  setHeight: (h: number) => void
  follow: boolean
  setFollow: (f: boolean) => void
}

const JobContext = createContext<JobContextValue | null>(null)

const HEADER_H = 40
const MIN_BODY_H = 80
const DEFAULT_H = 320
const TOP_GAP = 96 // keeps the top bar reachable when the panel is dragged up
const STORE_KEY = 'sm.jobPanel'

function maxHeight() {
  return Math.max(HEADER_H + MIN_BODY_H, window.innerHeight - TOP_GAP)
}

function clampHeight(h: number) {
  return Math.round(Math.min(Math.max(h, HEADER_H + MIN_BODY_H), maxHeight()))
}

function loadPrefs(): { height: number; follow: boolean } {
  try {
    const p = JSON.parse(localStorage.getItem(STORE_KEY) || '{}') as { height?: number; follow?: boolean }
    return { height: typeof p.height === 'number' ? p.height : DEFAULT_H, follow: p.follow ?? true }
  } catch {
    return { height: DEFAULT_H, follow: true }
  }
}

export function JobProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [job, setJob] = useState<JobState | null>(null)
  const [mode, setMode] = useState<PanelMode>('hidden')
  const [height, setHeightState] = useState(() => loadPrefs().height)
  const [follow, setFollow] = useState(() => loadPrefs().follow)
  const sourceRef = useRef<EventSource | null>(null)

  const setHeight = useCallback((h: number) => setHeightState(clampHeight(h)), [])

  useEffect(() => {
    localStorage.setItem(STORE_KEY, JSON.stringify({ height, follow }))
  }, [height, follow])

  useEffect(() => {
    const onResize = () => setHeightState((h) => clampHeight(h))
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  const openJob = useCallback(
    (id: number, title: string) => {
      sourceRef.current?.close()
      setJob({ id, title, lines: [], status: 'running' })
      setMode('open')
      setFollow(true)
      openStream(`/jobs/${id}/stream`).then((es) => {
        sourceRef.current = es
        es.onmessage = (e) => {
          setJob((j) => (j && j.id === id ? { ...j, lines: [...j.lines, e.data] } : j))
        }
        es.addEventListener('done', (e) => {
          const data = JSON.parse((e as MessageEvent).data) as { status: 'succeeded' | 'failed' }
          setJob((j) => (j && j.id === id ? { ...j, status: data.status } : j))
          es.close()
          qc.invalidateQueries({ queryKey: ['projects'] })
          qc.invalidateQueries({ queryKey: ['project'] })
          qc.invalidateQueries({ queryKey: ['pm'] })
          if (data.status === 'succeeded') toast.success(`${title} finished`)
          else toast.error(`${title} failed`)
        })
        es.onerror = () => {
          if (es.readyState === EventSource.CLOSED) return
          es.close()
        }
      })
    },
    [qc],
  )

  useEffect(() => () => sourceRef.current?.close(), [])

  return (
    <JobContext.Provider value={{ openJob, job, mode, setMode, height, setHeight, follow, setFollow }}>
      {children}
    </JobContext.Provider>
  )
}

function useJobContext() {
  const ctx = useContext(JobContext)
  if (!ctx) throw new Error('useJobs must be used inside JobProvider')
  return ctx
}

export function useJobs() {
  return { openJob: useJobContext().openJob }
}

function StatusIcon({ status, className }: { status: JobState['status']; className?: string }) {
  if (status === 'running') return <Loader2 className={cn('text-muted-foreground size-4 animate-spin', className)} />
  if (status === 'succeeded') return <CheckCircle2 className={cn('text-brand size-4', className)} />
  return <XCircle className={cn('text-destructive size-4', className)} />
}

function PanelButton({ label, onClick, active, children }: { label: string; onClick: () => void; active?: boolean; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={label}
          aria-pressed={active}
          onClick={(e) => {
            e.stopPropagation()
            onClick()
          }}
          className={cn('size-7', active && 'bg-accent text-foreground')}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

/**
 * Job output docked at the bottom of a layout. It is part of the layout's flex column, so the
 * page above shrinks instead of being covered. Drag the header to resize; the header stays
 * visible while collapsed, and a floating button brings a closed panel back.
 */
export function JobPanel() {
  const { job, mode, setMode, height, setHeight, follow, setFollow } = useJobContext()
  const scrollRef = useRef<HTMLDivElement>(null)
  const drag = useRef<{ startY: number; startH: number; moved: boolean } | null>(null)
  const [dragging, setDragging] = useState(false)

  useEffect(() => {
    const el = scrollRef.current
    if (el && follow && mode === 'open') el.scrollTop = el.scrollHeight
  }, [job?.lines.length, follow, mode, height])

  if (!job) return null

  if (mode === 'hidden') {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="outline"
            size="icon"
            aria-label="Show job output"
            onClick={() => setMode('open')}
            className="bg-background fixed bottom-4 left-4 z-40 size-10 rounded-full shadow-lg"
          >
            <Terminal className="size-4" />
            <StatusIcon status={job.status} className="bg-background absolute -top-1 -right-1 size-4 rounded-full" />
          </Button>
        </TooltipTrigger>
        <TooltipContent side="right">Show job output: {job.title}</TooltipContent>
      </Tooltip>
    )
  }

  const maximized = mode === 'open' && height >= maxHeight()

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0 || (e.target as HTMLElement).closest('button')) return
    e.currentTarget.setPointerCapture(e.pointerId)
    drag.current = { startY: e.clientY, startH: mode === 'open' ? height : HEADER_H, moved: false }
  }
  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current
    if (!d) return
    const dy = d.startY - e.clientY
    if (!d.moved && Math.abs(dy) < 4) return
    if (!d.moved) {
      d.moved = true
      setDragging(true)
    }
    const h = d.startH + dy
    if (h < HEADER_H + MIN_BODY_H / 2) {
      setMode('collapsed')
    } else {
      setMode('open')
      setHeight(h)
    }
  }
  const onPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current
    drag.current = null
    setDragging(false)
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId)
    if (d && !d.moved) setMode(mode === 'open' ? 'collapsed' : 'open')
  }

  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24
    if (atBottom !== follow) setFollow(atBottom)
  }

  return (
    <section
      aria-label="Job output"
      className={cn('bg-background flex shrink-0 flex-col border-t shadow-[0_-4px_12px_rgba(0,0,0,0.15)]', dragging && 'select-none')}
      style={{ height: mode === 'open' ? height : HEADER_H }}
    >
      <div
        role="separator"
        aria-orientation="horizontal"
        aria-label="Resize job output (click to collapse or expand)"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        className="group relative flex shrink-0 cursor-row-resize touch-none items-center gap-2 border-b px-3"
        style={{ height: HEADER_H }}
      >
        <span className="bg-border group-hover:bg-brand absolute top-0 left-1/2 h-1 w-12 -translate-x-1/2 rounded-b transition-colors" />
        <Terminal className="size-4 shrink-0" />
        <span className="truncate text-sm font-medium">{job.title}</span>
        <StatusIcon status={job.status} className="shrink-0" />
        <span className="text-muted-foreground hidden truncate text-xs sm:inline">Job #{job.id} - Supabase CLI output</span>
        <div className="ml-auto flex shrink-0 items-center gap-0.5">
          {mode === 'open' && (
            <PanelButton
              label={follow ? 'Staying at the bottom' : 'Stay at the bottom'}
              active={follow}
              onClick={() => {
                setFollow(!follow)
                const el = scrollRef.current
                if (!follow && el) el.scrollTop = el.scrollHeight
              }}
            >
              <ArrowDownToLine />
            </PanelButton>
          )}
          {mode === 'open' && (
            <PanelButton label={maximized ? 'Restore size' : 'Maximize'} onClick={() => setHeight(maximized ? DEFAULT_H : maxHeight())}>
              {maximized ? <Minimize2 /> : <Maximize2 />}
            </PanelButton>
          )}
          <PanelButton label={mode === 'open' ? 'Collapse' : 'Expand'} onClick={() => setMode(mode === 'open' ? 'collapsed' : 'open')}>
            {mode === 'open' ? <ChevronDown /> : <ChevronUp />}
          </PanelButton>
          <PanelButton label="Close (reopen from the terminal button)" onClick={() => setMode('hidden')}>
            <X />
          </PanelButton>
        </div>
      </div>
      {mode === 'open' && (
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="min-h-0 flex-1 overflow-auto bg-black/40 px-5 py-3 font-mono text-xs leading-relaxed"
        >
          {job.lines.map((l, i) => (
            <div
              key={i}
              className={cn('break-all whitespace-pre-wrap', l.startsWith('$ ') && 'text-brand', /^error|failed/i.test(l) && 'text-destructive')}
            >
              {l}
            </div>
          ))}
          {job.status === 'running' && <div className="text-muted-foreground animate-pulse">_</div>}
        </div>
      )}
    </section>
  )
}
