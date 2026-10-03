import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Loader2, Send } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Switch } from '@workspace/ui/components/switch'
import { api, errorMessage } from '@/lib/api'
import type { SMTPSettings } from '@/lib/types'

function Field({ id, label, hint, children }: { id: string; label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <span className="text-muted-foreground text-xs">{hint}</span>}
    </div>
  )
}

/** SMTP server fields. The password field stays empty unless it is being changed. */
export function SMTPForm({
  value,
  onChange,
  disabled,
}: {
  value: SMTPSettings
  onChange: (v: SMTPSettings) => void
  disabled?: boolean
}) {
  const set = <K extends keyof SMTPSettings>(k: K, v: SMTPSettings[K]) => onChange({ ...value, [k]: v })
  return (
    <div className="grid gap-4">
      <label className="flex items-center justify-between gap-4">
        <span className="grid">
          <span className="text-sm">Use a custom SMTP server</span>
          <span className="text-muted-foreground text-xs">
            Off: emails are not delivered, Mailpit captures them for testing. On: confirmations, invitations, magic links and password resets go
            through this server.
          </span>
        </span>
        <Switch checked={value.enabled} onCheckedChange={(v) => set('enabled', v)} disabled={disabled} />
      </label>
      {value.enabled && (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="smtp-host" label="Host">
            <Input id="smtp-host" value={value.host} onChange={(e) => set('host', e.target.value)} placeholder="smtp.sendgrid.net" disabled={disabled} />
          </Field>
          <Field id="smtp-port" label="Port" hint="587 uses STARTTLS, 465 uses TLS.">
            <Input id="smtp-port" type="number" min={1} max={65535} value={value.port} onChange={(e) => set('port', Number(e.target.value))} disabled={disabled} />
          </Field>
          <Field id="smtp-user" label="Username">
            <Input id="smtp-user" value={value.user} onChange={(e) => set('user', e.target.value)} autoComplete="off" disabled={disabled} />
          </Field>
          <Field id="smtp-pass" label="Password" hint={value.has_pass ? 'Stored encrypted. Leave empty to keep it.' : 'Stored encrypted, never written to config.toml.'}>
            <Input
              id="smtp-pass"
              type="password"
              value={value.pass ?? ''}
              onChange={(e) => set('pass', e.target.value)}
              placeholder={value.has_pass ? '••••••••' : ''}
              autoComplete="new-password"
              disabled={disabled}
            />
          </Field>
          <Field id="smtp-from" label="Sender email">
            <Input id="smtp-from" type="email" value={value.admin_email} onChange={(e) => set('admin_email', e.target.value)} placeholder="no-reply@example.com" disabled={disabled} />
          </Field>
          <Field id="smtp-name" label="Sender name">
            <Input id="smtp-name" value={value.sender_name} onChange={(e) => set('sender_name', e.target.value)} placeholder="My App" disabled={disabled} />
          </Field>
          <Field id="smtp-rate" label="Emails per hour" hint="Auth's rate limit for outgoing emails.">
            <Input id="smtp-rate" type="number" min={1} value={value.emails_per_hour} onChange={(e) => set('emails_per_hour', Number(e.target.value))} disabled={disabled} />
          </Field>
        </div>
      )}
    </div>
  )
}

/** Sends a test message with the settings currently in the form (stored password if left empty). */
export function SMTPTest({ path, value }: { path: string; value: SMTPSettings }) {
  const [to, setTo] = useState('')
  const test = useMutation({
    mutationFn: () => api.post(path, { ...value, to }),
    onSuccess: () => toast.success(`Test email sent to ${to}`),
    onError: (err) => toast.error(errorMessage(err)),
  })
  if (!value.enabled) return null
  return (
    <div className="flex flex-wrap items-end gap-2 border-t pt-4">
      <div className="grid flex-1 gap-1.5">
        <Label htmlFor="smtp-test">Send a test email</Label>
        <Input id="smtp-test" type="email" value={to} onChange={(e) => setTo(e.target.value)} placeholder="you@example.com" className="max-w-sm" />
      </div>
      <Button variant="outline" onClick={() => test.mutate()} disabled={!to.includes('@') || test.isPending}>
        {test.isPending ? <Loader2 className="animate-spin" /> : <Send />} Send test
      </Button>
    </div>
  )
}
