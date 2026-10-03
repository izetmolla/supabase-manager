import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Pause, Play, ScrollText, Search, Trash2 } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { EmptyState } from '@/components/page-header'
import { useSlug } from '@/hooks/use-projects'
import { api, openStream } from '@/lib/api'
import type { Container } from '@/lib/types'
import { cn } from '@workspace/ui/lib/utils'

interface LogLine {
  id: number
  stream: string
  ts: string
  text: string
}

const MAX_LINES = 3000
const levelColor = (t: string) =>
  /\b(error|fatal|panic)\b/i.test(t) ? 'text-red-400' : /\bwarn(ing)?\b/i.test(t) ? 'text-amber-400' : ''

export function LogsPage() {
  const slug = useSlug()
  const [service, setService] = useState('')
  const [tail, setTail] = useState('300')
  const [lines, setLines] = useState<LogLine[]>([])
  const [paused, setPaused] = useState(false)
  const [filter, setFilter] = useState('')
  const [connected, setConnected] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const counter = useRef(0)
  const pausedRef = useRef(paused)
  pausedRef.current = paused

  const { data: containers } = useQuery({
    queryKey: ['project', slug, 'containers'],
    queryFn: () => api.get<Container[]>(`/projects/${slug}/containers`),
  })

  useEffect(() => {
    if (!service && containers?.length) {
      setService(containers.find((c) => c.service === 'db')?.service ?? containers[0].service)
    }
  }, [containers, service])

  useEffect(() => {
    if (!service) return
    let es: EventSource | null = null
    let cancelled = false
    setLines([])
    openStream(`/projects/${slug}/containers/${service}/logs?tail=${tail}&follow=true`).then((source) => {
      if (cancelled) return source.close()
      es = source
      es.onopen = () => setConnected(true)
      es.onmessage = (e) => {
        if (pausedRef.current) return
        const { stream, line } = JSON.parse(e.data) as { stream: string; line: string }
        const sp = line.indexOf(' ')
        const ts = sp > 0 && /^\d{4}-/.test(line) ? line.slice(0, sp) : ''
        const text = ts ? line.slice(sp + 1) : line
        setLines((cur) => {
          const next = [...cur, { id: counter.current++, stream, ts, text }]
          return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
        })
      }
      es.addEventListener('end', () => {
        setConnected(false)
        es?.close()
      })
      es.onerror = () => setConnected(false)
    })
    return () => {
      cancelled = true
      es?.close()
      setConnected(false)
    }
  }, [slug, service, tail])

  const visible = useMemo(() => {
    if (!filter) return lines
    const f = filter.toLowerCase()
    return lines.filter((l) => l.text.toLowerCase().includes(f))
  }, [lines, filter])

  useEffect(() => {
    const el = scrollRef.current
    if (el && !paused) el.scrollTop = el.scrollHeight
  }, [visible.length, paused])

  if (containers && containers.length === 0) {
    return (
      <div className="mx-auto max-w-5xl px-6 py-8">
        <EmptyState icon={<ScrollText />} title="No containers" description="Start the project to stream service logs." />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
        <h1 className="mr-2 text-sm font-medium">Logs</h1>
        <Select value={service} onValueChange={setService}>
          <SelectTrigger className="w-44" size="sm">
            <SelectValue placeholder="Service" />
          </SelectTrigger>
          <SelectContent>
            {containers?.map((c) => (
              <SelectItem key={c.id} value={c.service}>
                {c.service}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={tail} onValueChange={setTail}>
          <SelectTrigger className="w-32" size="sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {['100', '300', '1000', '3000'].map((t) => (
              <SelectItem key={t} value={t}>
                Last {t}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="relative">
          <Search className="text-muted-foreground absolute top-1/2 left-2 size-3.5 -translate-y-1/2" />
          <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" className="h-7 w-56 pl-7 text-xs" />
        </div>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
            <span className={cn('size-1.5 rounded-full', connected ? 'bg-brand animate-pulse' : 'bg-muted-foreground')} />
            {connected ? 'Live' : 'Disconnected'}
          </span>
          <Button variant="outline" size="sm" onClick={() => setPaused((p) => !p)}>
            {paused ? <Play /> : <Pause />}
            {paused ? 'Resume' : 'Pause'}
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={() => setLines([])} aria-label="Clear">
            <Trash2 />
          </Button>
        </div>
      </div>
      <div ref={scrollRef} className="flex-1 overflow-auto bg-black/30 py-2 font-mono text-xs leading-5">
        {visible.length === 0 && <p className="text-muted-foreground px-4">Waiting for log lines...</p>}
        {visible.map((l) => (
          <div key={l.id} className="hover:bg-accent/30 flex gap-3 px-4">
            <span className="text-muted-foreground shrink-0 tabular-nums">{l.ts ? l.ts.slice(11, 23) : ''}</span>
            <span className={cn('break-all whitespace-pre-wrap', l.stream === 'stderr' && 'text-foreground/80', levelColor(l.text))}>{l.text}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
