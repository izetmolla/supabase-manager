import { useState } from 'react'
import { AlertTriangle, Plus, Trash2, Wand2 } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Switch } from '@workspace/ui/components/switch'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { useDockerInfo } from '@/hooks/use-docker'
import type { DockerInfo, DockerNetwork, NetworkConfig, NetworkDefaults, NetworkMode } from '@/lib/types'

const DOCKER_DEFAULT = 'docker-default'
const CUSTOM_DRIVER = '__custom__'

const OPT_BRIDGE_NAME = 'com.docker.network.bridge.name'
const OPT_MASQUERADE = 'com.docker.network.bridge.enable_ip_masquerade'
const OPT_ENCRYPTED = 'encrypted'
const DEDICATED_OPTIONS = [OPT_BRIDGE_NAME, OPT_MASQUERADE, OPT_ENCRYPTED]

interface DriverChoice {
  value: string
  label: string
  hint: string
  disabled?: string
}

function driverChoices(info: DockerInfo | undefined): DriverChoice[] {
  const builtin: DriverChoice[] = [
    { value: 'bridge', label: 'Bridge', hint: 'Isolated network on this host; ports are published through NAT. Recommended.' },
    {
      value: 'overlay',
      label: 'Overlay',
      hint: 'Spans the hosts of a Docker Swarm. Created as attachable so the Supabase containers can join it.',
      disabled: info && !info.swarm_active ? 'Docker Swarm is not active on this host' : undefined,
    },
    { value: 'macvlan', label: 'Macvlan', hint: '', disabled: 'Publishes no ports, which the CLI and manager need' },
    { value: 'ipvlan', label: 'IPvlan', hint: '', disabled: 'Publishes no ports, which the CLI and manager need' },
  ]
  const known = new Set(['bridge', 'overlay', 'macvlan', 'ipvlan', 'host', 'null', 'none'])
  const plugins = (info?.network_drivers ?? [])
    .filter((d) => !known.has(d))
    .map((d) => ({ value: d, label: d, hint: 'Network driver plugin installed on this host. Options are passed to it as-is.' }))
  return [...builtin, ...plugins]
}

function omit(o: Record<string, string> | null | undefined, keys: string[]) {
  return Object.fromEntries(Object.entries(o ?? {}).filter(([k]) => !keys.includes(k)))
}

function pick(o: Record<string, string> | null | undefined, keys: string[]) {
  return Object.fromEntries(Object.entries(o ?? {}).filter(([k]) => keys.includes(k)))
}

const bindPresets: { value: string; label: string; hint: string }[] = [
  { value: '127.0.0.1', label: 'This machine only (127.0.0.1)', hint: 'Ports are reachable from this host only.' },
  {
    value: '0.0.0.0',
    label: 'All interfaces (0.0.0.0)',
    hint: 'Ports are reachable from your LAN or the internet. Use a firewall to limit access to specific networks.',
  },
  { value: DOCKER_DEFAULT, label: 'Docker default', hint: "Uses the Docker daemon's default bind address (usually 0.0.0.0)." },
]

/** True when the configuration publishes ports beyond the local machine. */
function isExposed(cfg: NetworkConfig) {
  const mode = cfg.mode || 'auto'
  if (mode !== 'managed') return mode === 'auto'
  return cfg.bind_address !== '127.0.0.1'
}

function Field({ label, hint, children, htmlFor }: { label: string; hint?: string; children: React.ReactNode; htmlFor?: string }) {
  return (
    <div className="grid content-start gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint && <span className="text-muted-foreground text-xs">{hint}</span>}
    </div>
  )
}

export function OptionsEditor({
  value,
  onChange,
  disabled,
  keyPlaceholder = 'com.docker.network.bridge.name',
  valuePlaceholder = 'value',
}: {
  value: Record<string, string> | null | undefined
  onChange: (v: Record<string, string>) => void
  disabled?: boolean
  keyPlaceholder?: string
  valuePlaceholder?: string
}) {
  const [rows, setRows] = useState<[string, string][]>(() => Object.entries(value ?? {}))
  const update = (next: [string, string][]) => {
    setRows(next)
    onChange(Object.fromEntries(next.filter(([k]) => k.trim() !== '')))
  }
  return (
    <div className="grid gap-2">
      {rows.map(([k, v], i) => (
        <div key={i} className="grid grid-cols-[1fr_1fr_auto] gap-2">
          <Input
            value={k}
            placeholder={keyPlaceholder}
            className="font-mono text-xs"
            disabled={disabled}
            onChange={(e) => update(rows.map((r, j) => (j === i ? [e.target.value, r[1]] : r)))}
          />
          <Input
            value={v}
            placeholder={valuePlaceholder}
            className="font-mono text-xs"
            disabled={disabled}
            onChange={(e) => update(rows.map((r, j) => (j === i ? [r[0], e.target.value] : r)))}
          />
          <Button variant="ghost" size="icon" aria-label="Remove option" disabled={disabled} onClick={() => update(rows.filter((_, j) => j !== i))}>
            <Trash2 />
          </Button>
        </div>
      ))}
      <div>
        <Button variant="outline" size="xs" disabled={disabled} onClick={() => update([...rows, ['', '']])}>
          <Plus /> Add driver option
        </Button>
      </div>
    </div>
  )
}

export function NetworkForm<T extends NetworkConfig | NetworkDefaults>({
  value,
  onChange,
  variant,
  defaultName,
  networks,
  onSuggestSubnet,
  suggesting,
  disabled,
}: {
  value: T
  onChange: (v: T) => void
  variant: 'project' | 'defaults'
  /** Name the manager uses when the name field is left empty. */
  defaultName?: string
  /** Existing Docker networks for "Existing network" mode. */
  networks?: DockerNetwork[]
  onSuggestSubnet?: () => void
  suggesting?: boolean
  disabled?: boolean
}) {
  const mode: NetworkMode = (value.mode || 'auto') as NetworkMode
  const set = (patch: Partial<NetworkDefaults>) => onChange({ ...value, ...patch })
  const bindSel = value.bind_address || DOCKER_DEFAULT
  const defaults = value as NetworkDefaults
  const { data: info } = useDockerInfo()
  const choices = driverChoices(info)
  const driver = value.driver || 'bridge'
  const [customDriver, setCustomDriver] = useState(() => !choices.some((c) => c.value === driver))
  const driverSel = customDriver ? CUSTOM_DRIVER : driver
  const driverHint = customDriver
    ? 'Any network driver installed on the Docker host, passed to Docker as-is.'
    : choices.find((c) => c.value === driver)?.hint
  const setOpt = (key: string, v: string | undefined) => {
    const next = { ...(value.options ?? {}) }
    if (v === undefined) delete next[key]
    else next[key] = v
    set({ options: next })
  }
  const chooseDriver = (d: string) => {
    if (d === CUSTOM_DRIVER) {
      setCustomDriver(true)
      set({ driver: '', bind_address: '', options: omit(value.options, DEDICATED_OPTIONS) })
      return
    }
    setCustomDriver(false)
    // The bind address and the dedicated options only exist for the chosen driver.
    set({
      driver: d,
      bind_address: d === 'bridge' ? value.bind_address || '127.0.0.1' : '',
      options: omit(value.options, DEDICATED_OPTIONS),
    })
  }

  const modes: { value: NetworkMode; label: string; hint: string }[] = [
    {
      value: 'managed',
      label: 'Dedicated network (managed)',
      hint: 'The manager creates a Docker network for this project with the settings below.',
    },
    {
      value: 'auto',
      label: 'Supabase CLI default',
      hint: 'The CLI creates supabase_network_<project_id>. Ports use the Docker default bind address (usually 0.0.0.0).',
    },
  ]
  if (variant === 'project') {
    modes.push({
      value: 'external',
      label: 'Existing Docker network',
      hint: 'Join a network you created yourself (e.g. macvlan, ipvlan or a network shared with a reverse proxy).',
    })
  }

  return (
    <div className="grid gap-5">
      <Field label="Network mode" hint={modes.find((m) => m.value === mode)?.hint}>
        <Select value={mode} onValueChange={(m) => set({ mode: m as NetworkMode })} disabled={disabled}>
          <SelectTrigger className="w-full max-w-md">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {modes.map((m) => (
              <SelectItem key={m.value} value={m.value}>
                {m.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      {mode === 'external' && (
        <Field label="Docker network" hint="Each project needs its own network: Supabase containers use fixed names (db, kong, ...) on it.">
          <Select value={value.name || ''} onValueChange={(name) => set({ name })} disabled={disabled}>
            <SelectTrigger className="w-full max-w-md">
              <SelectValue placeholder="Choose a network" />
            </SelectTrigger>
            <SelectContent>
              {(networks ?? [])
                .filter((n) => !['host', 'none'].includes(n.name))
                .map((n) => (
                  <SelectItem key={n.id} value={n.name} disabled={!!n.used_by && n.name !== value.name}>
                    <span className="font-mono text-xs">{n.name}</span>
                    <span className="text-muted-foreground text-xs">
                      {n.driver}
                      {n.ipam?.[0]?.Subnet ? ` - ${n.ipam[0].Subnet}` : ''}
                      {n.used_by ? ` - used by ${n.used_by}` : ''}
                    </span>
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </Field>
      )}

      {mode === 'managed' && (
        <>
          <div className="grid gap-5 sm:grid-cols-2">
            <Field label="Network driver" hint={driverHint}>
              <Select value={driverSel} onValueChange={chooseDriver} disabled={disabled}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {choices.map((c) => (
                    <SelectItem key={c.value} value={c.value} disabled={!!c.disabled}>
                      <span>{c.label}</span>
                      <span className="text-muted-foreground font-mono text-xs">{c.value}</span>
                      {c.disabled && <span className="text-muted-foreground text-xs">- {c.disabled}</span>}
                    </SelectItem>
                  ))}
                  <SelectItem value={CUSTOM_DRIVER}>Other driver…</SelectItem>
                </SelectContent>
              </Select>
              {customDriver && (
                <Input
                  aria-label="Driver name"
                  value={value.driver ?? ''}
                  placeholder="e.g. weaveworks/net-plugin:latest_release"
                  onChange={(e) => set({ driver: e.target.value.trim() })}
                  className="font-mono"
                  disabled={disabled}
                />
              )}
            </Field>
            {driver === 'bridge' && !customDriver && (
              <Field label="Bind ports on" hint={bindPresets.find((b) => b.value === bindSel)?.hint}>
                <Select
                  value={bindSel}
                  disabled={disabled}
                  onValueChange={(v) => set({ bind_address: v === DOCKER_DEFAULT ? '' : v })}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {bindPresets.map((b) => (
                      <SelectItem key={b.value} value={b.value}>
                        {b.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
          </div>

          {driver === 'bridge' && !customDriver && (
            <div className="grid gap-5 sm:grid-cols-2">
              <Field label="Host bridge interface" htmlFor="net-bridge" hint="Name of the Linux bridge (max 15 characters). Empty lets Docker name it br-<id>.">
                <Input
                  id="net-bridge"
                  value={value.options?.[OPT_BRIDGE_NAME] ?? ''}
                  placeholder="br-supabase"
                  maxLength={15}
                  onChange={(e) => setOpt(OPT_BRIDGE_NAME, e.target.value.trim() || undefined)}
                  className="font-mono"
                  disabled={disabled}
                />
              </Field>
              <Field label="Outbound traffic" hint="Turn off only for air-gapped setups: containers then cannot reach the internet.">
                <label className="flex h-9 items-center gap-3">
                  <Switch
                    checked={value.options?.[OPT_MASQUERADE] !== 'false'}
                    onCheckedChange={(v) => setOpt(OPT_MASQUERADE, v ? undefined : 'false')}
                    disabled={disabled}
                  />
                  <span className="text-sm">IP masquerading (NAT to the internet)</span>
                </label>
              </Field>
            </div>
          )}

          {driver === 'overlay' && !customDriver && (
            <Field label="Encryption">
              <label className="flex h-9 items-center gap-3">
                <Switch
                  checked={value.options ? OPT_ENCRYPTED in value.options : false}
                  onCheckedChange={(v) => setOpt(OPT_ENCRYPTED, v ? '' : undefined)}
                  disabled={disabled}
                />
                <span className="text-sm">Encrypt traffic between swarm nodes (IPsec)</span>
              </label>
            </Field>
          )}

          {variant === 'project' ? (
            <>
              <Field label="Network name" htmlFor="net-name">
                <Input
                  id="net-name"
                  value={value.name ?? ''}
                  placeholder={defaultName}
                  onChange={(e) => set({ name: e.target.value.trim() })}
                  className="max-w-md font-mono"
                  disabled={disabled}
                />
              </Field>
              <div className="grid gap-5 sm:grid-cols-3">
                <Field label="Subnet (IPv4)" htmlFor="net-subnet" hint="Empty lets Docker pick one.">
                  <div className="flex gap-2">
                    <Input
                      id="net-subnet"
                      value={value.subnet ?? ''}
                      placeholder="10.210.0.0/24"
                      onChange={(e) => set({ subnet: e.target.value.trim() })}
                      className="font-mono"
                      disabled={disabled}
                    />
                    {onSuggestSubnet && (
                      <Button variant="outline" size="icon" aria-label="Pick a free subnet" title="Pick a free subnet" onClick={onSuggestSubnet} disabled={disabled || suggesting}>
                        <Wand2 />
                      </Button>
                    )}
                  </div>
                </Field>
                <Field label="Gateway" htmlFor="net-gw">
                  <Input
                    id="net-gw"
                    value={value.gateway ?? ''}
                    placeholder="10.210.0.1"
                    onChange={(e) => set({ gateway: e.target.value.trim() })}
                    className="font-mono"
                    disabled={disabled || !value.subnet}
                  />
                </Field>
                <Field label="Container IP range" htmlFor="net-range" hint="Optional part of the subnet for containers.">
                  <Input
                    id="net-range"
                    value={value.ip_range ?? ''}
                    placeholder="10.210.0.128/25"
                    onChange={(e) => set({ ip_range: e.target.value.trim() })}
                    className="font-mono"
                    disabled={disabled || !value.subnet}
                  />
                </Field>
              </div>
            </>
          ) : (
            <div className="grid gap-5 sm:grid-cols-2">
              <Field label="Subnet pool" htmlFor="net-pool" hint="Each new project gets the next free subnet of this range. Empty lets Docker pick.">
                <Input
                  id="net-pool"
                  value={defaults.subnet_pool ?? ''}
                  placeholder="10.210.0.0/16"
                  onChange={(e) => set({ subnet_pool: e.target.value.trim() })}
                  className="font-mono"
                  disabled={disabled}
                />
              </Field>
              <Field label="Subnet size per project" htmlFor="net-prefix">
                <Select value={String(defaults.subnet_prefix || 24)} onValueChange={(v) => set({ subnet_prefix: Number(v) })} disabled={disabled || !defaults.subnet_pool}>
                  <SelectTrigger id="net-prefix" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {[20, 22, 24, 26, 27, 28].map((b) => (
                      <SelectItem key={b} value={String(b)}>
                        /{b} ({2 ** (32 - b) - 3} containers)
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          )}

          <div className="grid gap-5 sm:grid-cols-2">
            <Field label="MTU" htmlFor="net-mtu" hint="Empty uses the Docker default (1500). Lower it for VPNs or overlay links.">
              <Input
                id="net-mtu"
                type="number"
                min={1280}
                max={9216}
                value={value.mtu || ''}
                placeholder="1500"
                onChange={(e) => set({ mtu: Number(e.target.value) || 0 })}
                className="font-mono"
                disabled={disabled}
              />
            </Field>
            <Field label="IPv6">
              <label className="flex h-9 items-center gap-3">
                <Switch checked={!!value.enable_ipv6} onCheckedChange={(v) => set({ enable_ipv6: v, subnet_v6: v ? value.subnet_v6 : '' })} disabled={disabled} />
                <span className="text-sm">Enable IPv6 on the network</span>
              </label>
              {value.enable_ipv6 && variant === 'project' && (
                <Input
                  value={value.subnet_v6 ?? ''}
                  placeholder="fd00:10::/64 (optional)"
                  onChange={(e) => set({ subnet_v6: e.target.value.trim() })}
                  className="font-mono"
                  disabled={disabled}
                />
              )}
            </Field>
          </div>

          <Field label="Advanced driver options" hint="Extra -o options for docker network create, e.g. com.docker.network.bridge.enable_icc=false.">
            <OptionsEditor
              key={driverSel}
              value={omit(value.options, DEDICATED_OPTIONS)}
              onChange={(rows) => set({ options: { ...pick(value.options, DEDICATED_OPTIONS), ...rows } })}
              disabled={disabled}
            />
          </Field>
        </>
      )}

      {isExposed(value) && mode !== 'external' && (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertDescription>
            Ports are published on all interfaces. Local Supabase uses well-known credentials (Postgres password <code>postgres</code>, demo
            JWT secret and API keys), so anyone who can reach this host can read and change the database. Use a firewall or bind to
            127.0.0.1 unless you need remote access.
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
