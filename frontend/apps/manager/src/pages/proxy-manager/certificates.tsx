import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Download, Loader2, Pencil, Plus, RefreshCw, ShieldCheck, Trash2, Upload } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Textarea } from '@workspace/ui/components/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@workspace/ui/components/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@workspace/ui/components/dialog'
import { cn } from '@workspace/ui/lib/utils'
import { toast } from 'sonner'
import { useJobs } from '@/components/job-drawer'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState, PageHeader } from '@/components/page-header'
import { PM, useAcmeAccounts, useCertificates, useDNSProviders, useJobAction, usePMMutation } from '@/hooks/use-proxy-manager'
import { api, errorMessage } from '@/lib/api'
import { formatDate } from '@/lib/format'
import type { Certificate, ChallengeType } from '@/lib/types'
import { CertStatus, Field, LinesInput, ToggleRow } from './shared'

const challenges: { value: ChallengeType; label: string; hint: string }[] = [
  { value: 'http-01', label: 'HTTP-01', hint: 'The CA fetches a token over port 80. Every proxy forwards /.well-known/acme-challenge/ to the manager.' },
  { value: 'dns-01', label: 'DNS-01', hint: 'A TXT record is created through your DNS provider. Required for wildcard domains; works behind firewalls.' },
  { value: 'tls-alpn-01', label: 'TLS-ALPN-01', hint: 'The CA connects to port 443. Needs an nginx instance with TLS-ALPN forwarding on port 443.' },
]

const keyTypes = [
  { value: 'ec256', label: 'ECDSA P-256' },
  { value: 'ec384', label: 'ECDSA P-384' },
  { value: 'rsa2048', label: 'RSA 2048' },
  { value: 'rsa4096', label: 'RSA 4096' },
]

interface AcmeForm {
  name: string
  domains: string[]
  challenge: ChallengeType
  key_type: string
  acme_account_id: number
  dns_provider_id: number
  auto_renew: boolean
}

function AcmeDialog({ open, onOpenChange, cert }: { open: boolean; onOpenChange: (o: boolean) => void; cert?: Certificate }) {
  const { data: accounts = [] } = useAcmeAccounts()
  const { data: dns = [] } = useDNSProviders()
  const { openJob } = useJobs()
  const [f, setF] = useState<AcmeForm>({ name: '', domains: [], challenge: 'http-01', key_type: 'ec256', acme_account_id: 0, dns_provider_id: 0, auto_renew: true })
  useEffect(() => {
    if (!open) return
    setF(
      cert
        ? {
            name: cert.name,
            domains: cert.domains,
            challenge: (cert.challenge || 'http-01') as ChallengeType,
            key_type: cert.key_type || 'ec256',
            acme_account_id: cert.acme_account_id,
            dns_provider_id: cert.dns_provider_id,
            auto_renew: cert.auto_renew,
          }
        : { name: '', domains: [], challenge: 'http-01', key_type: 'ec256', acme_account_id: 0, dns_provider_id: 0, auto_renew: true },
    )
  }, [open, cert])
  const set = (patch: Partial<AcmeForm>) => setF((cur) => ({ ...cur, ...patch }))
  const form: AcmeForm = {
    ...f,
    acme_account_id: f.acme_account_id || (accounts.find((a) => a.is_default)?.id ?? 0),
    dns_provider_id: f.dns_provider_id || (dns[0]?.id ?? 0),
  }
  const wildcard = f.domains.some((d) => d.startsWith('*.'))
  const save = usePMMutation<void, unknown>(
    () => (cert ? api.put(`${PM}/certificates/${cert.id}`, form) : api.post(`${PM}/certificates/acme`, form)),
    {
      onSuccess: (r) => {
        onOpenChange(false)
        if (cert) {
          toast.success('Certificate settings saved')
          return
        }
        const res = r as { certificate: Certificate; job_id?: number; warning?: string }
        if (res.job_id) openJob(res.job_id, `Issue ${res.certificate.name}`)
        else if (res.warning) toast.warning(res.warning)
      },
    },
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{cert ? `Settings for ${cert.name}` : 'Request a certificate'}</DialogTitle>
          <DialogDescription>Issued by the manager with an ACME CA and deployed to every proxy that uses it.</DialogDescription>
        </DialogHeader>
        {accounts.length === 0 ? (
          <p className="text-sm">
            Add an{' '}
            <Link to="/proxy-manager/acme-accounts" className="text-brand hover:underline">
              ACME account
            </Link>{' '}
            first.
          </p>
        ) : (
          <div className="grid gap-4">
            <Field label="Domains" hint={cert ? 'Domains cannot be changed; request a new certificate instead.' : 'One per line. *.example.com needs DNS-01.'}>
              {cert ? (
                <div className="font-mono text-xs">{cert.domains.join(', ')}</div>
              ) : (
                <LinesInput value={f.domains} onChange={(v) => set({ domains: v, challenge: v.some((d) => d.startsWith('*.')) ? 'dns-01' : f.challenge })} placeholder={'example.com\nwww.example.com'} />
              )}
            </Field>
            <Field label="Challenge" hint={challenges.find((c) => c.value === f.challenge)?.hint}>
              <div className="grid grid-cols-3 gap-2">
                {challenges.map((c) => (
                  <button
                    key={c.value}
                    type="button"
                    disabled={wildcard && c.value !== 'dns-01'}
                    onClick={() => set({ challenge: c.value })}
                    className={cn(
                      'rounded-md border px-3 py-2 text-sm transition-colors disabled:opacity-40',
                      f.challenge === c.value ? 'border-brand bg-brand/10' : 'hover:bg-muted/50',
                    )}
                  >
                    {c.label}
                  </button>
                ))}
              </div>
            </Field>
            {f.challenge === 'dns-01' && (
              <Field
                label="DNS provider"
                hint={
                  dns.length === 0 ? (
                    <>
                      No DNS providers yet.{' '}
                      <Link to="/proxy-manager/dns-providers" className="text-brand hover:underline">
                        Add one
                      </Link>
                    </>
                  ) : undefined
                }
              >
                <Select value={form.dns_provider_id ? String(form.dns_provider_id) : ''} onValueChange={(v) => set({ dns_provider_id: Number(v) })}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Choose a DNS provider" />
                  </SelectTrigger>
                  <SelectContent>
                    {dns.map((d) => (
                      <SelectItem key={d.id} value={String(d.id)}>
                        {d.name} ({d.code})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
            <div className="grid grid-cols-2 gap-4">
              <Field label="ACME account">
                <Select value={form.acme_account_id ? String(form.acme_account_id) : ''} onValueChange={(v) => set({ acme_account_id: Number(v) })}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Default account" />
                  </SelectTrigger>
                  <SelectContent>
                    {accounts.map((a) => (
                      <SelectItem key={a.id} value={String(a.id)}>
                        {a.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Key type">
                <Select value={f.key_type} onValueChange={(v) => set({ key_type: v })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {keyTypes.map((k) => (
                      <SelectItem key={k.value} value={k.value}>
                        {k.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
            <Field label="Name">
              <Input value={f.name} onChange={(e) => set({ name: e.target.value })} placeholder={f.domains[0] ?? 'example.com'} />
            </Field>
            <ToggleRow label="Renew automatically" description="Renewed 30 days before expiry; proxies without other pending changes are redeployed." checked={f.auto_renew} onChange={(v) => set({ auto_renew: v })} />
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || accounts.length === 0 || (!cert && f.domains.length === 0)}>
            {save.isPending && <Loader2 className="animate-spin" />}
            {cert ? 'Save' : 'Request certificate'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function UploadDialog({ open, onOpenChange, cert }: { open: boolean; onOpenChange: (o: boolean) => void; cert?: Certificate }) {
  const [name, setName] = useState('')
  const [certPem, setCertPem] = useState('')
  const [keyPem, setKeyPem] = useState('')
  useEffect(() => {
    if (open) {
      setName(cert?.name ?? '')
      setCertPem('')
      setKeyPem('')
    }
  }, [open, cert])
  const body = { name, cert_pem: certPem, key_pem: keyPem }
  const save = usePMMutation(() => (cert ? api.put(`${PM}/certificates/${cert.id}/upload`, body) : api.post(`${PM}/certificates/upload`, body)), {
    success: 'Certificate uploaded',
    onSuccess: () => onOpenChange(false),
  })
  const readFile = (set: (v: string) => void) => (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) file.text().then(set, (err) => toast.error(errorMessage(err)))
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{cert ? `Replace ${cert.name}` : 'Upload a certificate'}</DialogTitle>
          <DialogDescription>PEM certificate chain (leaf first) and its unencrypted private key. The key is stored encrypted.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Taken from the certificate when empty" />
          </Field>
          <Field label={<span className="flex w-full items-center justify-between">Certificate chain <input type="file" accept=".pem,.crt,.cer" onChange={readFile(setCertPem)} className="text-muted-foreground max-w-52 text-xs" /></span>}>
            <Textarea rows={6} value={certPem} onChange={(e) => setCertPem(e.target.value)} placeholder="-----BEGIN CERTIFICATE-----" className="font-mono text-xs" />
          </Field>
          <Field label={<span className="flex w-full items-center justify-between">Private key <input type="file" accept=".pem,.key" onChange={readFile(setKeyPem)} className="text-muted-foreground max-w-52 text-xs" /></span>}>
            <Textarea rows={6} value={keyPem} onChange={(e) => setKeyPem(e.target.value)} placeholder="-----BEGIN PRIVATE KEY-----" className="font-mono text-xs" />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || !certPem || !keyPem}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Upload
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function CertificatesPage() {
  const { data: certs, isLoading } = useCertificates()
  const [acme, setAcme] = useState<{ open: boolean; cert?: Certificate }>({ open: false })
  const [upload, setUpload] = useState<{ open: boolean; cert?: Certificate }>({ open: false })
  const [del, setDel] = useState<Certificate | null>(null)
  const issue = useJobAction<Certificate>(
    (c) => `/certificates/${c.id}/issue`,
    (c) => `Issue ${c.name}`,
  )
  const remove = usePMMutation((id: number) => api.delete(`${PM}/certificates/${id}`), { success: 'Certificate deleted' })

  const download = async (c: Certificate) => {
    try {
      const res = await fetch(`/api${PM}/certificates/${c.id}/pem`, { credentials: 'include' })
      if (!res.ok) throw new Error(res.statusText)
      const url = URL.createObjectURL(await res.blob())
      const a = document.createElement('a')
      a.href = url
      a.download = `${c.name.replace(/[^\w.-]/g, '_')}.pem`
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <>
      <PageHeader
        title="Certificates"
        description="TLS certificates issued with ACME (Let's Encrypt and other CAs) or uploaded."
        actions={
          <>
            <Button variant="outline" onClick={() => setUpload({ open: true })}>
              <Upload /> Upload
            </Button>
            <Button onClick={() => setAcme({ open: true })}>
              <Plus /> Request certificate
            </Button>
          </>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !certs?.length ? (
        <EmptyState
          icon={<ShieldCheck />}
          title="No certificates"
          description="Request one with HTTP-01, DNS-01 or TLS-ALPN-01, or let a proxy host with automatic TLS create it."
        />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Certificate</TableHead>
                <TableHead>Source</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>Status</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {certs.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>
                    <div className="font-medium">{c.name}</div>
                    <div className="text-muted-foreground max-w-80 truncate font-mono text-xs" title={c.domains?.join(', ')}>
                      {c.domains?.join(', ')}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    {c.source === 'acme' ? (
                      <>
                        ACME <span className="text-muted-foreground uppercase">{c.challenge}</span>
                        {c.auto_renew && <div className="text-muted-foreground">auto-renew</div>}
                      </>
                    ) : (
                      'Uploaded'
                    )}
                    {c.issuer && <div className="text-muted-foreground truncate">{c.issuer}</div>}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs">{c.not_after ? formatDate(c.not_after) : '-'}</TableCell>
                  <TableCell>
                    <CertStatus c={c} />
                    {c.last_error && (
                      <div className="text-destructive max-w-64 truncate text-[11px]" title={c.last_error}>
                        {c.last_error}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    {c.source === 'acme' ? (
                      <>
                        <Button variant="ghost" size="xs" onClick={() => issue.mutate(c)} disabled={issue.isPending}>
                          <RefreshCw /> {c.not_after ? 'Renew' : 'Issue'}
                        </Button>
                        <Button variant="ghost" size="icon-xs" aria-label="Settings" onClick={() => setAcme({ open: true, cert: c })}>
                          <Pencil />
                        </Button>
                      </>
                    ) : (
                      <Button variant="ghost" size="icon-xs" aria-label="Replace" onClick={() => setUpload({ open: true, cert: c })}>
                        <Upload />
                      </Button>
                    )}
                    {c.not_after && (
                      <Button variant="ghost" size="icon-xs" aria-label="Download chain" onClick={() => download(c)}>
                        <Download />
                      </Button>
                    )}
                    <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(c)}>
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <AcmeDialog open={acme.open} onOpenChange={(o) => setAcme((s) => ({ ...s, open: o }))} cert={acme.cert} />
      <UploadDialog open={upload.open} onOpenChange={(o) => setUpload((s) => ({ ...s, open: o }))} cert={upload.cert} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="Certificates used by proxy hosts cannot be deleted."
        confirmText="Delete"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
