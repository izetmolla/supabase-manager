import { useEffect, useMemo, useState } from 'react'
import { BadgeCheck, Loader2, Pencil, Plus, Trash2, UserCheck } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
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
import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState, PageHeader } from '@/components/page-header'
import { PM, useAcmeAccounts, usePMMutation, useProxyMeta } from '@/hooks/use-proxy-manager'
import { api } from '@/lib/api'
import type { AcmeAccount } from '@/lib/types'
import { Field, ToggleRow } from './shared'

const CUSTOM = 'custom'

function AccountDialog({ open, onOpenChange, account }: { open: boolean; onOpenChange: (o: boolean) => void; account?: AcmeAccount }) {
  const { data: meta } = useProxyMeta()
  const dirs = useMemo(() => meta?.directories ?? [], [meta])
  const [f, setF] = useState({ name: '', email: '', directory_url: '', eab_key_id: '', eab_hmac: '', is_default: false })
  const [preset, setPreset] = useState('letsencrypt')
  useEffect(() => {
    if (!open) return
    const url = account?.directory_url ?? dirs[0]?.url ?? ''
    setF({ name: account?.name ?? '', email: account?.email ?? '', directory_url: url, eab_key_id: account?.eab_key_id ?? '', eab_hmac: '', is_default: account?.is_default ?? false })
    setPreset(dirs.find((d) => d.url === url)?.id ?? CUSTOM)
  }, [open, account, dirs])
  const set = (patch: Partial<typeof f>) => setF((cur) => ({ ...cur, ...patch }))
  const save = usePMMutation(() => (account ? api.put(`${PM}/acme-accounts/${account.id}`, f) : api.post(`${PM}/acme-accounts`, f)), {
    success: 'ACME account saved',
    onSuccess: () => onOpenChange(false),
  })
  const needsEAB = /zerossl|pki\.goog/.test(f.directory_url)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{account ? `Edit ${account.name}` : 'New ACME account'}</DialogTitle>
          <DialogDescription>The account a CA issues certificates to. It is registered on first use, accepting the CA&apos;s terms of service.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <Field label="Certificate authority">
            <Select
              value={preset}
              onValueChange={(v) => {
                setPreset(v)
                const d = dirs.find((x) => x.id === v)
                if (d) set({ directory_url: d.url })
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {dirs.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name}
                  </SelectItem>
                ))}
                <SelectItem value={CUSTOM}>Custom ACME directory</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {preset === CUSTOM && (
            <Field label="Directory URL" hint="e.g. a private step-ca or Pebble for testing">
              <Input value={f.directory_url} onChange={(e) => set({ directory_url: e.target.value })} placeholder="https://ca.internal/acme/acme/directory" className="font-mono text-xs" />
            </Field>
          )}
          <div className="grid grid-cols-2 gap-4">
            <Field label="Email" hint="Expiry notices from the CA">
              <Input type="email" value={f.email} onChange={(e) => set({ email: e.target.value })} />
            </Field>
            <Field label="Name">
              <Input value={f.name} onChange={(e) => set({ name: e.target.value })} placeholder={f.email} />
            </Field>
          </div>
          {(needsEAB || f.eab_key_id) && (
            <div className="grid grid-cols-2 gap-4">
              <Field label="EAB key ID" hint="External account binding from the CA dashboard">
                <Input value={f.eab_key_id} onChange={(e) => set({ eab_key_id: e.target.value })} className="font-mono text-xs" />
              </Field>
              <Field label="EAB HMAC key">
                <Input type="password" value={f.eab_hmac} onChange={(e) => set({ eab_hmac: e.target.value })} placeholder={account?.eab_key_id ? 'stored' : ''} className="font-mono text-xs" />
              </Field>
            </div>
          )}
          <ToggleRow label="Default account" description="Used for new and automatic certificates." checked={f.is_default} onChange={(v) => set({ is_default: v })} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || !f.email || !f.directory_url}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AcmeAccountsPage() {
  const { data: accounts, isLoading } = useAcmeAccounts()
  const { data: meta } = useProxyMeta()
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<AcmeAccount>()
  const [del, setDel] = useState<AcmeAccount | null>(null)
  const register = usePMMutation((id: number) => api.post(`${PM}/acme-accounts/${id}/register`), { success: 'Account registered' })
  const remove = usePMMutation((id: number) => api.delete(`${PM}/acme-accounts/${id}`), { success: 'Account deleted' })
  const dirName = (url: string) => meta?.directories.find((d) => d.url === url)?.name ?? url
  return (
    <>
      <PageHeader
        title="ACME Accounts"
        description="Accounts at Let's Encrypt, ZeroSSL, Google, Buypass or any ACME CA."
        actions={
          <Button
            onClick={() => {
              setEdit(undefined)
              setOpen(true)
            }}
          >
            <Plus /> New account
          </Button>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !accounts?.length ? (
        <EmptyState
          icon={<UserCheck />}
          title="No ACME accounts"
          description="Add a Let's Encrypt account to issue free certificates. Use the staging CA while testing to avoid rate limits."
          action={
            <Button onClick={() => setOpen(true)}>
              <Plus /> New account
            </Button>
          }
        />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Account</TableHead>
                <TableHead>CA</TableHead>
                <TableHead>Registration</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {accounts.map((a) => (
                <TableRow key={a.id}>
                  <TableCell>
                    <div className="flex items-center gap-2 font-medium">
                      {a.name}
                      {a.is_default && <span className="text-brand border-brand/30 rounded border px-1.5 text-[11px]">default</span>}
                    </div>
                    <div className="text-muted-foreground text-xs">{a.email}</div>
                  </TableCell>
                  <TableCell className="max-w-64 truncate text-xs" title={a.directory_url}>
                    {dirName(a.directory_url)}
                  </TableCell>
                  <TableCell className="text-xs">
                    {a.registered ? (
                      <span className="text-brand flex items-center gap-1">
                        <BadgeCheck className="size-3.5" /> Registered
                      </span>
                    ) : (
                      <span className="text-muted-foreground">On first use</span>
                    )}
                    {a.last_error && (
                      <div className="text-destructive max-w-64 truncate" title={a.last_error}>
                        {a.last_error}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    {!a.registered && (
                      <Button variant="ghost" size="xs" onClick={() => register.mutate(a.id)} disabled={register.isPending}>
                        {register.isPending && <Loader2 className="animate-spin" />}
                        Register now
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="Edit"
                      onClick={() => {
                        setEdit(a)
                        setOpen(true)
                      }}
                    >
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(a)}>
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <AccountDialog open={open} onOpenChange={setOpen} account={edit} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="Accounts used by certificates cannot be deleted."
        confirmText="Delete"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
