import type { EditorLanguage } from '@/components/code-editor'
import type { Certificate, ProjectStatus, ProxyInstanceView } from '@/lib/types'

export function instanceStatus(v: ProxyInstanceView): ProjectStatus {
  const c = v.container
  if (!c || !c.exists) return 'unknown'
  if (c.restarting) return 'error'
  if (c.running) return 'running'
  if (c.exit_code !== 0) return 'error'
  return 'stopped'
}

export function daysLeft(c: Certificate): number | null {
  if (!c.not_after) return null
  return Math.floor((new Date(c.not_after).getTime() - Date.now()) / 86_400_000)
}

export function fileLanguage(path: string): EditorLanguage {
  if (path.endsWith('.json') || path.endsWith('.yml')) return 'json'
  if (path.endsWith('.conf')) return 'nginx'
  return 'plaintext'
}
