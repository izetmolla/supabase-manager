import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, X } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Switch } from '@workspace/ui/components/switch'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { PageHeader, Section } from '@/components/page-header'
import { useProject, useSettings, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { AuthSettings, Settings } from '@/lib/types'

function useAuthForm() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { data: project } = useProject()
  const { data: settings, isLoading } = useSettings()
  const [form, setForm] = useState<AuthSettings | null>(null)

  useEffect(() => {
    if (settings) setForm(settings.auth)
  }, [settings])

  const save = useMutation({
    mutationFn: (a: AuthSettings) => api.put<Settings>(`/projects/${slug}/config/auth`, a),
    onSuccess: (s) => {
      qc.setQueryData(['project', slug, 'config'], s)
      toast.success('Auth settings saved', {
        description: project?.status === 'running' ? 'Restart the project to apply the change.' : undefined,
      })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const set = <K extends keyof AuthSettings>(k: K, v: AuthSettings[K]) => setForm((f) => (f ? { ...f, [k]: v } : f))
  return { form, set, save, isLoading }
}

function SaveBar({ onSave, pending }: { onSave: () => void; pending: boolean }) {
  return (
    <div className="mt-4 flex justify-end">
      <Button onClick={onSave} disabled={pending}>
        {pending && <Loader2 className="animate-spin" />}
        Save changes
      </Button>
    </div>
  )
}

export function AuthUrlPage() {
  const { form, set, save, isLoading } = useAuthForm()
  const [newUrl, setNewUrl] = useState('')
  if (isLoading || !form) return <Skeleton className="h-64" />

  const addUrl = () => {
    const u = newUrl.trim()
    if (!u || form.additional_redirect_urls.includes(u)) return
    set('additional_redirect_urls', [...form.additional_redirect_urls, u])
    setNewUrl('')
  }

  return (
    <>
      <PageHeader title="URL Configuration" description="Configure site URL and redirect URLs for authentication." />
      <div className="grid gap-6">
        <Section title="Site URL" description="The default redirect URL used when a redirect URL is not specified or does not match the allow list.">
          <Input value={form.site_url} onChange={(e) => set('site_url', e.target.value)} className="font-mono" />
        </Section>
        <Section title="Redirect URLs" description="URLs auth providers are allowed to redirect to after authentication. Wildcards are supported.">
          <div className="grid gap-2">
            {form.additional_redirect_urls.length === 0 && <p className="text-muted-foreground text-sm">No redirect URLs.</p>}
            {form.additional_redirect_urls.map((u) => (
              <div key={u} className="flex items-center justify-between gap-2 rounded-md border px-3 py-2">
                <span className="truncate font-mono text-xs">{u}</span>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={`Remove ${u}`}
                  onClick={() => set('additional_redirect_urls', form.additional_redirect_urls.filter((x) => x !== u))}
                >
                  <X />
                </Button>
              </div>
            ))}
            <div className="mt-2 flex gap-2">
              <Input
                value={newUrl}
                onChange={(e) => setNewUrl(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && addUrl()}
                placeholder="http://127.0.0.1:3000/auth/callback"
                className="font-mono"
              />
              <Button variant="outline" onClick={addUrl}>
                <Plus /> Add URL
              </Button>
            </div>
          </div>
        </Section>
      </div>
      <SaveBar onSave={() => save.mutate(form)} pending={save.isPending} />
    </>
  )
}

function ToggleRow({ label, description, checked, onChange }: { label: string; description: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center justify-between gap-4 py-3">
      <span className="grid">
        <span className="text-sm">{label}</span>
        <span className="text-muted-foreground text-xs">{description}</span>
      </span>
      <Switch checked={checked} onCheckedChange={onChange} />
    </label>
  )
}

export function AuthSettingsPage() {
  const { form, set, save, isLoading } = useAuthForm()
  if (isLoading || !form) return <Skeleton className="h-64" />

  return (
    <>
      <PageHeader title="Auth Settings" description="User signups, sessions and email settings." />
      <div className="grid gap-6">
        <Section title="User Signups">
          <div className="divide-y">
            <ToggleRow label="Allow new users to sign up" description="If disabled, new users cannot sign up." checked={form.enable_signup} onChange={(v) => set('enable_signup', v)} />
            <ToggleRow label="Allow anonymous sign-ins" description="Users can sign in without credentials." checked={form.enable_anonymous_sign_ins} onChange={(v) => set('enable_anonymous_sign_ins', v)} />
            <ToggleRow label="Allow manual linking" description="Lets users link identities manually." checked={form.enable_manual_linking} onChange={(v) => set('enable_manual_linking', v)} />
          </div>
        </Section>
        <Section title="Email">
          <div className="divide-y">
            <ToggleRow label="Enable email signups" description="Users can sign up with email and password." checked={form.email_enable_signup} onChange={(v) => set('email_enable_signup', v)} />
            <ToggleRow label="Confirm email" description="Users must confirm their email before signing in." checked={form.email_enable_confirmations} onChange={(v) => set('email_enable_confirmations', v)} />
            <ToggleRow label="Secure email change" description="Confirm email changes on both old and new addresses." checked={form.email_double_confirm_changes} onChange={(v) => set('email_double_confirm_changes', v)} />
          </div>
        </Section>
        <Section title="Sessions & passwords">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="jwt-expiry">JWT expiry (seconds)</Label>
              <Input id="jwt-expiry" type="number" min={1} max={604800} value={form.jwt_expiry} onChange={(e) => set('jwt_expiry', Number(e.target.value))} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="min-pw">Minimum password length</Label>
              <Input id="min-pw" type="number" min={6} value={form.minimum_password_length} onChange={(e) => set('minimum_password_length', Number(e.target.value))} />
            </div>
          </div>
        </Section>
      </div>
      <SaveBar onSave={() => save.mutate(form)} pending={save.isPending} />
    </>
  )
}
