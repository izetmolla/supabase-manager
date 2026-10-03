import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUpCircle, CheckCircle2, Download, Loader2, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Badge } from '@workspace/ui/components/badge'
import { Input } from '@workspace/ui/components/input'
import { Switch } from '@workspace/ui/components/switch'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { PageContainer } from '@/components/layout/layouts'
import { PageHeader, Section } from '@/components/page-header'
import { Link } from 'react-router-dom'
import { NetworkForm } from '@/components/network-form'
import { StorageForm } from '@/components/storage-form'
import { api, errorMessage } from '@/lib/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import type { CLIInfo, ManagerUpdateInfo, NetworkDefaults, StorageConfig } from '@/lib/types'
import { timeAgo } from '@/lib/format'

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

/** Polls /api/version until the manager answers with target, then reloads the page. */
function useWaitForVersion() {
  const [target, setTarget] = useState<string | null>(null)
  const [timedOut, setTimedOut] = useState(false)
  useEffect(() => {
    if (!target) return
    let stop = false
    const started = Date.now()
    const tick = async () => {
      if (stop) return
      try {
        const res = await fetch('/api/version', { cache: 'no-store' })
        if (res.ok && ((await res.json()) as { version: string }).version === target) {
          toast.success(`Supabase Manager ${target} is running`)
          window.location.reload()
          return
        }
      } catch {
        // The manager is restarting.
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
  const [confirm, setConfirm] = useState(false)
  const { waitFor, waiting, timedOut } = useWaitForVersion()
  const { data: info, isLoading } = useQuery({
    queryKey: ['system', 'manager-update'],
    queryFn: () => api.get<ManagerUpdateInfo>('/system/update'),
    refetchInterval: (q) => (q.state.data?.update.running && !waiting ? 1500 : false),
  })

  useEffect(() => {
    if (info?.update.running && info.update.phase === 'restarting') waitFor(info.update.version)
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
          {info.update_available && (
            <div className="mt-4 flex flex-wrap items-center gap-2">
              <Button onClick={() => setConfirm(true)} disabled={!info.supported || apply.isPending}>
                <ArrowUpCircle />
                Update to {info.latest}
              </Button>
              {!info.supported && <span className="text-muted-foreground text-xs">Updating from the panel is unavailable: {info.unsupported}</span>}
            </div>
          )}
        </>
      )}

      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={`Update to ${info.latest}?`}
        description={
          <>
            The manager pulls <span className="font-mono">{`${info.repository}:${info.latest}`}</span> and restarts on it, which takes about a
            minute. Running projects keep running; job output that is still streaming is cut off.
          </>
        }
        confirmText="Update and restart"
        onConfirm={async () => {
          await apply.mutateAsync(info.latest)
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

export function SystemPage() {
  return (
    <PageContainer>
      <PageHeader title="System" description="Manager and Supabase CLI versions, and how new projects are networked and stored." />
      <div className="grid gap-6">
        <ManagerUpdateSection />
        <CLISection />
        <NetworkDefaultsSection />
        <StorageDefaultsSection />
      </div>
    </PageContainer>
  )
}
