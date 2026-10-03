import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ArrowRight, Globe, Lock, LockOpen, Plus, Search, Trash2 } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Switch } from '@workspace/ui/components/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@workspace/ui/components/table'
import { cn } from '@workspace/ui/lib/utils'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState, PageHeader } from '@/components/page-header'
import { PM, useCertificates, useHosts, useInstances, usePMMutation, useUpstreams } from '@/hooks/use-proxy-manager'
import { api } from '@/lib/api'
import type { ProxyHost } from '@/lib/types'

export function HostsPage() {
  const navigate = useNavigate()
  const { data: hosts, isLoading } = useHosts()
  const { data: upstreams = [] } = useUpstreams()
  const { data: instances = [] } = useInstances()
  const { data: certs = [] } = useCertificates()
  const [q, setQ] = useState('')
  const [del, setDel] = useState<ProxyHost | null>(null)
  const toggle = usePMMutation((h: ProxyHost) => api.put(`${PM}/hosts/${h.id}`, { ...h, enabled: !h.enabled }))
  const remove = usePMMutation((id: number) => api.delete(`${PM}/hosts/${id}`), { success: 'Host deleted' })
  const upName = new Map(upstreams.map((u) => [u.id, u.name]))
  const instName = new Map(instances.map((i) => [i.id, i.name]))
  const certById = new Map(certs.map((c) => [c.id, c]))
  const visible = (hosts ?? []).filter((h) => !q || `${h.name} ${h.domains.join(' ')}`.toLowerCase().includes(q.toLowerCase()))

  const target = (h: ProxyHost) => {
    if (h.kind === 'redirect') return <span className="text-muted-foreground">redirect to {h.redirect.url}</span>
    if (h.kind === 'error') return <span className="text-muted-foreground">static {h.error_page.code || 404} page</span>
    const routes = h.routes?.length ?? 0
    return (
      <>
        {upName.get(h.upstream_id) ?? <span className="text-muted-foreground">no default upstream</span>}
        {routes > 0 && <span className="text-muted-foreground"> + {routes} routes</span>}
      </>
    )
  }

  return (
    <>
      <PageHeader
        title="Proxy Hosts"
        description="Domains, the upstreams they forward to, and their rules."
        actions={
          <Button asChild>
            <Link to="/proxy-manager/hosts/new">
              <Plus /> Add proxy host
            </Link>
          </Button>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !hosts?.length ? (
        <EmptyState
          icon={<Globe />}
          title="No proxy hosts"
          description="A proxy host maps domains to an upstream, with TLS, routes, authentication and more."
          action={
            <Button asChild>
              <Link to="/proxy-manager/hosts/new">
                <Plus /> Add proxy host
              </Link>
            </Button>
          }
        />
      ) : (
        <div className="grid gap-3">
          <div className="relative w-72">
            <Search className="text-muted-foreground absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search domains" className="h-8 pl-8" />
          </div>
          <div className="bg-card overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domains</TableHead>
                  <TableHead>Forwards to</TableHead>
                  <TableHead>TLS</TableHead>
                  <TableHead>Instances</TableHead>
                  <TableHead>Enabled</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.map((h) => {
                  const cert = certById.get(h.tls?.certificate_id)
                  return (
                    <TableRow key={h.id} className={cn('cursor-pointer', !h.enabled && 'opacity-60')} onClick={() => navigate(`/proxy-manager/hosts/${h.id}`)}>
                      <TableCell>
                        <div className="font-medium">{h.domains[0]}</div>
                        {h.domains.length > 1 && <div className="text-muted-foreground text-xs">+{h.domains.length - 1} more</div>}
                      </TableCell>
                      <TableCell className="text-xs">
                        <span className="flex items-center gap-1.5">
                          <ArrowRight className="text-muted-foreground size-3" />
                          {target(h)}
                        </span>
                      </TableCell>
                      <TableCell className="text-xs">
                        {h.tls?.mode === 'none' || !h.tls?.mode ? (
                          <span className="text-muted-foreground flex items-center gap-1">
                            <LockOpen className="size-3.5" /> HTTP
                          </span>
                        ) : (
                          <span className={cn('flex items-center gap-1', cert?.status === 'valid' ? 'text-brand' : 'text-amber-500')}>
                            <Lock className="size-3.5" /> {cert?.status === 'valid' ? 'HTTPS' : (cert?.status ?? 'missing')}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs">
                        {(h.instance_ids ?? []).map((i) => instName.get(i) ?? `#${i}`).join(', ') || <span className="text-amber-500">none</span>}
                      </TableCell>
                      <TableCell onClick={(e) => e.stopPropagation()}>
                        <Switch checked={h.enabled} onCheckedChange={() => toggle.mutate(h)} />
                      </TableCell>
                      <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                        <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(h)}>
                          <Trash2 />
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        </div>
      )}
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="The host stops being served after the next deploy. Its certificate is kept."
        confirmText="Delete host"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
