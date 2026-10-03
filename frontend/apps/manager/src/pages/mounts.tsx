import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Boxes, Container, Database, HardDrive, Loader2, RefreshCw, Search } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Badge } from '@workspace/ui/components/badge'
import { Input } from '@workspace/ui/components/input'
import { Switch } from '@workspace/ui/components/switch'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@workspace/ui/components/tabs'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@workspace/ui/components/table'
import { PageContainer } from '@/components/layout/layouts'
import { EmptyState, PageHeader } from '@/components/page-header'
import { storageModeLabel } from '@/components/storage-form'
import { useDockerInfo } from '@/hooks/use-docker'
import { api, errorMessage } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { cn } from '@workspace/ui/lib/utils'
import type { ContainerMounts, MountView, MountsResponse, VolumeOverview } from '@/lib/types'

type Filter = 'all' | 'supabase' | 'custom'

const FILTERS: { value: Filter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'supabase', label: 'Supabase projects' },
  { value: 'custom', label: 'Custom storage' },
]

const isCustomVolume = (v: VolumeOverview) => v.managed && v.storage_mode !== 'docker'
const isSupabaseVolume = (v: VolumeOverview) => !!v.project || !!v.labels?.['com.supabase.cli.project']

function StorageBadge({ mode, managed }: { mode?: string; managed?: boolean }) {
  if (!mode) return null
  const custom = managed && mode !== 'docker'
  return (
    <Badge variant={custom ? 'default' : 'outline'} className="font-normal">
      {storageModeLabel(mode)}
    </Badge>
  )
}

function StateBadge({ state }: { state: string }) {
  const running = state === 'running'
  return (
    <Badge variant={running ? 'secondary' : 'outline'} className="gap-1.5 font-normal">
      <span className={cn('size-1.5 rounded-full', running ? 'bg-brand' : 'bg-muted-foreground')} />
      {state}
    </Badge>
  )
}

function MountLine({ m }: { m: MountView }) {
  const custom = m.managed && m.storage_mode !== 'docker'
  const source = m.type === 'volume' ? m.name : m.source
  return (
    <div className={cn('grid gap-0.5 rounded-md px-2 py-1.5', custom && 'bg-primary/5 ring-primary/20 ring-1')}>
      <div className="flex flex-wrap items-center gap-1.5 text-xs">
        <Badge variant="outline" className="px-1.5 py-0 font-mono text-[10px] uppercase">
          {m.type}
        </Badge>
        <span className="font-mono break-all">{source}</span>
        <span className="text-muted-foreground">→</span>
        <span className="font-mono break-all">{m.destination}</span>
        {!m.rw && (
          <Badge variant="outline" className="px-1.5 py-0 text-[10px]">
            read-only
          </Badge>
        )}
        {m.type === 'volume' && <StorageBadge mode={m.storage_mode} managed={m.managed} />}
      </div>
      {m.type === 'volume' && m.location && m.location !== source && (
        <div className="text-muted-foreground flex items-center gap-1 font-mono text-[11px] break-all">
          <HardDrive className="size-3 shrink-0" />
          {m.location}
        </div>
      )}
    </div>
  )
}

function Stat({ icon, label, value, hint }: { icon: React.ReactNode; label: string; value: React.ReactNode; hint?: string }) {
  return (
    <div className="bg-card grid gap-1 rounded-lg border px-4 py-3">
      <span className="text-muted-foreground flex items-center gap-1.5 text-xs [&_svg]:size-3.5">
        {icon}
        {label}
      </span>
      <span className="text-xl font-medium tabular-nums">{value}</span>
      {hint && <span className="text-muted-foreground truncate font-mono text-[11px]">{hint}</span>}
    </div>
  )
}

function ProjectLink({ slug }: { slug?: string }) {
  if (!slug) return <span className="text-muted-foreground text-xs">-</span>
  return (
    <Link to={`/projects/${slug}/settings/storage`} className="text-xs underline-offset-2 hover:underline">
      {slug}
    </Link>
  )
}

function ContainersTable({ containers }: { containers: ContainerMounts[] }) {
  if (containers.length === 0) {
    return <EmptyState icon={<Container />} title="No containers match" description="Try another filter or search term." />
  }
  return (
    <div className="bg-card overflow-hidden rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-72">Container</TableHead>
            <TableHead className="w-28">State</TableHead>
            <TableHead>Mounts</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {containers.map((c) => (
            <TableRow key={c.id} className={cn(c.custom_storage && 'border-l-primary border-l-2')}>
              <TableCell className="align-top whitespace-normal">
                <div className="font-mono text-sm break-all">{c.name}</div>
                <div className="text-muted-foreground font-mono text-xs break-all">{c.image}</div>
                <div className="mt-1 flex flex-wrap items-center gap-1.5">
                  {c.project && <ProjectLink slug={c.project} />}
                  {c.custom_storage && <Badge className="font-normal">Custom storage</Badge>}
                </div>
              </TableCell>
              <TableCell className="align-top">
                <StateBadge state={c.state} />
                <div className="text-muted-foreground mt-1 text-xs whitespace-normal">{c.status}</div>
              </TableCell>
              <TableCell className="align-top whitespace-normal">
                {c.mounts.length === 0 ? (
                  <span className="text-muted-foreground text-xs">No mounts</span>
                ) : (
                  <div className="grid gap-1">
                    {c.mounts.map((m, i) => (
                      <MountLine key={i} m={m} />
                    ))}
                  </div>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function VolumesTable({ volumes, showSizes }: { volumes: VolumeOverview[]; showSizes: boolean }) {
  if (volumes.length === 0) {
    return <EmptyState icon={<Database />} title="No volumes match" description="Try another filter or search term." />
  }
  return (
    <div className="bg-card overflow-hidden rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Volume</TableHead>
            <TableHead>Storage</TableHead>
            <TableHead>Location</TableHead>
            <TableHead>Used by</TableHead>
            {showSizes && <TableHead className="text-right">Size</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {volumes.map((v) => (
            <TableRow key={v.name} className={cn(isCustomVolume(v) && 'border-l-primary border-l-2')}>
              <TableCell className="max-w-72 align-top whitespace-normal">
                <div className="font-mono text-sm break-all">{v.name}</div>
                <div className="mt-1 flex flex-wrap items-center gap-1.5">
                  <ProjectLink slug={v.project} />
                  <span className="text-muted-foreground font-mono text-xs">{v.driver}</span>
                </div>
              </TableCell>
              <TableCell className="align-top">
                <StorageBadge mode={v.storage_mode} managed={v.managed} />
                {v.managed && <div className="text-muted-foreground mt-1 text-xs">Managed</div>}
              </TableCell>
              <TableCell className="max-w-96 align-top font-mono text-xs break-all whitespace-normal">{v.location}</TableCell>
              <TableCell className="align-top whitespace-normal">
                {v.used_by.length === 0 ? (
                  <span className="text-muted-foreground text-xs">Unused</span>
                ) : (
                  <div className="grid gap-0.5">
                    {v.used_by.map((n) => (
                      <span key={n} className="font-mono text-xs">
                        {n}
                      </span>
                    ))}
                  </div>
                )}
              </TableCell>
              {showSizes && (
                <TableCell className="text-right align-top text-xs tabular-nums">{v.size !== undefined && v.size >= 0 ? formatBytes(v.size) : '-'}</TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

export function MountsPage() {
  const [filter, setFilter] = useState<Filter>('all')
  const [search, setSearch] = useState('')
  const [showSizes, setShowSizes] = useState(false)
  const { data: info } = useDockerInfo()
  const { data, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ['system', 'mounts', showSizes],
    queryFn: () => api.get<MountsResponse>(`/system/mounts${showSizes ? '?sizes=1' : ''}`),
  })

  const q = search.trim().toLowerCase()
  const containers = useMemo(() => {
    const all = (data?.containers ?? []).filter((c) => c.mounts.length > 0)
    return all.filter((c) => {
      if (filter === 'supabase' && !c.project) return false
      if (filter === 'custom' && !c.custom_storage) return false
      if (!q) return true
      return [c.name, c.image, c.project ?? '', ...c.mounts.flatMap((m) => [m.name ?? '', m.source, m.destination, m.location])].some((s) =>
        s.toLowerCase().includes(q),
      )
    })
  }, [data, filter, q])
  const volumes = useMemo(() => {
    return (data?.volumes ?? []).filter((v) => {
      if (filter === 'supabase' && !isSupabaseVolume(v)) return false
      if (filter === 'custom' && !isCustomVolume(v)) return false
      if (!q) return true
      return [v.name, v.location, v.driver, v.project ?? '', ...v.used_by].some((s) => s.toLowerCase().includes(q))
    })
  }, [data, filter, q])

  const counts = useMemo(() => {
    const cts = (data?.containers ?? []).filter((c) => c.mounts.length > 0)
    const vols = data?.volumes ?? []
    return {
      all: cts.length + vols.length,
      supabase: cts.filter((c) => c.project).length + vols.filter(isSupabaseVolume).length,
      custom: cts.filter((c) => c.custom_storage).length + vols.filter(isCustomVolume).length,
      customVolumes: vols.filter(isCustomVolume).length,
      unused: vols.filter((v) => v.used_by.length === 0).length,
      containers: cts.length,
      volumes: vols.length,
    }
  }, [data])

  return (
    <PageContainer>
      <PageHeader
        title="Containers & volumes"
        description="Every container with its mounts and every Docker volume. Volumes whose location the manager controls (host folder, NFS or a volume driver) are highlighted."
        actions={
          <>
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={showSizes} onCheckedChange={setShowSizes} />
              Volume sizes
            </label>
            <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
              {isFetching ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              Refresh
            </Button>
          </>
        }
      />

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{errorMessage(error)}</AlertDescription>
        </Alert>
      ) : isLoading || !data ? (
        <Skeleton className="h-96" />
      ) : (
        <div className="grid gap-6">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Stat icon={<Container />} label="Containers with mounts" value={counts.containers} />
            <Stat icon={<Boxes />} label="Volumes" value={counts.volumes} hint={counts.unused ? `${counts.unused} unused` : undefined} />
            <Stat icon={<HardDrive />} label="On custom storage" value={counts.customVolumes} />
            <Stat
              icon={<Database />}
              label="Docker data root"
              value={<span className="text-sm">{info ? `${info.storage_driver} · Docker ${info.server_version}` : '-'}</span>}
              hint={data.docker_root}
            />
          </div>

          <Tabs defaultValue="containers">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <TabsList>
                <TabsTrigger value="containers">Containers ({containers.length})</TabsTrigger>
                <TabsTrigger value="volumes">Volumes ({volumes.length})</TabsTrigger>
              </TabsList>
              <div className="flex flex-wrap items-center gap-2">
                <div className="flex rounded-md border p-0.5" role="radiogroup" aria-label="Filter">
                  {FILTERS.map((f) => (
                    <button
                      key={f.value}
                      type="button"
                      role="radio"
                      aria-checked={filter === f.value}
                      onClick={() => setFilter(f.value)}
                      className={cn(
                        'rounded px-2.5 py-1 text-xs transition-colors',
                        filter === f.value ? 'bg-secondary text-secondary-foreground font-medium' : 'text-muted-foreground hover:text-foreground',
                      )}
                    >
                      {f.label} <span className="tabular-nums opacity-60">{counts[f.value]}</span>
                    </button>
                  ))}
                </div>
                <div className="relative">
                  <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
                  <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search name, path, project..." className="h-8 w-64 pl-8 text-xs" />
                </div>
              </div>
            </div>
            <TabsContent value="containers" className="mt-4">
              <ContainersTable containers={containers} />
            </TabsContent>
            <TabsContent value="volumes" className="mt-4">
              <VolumesTable volumes={volumes} showSizes={showSizes} />
            </TabsContent>
          </Tabs>
        </div>
      )}
    </PageContainer>
  )
}
