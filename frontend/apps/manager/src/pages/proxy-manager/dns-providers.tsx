import { useEffect, useMemo, useState } from 'react'
import { Check, ChevronsUpDown, ExternalLink, Globe2, Loader2, Pencil, Plus, Trash2 } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Popover, PopoverContent, PopoverTrigger } from '@workspace/ui/components/popover'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@workspace/ui/components/command'
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
import { PM, useDNSProviders, usePMMutation, useProxyMeta } from '@/hooks/use-proxy-manager'
import { api } from '@/lib/api'
import type { CatalogField, DNSProvider } from '@/lib/types'
import { Field } from './shared'

function ProviderDialog({ open, onOpenChange, provider }: { open: boolean; onOpenChange: (o: boolean) => void; provider?: DNSProvider }) {
  const { data: meta } = useProxyMeta()
  const catalog = useMemo(() => meta?.dns_providers ?? [], [meta])
  const [code, setCode] = useState('cloudflare')
  const [name, setName] = useState('')
  const [creds, setCreds] = useState<Record<string, string>>({})
  const [pick, setPick] = useState(false)
  const [showAll, setShowAll] = useState(false)
  useEffect(() => {
    if (!open) return
    setCode(provider?.code ?? 'cloudflare')
    setName(provider?.name ?? '')
    setCreds({})
    setShowAll(false)
  }, [open, provider])
  const entry = catalog.find((c) => c.code === code)
  const stored = new Set(provider?.code === code ? provider.keys : [])
  const save = usePMMutation(
    () => {
      const body = { name, code, credentials: creds }
      return provider ? api.put(`${PM}/dns-providers/${provider.id}`, body) : api.post(`${PM}/dns-providers`, body)
    },
    { success: 'DNS provider saved', onSuccess: () => onOpenChange(false) },
  )

  const fieldInput = (f: CatalogField) => (
    <Field key={f.key} label={<span className="font-mono text-xs">{f.key}</span>} hint={f.description}>
      <Input
        type={/TOKEN|SECRET|KEY|PASSWORD/.test(f.key) ? 'password' : 'text'}
        value={creds[f.key] ?? ''}
        onChange={(e) => setCreds((c) => ({ ...c, [f.key]: e.target.value }))}
        placeholder={stored.has(f.key) ? 'stored (leave empty to keep, "-" to remove)' : ''}
        autoComplete="off"
        className="font-mono text-xs"
      />
    </Field>
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{provider ? `Edit ${provider.name}` : 'New DNS provider'}</DialogTitle>
          <DialogDescription>Credentials for DNS-01 challenges. All lego providers are supported; values are stored encrypted.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid grid-cols-2 gap-4">
            <Field label="Provider">
              <Popover open={pick} onOpenChange={setPick}>
                <PopoverTrigger asChild>
                  <Button variant="outline" className="w-full justify-between font-normal">
                    {entry?.name ?? code}
                    <ChevronsUpDown className="text-muted-foreground" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-80 p-0" align="start">
                  <Command>
                    <CommandInput placeholder="Search providers..." />
                    <CommandList>
                      <CommandEmpty>No provider found.</CommandEmpty>
                      <CommandGroup>
                        {catalog.map((c) => (
                          <CommandItem
                            key={c.code}
                            value={`${c.name} ${c.code}`}
                            onSelect={() => {
                              setCode(c.code)
                              setCreds({})
                              setPick(false)
                            }}
                          >
                            <span className="flex-1 truncate">{c.name}</span>
                            <span className="text-muted-foreground font-mono text-xs">{c.code}</span>
                            {c.code === code && <Check className="size-4" />}
                          </CommandItem>
                        ))}
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </PopoverContent>
              </Popover>
            </Field>
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={entry?.name} />
            </Field>
          </div>
          {entry?.url && (
            <a href={entry.url} target="_blank" rel="noreferrer" className="text-brand inline-flex items-center gap-1 text-xs hover:underline">
              Provider documentation <ExternalLink className="size-3" />
            </a>
          )}
          {entry && (
            <>
              <div className="grid gap-3">{entry.credentials.map(fieldInput)}</div>
              {entry.additional.length > 0 && (
                <div className="grid gap-3">
                  <Button variant="link" size="sm" className="justify-self-start px-0" onClick={() => setShowAll((v) => !v)}>
                    {showAll ? 'Hide' : 'Show'} optional settings ({entry.additional.length})
                  </Button>
                  {showAll && entry.additional.map(fieldInput)}
                </div>
              )}
            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function DNSProvidersPage() {
  const { data: providers, isLoading } = useDNSProviders()
  const { data: meta } = useProxyMeta()
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<DNSProvider>()
  const [del, setDel] = useState<DNSProvider | null>(null)
  const remove = usePMMutation((id: number) => api.delete(`${PM}/dns-providers/${id}`), { success: 'DNS provider deleted' })
  const nameOf = new Map((meta?.dns_providers ?? []).map((c) => [c.code, c.name]))
  return (
    <>
      <PageHeader
        title="DNS Providers"
        description="Used by DNS-01 challenges, the only way to issue wildcard certificates."
        actions={
          <Button
            onClick={() => {
              setEdit(undefined)
              setOpen(true)
            }}
          >
            <Plus /> New DNS provider
          </Button>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !providers?.length ? (
        <EmptyState icon={<Globe2 />} title="No DNS providers" description="Add Cloudflare or any of the 190+ providers supported by lego." />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>Credentials</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {providers.map((p) => (
                <TableRow key={p.id}>
                  <TableCell className="font-medium">{p.name}</TableCell>
                  <TableCell className="text-xs">{nameOf.get(p.code) ?? p.code}</TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">{p.keys.join(', ')}</TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="Edit"
                      onClick={() => {
                        setEdit(p)
                        setOpen(true)
                      }}
                    >
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(p)}>
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <ProviderDialog open={open} onOpenChange={setOpen} provider={edit} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="Providers used by certificates cannot be deleted."
        confirmText="Delete"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
