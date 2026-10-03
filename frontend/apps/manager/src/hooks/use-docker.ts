import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { DockerInfo } from '@/lib/types'

/** Daemon capabilities: installed network/volume drivers, swarm state and data root. */
export function useDockerInfo() {
  return useQuery({
    queryKey: ['docker-info'],
    queryFn: () => api.get<DockerInfo>('/system/docker-info'),
    staleTime: 60_000,
  })
}
