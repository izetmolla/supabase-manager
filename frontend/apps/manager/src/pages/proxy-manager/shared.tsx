import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { AlertTriangle, FileDiff, Loader2, Plus, Rocket, X } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Switch } from '@workspace/ui/components/switch'
import { Textarea } from '@workspace/ui/components/textarea'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@workspace/ui/components/dialog'
import { cn } from '@workspace/ui/lib/utils'
import { LazyDiff } from '@/components/lazy-editor'
import { StatusBadge } from '@/components/status-badge'
import { useDeploy, useDeployAll, useInstances, usePreview, useProxyStatus } from '@/hooks/use-proxy-manager'
import type { Certificate, KV, ProxyInstanceView, PreviewFile } from '@/lib/types'
import { daysLeft, fileLanguage, instanceStatus } from './utils'

export function Field({ label, hint, children, className }: { label: ReactNode; hint?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div className={cn('grid gap-1.5', className)}>
      <Label className="text-sm">{label}</Label>
      {children}
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  )
}

export function ToggleRow({
  label,
  description,
  checked,
  onChange,
  disabled,
}: {
  label: ReactNode
  description?: ReactNode
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
}) {
  return (
    <div className="flex items-start justify-between gap-4 py-1">
      <div className="grid gap-0.5">
        <span className="text-sm">{label}</span>
        {description && <span className="text-muted-foreground text-xs">{description}</span>}
      </div>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} />
    </div>
  )
}

/** Integer input where an empty field means 0. */
export function NumberInput({
  value,
  onChange,
  className,
  ...rest
}: { value: number; onChange: (v: number) => void; className?: string } & Omit<React.ComponentProps<typeof Input>, 'value' | 'onChange'>) {
  return (
    <Input
      type="number"
      inputMode="numeric"
      value={value === 0 ? '' : String(value)}
      onChange={(e) => onChange(e.target.value === '' ? 0 : Number(e.target.value))}
      className={className}
      {...rest}
    />
  )
}

/** Editable list of name/value pairs (headers). */
export function KVEditor({
  value,
  onChange,
  namePlaceholder = 'Header',
  valuePlaceholder = 'Value',
  addLabel = 'Add header',
}: {
  value: KV[] | null
  onChange: (v: KV[]) => void
  namePlaceholder?: string
  valuePlaceholder?: string
  addLabel?: string
}) {
  const rows = value ?? []
  const set = (i: number, patch: Partial<KV>) => onChange(rows.map((r, j) => (i === j ? { ...r, ...patch } : r)))
  return (
    <div className="grid gap-2">
      {rows.map((r, i) => (
        <div key={i} className="flex gap-2">
          <Input value={r.name} placeholder={namePlaceholder} onChange={(e) => set(i, { name: e.target.value })} className="font-mono text-xs" />
          <Input value={r.value} placeholder={valuePlaceholder} onChange={(e) => set(i, { value: e.target.value })} className="font-mono text-xs" />
          <Button variant="ghost" size="icon-sm" aria-label="Remove" onClick={() => onChange(rows.filter((_, j) => j !== i))}>
            <X />
          </Button>
        </div>
      ))}
      <div>
        <Button variant="outline" size="sm" onClick={() => onChange([...rows, { name: '', value: '' }])}>
          <Plus /> {addLabel}
        </Button>
      </div>
    </div>
  )
}

/** One entry per line (domains, CIDRs, origins). */
export function LinesInput({
  value,
  onChange,
  placeholder,
  rows = 3,
}: {
  value: string[] | null
  onChange: (v: string[]) => void
  placeholder?: string
  rows?: number
}) {
  const joined = (value ?? []).join('\n')
  const [text, setText] = useState(joined)
  const emitted = useRef(joined)
  useEffect(() => {
    // Only external changes replace the text; our own edits keep the user's formatting.
    if (joined !== emitted.current) {
      emitted.current = joined
      setText(joined)
    }
  }, [joined])
  return (
    <Textarea
      rows={rows}
      value={text}
      placeholder={placeholder}
      className="font-mono text-xs"
      onChange={(e) => {
        const list = e.target.value
          .split(/[\n,]/)
          .map((l) => l.trim())
          .filter(Boolean)
        emitted.current = list.join('\n')
        setText(e.target.value)
        onChange(list)
      }}
    />
  )
}

export function InstanceBadge({ v }: { v: ProxyInstanceView }) {
  if (!v.container?.exists) {
    return (
      <span className="text-muted-foreground border-border bg-muted/50 inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium">
        <span className="bg-muted-foreground size-1.5 rounded-full" />
        Not deployed
      </span>
    )
  }
  return <StatusBadge status={instanceStatus(v)} />
}

/** Whether the instance's agent is connected and runs the deployed configuration. */
export function AgentBadge({ v }: { v: ProxyInstanceView }) {
  if (!v.container?.running) return null
  const [label, tone, title] = !v.agent.connected
    ? ['agent offline', 'text-destructive', 'The agent in the container is not connected to the manager']
    : !v.agent.proxy_running
      ? [`${v.kind} down`, 'text-destructive', v.agent.last_error || `${v.kind} is not running`]
      : v.deployed_revision_id && !v.in_sync
        ? ['out of sync', 'text-amber-500', 'The live configuration differs from the deployed revision']
        : ['agent connected', 'text-emerald-500', `Agent ${v.agent.agent_version ?? ''}, ${v.agent.proxy_version ?? ''}`]
  return (
    <span className={cn('inline-flex items-center gap-1 text-xs', tone)} title={title}>
      <span className="size-1.5 rounded-full bg-current" />
      {label}
    </span>
  )
}

export function KindBadge({ kind }: { kind: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded border px-1.5 py-px font-mono text-[11px] uppercase',
        kind === 'nginx' ? 'border-emerald-500/30 text-emerald-500' : 'border-sky-500/30 text-sky-500',
      )}
    >
      {kind}
    </span>
  )
}

/** Toggle buttons choosing which instances serve a host or stream. */
export function InstancePicker({ value, onChange }: { value: number[] | null; onChange: (v: number[]) => void }) {
  const { data: instances = [] } = useInstances()
  const ids = value ?? []
  if (instances.length === 0) {
    return <p className="text-muted-foreground text-sm">Add a proxy instance first (Proxy Instances).</p>
  }
  return (
    <div className="flex flex-wrap gap-2">
      {instances.map((i) => {
        const on = ids.includes(i.id)
        return (
          <button
            key={i.id}
            type="button"
            onClick={() => onChange(on ? ids.filter((x) => x !== i.id) : [...ids, i.id])}
            className={cn(
              'flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm transition-colors',
              on ? 'border-brand bg-brand/10 text-foreground' : 'text-muted-foreground hover:bg-muted',
            )}
          >
            <span className={cn('size-2 rounded-full', on ? 'bg-brand' : 'bg-muted-foreground/40')} />
            {i.name}
            <KindBadge kind={i.kind} />
            <span className="text-muted-foreground font-mono text-xs">
              {i.bind_ip || '*'}:{[i.http_port, i.https_port].filter(Boolean).join('/')}
            </span>
          </button>
        )
      })}
    </div>
  )
}

export function CertStatus({ c }: { c: Certificate }) {
  const d = daysLeft(c)
  if (c.status === 'error') return <span className="text-destructive text-xs font-medium">Error</span>
  if (c.status === 'pending' && !c.not_after) return <span className="text-xs font-medium text-amber-500">Pending</span>
  if (d === null) return <span className="text-muted-foreground text-xs">-</span>
  if (d < 0) return <span className="text-destructive text-xs font-medium">Expired</span>
  return (
    <span className={cn('text-xs font-medium', d < 14 ? 'text-destructive' : d < 30 ? 'text-amber-500' : 'text-brand')}>
      {d} days left
    </span>
  )
}

const statusColor: Record<PreviewFile['status'], string> = {
  added: 'text-brand',
  removed: 'text-destructive line-through',
  changed: 'text-amber-500',
  same: 'text-muted-foreground',
}

/** Rendered configuration of one instance compared with what is deployed, with a deploy button. */
export function DiffDialog({
  open,
  onOpenChange,
  instanceIds,
  initial,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  instanceIds?: number[]
  initial?: number
}) {
  const { data: instances = [] } = useInstances()
  const choices = useMemo(
    () => (instanceIds ? instances.filter((i) => instanceIds.includes(i.id)) : instances),
    [instances, instanceIds],
  )
  const [picked, setPicked] = useState<number>()
  const current = picked ?? initial ?? choices[0]?.id
  const inst = choices.find((i) => i.id === current)
  const { data: preview, isLoading } = usePreview(current, open)
  const [showSame, setShowSame] = useState(false)
  const [file, setFile] = useState<string>()
  const [note, setNote] = useState('')
  const deploy = useDeploy()

  const files = (preview?.files ?? []).filter((f) => showSame || f.status !== 'same')
  const selected = files.find((f) => f.path === file) ?? files[0]

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          setPicked(undefined)
          setFile(undefined)
        }
        onOpenChange(o)
      }}
    >
      <DialogContent className="flex max-h-[92vh] flex-col sm:max-w-6xl">
        <DialogHeader>
          <DialogTitle>Review changes</DialogTitle>
          <DialogDescription>
            The configuration rendered from your edits, compared with the revision running on the proxy.
          </DialogDescription>
        </DialogHeader>
        {choices.length > 1 && (
          <div className="flex flex-wrap gap-1.5">
            {choices.map((i) => (
              <Button
                key={i.id}
                size="sm"
                variant={i.id === current ? 'secondary' : 'ghost'}
                onClick={() => {
                  setPicked(i.id)
                  setFile(undefined)
                }}
              >
                {i.name}
                <KindBadge kind={i.kind} />
                {i.pending && <span className="size-1.5 rounded-full bg-amber-500" />}
              </Button>
            ))}
          </div>
        )}
        {isLoading || !preview ? (
          <div className="flex h-64 items-center justify-center">
            <Loader2 className="text-muted-foreground animate-spin" />
          </div>
        ) : (
          <div className="grid min-h-0 flex-1 gap-3 overflow-hidden">
            {!!preview.warnings?.length && (
              <div className="grid gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-xs">
                {preview.warnings.map((w, i) => (
                  <div key={i} className="flex gap-2">
                    <AlertTriangle className="size-3.5 shrink-0 text-amber-500" />
                    {w}
                  </div>
                ))}
              </div>
            )}
            <div className="grid min-h-0 grid-cols-[220px_1fr] gap-3">
              <div className="flex min-h-0 flex-col gap-2 overflow-auto rounded-md border p-2">
                {files.length === 0 && <p className="text-muted-foreground p-2 text-xs">No changes.</p>}
                {files.map((f) => (
                  <button
                    key={f.path}
                    type="button"
                    onClick={() => setFile(f.path)}
                    className={cn(
                      'truncate rounded px-2 py-1 text-left font-mono text-xs',
                      selected?.path === f.path ? 'bg-muted' : 'hover:bg-muted/50',
                      statusColor[f.status],
                    )}
                    title={f.path}
                  >
                    {f.path}
                  </button>
                ))}
                <label className="text-muted-foreground mt-auto flex items-center gap-2 px-2 pt-2 text-xs">
                  <Switch checked={showSame} onCheckedChange={setShowSame} />
                  Unchanged files
                </label>
              </div>
              <div className="min-w-0">
                {selected ? (
                  <LazyDiff original={selected.deployed} modified={selected.content} language={fileLanguage(selected.path)} height="52vh" />
                ) : (
                  <div className="text-muted-foreground flex h-[52vh] items-center justify-center rounded-md border text-sm">
                    {preview.pending ? 'Select a file' : 'The deployed configuration is up to date.'}
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
        <DialogFooter className="items-center gap-2 sm:justify-between">
          <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder="Revision note (optional)" className="sm:max-w-sm" />
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Close
            </Button>
            <Button
              disabled={!inst || deploy.isPending || !inst.enabled}
              onClick={() =>
                inst &&
                deploy.mutate(
                  { id: inst.id, name: inst.name, note },
                  {
                    onSuccess: () => {
                      setNote('')
                      onOpenChange(false)
                    },
                  },
                )
              }
            >
              {deploy.isPending ? <Loader2 className="animate-spin" /> : <Rocket />}
              Deploy {inst?.name}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Sticky bar shown while edits have not been deployed to every proxy. */
export function DeployBar() {
  const { data: status } = useProxyStatus()
  const { data: instances = [] } = useInstances()
  const deployAll = useDeployAll()
  const [open, setOpen] = useState(false)
  const pending = instances.filter((i) => i.pending)
  const ids = pending.length ? pending.map((i) => i.id) : (status?.pending_instances ?? [])
  if (ids.length === 0) return null
  return (
    <>
      <div className="bg-background/95 sticky top-0 z-10 flex flex-wrap items-center gap-3 border-b border-amber-500/30 px-6 py-2.5 backdrop-blur">
        <span className="size-2 animate-pulse rounded-full bg-amber-500" />
        <p className="text-sm">
          <span className="font-medium">Undeployed changes</span>
          <span className="text-muted-foreground">
            {' '}
            on {pending.map((p) => p.name).join(', ') || `${ids.length} proxies`}. Proxies keep serving the last deployed revision.
          </span>
        </p>
        <div className="ml-auto flex gap-2">
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
            <FileDiff /> Review changes
          </Button>
          <Button size="sm" onClick={() => deployAll.mutate()} disabled={deployAll.isPending}>
            {deployAll.isPending ? <Loader2 className="animate-spin" /> : <Rocket />}
            Deploy {ids.length > 1 ? 'all' : ''}
          </Button>
        </div>
      </div>
      <DiffDialog open={open} onOpenChange={setOpen} instanceIds={ids} />
    </>
  )
}

/** Wraps forms in a bordered card section with a title. */
export function Panel({ title, description, children, actions }: { title: string; description?: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="bg-card rounded-lg border">
      <div className="flex items-start justify-between gap-4 border-b px-5 py-3.5">
        <div className="grid gap-0.5">
          <h2 className="text-sm font-medium">{title}</h2>
          {description && <p className="text-muted-foreground text-xs">{description}</p>}
        </div>
        {actions}
      </div>
      <div className="grid gap-4 p-5">{children}</div>
    </section>
  )
}
