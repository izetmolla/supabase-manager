import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowUpCircle, Check, ChevronsUpDown, Loader2, Pencil, Plus, RefreshCw, Server, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@workspace/ui/components/command'
import { Popover, PopoverContent, PopoverTrigger } from '@workspace/ui/components/popover'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Textarea } from '@workspace/ui/components/textarea'
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
import {
  PM,
  updateLabel,
  useCheckProxyUpdates,
  useInstances,
  usePMMutation,
  useProxyImages,
  useProxyMeta,
  useProxyUpdates,
  useUpdateProxyImage,
} from '@/hooks/use-proxy-manager'
import { api, errorMessage } from '@/lib/api'
import { formatBytes, timeAgo } from '@/lib/format'
import type { ProxyImageTags, ProxyInstance, ProxyKind } from '@/lib/types'
import { AgentBadge, Field, InstanceBadge, KindBadge, NumberInput, ToggleRow } from './shared'

type InstanceForm = Pick<
  ProxyInstance,
  'name' | 'kind' | 'image' | 'bind_ip' | 'http_port' | 'https_port' | 'admin_port' | 'tls_alpn' | 'enabled' | 'notes'
>

const blank: InstanceForm = {
  name: '',
  kind: 'nginx',
  image: '',
  bind_ip: '',
  http_port: 80,
  https_port: 443,
  admin_port: 0,
  tls_alpn: true,
  enabled: true,
  notes: '',
}

/** Picks a proxy image tag from Docker Hub and the local images, or takes a custom image reference. */
function ImagePicker({
  kind,
  value,
  defaultImage,
  onChange,
}: {
  kind: ProxyKind
  value: string
  defaultImage?: string
  onChange: (image: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const qc = useQueryClient()
  const { data, isLoading, error } = useProxyImages(kind, open)
  const current = value || defaultImage || ''
  const custom = search.trim()
  const pick = (image: string) => {
    onChange(image === defaultImage ? '' : image)
    setOpen(false)
    setSearch('')
  }
  const refresh = async () => {
    setRefreshing(true)
    try {
      qc.setQueryData(['pm', 'images', kind], await api.get<ProxyImageTags>(`${PM}/images?kind=${kind}&refresh=1`))
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setRefreshing(false)
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" className="w-full justify-between px-3 font-normal">
          <span className="truncate font-mono text-xs">{current || 'Select an image'}</span>
          {!value && <span className="text-muted-foreground ml-auto text-xs">default</span>}
          <ChevronsUpDown className="text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[26rem] p-0" align="start">
        <Command>
          <CommandInput placeholder="Search tags or type an image..." value={search} onValueChange={setSearch} />
          <div className="text-muted-foreground flex items-center gap-2 border-b px-3 py-1.5 text-xs">
            <span className="truncate font-mono">{data?.repository ?? 'Docker Hub'}</span>
            <Button variant="ghost" size="icon-xs" className="ml-auto" aria-label="Refresh tags" onClick={refresh} disabled={refreshing || isLoading}>
              <RefreshCw className={cn(refreshing && 'animate-spin')} />
            </Button>
          </div>
          {data?.hub_error && <p className="border-b px-3 py-1.5 text-xs text-amber-500">Docker Hub: {data.hub_error}</p>}
          {error && <p className="text-destructive border-b px-3 py-1.5 text-xs">{errorMessage(error)}</p>}
          <CommandList>
            {isLoading ? (
              <div className="text-muted-foreground flex items-center gap-2 p-3 text-xs">
                <Loader2 className="size-3.5 animate-spin" /> Loading tags...
              </div>
            ) : (
              <CommandEmpty>No tag matches.</CommandEmpty>
            )}
            {custom.includes(':') && !data?.tags.some((t) => t.image === custom) && (
              <CommandGroup heading="Custom">
                <CommandItem value={`custom ${custom}`} onSelect={() => pick(custom)}>
                  <span className="truncate font-mono text-xs">Use {custom}</span>
                </CommandItem>
              </CommandGroup>
            )}
            {!!data?.tags.length && (
              <CommandGroup heading="Tags">
                {data.tags.map((t) => (
                  <CommandItem key={t.tag} value={t.image} onSelect={() => pick(t.image)}>
                    <span className="truncate font-mono text-xs">{t.tag}</span>
                    {t.image === data.default && <span className="text-brand text-[11px]">default</span>}
                    {t.local && <span className="text-muted-foreground rounded border px-1 text-[10px]">local</span>}
                    {!t.remote && <span className="rounded border border-amber-500/40 px-1 text-[10px] text-amber-500">not on Hub</span>}
                    <span className="text-muted-foreground ml-auto text-[11px] whitespace-nowrap">
                      {t.updated ? timeAgo(t.updated) : ''}
                      {t.size ? ` · ${formatBytes(t.size)}` : ''}
                    </span>
                    {t.image === current && <Check className="size-4" />}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

export function InstanceDialog({ open, onOpenChange, instance }: { open: boolean; onOpenChange: (o: boolean) => void; instance?: ProxyInstance }) {
  const { data: meta } = useProxyMeta()
  const [f, setF] = useState<InstanceForm>(blank)
  useEffect(() => {
    if (open) setF(instance ? { ...instance } : blank)
  }, [open, instance])
  const set = (patch: Partial<InstanceForm>) => setF((cur) => ({ ...cur, ...patch }))
  const editing = !!instance
  const save = usePMMutation(
    () => (editing ? api.put(`${PM}/instances/${instance.id}`, f) : api.post(`${PM}/instances`, f)),
    { success: editing ? 'Instance saved, deploy to apply' : 'Instance created, deploy it to start the proxy', onSuccess: () => onOpenChange(false) },
  )
  const defaultImage = f.kind === 'nginx' ? meta?.nginx_image : meta?.traefik_image

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${instance.name}` : 'New proxy instance'}</DialogTitle>
          <DialogDescription>
            A container on the host network, managed by the manager. Each instance listens on its own address and ports.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          {!editing && (
            <div className="grid grid-cols-2 gap-2">
              {(['nginx', 'traefik'] as ProxyKind[]).map((k) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => set({ kind: k, tls_alpn: k === 'nginx' })}
                  className={cn(
                    'grid gap-1 rounded-lg border p-3 text-left transition-colors',
                    f.kind === k ? 'border-brand bg-brand/5' : 'hover:bg-muted/50',
                  )}
                >
                  <span className="text-sm font-medium capitalize">{k}</span>
                  <span className="text-muted-foreground text-xs">
                    {k === 'nginx'
                      ? 'Config files, validated with nginx -t and hot-reloaded.'
                      : 'File provider, checked against Traefik’s error report after each reload.'}
                  </span>
                </button>
              ))}
            </div>
          )}
          <div className="grid grid-cols-2 gap-4">
            <Field label="Name">
              <Input value={f.name} onChange={(e) => set({ name: e.target.value })} placeholder="edge-1" />
            </Field>
            <Field label="Image" hint="Proxy image with the Supabase Manager agent">
              <ImagePicker kind={f.kind} value={f.image} defaultImage={defaultImage} onChange={(image) => set({ image })} />
            </Field>
          </div>
          <div className="grid grid-cols-3 gap-4">
            <Field label="Bind IP" hint="Empty for all addresses">
              <Input value={f.bind_ip} onChange={(e) => set({ bind_ip: e.target.value })} placeholder="0.0.0.0" className="font-mono text-xs" />
            </Field>
            <Field label="HTTP port" hint="0 to disable">
              <NumberInput value={f.http_port} onChange={(v) => set({ http_port: v })} />
            </Field>
            <Field label="HTTPS port" hint="0 to disable">
              <NumberInput value={f.https_port} onChange={(v) => set({ https_port: v })} />
            </Field>
          </div>
          {f.kind === 'traefik' && (
            <Field label="Admin API port" hint="Traefik API and ping on 127.0.0.1, used to verify deploys. Empty picks a free port.">
              <NumberInput value={f.admin_port} onChange={(v) => set({ admin_port: v })} className="w-40" />
            </Field>
          )}
          {f.kind === 'nginx' && (
            <ToggleRow
              label="TLS-ALPN-01 forwarding"
              description="nginx peeks at TLS connections on the HTTPS port and forwards ACME TLS-ALPN challenges to the manager."
              checked={f.tls_alpn}
              onChange={(v) => set({ tls_alpn: v })}
              disabled={!f.https_port}
            />
          )}
          <ToggleRow label="Enabled" description="Disabled instances are stopped and skipped by deploys." checked={f.enabled} onChange={(v) => set({ enabled: v })} />
          <Field label="Notes">
            <Textarea rows={2} value={f.notes} onChange={(e) => set({ notes: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || !f.name}>
            {save.isPending && <Loader2 className="animate-spin" />}
            {editing ? 'Save' : 'Create instance'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function InstancesPage() {
  const { data: instances, isLoading } = useInstances()
  const [edit, setEdit] = useState<ProxyInstance | undefined>()
  const [open, setOpen] = useState(false)
  const [del, setDel] = useState<ProxyInstance | null>(null)
  const remove = usePMMutation((id: number) => api.delete(`${PM}/instances/${id}`), { success: 'Instance deleted' })
  const { data: updates } = useProxyUpdates()
  const check = useCheckProxyUpdates()
  const update = useUpdateProxyImage()
  const updateFor = (id: number) => updates?.find((u) => u.instance_id === id && u.available)

  return (
    <>
      <PageHeader
        title="Proxy Instances"
        description="nginx and Traefik containers serving your hosts. Several instances can run side by side on different addresses or ports."
        actions={
          <>
            <Button variant="outline" onClick={() => check.mutate()} disabled={check.isPending}>
              <RefreshCw className={cn(check.isPending && 'animate-spin')} /> Check for updates
            </Button>
            <Button
              onClick={() => {
                setEdit(undefined)
                setOpen(true)
              }}
            >
              <Plus /> New instance
            </Button>
          </>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !instances?.length ? (
        <EmptyState
          icon={<Server />}
          title="No proxy instances"
          description="Create an nginx or Traefik instance. It runs as a container on the host network once deployed."
          action={
            <Button onClick={() => setOpen(true)}>
              <Plus /> New instance
            </Button>
          }
        />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Instance</TableHead>
                <TableHead>Listen</TableHead>
                <TableHead>Serves</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Deployed</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {instances.map((i) => (
                <TableRow key={i.id} className={cn(!i.enabled && 'opacity-60')}>
                  <TableCell>
                    <Link to={`/proxy-manager/instances/${i.id}`} className="flex items-center gap-2 font-medium hover:underline">
                      {i.name}
                      <KindBadge kind={i.kind} />
                    </Link>
                    <div className="text-muted-foreground font-mono text-xs">{i.image}</div>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {i.http_port > 0 && <div>http {i.bind_ip || '*'}:{i.http_port}</div>}
                    {i.https_port > 0 && <div>https {i.bind_ip || '*'}:{i.https_port}</div>}
                  </TableCell>
                  <TableCell className="text-xs">
                    {i.host_count} hosts, {i.stream_count} streams
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-col items-start gap-1">
                      <InstanceBadge v={i} />
                      <AgentBadge v={i} />
                      {i.pending && <span className="text-xs text-amber-500">changes pending</span>}
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">{i.deployed_at ? timeAgo(i.deployed_at) : 'never'}</TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    {(() => {
                      const u = updateFor(i.id)
                      return (
                        u && (
                          <Button
                            variant="outline"
                            size="xs"
                            className="mr-1"
                            title={`${u.current} → ${u.target}`}
                            onClick={() => update.mutate({ id: i.id, name: i.name })}
                            disabled={update.isPending}
                          >
                            <ArrowUpCircle /> {updateLabel(u)}
                          </Button>
                        )
                      )
                    })()}
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="Edit"
                      onClick={() => {
                        setEdit(i)
                        setOpen(true)
                      }}
                    >
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(i)}>
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <InstanceDialog open={open} onOpenChange={setOpen} instance={edit} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="The container, its configuration and log volumes and its revision history are removed. Hosts assigned to it stay, unassigned from this instance."
        confirmText="Delete instance"
        typeToConfirm={del?.name}
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
