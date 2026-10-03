import { Fragment, useEffect, useRef, useState } from 'react'
import { NavLink } from 'react-router-dom'
import {
  Code2,
  Database,
  ExternalLink,
  HardDrive,
  Home,
  Network,
  PanelLeftDashed,
  ScrollText,
  Settings,
  Table2,
  Users,
  type LucideIcon,
} from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@workspace/ui/components/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@workspace/ui/components/tooltip'
import { cn } from '@workspace/ui/lib/utils'
import { useProxyManagerSettings } from '@/hooks/use-proxy-manager'

/** Same choices as Studio's "Sidebar control": always open, icons only, or open while hovered. */
type SidebarBehavior = 'open' | 'closed' | 'expandable'

const BEHAVIOR_KEY = 'sm.sidebar-behavior'
const behaviors: { value: SidebarBehavior; label: string }[] = [
  { value: 'open', label: 'Expanded' },
  { value: 'closed', label: 'Collapsed' },
  { value: 'expandable', label: 'Expand on hover' },
]

function useSidebarBehavior() {
  const [behavior, setBehavior] = useState<SidebarBehavior>(() => {
    const v = localStorage.getItem(BEHAVIOR_KEY)
    return v === 'open' || v === 'closed' ? v : 'expandable'
  })
  useEffect(() => localStorage.setItem(BEHAVIOR_KEY, behavior), [behavior])
  return [behavior, setBehavior] as const
}

interface RailItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
  external?: boolean
}

const itemClass =
  'flex h-8 w-full items-center gap-2 overflow-hidden rounded-md px-1.5 py-2 text-left text-sm text-foreground-lighter outline-hidden ring-sidebar-ring transition-[width,height,padding] focus-visible:ring-2 hover:bg-sidebar-accent/50 hover:text-sidebar-accent-foreground [&>span:last-child]:truncate [&>svg]:size-5 [&>svg]:shrink-0'

/** Project navigation. */
export function IconRail({ slug, studioUrl }: { slug: string; studioUrl?: string }) {
  const base = `/projects/${slug}`
  return (
    <Rail
      groups={[
        [
          { to: base, label: 'Project Overview', icon: Home, end: true },
          ...(studioUrl ? [{ to: studioUrl, label: 'Table & SQL Editor', icon: Table2, external: true }] : []),
        ],
        [
          { to: `${base}/database`, label: 'Database', icon: Database },
          { to: `${base}/auth`, label: 'Authentication', icon: Users },
          { to: `${base}/functions`, label: 'Edge Functions', icon: Code2 },
        ],
        [{ to: `${base}/logs`, label: 'Logs', icon: ScrollText }],
        [{ to: `${base}/settings`, label: 'Project Settings', icon: Settings }],
      ]}
    />
  )
}

/** Navigation outside a project: the projects list and the manager-wide admin pages. */
export function AppRail({ isAdmin }: { isAdmin: boolean }) {
  const { data: proxyManager } = useProxyManagerSettings(isAdmin)
  return (
    <Rail
      groups={[
        [{ to: '/projects', label: 'Projects', icon: Home, end: true }],
        ...(isAdmin
          ? [
              [
                { to: '/settings/users', label: 'Team', icon: Users },
                { to: '/settings/storage', label: 'Containers & Volumes', icon: HardDrive },
              ],
              ...(proxyManager?.enabled ? [[{ to: '/proxy-manager', label: 'Proxy Manager', icon: Network }]] : []),
              [{ to: '/settings/system', label: 'System Settings', icon: Settings }],
            ]
          : []),
      ]}
    />
  )
}

/**
 * Modelled on Studio's sidebar: 3rem of icons that grows to 13rem. In "expand on hover" mode it
 * opens over the page; in "expanded" mode it takes the space.
 */
function Rail({ groups }: { groups: RailItem[][] }) {
  const [behavior, setBehavior] = useSidebarBehavior()
  const [hovered, setHovered] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const leaveTimer = useRef<number | undefined>(undefined)

  const open = behavior === 'open' || (behavior === 'expandable' && (hovered || menuOpen))
  const collapsed = !open

  const onEnter = () => {
    window.clearTimeout(leaveTimer.current)
    if (behavior === 'expandable') setHovered(true)
  }
  const onLeave = () => {
    window.clearTimeout(leaveTimer.current)
    leaveTimer.current = window.setTimeout(() => setHovered(false), 80)
  }
  useEffect(() => () => window.clearTimeout(leaveTimer.current), [])

  const withTooltip = (label: string, node: React.ReactElement) =>
    behavior === 'closed' ? (
      <Tooltip>
        <TooltipTrigger asChild>{node}</TooltipTrigger>
        <TooltipContent side="right">{label}</TooltipContent>
      </Tooltip>
    ) : (
      node
    )

  const renderItem = (it: RailItem) => {
    const sizing = collapsed && 'size-8! pr-2! pl-1.5!'
    if (it.external) {
      return withTooltip(
        it.label,
        <a href={it.to} target="_blank" rel="noreferrer" className={cn(itemClass, sizing)}>
          <it.icon strokeWidth={1.5} />
          <span className="flex items-center gap-1">
            {it.label}
            <ExternalLink className="size-3 shrink-0" />
          </span>
        </a>,
      )
    }
    return withTooltip(
      it.label,
      <NavLink
        to={it.to}
        end={it.end}
        className={({ isActive }) => cn(itemClass, isActive && 'bg-sidebar-accent text-foreground font-medium', sizing)}
      >
        <it.icon strokeWidth={1.5} />
        <span>{it.label}</span>
      </NavLink>,
    )
  }

  return (
    <div
      data-state={open ? 'expanded' : 'collapsed'}
      className={cn(
        'text-sidebar-foreground relative shrink-0 transition-[width] duration-100 ease-linear',
        behavior === 'open' ? 'w-52' : 'w-12',
      )}
    >
      <div
        onMouseEnter={onEnter}
        onMouseLeave={onLeave}
        className={cn(
          'bg-sidebar absolute inset-y-0 left-0 z-50 flex flex-col overflow-hidden border-r transition-[width] duration-100 ease-linear',
          open ? 'w-52' : 'w-12',
        )}
      >
        <nav className={cn('flex min-h-0 flex-1 flex-col gap-0 overflow-auto', collapsed && 'overflow-hidden')}>
          {groups.map((g, gi) => (
            <Fragment key={gi}>
              {gi > 0 && <div className="bg-border mx-auto h-px w-[calc(100%-1rem)] shrink-0" />}
              <div className="relative flex w-full min-w-0 flex-col gap-0.5 p-2">
                <ul className="flex w-full min-w-0 flex-col gap-1">
                  {g.map((it) => (
                    <li key={it.to} className="relative">
                      {renderItem(it)}
                    </li>
                  ))}
                </ul>
              </div>
            </Fragment>
          ))}
        </nav>
        <div className="flex flex-col gap-2 p-2">
          <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                aria-label="Sidebar control"
                className="text-foreground hover:bg-accent data-[state=open]:bg-accent mx-0.5 inline-flex h-[26px] w-min items-center justify-center rounded-md px-1.5 text-xs"
              >
                <PanelLeftDashed className="text-foreground-lighter size-3.5" strokeWidth={1.5} />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent side="top" align="start" className="w-40">
              <DropdownMenuLabel className="text-foreground-lighter text-xs font-normal">Sidebar control</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuRadioGroup value={behavior} onValueChange={(v) => setBehavior(v as SidebarBehavior)}>
                {behaviors.map((b) => (
                  <DropdownMenuRadioItem key={b.value} value={b.value} className="text-xs">
                    {b.label}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </div>
  )
}
