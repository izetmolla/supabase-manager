import { Fragment, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
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

const MENU_WIDTH_KEY = 'sm.product-menu-width'
const MENU_MIN = 200
const MENU_MAX = 400
const MENU_DEFAULT = 256

function useMenuWidth() {
  const [width, setWidth] = useState(() => {
    const n = Number(localStorage.getItem(MENU_WIDTH_KEY))
    return n >= MENU_MIN && n <= MENU_MAX ? n : MENU_DEFAULT
  })
  useEffect(() => localStorage.setItem(MENU_WIDTH_KEY, String(width)), [width])
  return [width, (w: number) => setWidth(Math.round(Math.min(MENU_MAX, Math.max(MENU_MIN, w))))] as const
}

/** Secondary sidebar used by Auth, Database and Settings, like Studio's product menu. */
export function ProductLayout({ title, groups }: { title: string; groups: ProductNavGroup[] }) {
  const [width, setWidth] = useMenuWidth()
  const drag = useRef<{ x: number; w: number } | null>(null)

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.currentTarget.setPointerCapture(e.pointerId)
    drag.current = { x: e.clientX, w: width }
  }
  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (drag.current) setWidth(drag.current.w + e.clientX - drag.current.x)
  }
  const onPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    drag.current = null
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId)
  }

  return (
    <div className="flex h-full min-h-0">
      <aside className="bg-sidebar hidden shrink-0 flex-col md:flex" style={{ width }}>
        <div className="flex min-h-12 shrink-0 items-center justify-between gap-2 border-b px-6">
          <h4 className="min-w-0 flex-1 truncate text-sm font-semibold">{title}</h4>
        </div>
        <nav className="flex flex-col overflow-auto">
          {groups.map((g, i) => (
            <Fragment key={i}>
              {i > 0 && <div className="bg-border h-px shrink-0" />}
              <div className="my-4 space-y-4">
                <div className="md:mx-3">
                  {g.title && (
                    <div className="mb-2 flex px-3 font-normal">
                      <span className="text-foreground-lighter w-full font-mono text-sm uppercase">{g.title}</span>
                    </div>
                  )}
                  <ul>
                    {g.items.map((it) => (
                      <li key={it.to}>
                        <NavLink
                          to={it.to}
                          end={it.end}
                          className={({ isActive }) =>
                            cn(
                              'my-px flex items-center rounded-md px-3 py-[3px] text-sm transition-colors',
                              isActive
                                ? 'bg-sidebar-accent text-foreground font-semibold'
                                : 'text-foreground-light hover:bg-sidebar-accent/50 hover:text-foreground',
                            )
                          }
                        >
                          <span className="min-w-0 flex-1 truncate">{it.label}</span>
                        </NavLink>
                      </li>
                    ))}
                  </ul>
                </div>
              </div>
            </Fragment>
          ))}
        </nav>
      </aside>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize menu"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onDoubleClick={() => setWidth(MENU_DEFAULT)}
        className="bg-border hover:bg-foreground-lighter relative hidden w-px shrink-0 cursor-col-resize touch-none transition-colors after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2 md:block"
      />
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
