import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowDown, ArrowLeft, ArrowUp, Loader2, Plus, Save, Trash2, X } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Textarea } from '@workspace/ui/components/textarea'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@workspace/ui/components/tabs'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { cn } from '@workspace/ui/lib/utils'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useJobs } from '@/components/job-drawer'
import { LazyEditor } from '@/components/lazy-editor'
import { PM, useAccessLists, useCertificates, useHost, useInstances, usePMMutation, useUpstreams } from '@/hooks/use-proxy-manager'
import { api } from '@/lib/api'
import type { HostKind, IPRule, PathType, ProxyHost, ProxyRoute } from '@/lib/types'
import { AccessLogView } from './instance-detail'
import { UpstreamDialog } from './upstreams'
import { CertStatus, Field, InstancePicker, KVEditor, LinesInput, NumberInput, Panel, ToggleRow } from './shared'

const NONE = '0'

function blankHost(): ProxyHost {
  return {
    id: 0,
    name: '',
    kind: 'proxy',
    domains: [],
    upstream_id: 0,
    routes: [],
    instance_ids: [],
    tls: { mode: 'none', certificate_id: 0, force_https: true, hsts: false, hsts_subdomains: false, http2: true },
    options: { websocket: true, connect_timeout: 0, read_timeout: 0, max_body_mb: 0 },
    security: { access_list_id: 0, ip_rules: [], rate_limit: { enabled: false, rps: 10, burst: 20 } },
    headers: { request: [], response: [], cors: { enabled: false, origins: [], methods: '', headers: '', credentials: false, max_age: 0 } },
    redirect: { url: '', code: 301, preserve_path: true },
    error_page: { code: 404, body: '' },
    raw_nginx: '',
    raw_traefik: '',
    enabled: true,
    notes: '',
  }
}

const blankRoute = (): ProxyRoute => ({
  id: 0,
  path_type: 'prefix',
  path: '/api',
  headers: [],
  upstream_id: 0,
  strip_prefix: false,
  rewrite_regex: '',
  rewrite_replacement: '',
  access_list_id: 0,
  request_headers: [],
  response_headers: [],
})

function UpstreamSelect({ value, onChange, allowNone, noneLabel }: { value: number; onChange: (v: number) => void; allowNone?: boolean; noneLabel?: string }) {
  const { data: upstreams = [] } = useUpstreams()
  const [create, setCreate] = useState(false)
  return (
    <div className="flex gap-2">
      <Select value={String(value || NONE)} onValueChange={(v) => onChange(Number(v))}>
        <SelectTrigger className="w-full">
          <SelectValue placeholder="Choose an upstream" />
        </SelectTrigger>
        <SelectContent>
          {allowNone && <SelectItem value={NONE}>{noneLabel ?? 'None'}</SelectItem>}
          {upstreams.map((u) => (
            <SelectItem key={u.id} value={String(u.id)}>
              {u.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button variant="outline" size="icon" aria-label="New upstream" onClick={() => setCreate(true)}>
        <Plus />
      </Button>
      <UpstreamDialog open={create} onOpenChange={setCreate} onSaved={(u) => onChange(u.id)} />
    </div>
  )
}

function AccessListSelect({ value, onChange, noneLabel = 'None' }: { value: number; onChange: (v: number) => void; noneLabel?: string }) {
  const { data: lists = [] } = useAccessLists()
  return (
    <Select value={String(value || NONE)} onValueChange={(v) => onChange(Number(v))}>
      <SelectTrigger className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={NONE}>{noneLabel}</SelectItem>
        {lists.map((l) => (
          <SelectItem key={l.id} value={String(l.id)}>
            {l.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function RouteCard({
  route,
  index,
  count,
  onChange,
  onMove,
  onRemove,
}: {
  route: ProxyRoute
  index: number
  count: number
  onChange: (r: ProxyRoute) => void
  onMove: (d: -1 | 1) => void
  onRemove: () => void
}) {
  const set = (patch: Partial<ProxyRoute>) => onChange({ ...route, ...patch })
  const [more, setMore] = useState(!!(route.rewrite_regex || route.access_list_id || route.request_headers?.length || route.response_headers?.length || route.headers?.length))
  return (
    <div className="bg-card grid gap-3 rounded-lg border p-4">
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground font-mono text-xs">#{index + 1}</span>
        <Select value={route.path_type} onValueChange={(v) => set({ path_type: v as PathType })}>
          <SelectTrigger size="sm" className="w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="prefix">Prefix</SelectItem>
            <SelectItem value="exact">Exact</SelectItem>
            <SelectItem value="regex">Regex</SelectItem>
          </SelectContent>
        </Select>
        <Input value={route.path} onChange={(e) => set({ path: e.target.value })} placeholder="/api" className="h-8 flex-1 font-mono text-xs" />
        <Button variant="ghost" size="icon-sm" aria-label="Move up" disabled={index === 0} onClick={() => onMove(-1)}>
          <ArrowUp />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label="Move down" disabled={index === count - 1} onClick={() => onMove(1)}>
          <ArrowDown />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label="Remove route" onClick={onRemove}>
          <X />
        </Button>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Upstream">
          <UpstreamSelect value={route.upstream_id} onChange={(v) => set({ upstream_id: v })} allowNone noneLabel="Host default" />
        </Field>
        <div className="grid content-end gap-1">
          <ToggleRow label="Strip prefix" description="Remove the matched prefix before forwarding" checked={route.strip_prefix} onChange={(v) => set({ strip_prefix: v })} disabled={route.path_type !== 'prefix'} />
        </div>
      </div>
      <button type="button" className="text-brand justify-self-start text-xs hover:underline" onClick={() => setMore((m) => !m)}>
        {more ? 'Hide' : 'Show'} header matching, rewrites, auth and headers
      </button>
      {more && (
        <div className="grid gap-4 border-t pt-3">
          <Field label="Match request headers" hint='All must match. Prefix a value with "~" for a regular expression.'>
            <KVEditor value={route.headers} onChange={(v) => set({ headers: v })} addLabel="Add header match" />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Rewrite (regex)" hint="Applied to the path after matching">
              <Input value={route.rewrite_regex} onChange={(e) => set({ rewrite_regex: e.target.value })} placeholder="^/old/(.*)" className="font-mono text-xs" />
            </Field>
            <Field label="Replacement">
              <Input value={route.rewrite_replacement} onChange={(e) => set({ rewrite_replacement: e.target.value })} placeholder="/new/$1" className="font-mono text-xs" />
            </Field>
          </div>
          <Field label="Access list" hint="Overrides the host's access list for this route">
            <AccessListSelect value={route.access_list_id} onChange={(v) => set({ access_list_id: v })} noneLabel="Host default" />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Request headers">
              <KVEditor value={route.request_headers} onChange={(v) => set({ request_headers: v })} />
            </Field>
            <Field label="Response headers">
              <KVEditor value={route.response_headers} onChange={(v) => set({ response_headers: v })} />
            </Field>
          </div>
        </div>
      )}
    </div>
  )
}

function IPRulesEditor({ value, onChange }: { value: IPRule[] | null; onChange: (v: IPRule[]) => void }) {
  const rows = value ?? []
  return (
    <div className="grid gap-2">
      {rows.map((r, i) => (
        <div key={i} className="flex gap-2">
          <Select value={r.action} onValueChange={(v) => onChange(rows.map((x, j) => (i === j ? { ...x, action: v as IPRule['action'] } : x)))}>
            <SelectTrigger className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="allow">Allow</SelectItem>
              <SelectItem value="deny">Deny</SelectItem>
            </SelectContent>
          </Select>
          <Input value={r.cidr} onChange={(e) => onChange(rows.map((x, j) => (i === j ? { ...x, cidr: e.target.value } : x)))} placeholder="192.168.0.0/16" className="font-mono text-xs" />
          <Button variant="ghost" size="icon-sm" aria-label="Remove" onClick={() => onChange(rows.filter((_, j) => j !== i))}>
            <X />
          </Button>
        </div>
      ))}
      <div>
        <Button variant="outline" size="sm" onClick={() => onChange([...rows, { action: 'allow', cidr: '' }])}>
          <Plus /> Add rule
        </Button>
      </div>
      <p className="text-muted-foreground text-xs">Evaluated in order; with any allow rule, everything else is denied. Traefik supports allow rules only.</p>
    </div>
  )
}

export function HostEditorPage() {
  const { id: idParam } = useParams()
  const id = idParam === 'new' ? undefined : Number(idParam)
  const navigate = useNavigate()
  const { openJob } = useJobs()
  const { data: existing, isLoading } = useHost(id)
  const { data: certs = [] } = useCertificates()
  const { data: instances = [] } = useInstances()
  const [h, setH] = useState<ProxyHost>(blankHost)
  const [tab, setTab] = useState('details')
  const [confirmDelete, setConfirmDelete] = useState(false)

  useEffect(() => {
    if (existing) {
      const b = blankHost()
      setH({
        ...b,
        ...existing,
        routes: existing.routes ?? [],
        instance_ids: existing.instance_ids ?? [],
        security: { ...b.security, ...existing.security, ip_rules: existing.security?.ip_rules ?? [] },
        headers: { ...b.headers, ...existing.headers, cors: { ...b.headers.cors, ...existing.headers?.cors } },
      })
    }
  }, [existing])

  const autoAssigned = useRef(false)
  useEffect(() => {
    if (id || autoAssigned.current || instances.length === 0) return
    autoAssigned.current = true
    if (instances.length === 1) setH((cur) => (cur.instance_ids.length ? cur : { ...cur, instance_ids: [instances[0].id] }))
  }, [id, instances])

  const set = (patch: Partial<ProxyHost>) => setH((cur) => ({ ...cur, ...patch }))
  const setTLS = (patch: Partial<ProxyHost['tls']>) => set({ tls: { ...h.tls, ...patch } })
  const setOpt = (patch: Partial<ProxyHost['options']>) => set({ options: { ...h.options, ...patch } })
  const setSec = (patch: Partial<ProxyHost['security']>) => set({ security: { ...h.security, ...patch } })
  const setHdr = (patch: Partial<ProxyHost['headers']>) => set({ headers: { ...h.headers, ...patch } })
  const setCORS = (patch: Partial<ProxyHost['headers']['cors']>) => setHdr({ cors: { ...h.headers.cors, ...patch } })

  const save = usePMMutation(
    () => (id ? api.put<{ host: ProxyHost; job_id?: number }>(`${PM}/hosts/${id}`, h) : api.post<{ host: ProxyHost; job_id?: number }>(`${PM}/hosts`, h)),
    {
      success: 'Host saved. Deploy to apply it.',
      onSuccess: (r) => {
        if (r.job_id) openJob(r.job_id, `Issue certificate for ${r.host.domains[0]}`)
        if (!id) navigate(`/proxy-manager/hosts/${r.host.id}`, { replace: true })
      },
    },
  )
  const remove = usePMMutation(() => api.delete(`${PM}/hosts/${id}`), {
    success: 'Host deleted',
    onSuccess: () => navigate('/proxy-manager/hosts'),
  })

  if (id && isLoading) return <Skeleton className="h-64" />
  if (id && !existing && !isLoading) return <p className="text-muted-foreground text-sm">Host not found.</p>

  const moveRoute = (i: number, d: -1 | 1) => {
    const r = [...h.routes]
    ;[r[i], r[i + d]] = [r[i + d], r[i]]
    set({ routes: r })
  }
  const selectedCert = certs.find((c) => c.id === h.tls.certificate_id)
  const traefikUsed = instances.some((i) => i.kind === 'traefik' && h.instance_ids.includes(i.id))
  const nginxUsed = instances.some((i) => i.kind === 'nginx' && h.instance_ids.includes(i.id))

  return (
    <>
      <Link to="/proxy-manager/hosts" className="text-muted-foreground hover:text-foreground mb-3 inline-flex items-center gap-1 text-xs">
        <ArrowLeft className="size-3" /> Proxy hosts
      </Link>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-medium tracking-tight">{id ? h.name || h.domains[0] : 'New proxy host'}</h1>
        <div className="flex gap-2">
          {id && (
            <Button variant="destructive" onClick={() => setConfirmDelete(true)} disabled={remove.isPending}>
              <Trash2 /> Delete
            </Button>
          )}
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || h.domains.length === 0}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            Save
          </Button>
        </div>
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="flex-wrap">
          <TabsTrigger value="details">Details</TabsTrigger>
          {h.kind === 'proxy' && <TabsTrigger value="routes">Routes{h.routes.length ? ` (${h.routes.length})` : ''}</TabsTrigger>}
          <TabsTrigger value="tls">TLS</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
          {h.kind === 'proxy' && <TabsTrigger value="headers">Headers & CORS</TabsTrigger>}
          <TabsTrigger value="advanced">Advanced</TabsTrigger>
          {id && h.instance_ids.length > 0 && <TabsTrigger value="traffic">Traffic</TabsTrigger>}
        </TabsList>

        <TabsContent value="details" className="mt-4 grid gap-4">
          <Panel title="Host">
            <div className="grid grid-cols-3 gap-2">
              {(
                [
                  ['proxy', 'Reverse proxy', 'Forward requests to an upstream'],
                  ['redirect', 'Redirect', 'Send visitors to another URL'],
                  ['error', 'Static page', 'Answer with a fixed status and body'],
                ] as [HostKind, string, string][]
              ).map(([k, label, desc]) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => set({ kind: k })}
                  className={cn('grid gap-0.5 rounded-lg border p-3 text-left transition-colors', h.kind === k ? 'border-brand bg-brand/5' : 'hover:bg-muted/50')}
                >
                  <span className="text-sm font-medium">{label}</span>
                  <span className="text-muted-foreground text-xs">{desc}</span>
                </button>
              ))}
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Domains" hint="One per line. Wildcards like *.example.com are allowed.">
                <LinesInput value={h.domains} onChange={(v) => set({ domains: v })} placeholder={'example.com\nwww.example.com'} />
              </Field>
              <div className="grid content-start gap-4">
                <Field label="Name">
                  <Input value={h.name} onChange={(e) => set({ name: e.target.value })} placeholder={h.domains[0] ?? 'Shown in lists'} />
                </Field>
                <ToggleRow label="Enabled" checked={h.enabled} onChange={(v) => set({ enabled: v })} />
              </div>
            </div>
            {h.kind === 'proxy' && (
              <Field label="Default upstream" hint="Requests not matched by a route go here.">
                <UpstreamSelect value={h.upstream_id} onChange={(v) => set({ upstream_id: v })} allowNone />
              </Field>
            )}
            {h.kind === 'redirect' && (
              <div className="grid gap-4 sm:grid-cols-[1fr_160px]">
                <Field label="Redirect to">
                  <Input value={h.redirect.url} onChange={(e) => set({ redirect: { ...h.redirect, url: e.target.value } })} placeholder="https://new.example.com" />
                </Field>
                <Field label="Status">
                  <Select value={String(h.redirect.code || 301)} onValueChange={(v) => set({ redirect: { ...h.redirect, code: Number(v) } })}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="301">301 Permanent</SelectItem>
                      <SelectItem value="302">302 Found</SelectItem>
                      <SelectItem value="307">307 Temporary</SelectItem>
                      <SelectItem value="308">308 Permanent</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                <ToggleRow label="Keep the path and query" checked={h.redirect.preserve_path} onChange={(v) => set({ redirect: { ...h.redirect, preserve_path: v } })} />
              </div>
            )}
            {h.kind === 'error' && (
              <div className="grid gap-4">
                <Field label="Status code">
                  <NumberInput value={h.error_page.code} onChange={(v) => set({ error_page: { ...h.error_page, code: v } })} className="w-32" />
                </Field>
                <Field label="Body (HTML)">
                  <Textarea rows={6} value={h.error_page.body} onChange={(e) => set({ error_page: { ...h.error_page, body: e.target.value } })} className="font-mono text-xs" placeholder="<h1>Maintenance</h1>" />
                </Field>
              </div>
            )}
          </Panel>
          <Panel title="Served by" description="The proxy instances that answer for these domains.">
            <InstancePicker value={h.instance_ids} onChange={(v) => set({ instance_ids: v })} />
          </Panel>
          <Panel title="Notes">
            <Textarea rows={2} value={h.notes} onChange={(e) => set({ notes: e.target.value })} />
          </Panel>
        </TabsContent>

        <TabsContent value="routes" className="mt-4 grid gap-3">
          <p className="text-muted-foreground text-sm">
            Routes send parts of the site to other upstreams. They are matched in order (first match wins); unmatched requests use the default upstream.
          </p>
          {h.routes.map((r, i) => (
            <RouteCard
              key={i}
              route={r}
              index={i}
              count={h.routes.length}
              onChange={(nr) => set({ routes: h.routes.map((x, j) => (i === j ? nr : x)) })}
              onMove={(d) => moveRoute(i, d)}
              onRemove={() => set({ routes: h.routes.filter((_, j) => j !== i) })}
            />
          ))}
          <div>
            <Button variant="outline" onClick={() => set({ routes: [...h.routes, blankRoute()] })}>
              <Plus /> Add route
            </Button>
          </div>
        </TabsContent>

        <TabsContent value="tls" className="mt-4 grid gap-4">
          <Panel title="Certificate">
            <div className="grid grid-cols-3 gap-2">
              {(
                [
                  ['none', 'HTTP only', 'No certificate'],
                  ['auto', 'Automatic', "Issue a Let's Encrypt certificate for these domains"],
                  ['certificate', 'Choose', 'Use an existing or uploaded certificate'],
                ] as const
              ).map(([m, label, desc]) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setTLS({ mode: m, certificate_id: m === 'none' ? 0 : h.tls.certificate_id })}
                  className={cn('grid gap-0.5 rounded-lg border p-3 text-left transition-colors', h.tls.mode === m ? 'border-brand bg-brand/5' : 'hover:bg-muted/50')}
                >
                  <span className="text-sm font-medium">{label}</span>
                  <span className="text-muted-foreground text-xs">{desc}</span>
                </button>
              ))}
            </div>
            {h.tls.mode === 'certificate' && (
              <Field label="Certificate">
                <Select value={String(h.tls.certificate_id || '')} onValueChange={(v) => setTLS({ certificate_id: Number(v) })}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Choose a certificate" />
                  </SelectTrigger>
                  <SelectContent>
                    {certs.map((c) => (
                      <SelectItem key={c.id} value={String(c.id)}>
                        {c.name} ({c.domains.join(', ')})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
            {h.tls.mode === 'auto' && (
              <p className="text-muted-foreground text-sm">
                {selectedCert ? (
                  <>
                    Using <span className="text-foreground">{selectedCert.name}</span> - <CertStatus c={selectedCert} />
                  </>
                ) : (
                  'On save, a certificate covering these domains is reused or requested with the default ACME account (HTTP-01, or DNS-01 for wildcards).'
                )}
              </p>
            )}
          </Panel>
          {h.tls.mode !== 'none' && (
            <Panel title="HTTPS options">
              <ToggleRow label="Force HTTPS" description="Redirect plain HTTP requests to HTTPS" checked={h.tls.force_https} onChange={(v) => setTLS({ force_https: v })} />
              <ToggleRow label="HTTP/2" checked={h.tls.http2} onChange={(v) => setTLS({ http2: v })} />
              <ToggleRow label="HSTS" description="Tell browsers to only use HTTPS for a year" checked={h.tls.hsts} onChange={(v) => setTLS({ hsts: v })} />
              {h.tls.hsts && <ToggleRow label="Include subdomains in HSTS" checked={h.tls.hsts_subdomains} onChange={(v) => setTLS({ hsts_subdomains: v })} />}
            </Panel>
          )}
        </TabsContent>

        <TabsContent value="security" className="mt-4 grid gap-4">
          <Panel title="Access list" description="Basic authentication and IP rules shared between hosts.">
            <AccessListSelect value={h.security.access_list_id} onChange={(v) => setSec({ access_list_id: v })} />
          </Panel>
          <Panel title="IP rules" description="Host-specific allow and deny rules.">
            <IPRulesEditor value={h.security.ip_rules} onChange={(v) => setSec({ ip_rules: v })} />
          </Panel>
          <Panel title="Rate limiting" description="Per client IP.">
            <ToggleRow label="Limit requests" checked={h.security.rate_limit.enabled} onChange={(v) => setSec({ rate_limit: { ...h.security.rate_limit, enabled: v } })} />
            {h.security.rate_limit.enabled && (
              <div className="grid grid-cols-2 gap-4">
                <Field label="Requests per second">
                  <NumberInput value={h.security.rate_limit.rps} onChange={(v) => setSec({ rate_limit: { ...h.security.rate_limit, rps: v } })} />
                </Field>
                <Field label="Burst" hint="Extra requests allowed in a spike">
                  <NumberInput value={h.security.rate_limit.burst} onChange={(v) => setSec({ rate_limit: { ...h.security.rate_limit, burst: v } })} />
                </Field>
              </div>
            )}
          </Panel>
        </TabsContent>

        <TabsContent value="headers" className="mt-4 grid gap-4">
          <Panel title="Request headers" description="Added to requests sent to the upstream. Host, X-Real-IP and X-Forwarded-* are set automatically.">
            <KVEditor value={h.headers.request} onChange={(v) => setHdr({ request: v })} />
          </Panel>
          <Panel title="Response headers" description="Added to responses sent to clients.">
            <KVEditor value={h.headers.response} onChange={(v) => setHdr({ response: v })} />
          </Panel>
          <Panel title="CORS">
            <ToggleRow label="Enable CORS" description="Answer preflight requests and add Access-Control-* headers" checked={h.headers.cors.enabled} onChange={(v) => setCORS({ enabled: v })} />
            {h.headers.cors.enabled && (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Allowed origins" hint="One per line, * for any">
                  <LinesInput value={h.headers.cors.origins} onChange={(v) => setCORS({ origins: v })} placeholder="https://app.example.com" />
                </Field>
                <div className="grid content-start gap-4">
                  <Field label="Methods">
                    <Input value={h.headers.cors.methods} onChange={(e) => setCORS({ methods: e.target.value })} placeholder="GET, POST, PUT, DELETE, OPTIONS" />
                  </Field>
                  <Field label="Headers">
                    <Input value={h.headers.cors.headers} onChange={(e) => setCORS({ headers: e.target.value })} placeholder="Authorization, Content-Type" />
                  </Field>
                  <Field label="Max age (s)">
                    <NumberInput value={h.headers.cors.max_age} onChange={(v) => setCORS({ max_age: v })} placeholder="86400" />
                  </Field>
                  <ToggleRow label="Allow credentials" checked={h.headers.cors.credentials} onChange={(v) => setCORS({ credentials: v })} />
                </div>
              </div>
            )}
          </Panel>
        </TabsContent>

        <TabsContent value="advanced" className="mt-4 grid gap-4">
          {h.kind === 'proxy' && (
            <Panel title="Connection">
              <ToggleRow label="WebSockets" description="Forward Upgrade requests (Realtime, dev servers)" checked={h.options.websocket} onChange={(v) => setOpt({ websocket: v })} />
              <div className="grid grid-cols-3 gap-4">
                <Field label="Connect timeout (s)" hint="Empty: proxy default">
                  <NumberInput value={h.options.connect_timeout} onChange={(v) => setOpt({ connect_timeout: v })} />
                </Field>
                <Field label="Read timeout (s)" hint="Raise for long polling and streams">
                  <NumberInput value={h.options.read_timeout} onChange={(v) => setOpt({ read_timeout: v })} />
                </Field>
                <Field label="Max body (MB)" hint="Empty: default, -1: unlimited">
                  <NumberInput value={h.options.max_body_mb} onChange={(v) => setOpt({ max_body_mb: v })} />
                </Field>
              </div>
            </Panel>
          )}
          <Panel title="Custom nginx directives" description="Inserted into this host's server block. Validated with nginx -t on deploy.">
            {!nginxUsed && <p className="text-muted-foreground text-xs">Only used by nginx instances.</p>}
            <LazyEditor value={h.raw_nginx} onChange={(v) => set({ raw_nginx: v })} language="nginx" height={180} />
          </Panel>
          <Panel title="Custom Traefik configuration" description="A JSON object deep-merged into the dynamic configuration, e.g. extra middlewares.">
            {!traefikUsed && <p className="text-muted-foreground text-xs">Only used by Traefik instances.</p>}
            <LazyEditor value={h.raw_traefik} onChange={(v) => set({ raw_traefik: v })} language="json" height={180} />
          </Panel>
        </TabsContent>

        {id && (
          <TabsContent value="traffic" className="mt-4">
            <HostTraffic host={h} />
          </TabsContent>
        )}
      </Tabs>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`Delete ${h.name || h.domains[0]}?`}
        description="The host stops being served after the next deploy. Its certificate is kept."
        confirmText="Delete host"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(undefined)
        }}
      />
    </>
  )
}

function HostTraffic({ host }: { host: ProxyHost }) {
  const { data: instances = [] } = useInstances()
  const mine = instances.filter((i) => host.instance_ids.includes(i.id))
  const [inst, setInst] = useState(mine[0]?.id)
  useEffect(() => {
    if (!inst && mine.length) setInst(mine[0].id)
  }, [inst, mine])
  if (!inst) return null
  return (
    <div className="grid gap-3">
      {mine.length > 1 && (
        <Select value={String(inst)} onValueChange={(v) => setInst(Number(v))}>
          <SelectTrigger className="w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {mine.map((i) => (
              <SelectItem key={i.id} value={String(i.id)}>
                {i.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <AccessLogView key={inst} instanceId={inst} hostId={host.id} />
    </div>
  )
}
