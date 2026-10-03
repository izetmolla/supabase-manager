import { Boxes, FolderOpen, HardDrive, Network, Puzzle } from 'lucide-react'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { OptionsEditor } from '@/components/network-form'
import { useDockerInfo } from '@/hooks/use-docker'
import { cn } from '@workspace/ui/lib/utils'
import type { StorageConfig, StorageMode } from '@/lib/types'

export const STORAGE_MODES: { value: StorageMode; label: string; hint: string; icon: typeof HardDrive }[] = [
  { value: 'docker', label: 'Docker volumes', hint: "Named volumes in Docker's data root. Simplest; data lives inside Docker.", icon: Boxes },
  { value: 'host', label: 'Host folder', hint: 'Bind the data to a folder on this host that you can browse and back up.', icon: FolderOpen },
  { value: 'nfs', label: 'NFS share', hint: 'Keep the data on a NAS or file server, mounted by Docker over NFS.', icon: Network },
  { value: 'driver', label: 'Volume driver', hint: 'Any Docker volume driver (CIFS via local, or a plugin) with your own options.', icon: Puzzle },
]

export function storageModeLabel(mode: string | undefined) {
  return STORAGE_MODES.find((m) => m.value === (mode || 'docker'))?.label ?? mode ?? ''
}

const VOLUMES = [
  { key: 'db', label: 'Database' },
  { key: 'storage', label: 'Storage files' },
]

function join(...parts: string[]) {
  return parts.join('/').replace(/\/+/g, '/')
}

/** Where each volume will live; mirrors targetFor in the backend. */
export function previewLocations(cfg: StorageConfig, ctx: { slug: string; projectId?: string; projectPath?: string; dockerRoot?: string }) {
  const mode = cfg.mode || 'docker'
  return VOLUMES.map(({ key, label }) => {
    let location: string
    switch (mode) {
      case 'host': {
        const root = cfg.path ? join(cfg.path, ctx.slug) : join(ctx.projectPath ?? '<project folder>', 'volumes')
        location = join(root, key)
        break
      }
      case 'nfs':
        location = `${cfg.server || '<server>'}:${join(cfg.export || '/<export>', ctx.slug, key)}`
        break
      case 'driver': {
        const dev = cfg.driver_options?.device?.replaceAll('{project}', ctx.slug).replaceAll('{volume}', key)
        location = `${cfg.driver || '<driver>'} driver${dev ? ` (${dev})` : ''}`
        break
      }
      default: {
        const name = `supabase_${key}_${ctx.projectId ?? '<project_id>'}`
        location = ctx.dockerRoot ? join(ctx.dockerRoot, 'volumes', name, '_data') : `Docker volume ${name}`
      }
    }
    return { key, label, location }
  })
}

function Field({ label, hint, children, htmlFor }: { label: string; hint?: React.ReactNode; children: React.ReactNode; htmlFor?: string }) {
  return (
    <div className="grid content-start gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint && <span className="text-muted-foreground text-xs">{hint}</span>}
    </div>
  )
}

export function StorageForm({
  value,
  onChange,
  variant,
  slug,
  projectId,
  projectPath,
  disabled,
}: {
  value: StorageConfig
  onChange: (v: StorageConfig) => void
  variant: 'project' | 'defaults'
  slug?: string
  projectId?: string
  projectPath?: string
  disabled?: boolean
}) {
  const { data: info } = useDockerInfo()
  const mode = (value.mode || 'docker') as StorageMode
  const set = (patch: Partial<StorageConfig>) => onChange({ ...value, ...patch })
  const volumeDrivers = info?.volume_drivers?.length ? info.volume_drivers : ['local']
  const preview = previewLocations(value, {
    slug: slug ?? '<project>',
    projectId,
    projectPath,
    dockerRoot: info?.docker_root_dir,
  })

  return (
    <div className="grid gap-5">
      <div role="radiogroup" aria-label="Storage location" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {STORAGE_MODES.map((m) => {
          const active = m.value === mode
          return (
            <button
              key={m.value}
              type="button"
              role="radio"
              aria-checked={active}
              disabled={disabled}
              onClick={() => set({ mode: m.value })}
              className={cn(
                'grid content-start gap-1.5 rounded-lg border p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60',
                active ? 'border-primary bg-primary/5 ring-primary/30 ring-2' : 'hover:bg-muted/50',
              )}
            >
              <span className="flex items-center gap-2 text-sm font-medium">
                <m.icon className="size-4" />
                {m.label}
              </span>
              <span className="text-muted-foreground text-xs">{m.hint}</span>
            </button>
          )
        })}
      </div>

      {mode === 'host' && (
        <Field
          label="Base folder on the host"
          htmlFor="st-path"
          hint={
            <>
              Each project gets <span className="font-mono">&lt;folder&gt;/{slug ?? '<project>'}/db</span> and{' '}
              <span className="font-mono">…/storage</span>. Empty keeps the data in the project folder ({variant === 'project' ? 'volumes/' : '<project>/volumes'}).
              The folder must be visible to the Docker daemon at this path.
            </>
          }
        >
          <Input
            id="st-path"
            value={value.path ?? ''}
            placeholder="/srv/supabase-data"
            onChange={(e) => set({ path: e.target.value.trim() })}
            className="max-w-md font-mono"
            disabled={disabled}
          />
        </Field>
      )}

      {mode === 'nfs' && (
        <div className="grid gap-5 sm:grid-cols-3">
          <Field label="NFS server" htmlFor="st-server" hint="Host name or IP address of the NAS.">
            <Input
              id="st-server"
              value={value.server ?? ''}
              placeholder="192.168.1.20"
              onChange={(e) => set({ server: e.target.value.trim() })}
              className="font-mono"
              disabled={disabled}
            />
          </Field>
          <Field label="Export path" htmlFor="st-export" hint="Sub-folders per project and volume are created automatically.">
            <Input
              id="st-export"
              value={value.export ?? ''}
              placeholder="/volume1/supabase"
              onChange={(e) => set({ export: e.target.value.trim() })}
              className="font-mono"
              disabled={disabled}
            />
          </Field>
          <Field label="Mount options" htmlFor="st-opts" hint="Default rw,nfsvers=4. The server address is added for you.">
            <Input
              id="st-opts"
              value={value.mount_options ?? ''}
              placeholder="rw,nfsvers=4.1,hard"
              onChange={(e) => set({ mount_options: e.target.value.trim() })}
              className="font-mono"
              disabled={disabled}
            />
          </Field>
        </div>
      )}

      {mode === 'driver' && (
        <>
          <Field label="Volume driver" hint="Drivers installed on the Docker host. Plugins must be installed with docker plugin install first.">
            <Select value={value.driver || ''} onValueChange={(driver) => set({ driver })} disabled={disabled}>
              <SelectTrigger className="w-full max-w-md">
                <SelectValue placeholder="Choose a driver" />
              </SelectTrigger>
              <SelectContent>
                {volumeDrivers.map((d) => (
                  <SelectItem key={d} value={d}>
                    <span className="font-mono text-xs">{d}</span>
                    {d === 'local' && <span className="text-muted-foreground text-xs">built-in (cifs, nfs, bind, tmpfs via options)</span>}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field
            label="Driver options"
            hint={
              <>
                Use <span className="font-mono">{'{project}'}</span> and <span className="font-mono">{'{volume}'}</span> (db or storage) so every
                volume gets its own location, e.g. CIFS: <span className="font-mono">type=cifs</span>,{' '}
                <span className="font-mono">o=addr=nas,username=u,password=p</span>,{' '}
                <span className="font-mono">device=//nas/share/{'{project}'}/{'{volume}'}</span>. The target folders must already exist.
              </>
            }
          >
            <OptionsEditor
              value={value.driver_options}
              onChange={(driver_options) => set({ driver_options })}
              disabled={disabled}
              keyPlaceholder="device"
              valuePlaceholder="//nas/share/{project}/{volume}"
            />
          </Field>
        </>
      )}

      <div className="bg-muted/40 rounded-md border px-4 py-3">
        <p className="text-muted-foreground mb-2 text-xs font-medium tracking-wide uppercase">
          {variant === 'defaults' ? 'New projects will store data in' : 'Data location'}
        </p>
        <dl className="grid gap-1.5">
          {preview.map((p) => (
            <div key={p.key} className="grid gap-1 sm:grid-cols-[8rem_1fr]">
              <dt className="flex items-center gap-1.5 text-sm">
                <HardDrive className="text-muted-foreground size-3.5" />
                {p.label}
              </dt>
              <dd className="font-mono text-xs break-all sm:pt-0.5">{p.location}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  )
}
