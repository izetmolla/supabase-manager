import { Link } from 'react-router-dom'
import { AlertTriangle, ArrowRight, Globe, HeartPulse, Network, Plus, Server, ShieldCheck, Waypoints } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { PageHeader } from '@/components/page-header'
import { useAcmeAccounts, useInstances, useProxyStatus } from '@/hooks/use-proxy-manager'
import { timeAgo } from '@/lib/format'
import { CertStatus, InstanceBadge, KindBadge } from './shared'

function Stat({ icon, label, value, to }: { icon: React.ReactNode; label: string; value: React.ReactNode; to: string }) {
  return (
    <Link to={to} className="bg-card hover:bg-muted/40 group flex items-center gap-4 rounded-lg border p-4 transition-colors">
      <div className="bg-muted text-muted-foreground flex size-9 items-center justify-center rounded-md [&_svg]:size-4">{icon}</div>
      <div className="grid">
        <span className="text-muted-foreground text-xs">{label}</span>
        <span className="text-xl font-medium tabular-nums">{value}</span>
      </div>
      <ArrowRight className="text-muted-foreground ml-auto size-4 opacity-0 transition-opacity group-hover:opacity-100" />
    </Link>
  )
}

export function ProxyOverviewPage() {
  const { data: st, isLoading } = useProxyStatus()
  const { data: instances = [] } = useInstances()
  const { data: accounts = [] } = useAcmeAccounts()

  const steps = [
    { done: instances.length > 0, label: 'Create a proxy instance (nginx or Traefik) with its bind address and ports', to: '/proxy-manager/instances' },
    { done: (st?.upstreams ?? 0) > 0, label: 'Add an upstream: static addresses or a Supabase project service', to: '/proxy-manager/upstreams' },
    { done: accounts.length > 0, label: "Add an ACME account to issue Let's Encrypt certificates", to: '/proxy-manager/acme-accounts' },
    { done: (st?.hosts ?? 0) > 0, label: 'Map domains to upstreams with a proxy host', to: '/proxy-manager/hosts' },
  ]
  const setupDone = steps.every((s) => s.done)

  return (
    <>
      <PageHeader
        title="Proxy Manager"
        description="Reverse proxies and load balancers in front of your projects. Edits are drafts until you deploy them."
        actions={
          <Button asChild>
            <Link to="/proxy-manager/hosts/new">
              <Plus /> Add proxy host
            </Link>
          </Button>
        }
      />
      {isLoading || !st ? (
        <Skeleton className="h-24" />
      ) : (
        <div className="grid gap-6">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Stat icon={<Server />} label="Proxy instances" value={`${st.running}/${st.instances} running`} to="/proxy-manager/instances" />
            <Stat icon={<Globe />} label="Proxy hosts" value={st.hosts} to="/proxy-manager/hosts" />
            <Stat icon={<Network />} label="Upstreams" value={st.upstreams} to="/proxy-manager/upstreams" />
            <Stat icon={<ShieldCheck />} label="Certificates" value={st.certificates} to="/proxy-manager/certificates" />
          </div>

          {!setupDone && (
            <section className="bg-card rounded-lg border">
              <div className="border-b px-5 py-3.5">
                <h2 className="text-sm font-medium">Get started</h2>
              </div>
              <ol className="divide-y">
                {steps.map((s, i) => (
                  <li key={i} className="flex items-center gap-3 px-5 py-3 text-sm">
                    <span
                      className={
                        s.done
                          ? 'bg-brand text-background flex size-5 items-center justify-center rounded-full text-xs'
                          : 'text-muted-foreground flex size-5 items-center justify-center rounded-full border text-xs'
                      }
                    >
                      {s.done ? '✓' : i + 1}
                    </span>
                    <span className={s.done ? 'text-muted-foreground line-through' : ''}>{s.label}</span>
                    {!s.done && (
                      <Button variant="link" size="sm" className="ml-auto" asChild>
                        <Link to={s.to}>Open</Link>
                      </Button>
                    )}
                  </li>
                ))}
              </ol>
            </section>
          )}

          <div className="grid gap-6 lg:grid-cols-2">
            <section className="bg-card rounded-lg border">
              <div className="flex items-center justify-between border-b px-5 py-3.5">
                <h2 className="text-sm font-medium">Instances</h2>
                <Button variant="ghost" size="sm" asChild>
                  <Link to="/proxy-manager/instances">Manage</Link>
                </Button>
              </div>
              {instances.length === 0 ? (
                <p className="text-muted-foreground px-5 py-6 text-sm">No proxy instances yet.</p>
              ) : (
                <ul className="divide-y">
                  {instances.map((i) => (
                    <li key={i.id}>
                      <Link to={`/proxy-manager/instances/${i.id}`} className="hover:bg-muted/40 flex items-center gap-3 px-5 py-3">
                        <KindBadge kind={i.kind} />
                        <span className="text-sm font-medium">{i.name}</span>
                        <span className="text-muted-foreground font-mono text-xs">
                          {i.bind_ip || '0.0.0.0'}:{[i.http_port, i.https_port].filter(Boolean).join(', ')}
                        </span>
                        <span className="ml-auto flex items-center gap-2">
                          {i.pending && <span className="text-xs text-amber-500">changes pending</span>}
                          <InstanceBadge v={i} />
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </section>

            <section className="bg-card rounded-lg border">
              <div className="flex items-center justify-between border-b px-5 py-3.5">
                <h2 className="text-sm font-medium">Alerts</h2>
              </div>
              {st.expiring.length === 0 && st.unhealthy_targets.length === 0 ? (
                <p className="text-muted-foreground flex items-center gap-2 px-5 py-6 text-sm">
                  <HeartPulse className="text-brand size-4" /> All certificates are valid and all checked targets are healthy.
                </p>
              ) : (
                <ul className="divide-y">
                  {st.expiring.map((c) => (
                    <li key={`c${c.id}`} className="flex items-center gap-3 px-5 py-3 text-sm">
                      <ShieldCheck className="size-4 text-amber-500" />
                      <Link to="/proxy-manager/certificates" className="truncate hover:underline">
                        {c.name}
                      </Link>
                      <span className="ml-auto">
                        <CertStatus c={c} />
                      </span>
                    </li>
                  ))}
                  {st.unhealthy_targets.map((t, i) => (
                    <li key={`t${i}`} className="flex items-center gap-3 px-5 py-3 text-sm">
                      <AlertTriangle className="text-destructive size-4" />
                      <span>
                        <span className="font-mono text-xs">{t.address}</span> in{' '}
                        <Link to="/proxy-manager/upstreams" className="hover:underline">
                          {t.upstream}
                        </Link>{' '}
                        is down
                      </span>
                      <span className="text-muted-foreground ml-auto max-w-48 truncate text-xs" title={t.error}>
                        {t.error}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>

          {instances.some((i) => i.deployed_at) && (
            <p className="text-muted-foreground flex items-center gap-2 text-xs">
              <Waypoints className="size-3.5" />
              Last deploy {timeAgo(instances.map((i) => i.deployed_at).filter(Boolean).sort().at(-1))}
            </p>
          )}
        </div>
      )}
    </>
  )
}
