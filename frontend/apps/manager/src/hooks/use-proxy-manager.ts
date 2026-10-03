import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useJobs } from '@/components/job-drawer'
import { api, errorMessage } from '@/lib/api'
import type {
  AccessList,
  AcmeAccount,
  Certificate,
  ConfigRevision,
  DNSProvider,
  ProjectService,
  ProxyHost,
  ProxyImageTags,
  ProxyInstanceView,
  ProxyMeta,
  ProxyPreview,
  ProxyStatus,
  ProxyStream,
  ProxyUpstream,
  TargetHealth,
} from '@/lib/types'

export const PM = '/proxy-manager'

export interface ProxyManagerSettings {
  enabled: boolean
}

export const useProxyManagerSettings = (enabled = true) =>
  useQuery({
    queryKey: ['system', 'proxy-manager'],
    queryFn: () => api.get<ProxyManagerSettings>('/system/proxy-manager'),
    enabled,
    staleTime: 60_000,
  })

const useList = <T>(key: string, path: string, refetchInterval?: number) =>
  useQuery({ queryKey: ['pm', key], queryFn: () => api.get<T[]>(`${PM}${path}`), refetchInterval })

export const useProxyMeta = () =>
  useQuery({ queryKey: ['pm', 'meta'], queryFn: () => api.get<ProxyMeta>(`${PM}/meta`), staleTime: 5 * 60_000 })
export const useProxyImages = (kind: string, enabled = true) =>
  useQuery({
    queryKey: ['pm', 'images', kind],
    queryFn: () => api.get<ProxyImageTags>(`${PM}/images?kind=${kind}`),
    enabled,
    staleTime: 5 * 60_000,
  })
export const useProxyStatus = () =>
  useQuery({ queryKey: ['pm', 'status'], queryFn: () => api.get<ProxyStatus>(`${PM}/status`), refetchInterval: 15_000 })
export const useInstances = () => useList<ProxyInstanceView>('instances', '/instances', 10_000)
export const useHosts = () => useList<ProxyHost>('hosts', '/hosts')
export const useUpstreams = () => useList<ProxyUpstream>('upstreams', '/upstreams')
export const useStreams = () => useList<ProxyStream>('streams', '/streams')
export const useAccessLists = () => useList<AccessList>('access-lists', '/access-lists')
export const useCertificates = () => useList<Certificate>('certificates', '/certificates')
export const useDNSProviders = () => useList<DNSProvider>('dns-providers', '/dns-providers')
export const useAcmeAccounts = () => useList<AcmeAccount>('acme-accounts', '/acme-accounts')
export const useProjectServices = () => useList<ProjectService>('project-services', '/project-services')

export const useHealth = () =>
  useQuery({
    queryKey: ['pm', 'health'],
    queryFn: () => api.get<Record<string, TargetHealth[]>>(`${PM}/health`),
    refetchInterval: 10_000,
  })

export const useHost = (id: number | undefined) =>
  useQuery({ queryKey: ['pm', 'host', id], queryFn: () => api.get<ProxyHost>(`${PM}/hosts/${id}`), enabled: !!id })

export const usePreview = (id: number | undefined, enabled = true) =>
  useQuery({
    queryKey: ['pm', 'preview', id],
    queryFn: () => api.get<ProxyPreview>(`${PM}/instances/${id}/preview`),
    enabled: !!id && enabled,
  })

export const useRevisions = (id: number | undefined) =>
  useQuery({
    queryKey: ['pm', 'revisions', id],
    queryFn: () => api.get<ConfigRevision[]>(`${PM}/instances/${id}/revisions`),
    enabled: !!id,
  })

/** Mutation that refreshes all proxy manager queries and reports errors as toasts. */
export function usePMMutation<TVars, TResult = unknown>(
  fn: (v: TVars) => Promise<TResult>,
  opts: { success?: string | ((r: TResult) => string); onSuccess?: (r: TResult) => void } = {},
) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ['pm'] })
      const msg = typeof opts.success === 'function' ? opts.success(r) : opts.success
      if (msg) toast.success(msg)
      opts.onSuccess?.(r)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
}

/** Starts a server-side job (deploy, rollback, certificate) and opens it in the job panel. */
export function useJobAction<TVars>(path: (v: TVars) => string, title: (v: TVars) => string, body?: (v: TVars) => unknown) {
  const { openJob } = useJobs()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: TVars) => {
      const res = await api.post<{ job_id: number }>(`${PM}${path(v)}`, body?.(v))
      return { res, v }
    },
    onSuccess: ({ res, v }) => {
      qc.invalidateQueries({ queryKey: ['pm'] })
      if (res.job_id) openJob(res.job_id, title(v))
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
}

export function useDeploy() {
  return useJobAction<{ id: number; name: string; note?: string }>(
    (v) => `/instances/${v.id}/deploy`,
    (v) => `Deploy ${v.name}`,
    (v) => ({ note: v.note ?? '' }),
  )
}

export function useDeployAll() {
  return useJobAction<void>(
    () => '/deploy',
    () => 'Deploy all proxies',
  )
}
