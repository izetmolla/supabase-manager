import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'
import { toast } from 'sonner'
import { api, errorMessage } from '@/lib/api'
import type { Job, Project, ProjectDomains, ProjectStatusResponse, Settings } from '@/lib/types'
import { useJobs } from '@/components/job-drawer'

const transitional = (s?: string) => s === 'starting' || s === 'stopping'

export function useProjects() {
  return useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<Project[]>('/projects'),
    refetchInterval: (q) => (q.state.data?.some((p) => transitional(p.status) || p.running_job) ? 3000 : 15000),
  })
}

export function useSlug() {
  return useParams<{ slug: string }>().slug!
}

export function useProject(slug = useSlug()) {
  return useQuery({
    queryKey: ['project', slug],
    queryFn: () => api.get<Project>(`/projects/${slug}`),
    refetchInterval: (q) => (transitional(q.state.data?.status) || q.state.data?.running_job ? 3000 : 15000),
  })
}

export function useProjectStatus(slug = useSlug()) {
  return useQuery({
    queryKey: ['project', slug, 'status'],
    queryFn: () => api.get<ProjectStatusResponse>(`/projects/${slug}/status`),
    refetchInterval: (q) => (transitional(q.state.data?.status) || q.state.data?.running_job ? 3000 : 20000),
  })
}

export function useSettings(slug = useSlug()) {
  return useQuery({
    queryKey: ['project', slug, 'config'],
    queryFn: () => api.get<Settings>(`/projects/${slug}/config`),
  })
}

/** Proxy host domains for the project; `enabled` is false when the Proxy Manager is off. */
export function useProjectDomains(slug = useSlug()) {
  return useQuery({
    queryKey: ['project', slug, 'domains'],
    queryFn: () => api.get<ProjectDomains>(`/projects/${slug}/domains`),
    staleTime: 30_000,
  })
}

/** The proxy host URLs picked for a project's services; missing ones use the local address. */
export function useProjectPublicUrls(slug = useSlug()) {
  const { data } = useProjectDomains(slug)
  return (data?.enabled && data.selected) || {}
}

const actionLabels = { start: 'Start project', stop: 'Stop project', restart: 'Restart project' }

/** Runs start/stop/restart and opens the job drawer. */
export function useLifecycle() {
  const qc = useQueryClient()
  const { openJob } = useJobs()
  return useMutation({
    mutationFn: ({ slug, action }: { slug: string; action: 'start' | 'stop' | 'restart' }) =>
      api.post<Job>(`/projects/${slug}/${action}`),
    onSuccess: (job, { action, slug }) => {
      openJob(job.id, `${actionLabels[action]} - ${slug}`)
      qc.invalidateQueries({ queryKey: ['projects'] })
      qc.invalidateQueries({ queryKey: ['project', slug] })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
}

/** Starts any job-returning endpoint and opens the job drawer. */
export function useJobAction(title: string) {
  const qc = useQueryClient()
  const { openJob } = useJobs()
  return useMutation({
    mutationFn: ({ path, body }: { path: string; body?: unknown }) => api.post<Job>(path, body),
    onSuccess: (job) => {
      openJob(job.id, title)
      qc.invalidateQueries({ queryKey: ['project'] })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
}
