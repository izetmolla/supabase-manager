export function formatBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return '-'
  const diff = (Date.now() - new Date(iso).getTime()) / 1000
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return '-'
  return new Date(iso).toLocaleString()
}

/** A project service served by the manager's reverse proxy on its own port. */
export function serviceUrl(slug: string, service: 'studio' | 'mail' | 'api'): string {
  return `/proxy/${slug}/${service}/`
}

export function migrationDate(version: string): string {
  const m = version.match(/^(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})/)
  if (!m) return version
  return `${m[1]}-${m[2]}-${m[3]} ${m[4]}:${m[5]}:${m[6]}`
}

export const providerLabels: Record<string, string> = {
  apple: 'Apple',
  azure: 'Azure',
  bitbucket: 'Bitbucket',
  discord: 'Discord',
  facebook: 'Facebook',
  github: 'GitHub',
  gitlab: 'GitLab',
  google: 'Google',
  keycloak: 'Keycloak',
  linkedin_oidc: 'LinkedIn (OIDC)',
  notion: 'Notion',
  slack: 'Slack',
  spotify: 'Spotify',
  twitch: 'Twitch',
  twitter: 'Twitter (legacy)',
  workos: 'WorkOS',
  x: 'X / Twitter (OAuth 2.0)',
  zoom: 'Zoom',
}
