import { Link, Navigate, Route, Routes } from 'react-router-dom'
import { Network } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { PageContainer, ProductLayout, type ProductNavGroup } from '@/components/layout/layouts'
import { useProxyManagerSettings } from '@/hooks/use-proxy-manager'
import { AccessListsPage } from './access-lists'
import { AcmeAccountsPage } from './acme-accounts'
import { CertificatesPage } from './certificates'
import { DNSProvidersPage } from './dns-providers'
import { HostEditorPage } from './host-editor'
import { HostsPage } from './hosts'
import { InstanceDetailPage, RevisionsPage } from './instance-detail'
import { InstancesPage } from './instances'
import { ProxyOverviewPage } from './overview'
import { DeployBar } from './shared'
import { StreamsPage } from './streams'
import { UpstreamsPage } from './upstreams'

const base = '/proxy-manager'

const groups: ProductNavGroup[] = [
  { items: [{ to: base, label: 'Overview', end: true }] },
  {
    title: 'Traffic',
    items: [
      { to: `${base}/hosts`, label: 'Proxy Hosts' },
      { to: `${base}/upstreams`, label: 'Upstreams' },
      { to: `${base}/streams`, label: 'Streams (TCP/UDP)' },
    ],
  },
  {
    title: 'Security',
    items: [
      { to: `${base}/access-lists`, label: 'Access Lists' },
      { to: `${base}/certificates`, label: 'Certificates' },
      { to: `${base}/dns-providers`, label: 'DNS Providers' },
      { to: `${base}/acme-accounts`, label: 'ACME Accounts' },
    ],
  },
  {
    title: 'Infrastructure',
    items: [
      { to: `${base}/instances`, label: 'Proxy Instances' },
      { to: `${base}/revisions`, label: 'Revisions' },
    ],
  },
]

function DisabledNotice() {
  return (
    <PageContainer>
      <div className="mx-auto mt-16 max-w-md rounded-lg border p-6 text-center">
        <Network className="text-muted-foreground mx-auto mb-3 size-8" />
        <h1 className="text-lg font-medium">Proxy Manager is disabled</h1>
        <p className="text-muted-foreground mt-2 text-sm">
          Turn it on in System settings to run nginx or Traefik proxies for your domains. Configuration you created before is kept.
        </p>
        <Button asChild className="mt-4">
          <Link to="/settings/system">Open System settings</Link>
        </Button>
      </div>
    </PageContainer>
  )
}

/** Routes under /proxy-manager/*, inside the product menu with the pending-changes bar. */
export function ProxyManagerRoutes() {
  const { data: settings, isLoading } = useProxyManagerSettings()
  if (isLoading) return null
  if (!settings?.enabled) return <DisabledNotice />
  return (
    <Routes>
      <Route element={<ProductLayout title="Proxy Manager" groups={groups} banner={<DeployBar />} wide />}>
        <Route index element={<ProxyOverviewPage />} />
        <Route path="hosts" element={<HostsPage />} />
        <Route path="hosts/:id" element={<HostEditorPage />} />
        <Route path="upstreams" element={<UpstreamsPage />} />
        <Route path="streams" element={<StreamsPage />} />
        <Route path="access-lists" element={<AccessListsPage />} />
        <Route path="certificates" element={<CertificatesPage />} />
        <Route path="dns-providers" element={<DNSProvidersPage />} />
        <Route path="acme-accounts" element={<AcmeAccountsPage />} />
        <Route path="instances" element={<InstancesPage />} />
        <Route path="instances/:id" element={<InstanceDetailPage />} />
        <Route path="revisions" element={<RevisionsPage />} />
        <Route path="*" element={<Navigate to={base} replace />} />
      </Route>
    </Routes>
  )
}
