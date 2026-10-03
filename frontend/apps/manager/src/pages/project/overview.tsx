import { useQuery, useMutation } from '@tanstack/react-query'
import { Activity, ExternalLink, Loader2, Mail, Play, RotateCw, Square, Table2, Terminal } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Badge } from '@workspace/ui/components/badge'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@workspace/ui/components/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@workspace/ui/components/tooltip'
import { PageContainer } from '@/components/layout/layouts'
import { EmptyState, PageHeader, Section } from '@/components/page-header'
import { CopyField } from '@/components/copy-field'
import { ProjectDomainsSection } from '@/components/project-domains'
import { StatusBadge } from '@/components/status-badge'
import { useJobs } from '@/components/job-drawer'
import { useLifecycle, useProject, useProjectPublicUrls, useProjectStatus, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { Container, ContainerStats, Job } from '@/lib/types'
import { formatBytes, serviceUrl, timeAgo } from '@/lib/format'
import { cn } from '@workspace/ui/lib/utils'

function LifecycleButtons() {
  const slug = useSlug()
  const { data: project } = useProject()
  const lifecycle = useLifecycle()
  if (!project) return null
  const busy = project.status === 'starting' || project.status === 'stopping' || !!project.running_job || lifecycle.isPending
  const running = project.status === 'running' || project.status === 'error'

  if (busy) {
    return (
      <Button variant="outline" disabled>
        <Loader2 className="animate-spin" /> Working...
      </Button>
    )
  }
  if (!running) {
    return (
      <Button onClick={() => lifecycle.mutate({ slug, action: 'start' })}>
        <Play /> Start project
      </Button>
    )
  }
  return (
    <>
      <Button variant="outline" onClick={() => lifecycle.mutate({ slug, action: 'restart' })}>
        <RotateCw /> Restart
      </Button>
      <Button variant="outline" onClick={() => lifecycle.mutate({ slug, action: 'stop' })}>
        <Square /> Stop
      </Button>
    </>
  )
}

function ServicesTable({ running }: { running: boolean }) {
  const slug = useSlug()
  const containers = useQuery({
    queryKey: ['project', slug, 'containers'],
    queryFn: () => api.get<Container[]>(`/projects/${slug}/containers`),
    refetchInterval: 10_000,
  })
  const stats = useQuery({
    queryKey: ['project', slug, 'stats'],
    queryFn: () => api.get<ContainerStats[]>(`/projects/${slug}/stats`),
    refetchInterval: 10_000,
    enabled: running,
  })
  const restart = useMutation({
    mutationFn: (service: string) => api.post(`/projects/${slug}/containers/${service}/restart`),
    onSuccess: (_, service) => {
      toast.success(`Restarted ${service}`)
      containers.refetch()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const statsBy = new Map((stats.data ?? []).map((s) => [s.service, s]))
  const totalMem = (stats.data ?? []).reduce((a, s) => a + s.mem_usage, 0)

  if (containers.isLoading) return <Skeleton className="h-40" />
  if (!containers.data?.length) {
    return <p className="text-muted-foreground text-sm">No containers. Start the project to see its services.</p>
  }

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Service</TableHead>
            <TableHead>State</TableHead>
            <TableHead>Host ports</TableHead>
            <TableHead className="text-right">CPU</TableHead>
            <TableHead className="text-right">Memory</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {containers.data.map((c) => {
            const s = statsBy.get(c.service)
            const published = (c.ports ?? []).filter((p) => p.public_port)
            return (
              <TableRow key={c.id}>
                <TableCell>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="font-medium">{c.service}</span>
                    </TooltipTrigger>
                    <TooltipContent>{c.image}</TooltipContent>
                  </Tooltip>
                </TableCell>
                <TableCell>
                  <span className="flex items-center gap-2 text-xs">
                    <span
                      className={cn(
                        'size-2 rounded-full',
                        c.state !== 'running' ? 'bg-destructive' : c.health === 'unhealthy' ? 'bg-amber-400' : 'bg-brand',
                      )}
                    />
                    {c.state}
                    {c.health && <span className="text-muted-foreground">({c.health})</span>}
                  </span>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {published.length ? published.map((p) => `${p.public_port}`).join(', ') : <span className="text-muted-foreground">internal</span>}
                </TableCell>
                <TableCell className="text-right font-mono text-xs">{s ? `${s.cpu_percent.toFixed(1)}%` : '-'}</TableCell>
                <TableCell className="text-right font-mono text-xs">{s ? formatBytes(s.mem_usage) : '-'}</TableCell>
                <TableCell className="text-right">
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Restart ${c.service}`}
                    disabled={restart.isPending}
                    onClick={() => restart.mutate(c.service)}
                  >
                    <RotateCw />
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
      {totalMem > 0 && (
        <p className="text-muted-foreground mt-3 text-xs">
          Total memory: <span className="font-mono">{formatBytes(totalMem)}</span>
        </p>
      )}
    </>
  )
}

function RecentJobs() {
  const slug = useSlug()
  const { openJob } = useJobs()
  const { data: jobs } = useQuery({
    queryKey: ['project', slug, 'jobs'],
    queryFn: () => api.get<Job[]>(`/projects/${slug}/jobs`),
    refetchInterval: 10_000,
  })
  if (!jobs?.length) return <p className="text-muted-foreground text-sm">No commands have been run yet.</p>
  return (
    <div className="divide-y">
      {jobs.slice(0, 8).map((j) => (
        <button
          key={j.id}
          onClick={() => openJob(j.id, j.command)}
          className="hover:bg-accent/40 flex w-full items-center justify-between gap-3 px-1 py-2 text-left"
        >
          <span className="flex min-w-0 items-center gap-2">
            <Terminal className="text-muted-foreground size-3.5 shrink-0" />
            <span className="truncate font-mono text-xs">{j.command}</span>
          </span>
          <span className="flex shrink-0 items-center gap-2">
            <Badge
              variant={j.status === 'failed' ? 'destructive' : 'secondary'}
              className={cn(j.status === 'succeeded' && 'text-brand', j.status === 'running' && 'text-amber-400')}
            >
              {j.status}
            </Badge>
            <span className="text-muted-foreground w-16 text-right text-xs">{timeAgo(j.created_at)}</span>
          </span>
        </button>
      ))}
    </div>
  )
}

export function OverviewPage() {
  const { data: project } = useProject()
  const { data: status, isLoading } = useProjectStatus()
  const lifecycle = useLifecycle()
  const publicUrls = useProjectPublicUrls()
  if (!project) return null
  const d = status?.details
  const apiUrl = (d?.api_url && publicUrls.api) || d?.api_url
  const running = project.status === 'running' || project.status === 'error'

  return (
    <PageContainer>
      <PageHeader
        title={project.name}
        description={
          <span className="flex flex-wrap items-center gap-2">
            <StatusBadge status={project.status} />
            <span className="font-mono text-xs">{project.path}</span>
          </span>
        }
        actions={<LifecycleButtons />}
      />

      {!running && !project.running_job && project.status !== 'starting' ? (
        <EmptyState
          icon={<Activity />}
          title="This project is not running"
          description="Start it to get API URLs, keys and service health. The first start may take a few minutes while Docker images download."
          action={
            <Button onClick={() => lifecycle.mutate({ slug: project.slug, action: 'start' })}>
              <Play /> Start project
            </Button>
          }
        />
      ) : (
        <div className="grid gap-6">
          <div className="grid gap-3 sm:grid-cols-3">
            {[
              {
                label: 'Studio',
                href: d?.studio_url && (publicUrls.studio || serviceUrl(project.slug, 'studio')),
                icon: Table2,
                hint: publicUrls.studio || 'Table & SQL editor',
              },
              {
                label: 'Mailpit',
                href: d?.inbucket_url && (publicUrls.mail || serviceUrl(project.slug, 'mail')),
                icon: Mail,
                hint: publicUrls.mail || 'Captured auth emails',
              },
              { label: 'API', href: apiUrl, icon: Activity, hint: apiUrl },
            ].map((l) => (
              <a
                key={l.label}
                href={l.href || undefined}
                target="_blank"
                rel="noreferrer"
                className={cn(
                  'bg-card hover:border-foreground/20 flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors',
                  !l.href && 'pointer-events-none opacity-50',
                )}
              >
                <l.icon className="text-brand size-5" />
                <span className="grid min-w-0 flex-1">
                  <span className="text-sm font-medium">{l.label}</span>
                  <span className="text-muted-foreground truncate text-xs">{l.href ? l.hint : 'disabled'}</span>
                </span>
                <ExternalLink className="text-muted-foreground size-3.5" />
              </a>
            ))}
          </div>

          <Section title="Project API" description="Use these to connect your app with supabase-js or any HTTP client.">
            {isLoading || !d ? (
              <Skeleton className="h-48" />
            ) : (
              <div className="grid gap-4">
                <CopyField label="Project URL" value={apiUrl ?? d.api_url} />
                {apiUrl !== d.api_url && <CopyField label="Local URL" value={d.api_url} />}
                <div className="grid gap-4 md:grid-cols-2">
                  <CopyField label="Publishable key" value={d.publishable_key} description="Safe to use in a browser." />
                  <CopyField label="Secret key" value={d.secret_key} secret description="Server-side only. Bypasses RLS." />
                  <CopyField label="anon key (legacy)" value={d.anon_key} secret />
                  <CopyField label="service_role key (legacy)" value={d.service_role_key} secret />
                </div>
              </div>
            )}
          </Section>

          <ProjectDomainsSection />

          <Section title="Database" description="Direct Postgres connection for psql, migrations and ORMs.">
            {d ? (
              <div className="grid gap-4 md:grid-cols-2">
                <CopyField label="Connection string" value={d.db_url} secret />
                <CopyField label="JWT secret" value={d.jwt_secret} secret />
              </div>
            ) : (
              <Skeleton className="h-16" />
            )}
          </Section>

          <Section title="Services" description="Docker containers of this project.">
            <ServicesTable running={running} />
          </Section>
        </div>
      )}

      <div className="mt-6">
        <Section title="Recent activity" description="Supabase CLI commands run by the manager. Click one to see its output.">
          <RecentJobs />
        </Section>
      </div>
    </PageContainer>
  )
}
