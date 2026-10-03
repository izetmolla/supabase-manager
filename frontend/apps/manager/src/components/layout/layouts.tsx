import type { ReactNode } from 'react'
import { Navigate, NavLink, Outlet, useLocation } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { TopBar } from '@/components/layout/top-bar'
import { IconRail } from '@/components/layout/icon-rail'
import { JobPanel } from '@/components/job-drawer'
import { useAuth } from '@/hooks/use-auth'
import { useProject, useProjectStatus, useSlug } from '@/hooks/use-projects'
import { serviceUrl } from '@/lib/format'
import { cn } from '@workspace/ui/lib/utils'

export function FullPageSpinner() {
  return (
    <div className="flex h-screen items-center justify-center">
      <Loader2 className="text-muted-foreground size-6 animate-spin" />
    </div>
  )
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading, needsSetup } = useAuth()
  const location = useLocation()
  if (loading) return <FullPageSpinner />
  if (needsSetup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  return <>{children}</>
}

export function RequireAdmin({ children }: { children: ReactNode }) {
  const { isAdmin } = useAuth()
  if (!isAdmin) return <Navigate to="/projects" replace />
  return <>{children}</>
}

export function AppLayout() {
  return (
    <div className="flex h-screen flex-col">
      <TopBar />
      <main className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </main>
      <JobPanel />
    </div>
  )
}

export function ProjectLayout() {
  const slug = useSlug()
  const { data: project, isLoading, error } = useProject(slug)
  const { data: status } = useProjectStatus(slug)
  const studioUrl = status?.details?.studio_url ? serviceUrl(slug, 'studio') : undefined

  if (isLoading) return <FullPageSpinner />
  if (error || !project) return <Navigate to="/projects" replace />

  return (
    <div className="flex h-screen flex-col">
      <TopBar project={project} studioUrl={studioUrl} />
      <div className="flex min-h-0 flex-1">
        <IconRail slug={slug} studioUrl={studioUrl} />
        <main className="min-w-0 flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
      <JobPanel />
    </div>
  )
}

export interface ProductNavGroup {
  title?: string
  items: { to: string; label: string; end?: boolean }[]
}

/** Secondary sidebar used by Auth, Database and Settings, like Studio's product menu. */
export function ProductLayout({ title, groups }: { title: string; groups: ProductNavGroup[] }) {
  return (
    <div className="flex h-full min-h-0">
      <aside className="bg-background hidden w-56 shrink-0 flex-col border-r md:flex">
        <div className="flex h-12 items-center border-b px-5">
          <h2 className="text-sm font-medium">{title}</h2>
        </div>
        <nav className="flex flex-col gap-4 overflow-auto px-3 py-4">
          {groups.map((g, i) => (
            <div key={i} className="flex flex-col gap-0.5">
              {g.title && (
                <p className="text-muted-foreground mb-1 px-2 font-mono text-[11px] tracking-wider uppercase">{g.title}</p>
              )}
              {g.items.map((it) => (
                <NavLink
                  key={it.to}
                  to={it.to}
                  end={it.end}
                  className={({ isActive }) =>
                    cn(
                      'rounded-md px-2 py-1.5 text-sm transition-colors',
                      isActive ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground',
                    )
                  }
                >
                  {it.label}
                </NavLink>
              ))}
            </div>
          ))}
        </nav>
      </aside>
      <div className="min-w-0 flex-1 overflow-auto">
        <div className="mx-auto max-w-5xl px-6 py-8">
          <Outlet />
        </div>
      </div>
    </div>
  )
}

export function PageContainer({ children, wide }: { children: ReactNode; wide?: boolean }) {
  return <div className={cn('mx-auto px-6 py-8', wide ? 'max-w-7xl' : 'max-w-5xl')}>{children}</div>
}
