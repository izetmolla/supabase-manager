import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUpCircle, CheckCircle2, Download, Loader2, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Badge } from '@workspace/ui/components/badge'
import { Input } from '@workspace/ui/components/input'
import { Switch } from '@workspace/ui/components/switch'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { ProductLayout, type ProductNavGroup } from '@/components/layout/layouts'
import { PageHeader, Section } from '@/components/page-header'
import { Link, Navigate, Route, Routes } from 'react-router-dom'
import { NetworkForm } from '@/components/network-form'
import { StorageForm } from '@/components/storage-form'
import { api, errorMessage } from '@/lib/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SMTPForm, SMTPTest } from '@/components/smtp-form'
import type { CLIInfo, ManagerUpdateInfo, NetworkDefaults, SMTPSettings, StorageConfig } from '@/lib/types'
import { timeAgo } from '@/lib/format'
import { AUTHOR_EMAIL, AUTHOR_MAILTO, AUTHOR_NAME } from '@/lib/author'
import { useProxyManagerSettings, type ProxyManagerSettings } from '@/hooks/use-proxy-manager'

const sourceLabel: Record<string, string> = {
  managed: 'Installed by the manager',
  homebrew: 'Homebrew',
  system: 'System / PATH',
}

function formatBytes(n: number) {
  if (n <= 0) return '0 MB'
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function useCLIInfo() {
  return useQuery({
    queryKey: ['system', 'cli'],
    queryFn: () => api.get<CLIInfo>('/system/cli'),
    refetchInterval: (q) => (q.state.data?.install.running ? 1000 : false),
  })
}

function CLISection() {
  const qc = useQueryClient()
  const { data: info, isLoading } = useCLIInfo()
  const [version, setVersion] = useState('')
  const setInfo = (i: CLIInfo) => {
    qc.setQueryData(['system', 'cli'], i)
    qc.invalidateQueries({ queryKey: ['system'], exact: true })
  }

  const wasRunning = useRef(false)
  useEffect(() => {
    if (!info) return
    if (info.install.running) {
      wasRunning.current = true
      return
    }
    if (!wasRunning.current) return
    wasRunning.current = false
    if (info.install.error) toast.error('Supabase CLI install failed', { description: info.install.error })
    else toast.success(`Supabase CLI ${info.install.version} installed`)
    qc.invalidateQueries({ queryKey: ['system'], exact: true })
  }, [info, qc])

  const check = useMutation({
    mutationFn: () => api.post<CLIInfo>('/system/cli/check'),
    onSuccess: (i) => {
      setInfo(i)
      if (i.check_error) toast.error(i.check_error)
      else toast.success(i.update_available ? `Version ${i.latest} is available` : 'The Supabase CLI is up to date')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const install = useMutation({
    mutationFn: (v: string) => api.post<CLIInfo>('/system/cli/install', { version: v }),
    onSuccess: setInfo,
    onError: (err) => toast.error(errorMessage(err)),
  })
  const useSystem = useMutation({
    mutationFn: () => api.post<CLIInfo>('/system/cli/use-system'),
    onSuccess: (i) => {
      setInfo(i)
      toast.success(`Using ${i.path}`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const autoUpdate = useMutation({
    mutationFn: (on: boolean) => api.put<CLIInfo>('/system/cli/settings', { auto_update: on }),
    onSuccess: setInfo,
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !info) return <Skeleton className="h-48" />
  const inst = info.install
  const pct = inst.total > 0 ? Math.min(100, Math.round((inst.downloaded / inst.total) * 100)) : 0
  const rows: { k: string; v: React.ReactNode }[] = [
    {
      k: 'Installed version',
      v: info.version ? (
        <span className="flex items-center gap-2">
          {info.version}
          {info.update_available ? (
            <Badge variant="secondary">update available</Badge>
          ) : info.latest ? (
            <CheckCircle2 className="size-3.5 text-emerald-500" />
          ) : null}
        </span>
      ) : (
        <span className="text-destructive">{info.installed ? info.version_error || 'not working' : 'not installed'}</span>
      ),
    },
    { k: 'Binary', v: info.path || '-' },
    { k: 'Source', v: sourceLabel[info.source] ?? '-' },
    {
      k: 'Latest release',
      v: info.latest ? (
        <span>
          {info.latest}
          {info.checked_at && <span className="text-muted-foreground"> (checked {timeAgo(info.checked_at)})</span>}
        </span>
      ) : (
        <span className="text-muted-foreground">{info.check_error || 'not checked yet'}</span>
      ),
    },
    { k: 'Install directory', v: info.managed_dir },
  ]

  return (
    <Section
      title="Supabase CLI"
      description="The manager runs every project command with this binary."
      actions={
        <Button variant="outline" size="xs" onClick={() => check.mutate()} disabled={check.isPending}>
          {check.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          Check for updates
        </Button>
      }
    >
      {!info.installed && !inst.running && (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>The Supabase CLI was not found. Install it to start projects.</AlertDescription>
        </Alert>
      )}
      <dl className="divide-y">
        {rows.map(({ k, v }) => (
          <div key={k} className="grid gap-1 py-2 sm:grid-cols-3">
            <dt className="text-muted-foreground text-sm">{k}</dt>
            <dd className="font-mono text-xs break-all sm:col-span-2">{v}</dd>
          </div>
        ))}
      </dl>

      {inst.running ? (
        <div className="mt-4 grid gap-2">
          <div className="flex items-center justify-between text-sm">
            <span className="flex items-center gap-2">
              <Loader2 className="size-4 animate-spin" />
              Installing {inst.version || 'latest'}: {inst.phase}
            </span>
            {inst.phase === 'downloading' && (
              <span className="text-muted-foreground font-mono text-xs">
                {formatBytes(inst.downloaded)} / {formatBytes(inst.total)}
              </span>
            )}
          </div>
          <div className="bg-muted h-1.5 overflow-hidden rounded-full">
            <div className="bg-brand h-full transition-all" style={{ width: `${inst.phase === 'downloading' ? pct : inst.phase === 'activating' ? 100 : 5}%` }} />
          </div>
        </div>
      ) : (
        <>
          {inst.error && (
            <Alert variant="destructive" className="mt-4">
              <AlertDescription>Last install failed: {inst.error}</AlertDescription>
            </Alert>
          )}
          <div className="mt-4 flex flex-wrap items-center gap-2">
            {(!info.installed || info.update_available || !info.latest) && (
              <Button onClick={() => install.mutate('')} disabled={install.isPending || !info.platform}>
                {info.installed ? <ArrowUpCircle /> : <Download />}
                {info.installed ? `Update to ${info.latest || 'latest'}` : 'Install latest'}
              </Button>
            )}
            <Input
              value={version}
              onChange={(e) => setVersion(e.target.value.trim())}
              placeholder="e.g. 2.119.0"
              className="h-8 w-32 font-mono text-xs"
              disabled={!info.platform}
            />
            <Button variant="outline" size="sm" onClick={() => install.mutate(version)} disabled={!version || install.isPending || !info.platform}>
              Install version
            </Button>
            {info.source === 'managed' && (
              <Button variant="ghost" size="sm" onClick={() => useSystem.mutate()} disabled={useSystem.isPending}>
                Use system CLI instead
              </Button>
            )}
          </div>
          {!info.platform && <p className="text-muted-foreground mt-2 text-xs">Automatic install is not supported on this platform.</p>}
        </>
      )}

      <label className="mt-5 flex items-center justify-between gap-4 border-t pt-4">
        <span className="grid">
          <span className="text-sm">Install updates automatically</span>
          <span className="text-muted-foreground text-xs">
            Checks every 6 hours and installs new releases into the install directory while no project job is running. A Homebrew or
            system installation is never modified.
          </span>
        </span>
        <Switch checked={info.auto_update} onCheckedChange={(v) => autoUpdate.mutate(v)} disabled={autoUpdate.isPending} />
      </label>
    </Section>
  )
}

const RESTART_TIMEOUT_MS = 3 * 60 * 1000

interface VersionTarget {
  version: string
  commit?: string
  /** what was running before, so a re-run of the same version waits for the restart */
  current: { version: string; commit: string }
}

/** Polls /api/version until the manager answers with the target build, then reloads the page. */
function useWaitForVersion() {
  const [target, setTarget] = useState<VersionTarget | null>(null)
  const [timedOut, setTimedOut] = useState(false)
  useEffect(() => {
    if (!target) return
    let stop = false
    let sawDown = false
    const started = Date.now()
    const tick = async () => {
      if (stop) return
      try {
        const res = await fetch('/api/version', { cache: 'no-store' })
        if (!res.ok) sawDown = true
        const v = res.ok ? ((await res.json()) as { version: string; commit: string }) : null
        const changed = !!v && (v.version !== target.current.version || (!!target.commit && v.commit === target.commit && v.commit !== target.current.commit))
        if (v && v.version === target.version && (sawDown || changed)) {
          toast.success(`Supabase Manager ${target.version} is running`)
          window.location.reload()
          return
        }
      } catch {
        sawDown = true
      }
      if (Date.now() - started > RESTART_TIMEOUT_MS) {
        setTimedOut(true)
        return
      }
      setTimeout(tick, 2000)
    }
    const t = setTimeout(tick, 3000)
    return () => {
      stop = true
      clearTimeout(t)
    }
  }, [target])
  return { waitFor: setTarget, waiting: !!target && !timedOut, timedOut }
}

function ManagerUpdateSection() {
  const qc = useQueryClient()
  const [confirm, setConfirm] = useState<'release' | 'latest' | null>(null)
  const { waitFor, waiting, timedOut } = useWaitForVersion()
  const { data: info, isLoading } = useQuery({
    queryKey: ['system', 'manager-update'],
    queryFn: () => api.get<ManagerUpdateInfo>('/system/update'),
    refetchInterval: (q) => (q.state.data?.update.running && !waiting ? 1500 : false),
  })

  useEffect(() => {
    if (info?.update.running && info.update.phase === 'restarting') {
      waitFor({ version: info.update.version, commit: info.update.commit, current: { version: info.current, commit: info.commit } })
    }
  }, [info, waitFor])

  const check = useMutation({
    mutationFn: () => api.post<ManagerUpdateInfo>('/system/update/check'),
    onSuccess: (i) => {
      qc.setQueryData(['system', 'manager-update'], i)
      if (i.check_error) toast.error(i.check_error)
      else toast.success(i.update_available ? `Version ${i.latest} is available` : 'Supabase Manager is up to date')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const apply = useMutation({
    mutationFn: (version: string) => api.post<ManagerUpdateInfo>('/system/update/apply', { version }),
    onSuccess: (i) => qc.setQueryData(['system', 'manager-update'], i),
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !info) return <Skeleton className="h-40" />
  const up = info.update
  const busy = up.running || waiting
  const rows: { k: string; v: React.ReactNode }[] = [
    {
      k: 'Running version',
      v: (
        <span className="flex items-center gap-2">
          {info.current}
          <span className="text-muted-foreground">({info.commit})</span>
          {info.update_available ? (
            <Badge variant="secondary">update available</Badge>
          ) : info.latest ? (
            <CheckCircle2 className="size-3.5 text-emerald-500" />
          ) : null}
        </span>
      ),
    },
    {
      k: 'Latest on Docker Hub',
      v: info.latest ? (
        <span>
          {info.latest}
          {info.checked_at && <span className="text-muted-foreground"> (checked {timeAgo(info.checked_at)})</span>}
        </span>
      ) : (
        <span className="text-muted-foreground">{info.check_error || 'not checked yet'}</span>
      ),
    },
    { k: 'Image', v: info.repository },
    { k: 'Container', v: info.container || <span className="text-muted-foreground">{info.unsupported}</span> },
  ]

  return (
    <Section
      title="Supabase Manager"
      description="New releases are published as Docker images. Updating pulls the image and re-creates this container with the same settings; data and projects are kept."
      actions={
        <Button variant="outline" size="xs" onClick={() => check.mutate()} disabled={check.isPending || busy}>
          {check.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          Check for updates
        </Button>
      }
    >
      <dl className="divide-y">
        {rows.map(({ k, v }) => (
          <div key={k} className="grid gap-1 py-2 sm:grid-cols-3">
            <dt className="text-muted-foreground text-sm">{k}</dt>
            <dd className="font-mono text-xs break-all sm:col-span-2">{v}</dd>
          </div>
        ))}
      </dl>

      {busy ? (
        <div className="mt-4 flex items-center gap-2 text-sm">
          <Loader2 className="size-4 animate-spin" />
          {up.phase === 'pulling' && !waiting
            ? `Downloading ${up.version}...`
            : `Restarting on ${up.version}; this page reloads when the new version answers.`}
        </div>
      ) : (
        <>
          {up.error && (
            <Alert variant="destructive" className="mt-4">
              <AlertDescription>Last update failed: {up.error}</AlertDescription>
            </Alert>
          )}
          {timedOut && (
            <Alert variant="destructive" className="mt-4">
              <AlertDescription>
                The manager did not come back with the new version within 3 minutes. Check `docker logs supabase-manager-updater` and `docker
                ps -a`; the previous container is restored when the new one fails to start.
              </AlertDescription>
            </Alert>
          )}
          <div className="mt-4 flex flex-wrap items-center gap-2">
            {info.update_available && (
              <Button onClick={() => setConfirm('release')} disabled={!info.supported || apply.isPending}>
                <ArrowUpCircle />
                Update to {info.latest}
              </Button>
            )}
            <Button variant="outline" onClick={() => setConfirm('latest')} disabled={!info.supported || apply.isPending}>
              <Download />
              Pull :latest and restart
            </Button>
            {!info.supported && <span className="text-muted-foreground text-xs">Updating from the panel is unavailable: {info.unsupported}</span>}
          </div>
        </>
      )}

      <ConfirmDialog
        open={!!confirm}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={confirm === 'latest' ? 'Pull the latest image and restart?' : `Update to ${info.latest}?`}
        description={
          <>
            The manager pulls{' '}
            <span className="font-mono">{`${info.repository}:${confirm === 'latest' ? 'latest' : info.latest}`}</span> and restarts on it, which
            takes about a minute.
            {confirm === 'latest' && ' This is the newest image pushed to Docker Hub, which may be a build that is not a release yet.'} Running
            projects keep running; job output that is still streaming is cut off.
          </>
        }
        confirmText={confirm === 'latest' ? 'Pull and restart' : 'Update and restart'}
        onConfirm={async () => {
          await apply.mutateAsync(confirm === 'latest' ? 'latest' : info.latest)
        }}
      />
    </Section>
  )
}

function NetworkDefaultsSection() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['system', 'network-defaults'],
    queryFn: () => api.get<NetworkDefaults>('/system/network-defaults'),
  })
  const [form, setForm] = useState<NetworkDefaults | null>(null)
  const [formKey, setFormKey] = useState(0)
  useEffect(() => {
    if (data) {
      setForm({ ...data, mode: data.mode || 'auto' })
      setFormKey((k) => k + 1)
    }
  }, [data])
  const save = useMutation({
    mutationFn: (d: NetworkDefaults) => api.put<NetworkDefaults>('/system/network-defaults', d),
    onSuccess: (d) => {
      qc.setQueryData(['system', 'network-defaults'], d)
      toast.success('Network defaults saved', { description: 'They apply to projects created from now on.' })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !form) return <Skeleton className="h-64" />
  return (
    <Section
      title="Default network for new projects"
      description="Used when a project is created. Imported projects keep the Supabase CLI default; each project can be changed under Project Settings > Network."
    >
      <NetworkForm key={formKey} value={form} onChange={setForm} variant="defaults" />
      <div className="mt-5 flex justify-end">
        <Button onClick={() => save.mutate(form)} disabled={save.isPending}>
          {save.isPending && <Loader2 className="animate-spin" />}
          Save defaults
        </Button>
      </div>
    </Section>
  )
}

function StorageDefaultsSection() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['system', 'storage-defaults'],
    queryFn: () => api.get<StorageConfig>('/system/storage-defaults'),
  })
  const [form, setForm] = useState<StorageConfig | null>(null)
  const [formKey, setFormKey] = useState(0)
  useEffect(() => {
    if (data) {
      setForm({ ...data, mode: data.mode || 'docker' })
      setFormKey((k) => k + 1)
    }
  }, [data])
  const save = useMutation({
    mutationFn: (d: StorageConfig) => api.put<StorageConfig>('/system/storage-defaults', d),
    onSuccess: (d) => {
      qc.setQueryData(['system', 'storage-defaults'], d)
      toast.success('Storage defaults saved', { description: 'They apply to projects created from now on.' })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !form) return <Skeleton className="h-64" />
  return (
    <Section
      title="Default storage for new projects"
      description={
        <>
          Where new projects keep their database and uploaded files. Each project can be changed under Project Settings &gt; Storage; all
          mounts are listed under{' '}
          <Link to="/settings/storage" className="underline underline-offset-2">
            Containers &amp; volumes
          </Link>
          .
        </>
      }
    >
      <StorageForm key={formKey} value={form} onChange={setForm} variant="defaults" />
      <div className="mt-5 flex justify-end">
        <Button onClick={() => save.mutate(form)} disabled={save.isPending}>
          {save.isPending && <Loader2 className="animate-spin" />}
          Save defaults
        </Button>
      </div>
    </Section>
  )
}

function ProxyManagerSection() {
  const qc = useQueryClient()
  const { data, isLoading } = useProxyManagerSettings()
  const [confirmOff, setConfirmOff] = useState(false)
  const save = useMutation({
    mutationFn: (enabled: boolean) => api.put<ProxyManagerSettings>('/system/proxy-manager', { enabled }),
    onSuccess: (d) => {
      qc.setQueryData(['system', 'proxy-manager'], d)
      qc.invalidateQueries({ queryKey: ['pm'] })
      toast.success(d.enabled ? 'Proxy Manager enabled' : 'Proxy Manager disabled')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !data) return <Skeleton className="h-28" />
  return (
    <Section
      title="Proxy Manager"
      description="Run nginx or Traefik proxy containers on this host and publish domains with TLS certificates. Off by default."
    >
      <div className="flex items-center justify-between gap-4">
        <div className="text-sm">
          <div className="flex items-center gap-2 font-medium">
            Enable Proxy Manager
            <Badge variant={data.enabled ? 'default' : 'secondary'}>{data.enabled ? 'On' : 'Off'}</Badge>
          </div>
          <p className="text-muted-foreground mt-1">
            {data.enabled ? (
              <>
                Manage it under{' '}
                <Link to="/proxy-manager" className="underline underline-offset-2">
                  Proxy Manager
                </Link>
                . Turning it off stops every proxy container; the configuration is kept.
              </>
            ) : (
              'Adds Proxy Manager to the sidebar. Proxies deployed before start again when it is turned on.'
            )}
          </p>
        </div>
        <Switch
          checked={data.enabled}
          disabled={save.isPending}
          onCheckedChange={(on) => (on ? save.mutate(true) : setConfirmOff(true))}
        />
      </div>
      <ConfirmDialog
        open={confirmOff}
        onOpenChange={setConfirmOff}
        title="Disable Proxy Manager?"
        description="Every proxy container is stopped, so the domains they serve go offline. Certificate renewal and health checks pause. Hosts, certificates and revisions are kept and come back when you enable it again."
        confirmText="Disable and stop proxies"
        onConfirm={async () => {
          await save.mutateAsync(false)
        }}
      />
    </Section>
  )
}

function SMTPDefaultsSection() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({ queryKey: ['system', 'smtp'], queryFn: () => api.get<SMTPSettings>('/system/smtp') })
  const [form, setForm] = useState<SMTPSettings | null>(null)
  useEffect(() => {
    if (data) setForm({ ...data, pass: '' })
  }, [data])
  const save = useMutation({
    mutationFn: (d: SMTPSettings) => api.put<SMTPSettings>('/system/smtp', d),
    onSuccess: (d) => {
      qc.setQueryData(['system', 'smtp'], d)
      toast.success('Default SMTP server saved', { description: 'New projects use it; existing ones can copy it under Authentication > Emails.' })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !form) return <Skeleton className="h-28" />
  return (
    <Section
      title="Default SMTP server"
      description="Used by new projects for auth emails (confirmations, invitations, magic links, password resets). Each project can override it."
    >
      <div className="grid gap-4">
        <SMTPForm value={form} onChange={setForm} />
        <SMTPTest path="/system/smtp/test" value={form} />
      </div>
      <div className="mt-5 flex justify-end">
        <Button onClick={() => save.mutate(form)} disabled={save.isPending}>
          {save.isPending && <Loader2 className="animate-spin" />}
          Save defaults
        </Button>
      </div>
    </Section>
  )
}

function AboutSection() {
  return (
    <Section title="About" description="Who builds and maintains Supabase Manager.">
      <div className="grid gap-1 text-sm">
        <p>
          Created by <span className="font-medium">{AUTHOR_NAME}</span>
        </p>
        <a href={AUTHOR_MAILTO} className="text-muted-foreground hover:text-foreground w-fit underline underline-offset-4">
          {AUTHOR_EMAIL}
        </a>
      </div>
    </Section>
  )
}

function SystemSubPage({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <>
      <PageHeader title={title} description={description} />
      <div className="grid gap-6">{children}</div>
    </>
  )
}

const base = '/settings/system'

const groups: ProductNavGroup[] = [
  {
    title: 'General',
    items: [
      { to: `${base}/updates`, label: 'Updates' },
      { to: `${base}/cli`, label: 'Supabase CLI' },
      { to: `${base}/about`, label: 'About' },
    ],
  },
  {
    title: 'Features',
    items: [{ to: `${base}/proxy-manager`, label: 'Proxy Manager' }],
  },
  {
    title: 'Project defaults',
    items: [
      { to: `${base}/network`, label: 'Network' },
      { to: `${base}/storage`, label: 'Storage' },
      { to: `${base}/smtp`, label: 'Emails (SMTP)' },
    ],
  },
]

/** Routes under /settings/system/*, one page per settings area. */
export function SystemSettingsRoutes() {
  return (
    <Routes>
      <Route element={<ProductLayout title="System Settings" groups={groups} />}>
        <Route index element={<Navigate to="updates" replace />} />
        <Route
          path="updates"
          element={
            <SystemSubPage title="Updates" description="Check for new Supabase Manager releases and update this container.">
              <ManagerUpdateSection />
            </SystemSubPage>
          }
        />
        <Route
          path="cli"
          element={
            <SystemSubPage title="Supabase CLI" description="The Supabase CLI binary used for every project command.">
              <CLISection />
            </SystemSubPage>
          }
        />
        <Route
          path="about"
          element={
            <SystemSubPage title="About" description="Supabase Manager author and contact.">
              <AboutSection />
            </SystemSubPage>
          }
        />
        <Route
          path="proxy-manager"
          element={
            <SystemSubPage title="Proxy Manager" description="Turn the built-in nginx / Traefik proxy feature on or off.">
              <ProxyManagerSection />
            </SystemSubPage>
          }
        />
        <Route
          path="network"
          element={
            <SystemSubPage title="Network defaults" description="How new projects are networked.">
              <NetworkDefaultsSection />
            </SystemSubPage>
          }
        />
        <Route
          path="storage"
          element={
            <SystemSubPage title="Storage defaults" description="Where new projects keep their data.">
              <StorageDefaultsSection />
            </SystemSubPage>
          }
        />
        <Route
          path="smtp"
          element={
            <SystemSubPage title="Email defaults (SMTP)" description="The SMTP server new projects use for auth emails.">
              <SMTPDefaultsSection />
            </SystemSubPage>
          }
        />
        <Route path="*" element={<Navigate to={base} replace />} />
      </Route>
    </Routes>
  )
}
