import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, FolderInput, Loader2, MoreHorizontal, Play, Plus, RotateCw, Search, Server, Square } from 'lucide-react'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Switch } from '@workspace/ui/components/switch'
import { Skeleton } from '@workspace/ui/components/skeleton'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@workspace/ui/components/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@workspace/ui/components/dropdown-menu'
import { PageContainer } from '@/components/layout/layouts'
import { EmptyState, PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { useJobs } from '@/components/job-drawer'
import { useAuth } from '@/hooks/use-auth'
import { useLifecycle, useProjects } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { Job, Ports, Project, Services, SystemInfo } from '@/lib/types'
const defaultServices: Services = {
  studio: true,
  analytics: false,
  inbucket: true,
  edge_runtime: true,
  storage: true,
  realtime: true,
  pooler: false,
}

const serviceInfo: { key: keyof Services; label: string; description: string }[] = [
  { key: 'studio', label: 'Studio', description: 'Dashboard with table and SQL editor' },
  { key: 'storage', label: 'Storage', description: 'File storage API' },
  { key: 'realtime', label: 'Realtime', description: 'Postgres changes over websockets' },
  { key: 'edge_runtime', label: 'Edge Functions', description: 'Deno edge runtime' },
  { key: 'inbucket', label: 'Email testing', description: 'Captures auth emails (Mailpit)' },
  { key: 'analytics', label: 'Analytics', description: 'Logs explorer, ~600 MB RAM' },
  { key: 'pooler', label: 'Connection pooler', description: 'Supavisor pooler' },
]

function slugify(s: string) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
}

function NewProjectDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { openJob } = useJobs()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugTouched, setSlugTouched] = useState(false)
  const [siteUrl, setSiteUrl] = useState('http://127.0.0.1:3000')
  const [services, setServices] = useState<Services>(defaultServices)
  const [start, setStart] = useState(true)

  const preview = useQuery({
    queryKey: ['ports-preview'],
    queryFn: () => api.get<Ports>('/projects/ports-preview'),
    enabled: open,
    staleTime: 0,
  })

  useEffect(() => {
    if (!slugTouched) setSlug(slugify(name))
  }, [name, slugTouched])

  const create = useMutation({
    mutationFn: () =>
      api.post<{ project: Project; job?: Job }>('/projects', { name, slug, site_url: siteUrl, services, start }),
    onSuccess: ({ project, job }) => {
      qc.invalidateQueries({ queryKey: ['projects'] })
      toast.success(`Project ${project.name} created`)
      onOpenChange(false)
      setName('')
      setSlugTouched(false)
      navigate(`/projects/${project.slug}`)
      if (job) openJob(job.id, `Start project - ${project.slug}`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const p = preview.data
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Create a new project</DialogTitle>
          <DialogDescription>
            A new folder is initialised with <code className="font-mono">supabase init</code> and gets its own project ID
            and port block, so it can run next to your other projects.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="np-name">Project name</Label>
              <Input id="np-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="My awesome app" autoFocus />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="np-slug">Project ID (slug)</Label>
              <Input
                id="np-slug"
                value={slug}
                className="font-mono"
                onChange={(e) => {
                  setSlugTouched(true)
                  setSlug(slugify(e.target.value))
                }}
                placeholder="my-awesome-app"
              />
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="np-site">Site URL</Label>
            <Input id="np-site" value={siteUrl} onChange={(e) => setSiteUrl(e.target.value)} />
            <p className="text-muted-foreground text-xs">Where your app runs. Used for auth redirects and email links.</p>
          </div>

          <div className="grid gap-2">
            <Label>Services</Label>
            <div className="grid gap-2 sm:grid-cols-2">
              {serviceInfo.map((s) => (
                <label key={s.key} className="hover:bg-accent/50 flex cursor-pointer items-center justify-between gap-3 rounded-md border px-3 py-2">
                  <span className="grid">
                    <span className="text-sm">{s.label}</span>
                    <span className="text-muted-foreground text-xs">{s.description}</span>
                  </span>
                  <Switch checked={services[s.key]} onCheckedChange={(v) => setServices((cur) => ({ ...cur, [s.key]: v }))} />
                </label>
              ))}
            </div>
          </div>

          <div className="bg-muted/40 rounded-md border px-4 py-3">
            <p className="text-muted-foreground mb-2 text-xs font-medium tracking-wide uppercase">Assigned ports</p>
            {preview.isLoading ? (
              <Skeleton className="h-5 w-full" />
            ) : preview.error ? (
              <p className="text-destructive text-sm">{errorMessage(preview.error)}</p>
            ) : (
              p && (
                <div className="grid grid-cols-2 gap-x-6 gap-y-1 font-mono text-xs sm:grid-cols-4">
                  <span>API {p.api}</span>
                  <span>DB {p.db}</span>
                  <span>Studio {p.studio}</span>
                  <span>Mail {p.smtp}</span>
                  <span>Shadow {p.shadow}</span>
                  <span>Pooler {p.pooler}</span>
                  <span>Analytics {p.analytics}</span>
                  <span>Inspector {p.inspector}</span>
                </div>
              )
            )}
          </div>

          <label className="flex items-center gap-3">
            <Switch checked={start} onCheckedChange={setStart} />
            <span className="text-sm">Start the project after creating it</span>
          </label>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => create.mutate()} disabled={!name || slug.length < 3 || create.isPending}>
            {create.isPending && <Loader2 className="animate-spin" />}
            Create project
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ImportDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const imp = useMutation({
    mutationFn: () => api.post<Project>('/projects/import', { path, name }),
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ['projects'] })
      toast.success(`Imported ${p.name}`)
      onOpenChange(false)
      navigate(`/projects/${p.slug}`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Import an existing project</DialogTitle>
          <DialogDescription>
            Register a folder that already contains <code className="font-mono">supabase/config.toml</code>. Its files
            stay where they are and are never deleted by the manager.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="imp-path">Project directory</Label>
            <Input id="imp-path" value={path} onChange={(e) => setPath(e.target.value)} placeholder="/root" className="font-mono" />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="imp-name">Display name (optional)</Label>
            <Input id="imp-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => imp.mutate()} disabled={!path || imp.isPending}>
            {imp.isPending && <Loader2 className="animate-spin" />}
            Import
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ProjectCard({ project }: { project: Project }) {
  const lifecycle = useLifecycle()
  const busy = project.status === 'starting' || project.status === 'stopping' || !!project.running_job
  const running = project.status === 'running' || project.status === 'error'

  return (
    <div className="group bg-card hover:border-foreground/20 relative flex flex-col rounded-lg border transition-colors">
      <Link to={`/projects/${project.slug}`} className="absolute inset-0 rounded-lg" aria-label={project.name} />
      <div className="flex items-start justify-between gap-3 px-5 pt-5">
        <div className="min-w-0">
          <h3 className="truncate font-medium">{project.name}</h3>
          <p className="text-muted-foreground truncate font-mono text-xs">{project.supabase_project_id}</p>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon-sm" className="relative z-10" aria-label="Project actions">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem disabled={busy || running} onSelect={() => lifecycle.mutate({ slug: project.slug, action: 'start' })}>
              <Play /> Start
            </DropdownMenuItem>
            <DropdownMenuItem disabled={busy || !running} onSelect={() => lifecycle.mutate({ slug: project.slug, action: 'restart' })}>
              <RotateCw /> Restart
            </DropdownMenuItem>
            <DropdownMenuItem disabled={busy || !running} onSelect={() => lifecycle.mutate({ slug: project.slug, action: 'stop' })}>
              <Square /> Stop
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to={`/projects/${project.slug}/settings`}>Settings</Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div className="text-muted-foreground grid grid-cols-2 gap-2 px-5 py-4 font-mono text-xs">
        <span>API :{project.ports?.api ?? '-'}</span>
        <span>DB :{project.ports?.db ?? '-'}</span>
        <span>Studio :{project.ports?.studio ?? '-'}</span>
        <span>Mail :{project.ports?.smtp ?? '-'}</span>
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 border-t px-5 py-3">
        <div className="flex items-center gap-2">
          <StatusBadge status={project.status} />
          {project.imported && <span className="text-muted-foreground text-xs">imported</span>}
        </div>
        {busy ? (
          <Loader2 className="text-muted-foreground size-4 animate-spin" />
        ) : running ? (
          <Button
            variant="outline"
            size="xs"
            className="relative z-10"
            onClick={() => lifecycle.mutate({ slug: project.slug, action: 'stop' })}
          >
            <Square /> Stop
          </Button>
        ) : (
          <Button size="xs" className="relative z-10" onClick={() => lifecycle.mutate({ slug: project.slug, action: 'start' })}>
            <Play /> Start
          </Button>
        )}
      </div>
    </div>
  )
}

export function ProjectsPage() {
  const { isAdmin } = useAuth()
  const { data: projects, isLoading } = useProjects()
  const { data: system } = useQuery({ queryKey: ['system'], queryFn: () => api.get<SystemInfo>('/system'), staleTime: 60_000 })
  const [params, setParams] = useSearchParams()
  const [search, setSearch] = useState('')
  const [importOpen, setImportOpen] = useState(false)
  const newOpen = params.get('new') === '1'
  const setNewOpen = (o: boolean) => setParams(o ? { new: '1' } : {}, { replace: true })

  const filtered = useMemo(
    () =>
      (projects ?? []).filter((p) =>
        `${p.name} ${p.slug} ${p.supabase_project_id}`.toLowerCase().includes(search.toLowerCase()),
      ),
    [projects, search],
  )
  const running = projects?.filter((p) => p.status === 'running').length ?? 0

  return (
    <PageContainer wide>
      <PageHeader
        title="Projects"
        description={
          projects
            ? `${projects.length} project${projects.length === 1 ? '' : 's'}, ${running} running${
                system ? ` - Supabase CLI ${system.supabase_cli || 'not found'} - Docker ${system.docker ? 'connected' : 'unavailable'}` : ''
              }`
            : undefined
        }
        actions={
          isAdmin && (
            <>
              <Button variant="outline" onClick={() => setImportOpen(true)}>
                <FolderInput /> Import existing
              </Button>
              <Button onClick={() => setNewOpen(true)}>
                <Plus /> New project
              </Button>
            </>
          )
        }
      />

      {system && (!system.supabase_cli || system.supabase_cli_update) && (
        <Alert variant={system.supabase_cli ? 'default' : 'destructive'} className="mb-6">
          <AlertTriangle />
          <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
            <span>
              {system.supabase_cli
                ? `A newer Supabase CLI than ${system.supabase_cli} is available.`
                : `The Supabase CLI is not available${system.supabase_cli_error ? ` (${system.supabase_cli_error})` : ''}. Projects cannot start without it.`}
            </span>
            {isAdmin && (
              <Button size="xs" variant="outline" asChild>
                <Link to="/settings/system/cli">{system.supabase_cli ? 'Update' : 'Install'} Supabase CLI</Link>
              </Button>
            )}
          </AlertDescription>
        </Alert>
      )}

      <div className="relative mb-6 max-w-xs">
        <Search className="text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
        <Input placeholder="Search for a project" value={search} onChange={(e) => setSearch(e.target.value)} className="pl-8" />
      </div>

      {isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-44 rounded-lg" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<Server />}
          title={search ? 'No matching projects' : 'No projects yet'}
          description={search ? undefined : 'Create a new local Supabase project or import one that already exists on this machine.'}
          action={
            isAdmin &&
            !search && (
              <Button onClick={() => setNewOpen(true)}>
                <Plus /> New project
              </Button>
            )
          }
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {filtered.map((p) => (
            <ProjectCard key={p.id} project={p} />
          ))}
        </div>
      )}

      {system && (
        <p className="text-muted-foreground mt-6 text-xs">
          New projects are created in <span className="font-mono">{system.projects_root}</span>
        </p>
      )}

      {isAdmin && (
        <>
          <NewProjectDialog open={newOpen} onOpenChange={setNewOpen} />
          <ImportDialog open={importOpen} onOpenChange={setImportOpen} />
        </>
      )}
    </PageContainer>
  )
}
