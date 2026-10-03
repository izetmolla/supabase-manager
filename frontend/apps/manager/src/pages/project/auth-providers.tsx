import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, Info, KeyRound, Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Switch } from '@workspace/ui/components/switch'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from '@workspace/ui/components/sheet'
import { PageHeader } from '@/components/page-header'
import { CopyField } from '@/components/copy-field'
import { useProject, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { Provider } from '@/lib/types'
import { providerLabels } from '@/lib/format'

const needsUrl = new Set(['azure', 'keycloak', 'gitlab', 'workos'])

function ProviderSheet({ provider, onClose }: { provider: Provider | null; onClose: () => void }) {
  const slug = useSlug()
  const qc = useQueryClient()
  const { data: project } = useProject()
  const [form, setForm] = useState<Provider | null>(provider)
  const [secret, setSecret] = useState('')

  useEffect(() => {
    setForm(provider)
    setSecret('')
  }, [provider])

  const save = useMutation({
    mutationFn: (p: Provider) => api.put<Provider>(`/projects/${slug}/auth/providers/${p.name}`, { ...p, secret }),
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ['project', slug, 'providers'] })
      qc.invalidateQueries({ queryKey: ['project', slug, 'secrets'] })
      toast.success(`${providerLabels[p.name]} saved`, {
        description: project?.status === 'running' ? 'Restart the project to apply the change.' : undefined,
      })
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!form) return null
  const label = providerLabels[form.name] ?? form.name
  const set = <K extends keyof Provider>(k: K, v: Provider[K]) => setForm((f) => (f ? { ...f, [k]: v } : f))
  const isApple = form.name === 'apple'

  return (
    <Sheet open={!!provider} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-lg">
        <SheetHeader className="border-b">
          <SheetTitle>{label}</SheetTitle>
          <SheetDescription>
            Written to <code className="font-mono">[auth.external.{form.name}]</code> in config.toml.
          </SheetDescription>
        </SheetHeader>

        <div className="grid min-w-0 flex-1 content-start gap-5 overflow-auto p-4 [&>*]:min-w-0">
          <label className="flex items-center justify-between gap-4 rounded-md border px-4 py-3">
            <span className="grid">
              <span className="text-sm font-medium">Enable sign in with {label}</span>
              <span className="text-muted-foreground text-xs">Users can sign in with their {label} account.</span>
            </span>
            <Switch checked={form.enabled} onCheckedChange={(v) => set('enabled', v)} />
          </label>

          {isApple && (
            <Alert>
              <Info />
              <AlertDescription className="text-xs leading-relaxed">
                Put your <strong>Services ID</strong> first (for example <code>com.example.app.web</code>), then any native
                App IDs, separated by commas. The secret is a JWT generated from your <code>.p8</code> key and must be
                rotated every 6 months for the web OAuth flow.
              </AlertDescription>
            </Alert>
          )}

          <div className="grid gap-2">
            <Label htmlFor="client-id">{isApple ? 'Client IDs' : 'Client ID'}</Label>
            <Input id="client-id" value={form.client_id} onChange={(e) => set('client_id', e.target.value)} className="font-mono" />
          </div>

          <div className="grid gap-2">
            <Label htmlFor="secret">{isApple ? 'Secret key (for OAuth)' : 'Client secret'}</Label>
            <Input
              id="secret"
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder={form.secret_stored ? 'Stored - leave empty to keep the current value' : 'Paste the secret'}
              className="font-mono"
              autoComplete="off"
            />
            <p className="text-muted-foreground text-xs leading-relaxed break-words">
              <KeyRound className="mr-1 inline size-3 align-[-2px]" />
              Encrypted in the manager and injected as <code className="font-mono break-all">{form.secret_env}</code>.
              config.toml only contains <code className="font-mono break-all">env({form.secret_env})</code>.
            </p>
            {form.has_secret && (
              <p className="text-amber-400 text-xs">
                config.toml currently contains a plain-text secret. Saving replaces it with an env() reference, so enter
                the secret here.
              </p>
            )}
          </div>

          {needsUrl.has(form.name) && (
            <div className="grid gap-2">
              <Label htmlFor="url">Provider URL</Label>
              <Input id="url" value={form.url} onChange={(e) => set('url', e.target.value)} placeholder="https://..." />
            </div>
          )}

          <div className="grid gap-2">
            <Label htmlFor="redirect">Redirect URI override (optional)</Label>
            <Input id="redirect" value={form.redirect_uri} onChange={(e) => set('redirect_uri', e.target.value)} placeholder={form.callback_url} />
          </div>

          <CopyField
            label="Callback URL (for OAuth)"
            value={form.redirect_uri || form.callback_url}
            description={`Register this URL in the ${label} developer console.`}
          />

          <div className="grid gap-3 rounded-md border px-4 py-3">
            <label className="flex items-center justify-between gap-4">
              <span className="grid">
                <span className="text-sm">Allow users without an email</span>
                <span className="text-muted-foreground text-xs">email_optional</span>
              </span>
              <Switch checked={form.email_optional} onCheckedChange={(v) => set('email_optional', v)} />
            </label>
            <label className="flex items-center justify-between gap-4">
              <span className="grid">
                <span className="text-sm">Skip nonce check</span>
                <span className="text-muted-foreground text-xs">Needed for some native flows (e.g. local Google sign in)</span>
              </span>
              <Switch checked={form.skip_nonce_check} onCheckedChange={(v) => set('skip_nonce_check', v)} />
            </label>
          </div>
        </div>

        <SheetFooter className="flex-row justify-end border-t">
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(form)} disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

export function ProvidersPage() {
  const slug = useSlug()
  const [selected, setSelected] = useState<Provider | null>(null)
  const { data: providers, isLoading } = useQuery({
    queryKey: ['project', slug, 'providers'],
    queryFn: () => api.get<Provider[]>(`/projects/${slug}/auth/providers`),
  })

  const sorted = [...(providers ?? [])].sort((a, b) => Number(b.enabled) - Number(a.enabled))

  return (
    <>
      <PageHeader
        title="Sign In / Providers"
        description="Configure OAuth providers for this project. Changes apply after the project restarts."
      />
      <div className="bg-card divide-y overflow-hidden rounded-lg border">
        {isLoading
          ? Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="m-3 h-8" />)
          : sorted.map((p) => (
              <button
                key={p.name}
                onClick={() => setSelected(p)}
                className="hover:bg-accent/40 flex w-full items-center justify-between gap-3 px-5 py-3 text-left transition-colors"
              >
                <span className="flex items-center gap-3">
                  <span className="bg-muted flex size-8 items-center justify-center rounded-md font-mono text-xs font-semibold uppercase">
                    {p.name.slice(0, 2)}
                  </span>
                  <span className="text-sm font-medium">{providerLabels[p.name] ?? p.name}</span>
                </span>
                <span className="flex items-center gap-3">
                  <span
                    className={
                      p.enabled
                        ? 'text-brand border-brand/30 bg-brand/10 rounded-full border px-2 py-0.5 text-xs'
                        : 'text-muted-foreground rounded-full border px-2 py-0.5 text-xs'
                    }
                  >
                    {p.enabled ? 'Enabled' : 'Disabled'}
                  </span>
                  <ChevronRight className="text-muted-foreground size-4" />
                </span>
              </button>
            ))}
      </div>
      <ProviderSheet provider={selected} onClose={() => setSelected(null)} />
    </>
  )
}
