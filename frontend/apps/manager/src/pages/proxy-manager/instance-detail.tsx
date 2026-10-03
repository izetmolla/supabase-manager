import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, FileDiff, Loader2, Pause, Pencil, Play, RotateCcw, Rocket, Search, Square, Trash2 } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@workspace/ui/components/tabs'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@workspace/ui/components/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@workspace/ui/components/dialog'
import { cn } from '@workspace/ui/lib/utils'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LazyEditor } from '@/components/lazy-editor'
import { PM, useDeploy, useHosts, useInstances, useJobAction, usePMMutation, usePreview, useRevisions } from '@/hooks/use-proxy-manager'
import { api, openStream } from '@/lib/api'
import { formatBytes, formatDate, timeAgo } from '@/lib/format'
import type { AccessEntry, ConfigRevision, ProxyInstanceView } from '@/lib/types'
import { InstanceDialog } from './instances'
import { AgentBadge, DiffDialog, InstanceBadge, KindBadge } from './shared'
import { fileLanguage } from './utils'

function Info({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-0.5">
      <span className="text-muted-foreground text-xs">{label}</span>
      <span className="text-sm">{children}</span>
    </div>
  )
}

function ConfigTab({ inst }: { inst: ProxyInstanceView }) {
  const { data: preview, isLoading } = usePreview(inst.id)
  const [file, setFile] = useState<string>()
  if (isLoading || !preview) return <Skeleton className="h-64" />
  const selected = preview.files.find((f) => f.path === file) ?? preview.files[0]
  return (
    <div className="grid grid-cols-[220px_1fr] gap-3">
      <div className="flex max-h-[60vh] flex-col gap-0.5 overflow-auto rounded-md border p-2">
        {preview.files.map((f) => (
          <button
            key={f.path}
            type="button"
            onClick={() => setFile(f.path)}
            className={cn(
              'truncate rounded px-2 py-1 text-left font-mono text-xs',
              selected?.path === f.path ? 'bg-muted' : 'hover:bg-muted/50',
              f.status === 'same' ? '' : 'text-amber-500',
            )}
            title={f.path}
          >
            {f.path}
          </button>
        ))}
      </div>
      <div className="min-w-0">
        {selected && <LazyEditor value={selected.content} language={fileLanguage(selected.path)} readOnly height="60vh" />}
        <p className="text-muted-foreground mt-2 text-xs">
          Rendered from the current drafts{preview.pending ? '; highlighted files differ from the deployed revision' : ' (identical to the deployed revision)'}.
          Private keys and tokens are hidden.
        </p>
      </div>
    </div>
  )
}

export function RevisionsPanel({ inst }: { inst: ProxyInstanceView }) {
  const { data: revisions, isLoading } = useRevisions(inst.id)
  const [view, setView] = useState<ConfigRevision | null>(null)
  const [files, setFiles] = useState<Record<string, string> | null>(null)
  const [file, setFile] = useState<string>()
  const [rollback, setRollback] = useState<ConfigRevision | null>(null)
  const roll = useJobAction<ConfigRevision>(
    (r) => `/instances/${inst.id}/revisions/${r.id}/rollback`,
    (r) => `Roll back ${inst.name} to revision ${r.number}`,
  )

  useEffect(() => {
    if (!view) return
    setFiles(null)
    api.get<Record<string, string>>(`${PM}/instances/${inst.id}/revisions/${view.id}`).then(setFiles, () => setFiles({}))
  }, [view, inst.id])

  if (isLoading) return <Skeleton className="h-32" />
  if (!revisions?.length) return <p className="text-muted-foreground text-sm">No revisions yet. Every deploy and rollback creates one.</p>
  const names = files ? Object.keys(files).sort() : []
  const shown = file && files?.[file] !== undefined ? file : names[0]

  return (
    <>
      <div className="bg-card overflow-hidden rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>#</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Note</TableHead>
              <TableHead>When</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {revisions.map((r) => (
              <TableRow key={r.id}>
                <TableCell className="font-mono text-xs">
                  {r.number}
                  {r.id === inst.deployed_revision_id && <span className="text-brand ml-2 font-sans">live</span>}
                </TableCell>
                <TableCell className="text-xs capitalize">{r.kind}</TableCell>
                <TableCell>
                  <span
                    className={cn(
                      'text-xs font-medium',
                      r.status === 'applied' ? 'text-brand' : r.status === 'failed' ? 'text-destructive' : 'text-amber-500',
                    )}
                  >
                    {r.status}
                  </span>
                  {r.error && (
                    <div className="text-destructive max-w-80 truncate font-mono text-[11px]" title={r.error}>
                      {r.error}
                    </div>
                  )}
                </TableCell>
                <TableCell className="text-muted-foreground max-w-56 truncate text-xs">{r.note}</TableCell>
                <TableCell className="text-muted-foreground text-xs" title={formatDate(r.created_at)}>
                  {timeAgo(r.created_at)}
                </TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  <Button variant="ghost" size="xs" onClick={() => setView(r)}>
                    View
                  </Button>
                  {r.status === 'applied' && r.id !== inst.deployed_revision_id && (
                    <Button variant="ghost" size="xs" onClick={() => setRollback(r)}>
                      <RotateCcw /> Roll back
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <Dialog open={!!view} onOpenChange={(o) => !o && setView(null)}>
        <DialogContent className="sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle>Revision {view?.number}</DialogTitle>
            <DialogDescription>
              {view && `${view.kind}, ${view.status}, ${formatDate(view.created_at)}`}
              {!!view?.warnings?.length && ` - ${view.warnings.length} warnings`}
            </DialogDescription>
          </DialogHeader>
          {!files ? (
            <Skeleton className="h-64" />
          ) : (
            <div className="grid grid-cols-[200px_1fr] gap-3">
              <div className="flex max-h-[60vh] flex-col gap-0.5 overflow-auto rounded-md border p-2">
                {names.map((n) => (
                  <button
                    key={n}
                    type="button"
                    onClick={() => setFile(n)}
                    className={cn('truncate rounded px-2 py-1 text-left font-mono text-xs', shown === n ? 'bg-muted' : 'hover:bg-muted/50')}
                  >
                    {n}
                  </button>
                ))}
              </div>
              {shown && <LazyEditor value={files[shown]} language={fileLanguage(shown)} readOnly height="60vh" />}
            </div>
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={!!rollback}
        onOpenChange={(o) => !o && setRollback(null)}
        title={`Roll back to revision ${rollback?.number}?`}
        description="The stored files of that revision are validated and applied as a new revision. Your drafts are not changed, so the proxy will show pending changes afterwards."
        confirmText="Roll back"
        onConfirm={async () => {
          await roll.mutateAsync(rollback!)
        }}
      />
    </>
  )
}

function ContainerLogs({ inst }: { inst: ProxyInstanceView }) {
  const [lines, setLines] = useState<{ id: number; text: string; err: boolean }[]>([])
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(paused)
  useEffect(() => {
    pausedRef.current = paused
  }, [paused])
  const ref = useRef<HTMLDivElement>(null)
  const n = useRef(0)
  useEffect(() => {
    let es: EventSource | null = null
    let cancelled = false
    setLines([])
    openStream(`${PM}/instances/${inst.id}/logs?tail=300`).then((s) => {
      if (cancelled) return s.close()
      es = s
      es.onmessage = (e) => {
        if (pausedRef.current) return
        const { stream, line } = JSON.parse(e.data) as { stream: string; line: string }
        setLines((cur) => [...cur.slice(-2000), { id: n.current++, text: line, err: stream === 'stderr' }])
      }
      es.addEventListener('end', () => es?.close())
    })
    return () => {
      cancelled = true
      es?.close()
    }
  }, [inst.id, inst.container?.id])
  useEffect(() => {
    if (ref.current && !paused) ref.current.scrollTop = ref.current.scrollHeight
  }, [lines.length, paused])
  return (
    <div className="grid gap-2">
      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={() => setPaused((p) => !p)}>
          {paused ? <Play /> : <Pause />} {paused ? 'Resume' : 'Pause'}
        </Button>
      </div>
      <div ref={ref} className="h-[60vh] overflow-auto rounded-md border bg-black/30 py-2 font-mono text-xs leading-5">
        {lines.length === 0 && <p className="text-muted-foreground px-4">{inst.container?.exists ? 'Waiting for log lines...' : 'The container does not exist yet. Deploy the instance first.'}</p>}
        {lines.map((l) => (
          <div key={l.id} className={cn('px-4 break-all whitespace-pre-wrap', l.err && 'text-amber-300/90')}>
            {l.text}
          </div>
        ))}
      </div>
    </div>
  )
}

const statusTone = (s: number) => (s >= 500 ? 'text-destructive' : s >= 400 ? 'text-amber-500' : s >= 300 ? 'text-sky-400' : 'text-brand')

export function AccessLogView({ instanceId, hostId: fixedHost }: { instanceId: number; hostId?: number }) {
  const { data: hosts = [] } = useHosts()
  const [host, setHost] = useState(fixedHost ? String(fixedHost) : 'all')
  const [rows, setRows] = useState<(AccessEntry & { k: number })[]>([])
  const [filter, setFilter] = useState('')
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(paused)
  useEffect(() => {
    pausedRef.current = paused
  }, [paused])
  const n = useRef(0)
  useEffect(() => {
    let es: EventSource | null = null
    let cancelled = false
    setRows([])
    const q = host !== 'all' ? `&host=${host}` : ''
    openStream(`${PM}/instances/${instanceId}/access-log?tail=200${q}`).then((s) => {
      if (cancelled) return s.close()
      es = s
      es.onmessage = (e) => {
        if (pausedRef.current) return
        const entry = JSON.parse(e.data) as AccessEntry
        setRows((cur) => [{ ...entry, k: n.current++ }, ...cur].slice(0, 1000))
      }
      es.addEventListener('end', () => es?.close())
    })
    return () => {
      cancelled = true
      es?.close()
    }
  }, [instanceId, host])
  const hostName = useMemo(() => new Map(hosts.map((h) => [h.id, h.name])), [hosts])
  const visible = filter
    ? rows.filter((r) => `${r.host} ${r.uri} ${r.remote} ${r.status} ${r.method}`.toLowerCase().includes(filter.toLowerCase()))
    : rows
  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {!fixedHost && (
          <Select value={host} onValueChange={setHost}>
            <SelectTrigger size="sm" className="w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All hosts</SelectItem>
              {hosts
                .filter((h) => h.instance_ids?.includes(instanceId))
                .map((h) => (
                  <SelectItem key={h.id} value={String(h.id)}>
                    {h.name}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        )}
        <div className="relative">
          <Search className="text-muted-foreground absolute top-1/2 left-2 size-3.5 -translate-y-1/2" />
          <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" className="h-7 w-56 pl-7 text-xs" />
        </div>
        <Button variant="outline" size="sm" className="ml-auto" onClick={() => setPaused((p) => !p)}>
          {paused ? <Play /> : <Pause />} {paused ? 'Resume' : 'Pause'}
        </Button>
      </div>
      <div className="bg-card max-h-[60vh] overflow-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Request</TableHead>
              <TableHead>Client</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className="text-right">Time</TableHead>
              <TableHead>Upstream</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visible.length === 0 && (
              <TableRow>
                <TableCell colSpan={7} className="text-muted-foreground py-6 text-center text-sm">
                  Waiting for requests...
                </TableCell>
              </TableRow>
            )}
            {visible.map((r) => (
              <TableRow key={r.k} className="font-mono text-xs">
                <TableCell className="text-muted-foreground whitespace-nowrap">{r.time.replace('T', ' ').slice(5, 19)}</TableCell>
                <TableCell className={statusTone(r.status)}>{r.status}</TableCell>
                <TableCell className="max-w-96 truncate" title={`${r.method} ${r.host}${r.uri}`}>
                  <span className="text-muted-foreground">{r.method}</span> {r.host}
                  {r.uri}
                  {r.host_id > 0 && !fixedHost && host === 'all' && (
                    <span className="text-muted-foreground ml-2 font-sans">({hostName.get(r.host_id) ?? `#${r.host_id}`})</span>
                  )}
                </TableCell>
                <TableCell>{r.remote}</TableCell>
                <TableCell className="text-right">{formatBytes(r.bytes)}</TableCell>
                <TableCell className="text-right">{r.duration_ms.toFixed(0)} ms</TableCell>
                <TableCell className="text-muted-foreground max-w-40 truncate">{r.upstream}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

export function InstanceDetailPage() {
  const id = Number(useParams().id)
  const navigate = useNavigate()
  const { data: instances, isLoading } = useInstances()
  const inst = instances?.find((i) => i.id === id)
  const [diff, setDiff] = useState(false)
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  const deploy = useDeploy()
  const action = usePMMutation((a: 'start' | 'stop' | 'restart') => api.post(`${PM}/instances/${id}/${a}`), {
    success: 'Done',
  })
  const remove = usePMMutation(() => api.delete(`${PM}/instances/${id}`), { success: 'Instance deleted' })

  if (isLoading) return <Skeleton className="h-48" />
  if (!inst) return <p className="text-muted-foreground text-sm">Instance not found.</p>
  const running = !!inst.container?.running

  return (
    <>
      <Link to="/proxy-manager/instances" className="text-muted-foreground hover:text-foreground mb-3 inline-flex items-center gap-1 text-xs">
        <ArrowLeft className="size-3" /> Proxy instances
      </Link>
      <div className="mb-6 flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-medium tracking-tight">{inst.name}</h1>
        <KindBadge kind={inst.kind} />
        <InstanceBadge v={inst} />
        <AgentBadge v={inst} />
        {inst.pending && <span className="text-xs text-amber-500">changes pending</span>}
        <div className="ml-auto flex flex-wrap gap-2">
          {inst.container?.exists &&
            (running ? (
              <>
                <Button variant="outline" size="sm" onClick={() => action.mutate('restart')} disabled={action.isPending}>
                  <RotateCcw /> Restart
                </Button>
                <Button variant="outline" size="sm" onClick={() => action.mutate('stop')} disabled={action.isPending}>
                  <Square /> Stop
                </Button>
              </>
            ) : (
              <Button variant="outline" size="sm" onClick={() => action.mutate('start')} disabled={action.isPending}>
                <Play /> Start
              </Button>
            ))}
          <Button variant="outline" size="sm" onClick={() => setEdit(true)}>
            <Pencil /> Edit
          </Button>
          <Button variant="outline" size="sm" onClick={() => setDiff(true)}>
            <FileDiff /> Review
          </Button>
          <Button size="sm" onClick={() => deploy.mutate({ id: inst.id, name: inst.name })} disabled={deploy.isPending || !inst.enabled}>
            {deploy.isPending ? <Loader2 className="animate-spin" /> : <Rocket />}
            Deploy
          </Button>
        </div>
      </div>

      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="config">Configuration</TabsTrigger>
          <TabsTrigger value="revisions">Revisions</TabsTrigger>
          <TabsTrigger value="logs">Container logs</TabsTrigger>
          <TabsTrigger value="access">Access log</TabsTrigger>
        </TabsList>
        <TabsContent value="overview" className="mt-4 grid gap-4">
          <div className="bg-card grid gap-5 rounded-lg border p-5 sm:grid-cols-3">
            <Info label="Image">
              <span className="font-mono text-xs">{inst.image}</span>
            </Info>
            <Info label="HTTP">
              <span className="font-mono text-xs">{inst.http_port ? `${inst.bind_ip || '0.0.0.0'}:${inst.http_port}` : 'disabled'}</span>
            </Info>
            <Info label="HTTPS">
              <span className="font-mono text-xs">{inst.https_port ? `${inst.bind_ip || '0.0.0.0'}:${inst.https_port}` : 'disabled'}</span>
            </Info>
            {inst.kind === 'traefik' && (
              <Info label="Admin API">
                <span className="font-mono text-xs">127.0.0.1:{inst.admin_port}</span>
              </Info>
            )}
            {inst.kind === 'nginx' && <Info label="TLS-ALPN-01 forwarding">{inst.tls_alpn ? 'On' : 'Off'}</Info>}
            <Info label="Serves">
              {inst.host_count} hosts, {inst.stream_count} streams
            </Info>
            <Info label="Container">
              <span className="font-mono text-xs">
                sm-proxy-{inst.id} {inst.container?.exists ? `(${inst.container.status})` : '(not created)'}
              </span>
            </Info>
            <Info label="Agent">
              {inst.agent.connected ? (
                <span className="font-mono text-xs">
                  {inst.agent.agent_version} ({inst.agent.proxy_version})
                </span>
              ) : (
                <span className="text-muted-foreground text-xs">not connected</span>
              )}
            </Info>
            <Info label="Live configuration">
              {!inst.agent.connected ? (
                '-'
              ) : inst.agent.config_checksum ? (
                <span className={cn('font-mono text-xs', !inst.in_sync && 'text-amber-500')}>
                  {inst.agent.config_checksum.slice(0, 12)} {inst.in_sync ? '(deployed revision)' : '(differs from the deployed revision)'}
                </span>
              ) : (
                <span className="text-muted-foreground text-xs">none</span>
              )}
            </Info>
            <Info label="Proxy restarts">{inst.agent.connected ? inst.agent.restarts : '-'}</Info>
            <Info label="Started">{inst.container?.started_at ? timeAgo(inst.container.started_at) : '-'}</Info>
            <Info label="Last deploy">{inst.deployed_at ? formatDate(inst.deployed_at) : 'never'}</Info>
          </div>
          {inst.container?.error && <p className="text-destructive text-sm">{inst.container.error}</p>}
          {inst.agent.last_error && <p className="text-destructive text-sm">{inst.agent.last_error}</p>}
          {inst.notes && <p className="text-muted-foreground text-sm whitespace-pre-wrap">{inst.notes}</p>}
          <div>
            <Button variant="destructive" size="sm" onClick={() => setDel(true)}>
              <Trash2 /> Delete instance
            </Button>
          </div>
        </TabsContent>
        <TabsContent value="config" className="mt-4">
          <ConfigTab inst={inst} />
        </TabsContent>
        <TabsContent value="revisions" className="mt-4">
          <RevisionsPanel inst={inst} />
        </TabsContent>
        <TabsContent value="logs" className="mt-4">
          <ContainerLogs inst={inst} />
        </TabsContent>
        <TabsContent value="access" className="mt-4">
          <AccessLogView instanceId={inst.id} />
        </TabsContent>
      </Tabs>

      <DiffDialog open={diff} onOpenChange={setDiff} instanceIds={[inst.id]} initial={inst.id} />
      <InstanceDialog open={edit} onOpenChange={setEdit} instance={inst} />
      <ConfirmDialog
        open={del}
        onOpenChange={setDel}
        title={`Delete ${inst.name}?`}
        description="The container, its volumes and its revision history are removed."
        confirmText="Delete instance"
        typeToConfirm={inst.name}
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(undefined)
          navigate('/proxy-manager/instances')
        }}
      />
    </>
  )
}

export function RevisionsPage() {
  const { data: instances = [], isLoading } = useInstances()
  const [id, setId] = useState<string>('')
  useEffect(() => {
    if (!id && instances.length) setId(String(instances[0].id))
  }, [instances, id])
  const inst = instances.find((i) => String(i.id) === id)
  return (
    <>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div className="grid gap-1">
          <h1 className="text-2xl font-medium tracking-tight">Revisions</h1>
          <p className="text-muted-foreground text-sm">Every deploy is stored per instance and can be inspected or rolled back.</p>
        </div>
        {instances.length > 0 && (
          <Select value={id} onValueChange={setId}>
            <SelectTrigger className="w-56">
              <SelectValue placeholder="Instance" />
            </SelectTrigger>
            <SelectContent>
              {instances.map((i) => (
                <SelectItem key={i.id} value={String(i.id)}>
                  {i.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
      {isLoading ? <Skeleton className="h-32" /> : inst ? <RevisionsPanel inst={inst} /> : <p className="text-muted-foreground text-sm">No proxy instances yet.</p>}
    </>
  )
}
