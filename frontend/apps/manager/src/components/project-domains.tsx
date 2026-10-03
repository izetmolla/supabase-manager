import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { Section } from '@/components/page-header'
import { useAuth } from '@/hooks/use-auth'
import { useProjectDomains, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { ProjectDomain, ProjectDomainService } from '@/lib/types'

const LOCAL = 'local'

const rows: { service: ProjectDomainService; label: string; hint: string }[] = [
  { service: 'api', label: 'API', hint: 'Shown as the Project URL; also becomes auth.external_url.' },
  { service: 'studio', label: 'Studio', hint: 'Opened by the Studio links.' },
  { service: 'mail', label: 'Mailpit', hint: 'Opened by the Mailpit link.' },
  { service: 'site', label: 'Site URL', hint: 'Your app: sets auth.site_url and adds it to the redirect URLs.' },
]

/** Lets admins pick proxy host domains for a project instead of its local addresses. */
export function ProjectDomainsSection() {
  const slug = useSlug()
  const { isAdmin } = useAuth()
  const qc = useQueryClient()
  const { data } = useProjectDomains()
  const [sel, setSel] = useState<Partial<Record<ProjectDomainService, string>>>({})
  useEffect(() => setSel(data?.selected ?? {}), [data?.selected])
  const save = useMutation({
    mutationFn: () => api.put<{ config_changed: boolean }>(`/projects/${slug}/domains`, { selected: sel }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ['project', slug] })
      toast.success(r.config_changed ? 'Domains saved. Restart the project to apply the auth URLs.' : 'Domains saved')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!data?.enabled) return null
  const options = (service: ProjectDomainService): ProjectDomain[] =>
    service === 'site' ? (data.sites ?? []) : (data.available ?? []).filter((d) => d.service === service)
  const dirty = rows.some((r) => (sel[r.service] ?? '') !== (data.selected?.[r.service] ?? ''))
  const none = !data.available?.length && !data.sites?.length

  return (
    <Section
      title="Public domains"
      description="Use your Proxy Manager domains instead of the local addresses for this project."
      actions={
        isAdmin && (
          <Button size="sm" onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />} Save
          </Button>
        )
      }
    >
      {none ? (
        <p className="text-muted-foreground text-sm">
          No proxy host routes to this project yet.{' '}
          <Link to="/proxy-manager/hosts" className="underline">
            Create a host
          </Link>{' '}
          with an upstream targeting one of its services, then pick its domain here.
        </p>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {rows.map((r) => {
            const opts = options(r.service)
            return (
              <div key={r.service} className="grid gap-1.5">
                <span className="text-sm font-medium">{r.label}</span>
                <Select
                  value={sel[r.service] || LOCAL}
                  onValueChange={(v) => setSel((cur) => ({ ...cur, [r.service]: v === LOCAL ? '' : v }))}
                  disabled={!isAdmin || !opts.length}
                >
                  <SelectTrigger className="w-full font-mono text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={LOCAL}>{r.service === 'site' ? 'Keep the current Site URL' : 'Local address'}</SelectItem>
                    {opts.map((d) => (
                      <SelectItem key={`${d.host_id}-${d.url}`} value={d.url} className="font-mono text-xs">
                        {d.url}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <span className="text-muted-foreground text-xs">{opts.length ? r.hint : 'No proxy host serves this yet.'}</span>
              </div>
            )
          })}
        </div>
      )}
    </Section>
  )
}
