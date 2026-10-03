import { useEffect, useState } from 'react'
import { Loader2, Network, Pencil, Plus, Trash2, X } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { Switch } from '@workspace/ui/components/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@workspace/ui/components/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@workspace/ui/components/dialog'
import { cn } from '@workspace/ui/lib/utils'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState, PageHeader } from '@/components/page-header'
import { PM, useHealth, usePMMutation, useProjectServices, useUpstreams } from '@/hooks/use-proxy-manager'
import { api } from '@/lib/api'
import type { ProxyUpstream, UpstreamTarget } from '@/lib/types'
import { Field, NumberInput, ToggleRow } from './shared'

const algorithms = [
  { value: 'round_robin', label: 'Round robin (weighted)' },
  { value: 'least_conn', label: 'Least connections (nginx)' },
  { value: 'ip_hash', label: 'Client IP hash (nginx)' },
]

const blankTarget: UpstreamTarget = { kind: 'static', address: '127.0.0.1', port: 3000, project: '', service: '', weight: 1, backup: false }

const blank = (): Omit<ProxyUpstream, 'id'> & { id?: number } => ({
  name: '',
  algorithm: 'round_robin',
  scheme: 'http',
  tls_skip_verify: false,
  sticky: false,
  targets: [{ ...blankTarget }],
  health: { enabled: false, path: '/', interval: 10, timeout: 3, expect_status: 0 },
  notes: '',
})

export function UpstreamDialog({
  open,
  onOpenChange,
  upstream,
  onSaved,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  upstream?: ProxyUpstream
  onSaved?: (u: ProxyUpstream) => void
}) {
  const { data: services = [] } = useProjectServices()
  const [f, setF] = useState(blank())
  useEffect(() => {
    if (open) setF(upstream ? { ...upstream, targets: upstream.targets ?? [] } : blank())
  }, [open, upstream])
  const set = (patch: Partial<ProxyUpstream>) => setF((cur) => ({ ...cur, ...patch }))
  const setTarget = (i: number, patch: Partial<UpstreamTarget>) =>
    set({ targets: f.targets.map((t, j) => (i === j ? { ...t, ...patch } : t)) })
  const editing = !!upstream
  const save = usePMMutation(
    () => (editing ? api.put<ProxyUpstream>(`${PM}/upstreams/${upstream.id}`, f) : api.post<ProxyUpstream>(`${PM}/upstreams`, f)),
    {
      success: 'Upstream saved',
      onSuccess: (u) => {
        onSaved?.(u)
        onOpenChange(false)
      },
    },
  )
  const projects = [...new Map(services.map((s) => [s.project, s.project_name || s.project])).entries()]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${upstream.name}` : 'New upstream'}</DialogTitle>
          <DialogDescription>A pool of backends that hosts, routes and streams forward to.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid grid-cols-3 gap-4">
            <Field label="Name">
              <Input value={f.name} onChange={(e) => set({ name: e.target.value })} placeholder="app-backend" />
            </Field>
            <Field label="Load balancing">
              <Select value={f.algorithm} onValueChange={(v) => set({ algorithm: v as ProxyUpstream['algorithm'] })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {algorithms.map((a) => (
                    <SelectItem key={a.value} value={a.value}>
                      {a.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Backend protocol">
              <Select value={f.scheme} onValueChange={(v) => set({ scheme: v as 'http' | 'https' })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="http">HTTP</SelectItem>
                  <SelectItem value="https">HTTPS</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>

          <div className="grid gap-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Targets</span>
              <Button variant="outline" size="sm" onClick={() => set({ targets: [...f.targets, { ...blankTarget }] })}>
                <Plus /> Add target
              </Button>
            </div>
            {f.targets.length === 0 && <p className="text-muted-foreground text-xs">Add at least one target.</p>}
            {f.targets.map((t, i) => (
              <div key={i} className="grid grid-cols-[110px_1fr_90px_70px_auto_auto] items-center gap-2 rounded-md border p-2">
                <Select value={t.kind} onValueChange={(v) => setTarget(i, { kind: v as UpstreamTarget['kind'] })}>
                  <SelectTrigger size="sm" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="static">Address</SelectItem>
                    <SelectItem value="project">Project</SelectItem>
                  </SelectContent>
                </Select>
                {t.kind === 'static' ? (
                  <>
                    <Input value={t.address} onChange={(e) => setTarget(i, { address: e.target.value })} placeholder="10.0.0.5 or app.internal" className="h-8 font-mono text-xs" />
                    <NumberInput value={t.port} onChange={(v) => setTarget(i, { port: v })} placeholder="port" className="h-8" />
                  </>
                ) : (
                  <>
                    <Select value={t.project} onValueChange={(v) => setTarget(i, { project: v })}>
                      <SelectTrigger size="sm" className="w-full">
                        <SelectValue placeholder="Project" />
                      </SelectTrigger>
                      <SelectContent>
                        {projects.map(([slug, name]) => (
                          <SelectItem key={slug} value={slug}>
                            {name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Select value={t.service} onValueChange={(v) => setTarget(i, { service: v })}>
                      <SelectTrigger size="sm" className="w-full">
                        <SelectValue placeholder="Service" />
                      </SelectTrigger>
                      <SelectContent>
                        {services
                          .filter((s) => s.project === t.project)
                          .map((s) => (
                            <SelectItem key={s.service} value={s.service}>
                              {s.label} :{s.port}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                  </>
                )}
                <NumberInput value={t.weight} onChange={(v) => setTarget(i, { weight: v })} placeholder="weight" className="h-8" title="Weight" />
                <label className="text-muted-foreground flex items-center gap-1.5 text-xs" title="Only used when the other targets are down">
                  <Switch checked={t.backup} onCheckedChange={(v) => setTarget(i, { backup: v })} />
                  backup
                </label>
                <Button variant="ghost" size="icon-sm" aria-label="Remove target" onClick={() => set({ targets: f.targets.filter((_, j) => j !== i) })}>
                  <X />
                </Button>
              </div>
            ))}
            <p className="text-muted-foreground text-xs">
              Project targets follow the project&apos;s ports from config.toml; they are resolved at every deploy.
            </p>
          </div>

          <div className="grid gap-2 rounded-md border p-3">
            <ToggleRow
              label="Health checks"
              description="The manager probes each target and reports failures on the overview. Traefik also removes failing targets itself."
              checked={f.health.enabled}
              onChange={(v) => set({ health: { ...f.health, enabled: v } })}
            />
            {f.health.enabled && (
              <div className="grid grid-cols-4 gap-3">
                <Field label="Path" hint="Empty for a TCP check">
                  <Input value={f.health.path} onChange={(e) => set({ health: { ...f.health, path: e.target.value } })} className="font-mono text-xs" />
                </Field>
                <Field label="Interval (s)">
                  <NumberInput value={f.health.interval} onChange={(v) => set({ health: { ...f.health, interval: v } })} />
                </Field>
                <Field label="Timeout (s)">
                  <NumberInput value={f.health.timeout} onChange={(v) => set({ health: { ...f.health, timeout: v } })} />
                </Field>
                <Field label="Expect status" hint="Empty: any below 500">
                  <NumberInput value={f.health.expect_status} onChange={(v) => set({ health: { ...f.health, expect_status: v } })} />
                </Field>
              </div>
            )}
          </div>
          <div className="grid gap-1 rounded-md border p-3">
            <ToggleRow label="Sticky sessions" description="Keep a client on the same target with a cookie (Traefik) or IP hash (nginx)." checked={f.sticky} onChange={(v) => set({ sticky: v })} />
            {f.scheme === 'https' && (
              <ToggleRow label="Skip TLS verification" description="Accept self-signed backend certificates." checked={f.tls_skip_verify} onChange={(v) => set({ tls_skip_verify: v })} />
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || !f.name}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save upstream
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function targetLabel(t: UpstreamTarget) {
  return t.kind === 'project' ? `${t.project}/${t.service}` : `${t.address}:${t.port}`
}

export function UpstreamsPage() {
  const { data: upstreams, isLoading } = useUpstreams()
  const { data: health = {} } = useHealth()
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<ProxyUpstream>()
  const [del, setDel] = useState<ProxyUpstream | null>(null)
  const remove = usePMMutation((id: number) => api.delete(`${PM}/upstreams/${id}`), { success: 'Upstream deleted' })

  return (
    <>
      <PageHeader
        title="Upstreams"
        description="Backend pools with load balancing and health checks."
        actions={
          <Button
            onClick={() => {
              setEdit(undefined)
              setOpen(true)
            }}
          >
            <Plus /> New upstream
          </Button>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !upstreams?.length ? (
        <EmptyState
          icon={<Network />}
          title="No upstreams"
          description="Upstreams point at your services: fixed addresses or Supabase project services."
          action={
            <Button onClick={() => setOpen(true)}>
              <Plus /> New upstream
            </Button>
          }
        />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Targets</TableHead>
                <TableHead>Balancing</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {upstreams.map((u) => {
                const h = health[String(u.id)] ?? []
                return (
                  <TableRow key={u.id}>
                    <TableCell className="font-medium">
                      {u.name}
                      <div className="text-muted-foreground text-xs uppercase">{u.scheme}</div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1.5">
                        {(u.targets ?? []).map((t, i) => {
                          const st = u.health.enabled ? h[i] : undefined
                          return (
                            <span
                              key={i}
                              title={st?.error ?? (st ? `${st.latency_ms} ms` : undefined)}
                              className={cn(
                                'inline-flex items-center gap-1.5 rounded border px-1.5 py-0.5 font-mono text-xs',
                                t.backup && 'border-dashed',
                              )}
                            >
                              {st && <span className={cn('size-1.5 rounded-full', st.state === 'up' ? 'bg-brand' : 'bg-destructive')} />}
                              {targetLabel(t)}
                              {t.weight > 1 && <span className="text-muted-foreground">x{t.weight}</span>}
                            </span>
                          )
                        })}
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {algorithms.find((a) => a.value === u.algorithm)?.label.split(' (')[0]}
                      {u.health.enabled && ', health checked'}
                      {u.sticky && ', sticky'}
                    </TableCell>
                    <TableCell className="text-right whitespace-nowrap">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        aria-label="Edit"
                        onClick={() => {
                          setEdit(u)
                          setOpen(true)
                        }}
                      >
                        <Pencil />
                      </Button>
                      <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(u)}>
                        <Trash2 />
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
      <UpstreamDialog open={open} onOpenChange={setOpen} upstream={edit} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="Upstreams used by hosts, routes or streams cannot be deleted."
        confirmText="Delete"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
