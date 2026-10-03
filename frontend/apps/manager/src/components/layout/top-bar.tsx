import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Check, ChevronsUpDown, ExternalLink, HardDrive, LogOut, Moon, Plus, Settings2, Sun, Users } from 'lucide-react'
import { useTheme } from 'next-themes'
import { Logo } from '@/components/logo'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@workspace/ui/components/button'
import { Popover, PopoverContent, PopoverTrigger } from '@workspace/ui/components/popover'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@workspace/ui/components/command'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@workspace/ui/components/dropdown-menu'
import { Avatar, AvatarFallback } from '@workspace/ui/components/avatar'
import { useAuth } from '@/hooks/use-auth'
import { useProjects } from '@/hooks/use-projects'
import type { Project } from '@/lib/types'
import { cn } from '@workspace/ui/lib/utils'

function ProjectSwitcher({ current }: { current: Project }) {
  const [open, setOpen] = useState(false)
  const { data: projects = [] } = useProjects()
  const navigate = useNavigate()

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-2 px-2 font-normal">
          <span className="max-w-48 truncate font-medium">{current.name}</span>
          <StatusBadge status={current.status} className="hidden sm:inline-flex" />
          <ChevronsUpDown className="text-muted-foreground size-3.5" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0" align="start">
        <Command>
          <CommandInput placeholder="Find project..." />
          <CommandList>
            <CommandEmpty>No projects found.</CommandEmpty>
            <CommandGroup heading="Projects">
              {projects.map((p) => (
                <CommandItem
                  key={p.id}
                  value={`${p.name} ${p.slug}`}
                  onSelect={() => {
                    setOpen(false)
                    navigate(`/projects/${p.slug}`)
                  }}
                >
                  <span
                    className={cn('size-1.5 rounded-full', p.status === 'running' ? 'bg-brand' : 'bg-muted-foreground')}
                  />
                  <span className="flex-1 truncate">{p.name}</span>
                  {p.slug === current.slug && <Check className="size-4" />}
                </CommandItem>
              ))}
            </CommandGroup>
            <CommandSeparator />
            <CommandGroup>
              <CommandItem
                onSelect={() => {
                  setOpen(false)
                  navigate('/projects?new=1')
                }}
              >
                <Plus className="size-4" />
                New project
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

export function TopBar({ project, studioUrl }: { project?: Project; studioUrl?: string }) {
  const { user, isAdmin, logout } = useAuth()
  const { resolvedTheme, setTheme } = useTheme()
  const navigate = useNavigate()
  const initials = (user?.name || user?.email || '?').slice(0, 2).toUpperCase()

  return (
    <header className="bg-sidebar flex h-12 shrink-0 items-center gap-2 border-b px-3">
      <Link to="/projects" className="flex items-center gap-2 pr-1">
        <Logo className="size-5" />
      </Link>
      <span className="text-muted-foreground/50 text-lg">/</span>
      <Button variant="ghost" size="sm" className="px-2 font-normal" asChild>
        <Link to="/projects">Projects</Link>
      </Button>
      {project && (
        <>
          <span className="text-muted-foreground/50 text-lg">/</span>
          <ProjectSwitcher current={project} />
        </>
      )}

      <div className="ml-auto flex items-center gap-1.5">
        {studioUrl && (
          <Button variant="outline" size="sm" asChild>
            <a href={studioUrl} target="_blank" rel="noreferrer">
              Open Studio
              <ExternalLink />
            </a>
          </Button>
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Toggle theme"
          onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}
        >
          {resolvedTheme === 'dark' ? <Sun /> : <Moon />}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <Avatar className="size-7">
                <AvatarFallback className="bg-brand/15 text-brand text-xs font-medium">{initials}</AvatarFallback>
              </Avatar>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel className="grid gap-0.5">
              <span className="truncate text-sm">{user?.name || user?.email}</span>
              <span className="text-muted-foreground truncate text-xs font-normal">
                {user?.email} - {user?.role}
              </span>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            {isAdmin && (
              <DropdownMenuItem onSelect={() => navigate('/settings/users')}>
                <Users />
                Team & audit log
              </DropdownMenuItem>
            )}
            {isAdmin && (
              <DropdownMenuItem onSelect={() => navigate('/settings/system')}>
                <Settings2 />
                System settings
              </DropdownMenuItem>
            )}
            {isAdmin && (
              <DropdownMenuItem onSelect={() => navigate('/settings/storage')}>
                <HardDrive />
                Containers & volumes
              </DropdownMenuItem>
            )}
            <DropdownMenuItem
              onSelect={async () => {
                await logout()
                navigate('/login')
              }}
            >
              <LogOut />
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  )
}
