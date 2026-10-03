import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, KeyRound, Loader2, Plus, Save, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Switch } from '@workspace/ui/components/switch'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@workspace/ui/components/table'
import { PageHeader, Section } from '@/components/page-header'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LazyEditor } from '@/components/lazy-editor'
import { useAuth } from '@/hooks/use-auth'
import { useLifecycle, useProject, useSettings, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import { NetworkForm } from '@/components/network-form'
import { StorageForm, storageModeLabel } from '@/components/storage-form'
import { Badge } from '@workspace/ui/components/badge'
import type { DockerNetwork, NetworkConfig, Ports, Project, ProjectNetwork, ProjectStorage, Secret, Services, Settings, StorageConfig } from '@/lib/types'
import { formatDate, timeAgo } from '@/lib/format'

function RestartHint() {
  const { data: project } = useProject()
  const slug = useSlug()
  const lifecycle = useLifecycle()
  if (project?.status !== 'running') return null
  return (
    <Alert className="mb-6">
      <AlertTriangle />
      <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
        <span>The project is running. Changes here take effect after a restart.</span>
        <Button size="xs" variant="outline" onClick={() => lifecycle.mutate({ slug, action: 'restart' })} disabled={lifecycle.isPending}>
          Restart now
        </Button>
      </AlertDescription>
    </Alert>
  )
}

export function GeneralSettingsPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { data: project } = useProject()
  const [name, setName] = useState('')
  useEffect(() => {
    if (project) setName(project.name)
  }, [project])

  const save = useMutation({
    mutationFn: () => api.patch<Project>(`/projects/${slug}`, { name }),
    onSuccess: (p) => {
      qc.setQueryData(['project', slug], p)
      qc.invalidateQueries({ queryKey: ['projects'] })
      toast.success('Project updated')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!project) return <Skeleton className="h-64" />
  const rows: [string, string][] = [
    ['Project ID', project.supabase_project_id],
    ['Directory', project.path],
    ['config.toml', `${project.path}/supabase/config.toml`],
    ['Port block', String(project.port_base)],
    ['Source', project.imported ? 'Imported (files are never deleted by the manager)' : 'Created by the manager'],
    ['Created', `${formatDate(project.created_at)} (${timeAgo(project.created_at)})`],
  ]

  return (
    <>
      <PageHeader title="General" description="Project name and details." />
      <div className="grid gap-6">
        <Section title="Project name">
          <div className="flex gap-2">
            <Input value={name} onChange={(e) => setName(e.target.value)} className="max-w-md" />
            <Button onClick={() => save.mutate()} disabled={!name || name === project.name || save.isPending}>
              {save.isPending && <Loader2 className="animate-spin" />}
              Save
            </Button>
          </div>
        </Section>
        <Section title="Project details">
          <dl className="divide-y">
            {rows.map(([k, v]) => (
              <div key={k} className="grid gap-1 py-2.5 sm:grid-cols-3">
                <dt className="text-muted-foreground text-sm">{k}</dt>
                <dd className="font-mono text-xs break-all sm:col-span-2">{v}</dd>
              </div>
            ))}
          </dl>
        </Section>
      </div>
    </>
  )
}

const serviceRows: { key: keyof Services; label: string; description: string }[] = [
  { key: 'studio', label: 'Studio', description: '[studio] - dashboard with table and SQL editor' },
  { key: 'storage', label: 'Storage', description: '[storage] - file storage API' },
  { key: 'realtime', label: 'Realtime', description: '[realtime] - websocket subscriptions' },
  { key: 'edge_runtime', label: 'Edge Functions', description: '[edge_runtime] - Deno runtime' },
  { key: 'inbucket', label: 'Email testing (Mailpit)', description: '[local_smtp] / [inbucket] - captures emails' },
  { key: 'analytics', label: 'Analytics', description: '[analytics] - logs explorer, uses ~600 MB RAM' },
  { key: 'pooler', label: 'Connection pooler', description: '[db.pooler] - Supavisor' },
]

export function ServicesPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { data: settings, isLoading } = useSettings()
  const [form, setForm] = useState<Services | null>(null)
  useEffect(() => {
    if (settings) setForm(settings.services)
  }, [settings])

  const save = useMutation({
    mutationFn: (s: Services) => api.put<Settings>(`/projects/${slug}/config/services`, s),
    onSuccess: (s) => {
      qc.setQueryData(['project', slug, 'config'], s)
      toast.success('Services updated')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !form) return <Skeleton className="h-64" />
  return (
    <>
      <PageHeader title="Services" description="Choose which Supabase services run for this project." />
      <RestartHint />
      <Section title="Services">
        <div className="divide-y">
          {serviceRows.map((s) => (
            <label key={s.key} className="flex items-center justify-between gap-4 py-3">
              <span className="grid">
                <span className="text-sm">{s.label}</span>
                <span className="text-muted-foreground font-mono text-xs">{s.description}</span>
              </span>
              <Switch checked={form[s.key]} onCheckedChange={(v) => setForm({ ...form, [s.key]: v })} />
            </label>
          ))}
        </div>
      </Section>
      <div className="mt-4 flex justify-end">
        <Button onClick={() => save.mutate(form)} disabled={save.isPending}>
          {save.isPending && <Loader2 className="animate-spin" />}
          Save changes
        </Button>
      </div>
    </>
  )
}

const portRows: { key: keyof Ports; label: string; section: string }[] = [
  { key: 'api', label: 'API gateway', section: '[api] port' },
  { key: 'db', label: 'Database', section: '[db] port' },
  { key: 'shadow', label: 'Shadow database', section: '[db] shadow_port' },
  { key: 'pooler', label: 'Connection pooler', section: '[db.pooler] port' },
  { key: 'studio', label: 'Studio', section: '[studio] port' },
  { key: 'smtp', label: 'Mailpit web UI', section: '[local_smtp] port' },
  { key: 'analytics', label: 'Analytics', section: '[analytics] port' },
  { key: 'inspector', label: 'Edge runtime inspector', section: '[edge_runtime] inspector_port' },
]

export function PortsPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { data: project } = useProject()
  const { data: settings, isLoading } = useSettings()
  const [form, setForm] = useState<Ports | null>(null)
  useEffect(() => {
    if (settings) setForm(settings.ports)
  }, [settings])

  const save = useMutation({
    mutationFn: (p: Ports) => api.put<Settings>(`/projects/${slug}/config/ports`, p),
    onSuccess: (s) => {
      qc.setQueryData(['project', slug, 'config'], s)
      qc.invalidateQueries({ queryKey: ['project', slug] })
      qc.invalidateQueries({ queryKey: ['projects'] })
      toast.success('Ports updated')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !form) return <Skeleton className="h-64" />
  const running = project?.status === 'running'
  const applyBlock = (base: number) =>
    setForm({
      ...form,
      api: base + 21,
      db: base + 22,
      shadow: base + 20,
      pooler: base + 29,
      studio: base + 23,
      smtp: base + 24,
      analytics: base + 27,
    })

  return (
    <>
      <PageHeader
        title="Ports"
        description="Each project uses its own block of host ports so several projects can run at the same time."
      />
      {running && (
        <Alert className="mb-6">
          <AlertTriangle />
          <AlertDescription>Stop the project before changing its ports, otherwise the running containers keep the old ports.</AlertDescription>
        </Alert>
      )}
      <Section
        title="Host ports"
        description={project ? `Ports are bound on ${bindDescription(project.network)}. Change it under Network.` : undefined}
        actions={
          <div className="flex items-center gap-2">
            <Label htmlFor="block" className="text-muted-foreground text-xs">
              Block base
            </Label>
            <Input
              id="block"
              type="number"
              step={100}
              className="h-7 w-24 text-xs"
              defaultValue={form.api - 21}
              onBlur={(e) => applyBlock(Number(e.target.value))}
            />
          </div>
        }
      >
        <div className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
          {portRows.map((r) => (
            <div key={r.key} className="grid gap-1.5">
              <Label htmlFor={`port-${r.key}`}>{r.label}</Label>
              <Input
                id={`port-${r.key}`}
                type="number"
                min={1024}
                max={65535}
                value={form[r.key]}
                onChange={(e) => setForm({ ...form, [r.key]: Number(e.target.value) })}
                className="font-mono"
              />
              <span className="text-muted-foreground font-mono text-[11px]">{r.section}</span>
            </div>
          ))}
        </div>
      </Section>
      <div className="mt-4 flex justify-end">
        <Button onClick={() => save.mutate(form)} disabled={save.isPending || running}>
          {save.isPending && <Loader2 className="animate-spin" />}
          Save ports
        </Button>
      </div>
    </>
  )
}

function bindDescription(n: NetworkConfig | undefined) {
  if (n?.mode === 'managed') {
    if (!n.bind_address) return 'the Docker default address (usually 0.0.0.0)'
    return n.bind_address === '0.0.0.0' ? 'all interfaces (0.0.0.0)' : `${n.bind_address} only`
  }
  if (n?.mode === 'external') return 'the address configured for the external network'
  return 'the Docker default address (usually 0.0.0.0)'
}

export function NetworkPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { isAdmin } = useAuth()
  const { data: project } = useProject()
  const { data, isLoading } = useQuery({
    queryKey: ['project', slug, 'network'],
    queryFn: () => api.get<ProjectNetwork>(`/projects/${slug}/network`),
  })
  const [form, setForm] = useState<NetworkConfig | null>(null)
  const [formKey, setFormKey] = useState(0)
  useEffect(() => {
    if (data) {
      setForm({ ...data.config, mode: data.config.mode || 'auto' })
      setFormKey((k) => k + 1)
    }
  }, [data])
  const { data: networks } = useQuery({
    queryKey: ['docker-networks'],
    queryFn: () => api.get<DockerNetwork[]>('/system/docker-networks'),
    enabled: form?.mode === 'external',
  })

  const save = useMutation({
    mutationFn: (cfg: NetworkConfig) => api.put<ProjectNetwork>(`/projects/${slug}/network`, cfg),
    onSuccess: (d) => {
      qc.setQueryData(['project', slug, 'network'], d)
      qc.invalidateQueries({ queryKey: ['project', slug] })
      qc.invalidateQueries({ queryKey: ['docker-networks'] })
      toast.success('Network settings saved', { description: 'They apply the next time the project starts.' })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const suggest = useMutation({
    mutationFn: () => api.post<{ subnet: string; gateway: string }>('/system/free-subnet', { project: slug }),
    onSuccess: (r) => form && setForm({ ...form, subnet: r.subnet, gateway: r.gateway, ip_range: '' }),
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !data || !form || !project) return <Skeleton className="h-64" />
  const live = data.live
  const liveRows: [string, string][] = live
    ? [
        ['Driver', live.driver],
        ['Subnet', live.ipam?.map((c) => c.Subnet).filter(Boolean).join(', ') || '-'],
        ['Gateway', live.ipam?.map((c) => c.Gateway).filter(Boolean).join(', ') || '-'],
        ['Port bind address', live.options?.['com.docker.network.bridge.host_binding_ipv4'] || 'Docker default'],
        ['MTU', live.options?.['com.docker.network.driver.mtu'] || 'default'],
        ['Containers attached', String(live.containers?.length ?? 0)],
      ]
    : []

  return (
    <>
      <PageHeader
        title="Network"
        description="Where the project's ports are published and which Docker network its containers use."
      />
      <RestartHint />
      <div className="grid gap-6">
        <Section title="Docker network" description={<span className="font-mono">{data.name}</span>}>
          {live ? (
            <dl className="divide-y">
              {liveRows.map(([k, v]) => (
                <div key={k} className="grid gap-1 py-2 sm:grid-cols-3">
                  <dt className="text-muted-foreground text-sm">{k}</dt>
                  <dd className="font-mono text-xs break-all sm:col-span-2">{v}</dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="text-muted-foreground text-sm">
              {data.live_error ? `Docker is not reachable: ${data.live_error}` : 'The network does not exist yet. It is created when the project starts.'}
            </p>
          )}
          <p className="text-muted-foreground mt-3 text-xs">
            The manager connects to this project through <span className="font-mono">{data.connect_host}</span>.
          </p>
        </Section>

        <Section title="Settings">
          <NetworkForm
            key={formKey}
            value={form}
            onChange={setForm}
            variant="project"
            defaultName={`supabase_manager_${slug}`}
            networks={networks}
            onSuggestSubnet={() => suggest.mutate()}
            suggesting={suggest.isPending}
            disabled={!isAdmin}
          />
        </Section>
      </div>
      {isAdmin ? (
        <div className="mt-4 flex justify-end gap-2">
          <Button
            variant="ghost"
            onClick={() => {
              setForm({ ...data.config, mode: data.config.mode || 'auto' })
              setFormKey((k) => k + 1)
            }}
          >
            Reset
          </Button>
          <Button onClick={() => save.mutate(form)} disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save network settings
          </Button>
        </div>
      ) : (
        <p className="text-muted-foreground mt-3 text-xs">Only admins can change network settings.</p>
      )}
    </>
  )
}

export function StoragePage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { isAdmin } = useAuth()
  const { data: project } = useProject()
  const { data, isLoading } = useQuery({
    queryKey: ['project', slug, 'storage'],
    queryFn: () => api.get<ProjectStorage>(`/projects/${slug}/storage`),
  })
  const [form, setForm] = useState<StorageConfig | null>(null)
  const [formKey, setFormKey] = useState(0)
  const resetForm = (d: ProjectStorage) => {
    setForm({ ...d.config, mode: d.config.mode || 'docker' })
    setFormKey((k) => k + 1)
  }
  useEffect(() => {
    if (data) {
      setForm({ ...data.config, mode: data.config.mode || 'docker' })
      setFormKey((k) => k + 1)
    }
  }, [data])

  const save = useMutation({
    mutationFn: (cfg: StorageConfig) => api.put<ProjectStorage>(`/projects/${slug}/storage`, cfg),
    onSuccess: (d) => {
      qc.setQueryData(['project', slug, 'storage'], d)
      qc.invalidateQueries({ queryKey: ['project', slug] })
      const moves = d.volumes?.some((v) => v.exists && !v.in_sync)
      toast.success('Storage settings saved', {
        description: moves ? 'Existing data is moved to the new location the next time the project starts.' : 'They apply the next time the project starts.',
      })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !data || !form || !project) return <Skeleton className="h-64" />
  const pending = data.volumes?.filter((v) => v.exists && !v.in_sync) ?? []

  return (
    <>
      <PageHeader
        title="Storage"
        description="Where the database and uploaded files of this project are kept on disk. The data survives stopping, restarting and upgrading the containers."
      />
      <RestartHint />
      <div className="grid gap-6">
        <Section title="Persistent volumes" description={`Current mode: ${storageModeLabel(data.config.mode)}`}>
          {data.volumes ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Volume</TableHead>
                  <TableHead>Current location</TableHead>
                  <TableHead>Configured location</TableHead>
                  <TableHead className="text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.volumes.map((v) => (
                  <TableRow key={v.key}>
                    <TableCell className="align-top">
                      <div className="text-sm">{v.label}</div>
                      <div className="text-muted-foreground font-mono text-xs">{v.name}</div>
                      <div className="text-muted-foreground font-mono text-xs">→ {v.mount}</div>
                    </TableCell>
                    <TableCell className="max-w-64 align-top font-mono text-xs break-all whitespace-normal">
                      {v.exists ? (
                        <>
                          <div className="text-muted-foreground mb-0.5 font-sans">{storageModeLabel(v.current_mode)}</div>
                          {v.location}
                        </>
                      ) : (
                        <span className="text-muted-foreground font-sans">Not created yet</span>
                      )}
                    </TableCell>
                    <TableCell className="max-w-64 align-top font-mono text-xs break-all whitespace-normal">{v.target}</TableCell>
                    <TableCell className="text-right align-top">
                      {!v.exists ? (
                        <Badge variant="outline">Created on start</Badge>
                      ) : v.in_sync ? (
                        <Badge variant="secondary">In place</Badge>
                      ) : (
                        <Badge variant="destructive">Moves on next start</Badge>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-muted-foreground text-sm">Docker is not reachable: {data.live_error}</p>
          )}
          <p className="text-muted-foreground mt-3 text-xs">
            The Edge Functions cache and other service volumes stay in Docker storage; they hold no data worth keeping.
          </p>
        </Section>

        {pending.length > 0 && (
          <Alert>
            <AlertTriangle />
            <AlertDescription>
              {pending.map((v) => v.label).join(' and ')} will be copied to the configured location on the next start (restart) of the project.
              The copy can take a while for large databases; the original stays untouched until the copy succeeds. Volumes using a third-party
              driver cannot be moved automatically.
            </AlertDescription>
          </Alert>
        )}

        <Section title="Settings">
          <StorageForm
            key={formKey}
            value={form}
            onChange={setForm}
            variant="project"
            slug={slug}
            projectId={project.supabase_project_id}
            projectPath={project.path}
            disabled={!isAdmin}
          />
        </Section>
      </div>
      {isAdmin ? (
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => resetForm(data)}>
            Reset
          </Button>
          <Button onClick={() => save.mutate(form)} disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save storage settings
          </Button>
        </div>
      ) : (
        <p className="text-muted-foreground mt-3 text-xs">Only admins can change storage settings.</p>
      )}
    </>
  )
}

export function SecretsPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [toDelete, setToDelete] = useState<string | null>(null)
  const { data: secrets, isLoading } = useQuery({
    queryKey: ['project', slug, 'secrets'],
    queryFn: () => api.get<Secret[]>(`/projects/${slug}/secrets`),
  })
  const save = useMutation({
    mutationFn: () => api.put<Secret[]>(`/projects/${slug}/secrets`, { key, value }),
    onSuccess: (s) => {
      qc.setQueryData(['project', slug, 'secrets'], s)
      toast.success(`Secret ${key} saved`)
      setKey('')
      setValue('')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <>
      <PageHeader
        title="Secrets"
        description={
          <>
            Encrypted values passed as environment variables to every Supabase CLI command for this project. Reference them in
            config.toml with <code className="font-mono">env(NAME)</code>.
          </>
        }
      />
      <RestartHint />
      <div className="grid gap-6">
        <Section title="Add or update a secret">
          <div className="grid gap-3 sm:grid-cols-[1fr_1.5fr_auto] sm:items-end">
            <div className="grid gap-1.5">
              <Label htmlFor="sec-key">Name</Label>
              <Input
                id="sec-key"
                value={key}
                onChange={(e) => setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, '_'))}
                placeholder="SUPABASE_AUTH_EXTERNAL_GITHUB_SECRET"
                className="font-mono"
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="sec-value">Value</Label>
              <Input id="sec-value" type="password" value={value} onChange={(e) => setValue(e.target.value)} autoComplete="off" className="font-mono" />
            </div>
            <Button onClick={() => save.mutate()} disabled={!key || !value || save.isPending}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
              Save
            </Button>
          </div>
        </Section>
        <div className="bg-card overflow-hidden rounded-lg border">
          {isLoading ? (
            <Skeleton className="m-4 h-20" />
          ) : !secrets?.length ? (
            <div className="text-muted-foreground flex items-center gap-2 px-5 py-8 text-sm">
              <KeyRound className="size-4" /> No secrets stored yet.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead>Updated</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {secrets.map((s) => (
                  <TableRow key={s.key}>
                    <TableCell className="font-mono text-xs">{s.key}</TableCell>
                    <TableCell className="text-muted-foreground font-mono text-xs">{s.preview}</TableCell>
                    <TableCell className="text-muted-foreground text-xs">{timeAgo(s.updated_at)}</TableCell>
                    <TableCell className="text-right">
                      <Button variant="ghost" size="icon-xs" aria-label={`Delete ${s.key}`} onClick={() => setToDelete(s.key)}>
                        <Trash2 />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={`Delete ${toDelete}?`}
        description="Anything in config.toml that references this secret will resolve to an empty value."
        confirmText="Delete secret"
        destructive
        onConfirm={async () => {
          try {
            await api.delete(`/projects/${slug}/secrets/${toDelete}`)
            qc.invalidateQueries({ queryKey: ['project', slug, 'secrets'] })
            toast.success('Secret deleted')
          } catch (err) {
            toast.error(errorMessage(err))
            throw err
          }
        }}
      />
    </>
  )
}

export function ConfigEditorPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { isAdmin } = useAuth()
  const [content, setContent] = useState('')
  const { data, isLoading } = useQuery({
    queryKey: ['project', slug, 'config-raw'],
    queryFn: () => api.get<{ content: string }>(`/projects/${slug}/config/raw`),
  })
  useEffect(() => {
    if (data) setContent(data.content)
  }, [data])

  const save = useMutation({
    mutationFn: () => api.put<{ content: string }>(`/projects/${slug}/config/raw`, { content }),
    onSuccess: (d) => {
      qc.setQueryData(['project', slug, 'config-raw'], d)
      qc.invalidateQueries({ queryKey: ['project', slug] })
      toast.success('config.toml saved', { description: 'The previous version was kept as config.toml.bak.' })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const dirty = data && content !== data.content

  return (
    <>
      <PageHeader
        title="config.toml"
        description="Full access to the Supabase CLI configuration. The file is validated before saving."
        actions={
          isAdmin && (
            <>
              {dirty && (
                <Button variant="ghost" onClick={() => data && setContent(data.content)}>
                  Discard
                </Button>
              )}
              <Button onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
                {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
                Save
              </Button>
            </>
          )
        }
      />
      <RestartHint />
      {!isAdmin && <p className="text-muted-foreground mb-3 text-sm">Read-only: only admins can edit the raw configuration.</p>}
      {isLoading ? <Skeleton className="h-[65vh]" /> : <LazyEditor language="toml" value={content} onChange={setContent} readOnly={!isAdmin} height="65vh" />}
    </>
  )
}

export function DangerZonePage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { isAdmin } = useAuth()
  const { data: project } = useProject()
  const lifecycle = useLifecycle()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  if (!project) return <Skeleton className="h-64" />
  const running = project.status === 'running' || project.status === 'error'

  return (
    <>
      <PageHeader title="Danger zone" description="Destructive actions for this project." />
      <div className="border-destructive/40 divide-destructive/20 divide-y overflow-hidden rounded-lg border">
        <div className="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
          <div className="grid gap-0.5">
            <p className="text-sm font-medium">Stop project</p>
            <p className="text-muted-foreground text-xs">Stops all containers. Data is kept in Docker volumes.</p>
          </div>
          <Button variant="outline" disabled={!running || lifecycle.isPending} onClick={() => lifecycle.mutate({ slug, action: 'stop' })}>
            Stop project
          </Button>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
          <div className="grid gap-0.5">
            <p className="text-sm font-medium">Delete project</p>
            <p className="text-muted-foreground text-xs">
              Stops the project, removes its Docker volumes and unregisters it from the manager.
            </p>
          </div>
          <Button variant="destructive" disabled={!isAdmin} onClick={() => setDeleteOpen(true)}>
            <Trash2 /> Delete project
          </Button>
        </div>
      </div>
      {!isAdmin && <p className="text-muted-foreground mt-3 text-xs">Only admins can delete projects.</p>}

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={`Delete ${project.name}?`}
        description="All containers and database volumes of this project are removed. This cannot be undone."
        typeToConfirm={slug}
        confirmText="Delete project"
        destructive
        onConfirm={async () => {
          try {
            await api.delete(`/projects/${slug}`, { confirm: slug, delete_files: deleteFiles })
            qc.invalidateQueries({ queryKey: ['projects'] })
            toast.success(`${project.name} deleted`)
            navigate('/projects')
          } catch (err) {
            toast.error(errorMessage(err))
            throw err
          }
        }}
      >
        {!project.imported && (
          <label className="flex items-center gap-3 rounded-md border px-3 py-2">
            <Switch checked={deleteFiles} onCheckedChange={setDeleteFiles} />
            <span className="grid">
              <span className="text-sm">Also delete the project folder</span>
              <span className="text-muted-foreground font-mono text-xs">{project.path}</span>
            </span>
          </label>
        )}
      </ConfirmDialog>
    </>
  )
}
