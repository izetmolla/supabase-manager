import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { PageHeader, Section } from '@/components/page-header'
import { SMTPForm, SMTPTest } from '@/components/smtp-form'
import { useAuth } from '@/hooks/use-auth'
import { useProject, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { SMTPSettings } from '@/lib/types'

export function SMTPPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { isAdmin } = useAuth()
  const { data: project } = useProject()
  const key = ['project', slug, 'smtp']
  const { data, isLoading } = useQuery({ queryKey: key, queryFn: () => api.get<SMTPSettings>(`/projects/${slug}/smtp`) })
  const defaults = useQuery({
    queryKey: ['system', 'smtp'],
    queryFn: () => api.get<SMTPSettings>('/system/smtp'),
    enabled: isAdmin,
  })
  const [form, setForm] = useState<SMTPSettings | null>(null)
  useEffect(() => {
    if (data) setForm({ ...data, pass: '' })
  }, [data])

  const save = useMutation({
    mutationFn: (useDefaults: boolean) =>
      useDefaults ? api.put<SMTPSettings>(`/projects/${slug}/smtp?defaults=1`) : api.put<SMTPSettings>(`/projects/${slug}/smtp`, form),
    onSuccess: (s) => {
      qc.setQueryData(key, s)
      toast.success('Email settings saved', {
        description: project?.status === 'running' ? 'Restart the project to apply the change.' : undefined,
      })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (isLoading || !form) return <Skeleton className="h-64" />
  const hasDefaults = !!defaults.data?.enabled && !!defaults.data.host

  return (
    <>
      <PageHeader
        title="Emails (SMTP)"
        description="The mail server Auth uses for sign-up confirmations, invitations, magic links, password resets and email changes."
      />
      <Section
        title="SMTP server"
        actions={
          isAdmin &&
          hasDefaults && (
            <Button variant="outline" size="sm" onClick={() => save.mutate(true)} disabled={save.isPending}>
              <Copy /> Use system default ({defaults.data!.host})
            </Button>
          )
        }
      >
        <div className="grid gap-4">
          <SMTPForm value={form} onChange={setForm} disabled={!isAdmin} />
          {isAdmin && <SMTPTest path={`/projects/${slug}/smtp/test`} value={form} />}
        </div>
      </Section>
      {isAdmin && (
        <div className="mt-4 flex justify-end">
          <Button onClick={() => save.mutate(false)} disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save changes
          </Button>
        </div>
      )}
    </>
  )
}
