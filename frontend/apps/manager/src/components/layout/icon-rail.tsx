import { NavLink } from 'react-router-dom'
import {
  Code2,
  Database,
  ExternalLink,
  Home,
  ScrollText,
  Settings,
  Table2,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@workspace/ui/lib/utils'

interface RailItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
}

export function IconRail({ slug, studioUrl }: { slug: string; studioUrl?: string }) {
  const base = `/projects/${slug}`
  const groups: RailItem[][] = [
    [{ to: base, label: 'Project overview', icon: Home, end: true }],
    [
      { to: `${base}/database`, label: 'Database', icon: Database },
      { to: `${base}/auth`, label: 'Authentication', icon: Users },
      { to: `${base}/functions`, label: 'Edge Functions', icon: Code2 },
    ],
    [{ to: `${base}/logs`, label: 'Logs', icon: ScrollText }],
  ]

  const item = (label: string, Icon: LucideIcon, active: boolean) => (
    <span
      className={cn(
        'flex h-9 items-center gap-3 rounded-md px-2.5 text-sm transition-colors',
        active ? 'bg-sidebar-accent text-foreground' : 'text-muted-foreground hover:bg-sidebar-accent hover:text-foreground',
      )}
    >
      <Icon className="size-[18px] shrink-0" strokeWidth={1.6} />
      <span className="truncate opacity-0 transition-opacity group-hover/rail:opacity-100">{label}</span>
    </span>
  )

  return (
    <div className="relative w-14 shrink-0">
      <nav className="group/rail bg-sidebar absolute inset-y-0 left-0 z-30 flex w-14 flex-col gap-1 overflow-hidden border-r px-2 py-2 transition-[width] duration-200 hover:w-52 hover:shadow-2xl">
        {groups.map((g, gi) => (
          <div key={gi} className={cn('flex flex-col gap-1', gi > 0 && 'border-t pt-1')}>
            {g.map((it) => (
              <NavLink key={it.to} to={it.to} end={it.end} title={it.label}>
                {({ isActive }) => item(it.label, it.icon, isActive)}
              </NavLink>
            ))}
          </div>
        ))}
        {studioUrl && (
          <div className="flex flex-col gap-1 border-t pt-1">
            <a href={studioUrl} target="_blank" rel="noreferrer" title="Table & SQL editor (Studio)">
              <span className="text-muted-foreground hover:bg-sidebar-accent hover:text-foreground flex h-9 items-center gap-3 rounded-md px-2.5 text-sm">
                <Table2 className="size-[18px] shrink-0" strokeWidth={1.6} />
                <span className="flex items-center gap-1 truncate opacity-0 transition-opacity group-hover/rail:opacity-100">
                  Table & SQL editor <ExternalLink className="size-3" />
                </span>
              </span>
            </a>
          </div>
        )}
        <div className="mt-auto border-t pt-1">
          <NavLink to={`${base}/settings`} title="Project settings">
            {({ isActive }) => item('Project settings', Settings, isActive)}
          </NavLink>
        </div>
      </nav>
    </div>
  )
}
