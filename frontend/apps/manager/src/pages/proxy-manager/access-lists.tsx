import { useEffect, useState } from 'react'
import { KeyRound, Loader2, Pencil, Plus, Trash2, X } from 'lucide-react'
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
import { PM, useAccessLists, usePMMutation } from '@/hooks/use-proxy-manager'
import { api } from '@/lib/api'
import type { AccessList } from '@/lib/types'
import { Field, LinesInput } from './shared'

interface UserRow {
  username: string
  password: string
  existing: boolean
}

function AccessListDialog({ open, onOpenChange, list }: { open: boolean; onOpenChange: (o: boolean) => void; list?: AccessList }) {
  const [name, setName] = useState('')
  const [users, setUsers] = useState<UserRow[]>([])
  const [allow, setAllow] = useState<string[]>([])
  const [deny, setDeny] = useState<string[]>([])
  const [satisfy, setSatisfy] = useState<'any' | 'all'>('all')
  useEffect(() => {
    if (!open) return
    setName(list?.name ?? '')
    setUsers((list?.users ?? []).map((u) => ({ username: u.username, password: '', existing: true })))
    setAllow(list?.allow ?? [])
    setDeny(list?.deny ?? [])
    setSatisfy(list?.satisfy ?? 'all')
  }, [open, list])
  const body = () => ({ name, users: users.map(({ username, password }) => ({ username, password })), allow, deny, satisfy })
  const save = usePMMutation(() => (list ? api.put(`${PM}/access-lists/${list.id}`, body()) : api.post(`${PM}/access-lists`, body())), {
    success: 'Access list saved',
    onSuccess: () => onOpenChange(false),
  })
  const setUser = (i: number, patch: Partial<UserRow>) => setUsers((cur) => cur.map((u, j) => (i === j ? { ...u, ...patch } : u)))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{list ? `Edit ${list.name}` : 'New access list'}</DialogTitle>
          <DialogDescription>Basic authentication users and IP rules, reusable across hosts and routes.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="staff" />
          </Field>
          <div className="grid gap-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Users</span>
              <Button variant="outline" size="sm" onClick={() => setUsers([...users, { username: '', password: '', existing: false }])}>
                <Plus /> Add user
              </Button>
            </div>
            {users.map((u, i) => (
              <div key={i} className="flex gap-2">
                <Input value={u.username} onChange={(e) => setUser(i, { username: e.target.value })} placeholder="username" disabled={u.existing} />
                <Input
                  type="password"
                  value={u.password}
                  onChange={(e) => setUser(i, { password: e.target.value })}
                  placeholder={u.existing ? 'unchanged' : 'password'}
                  autoComplete="new-password"
                />
                <Button variant="ghost" size="icon-sm" aria-label="Remove user" onClick={() => setUsers(users.filter((_, j) => j !== i))}>
                  <X />
                </Button>
              </div>
            ))}
            <p className="text-muted-foreground text-xs">Passwords are stored as $apr1$ hashes, which both nginx and Traefik verify.</p>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Allow" hint="IPs or CIDR ranges, one per line">
              <LinesInput value={allow} onChange={setAllow} placeholder="10.0.0.0/8" />
            </Field>
            <Field label="Deny" hint="Checked before allow (nginx only)">
              <LinesInput value={deny} onChange={setDeny} placeholder="203.0.113.7" />
            </Field>
          </div>
          <Field label="Satisfy" hint="With both users and IPs: require both, or let either pass (nginx only; Traefik requires both).">
            <Select value={satisfy} onValueChange={(v) => setSatisfy(v as 'any' | 'all')}>
              <SelectTrigger className="w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All: allowed IP and valid login</SelectItem>
                <SelectItem value="any">Any: allowed IP or valid login</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || !name}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AccessListsPage() {
  const { data: lists, isLoading } = useAccessLists()
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<AccessList>()
  const [del, setDel] = useState<AccessList | null>(null)
  const remove = usePMMutation((id: number) => api.delete(`${PM}/access-lists/${id}`), { success: 'Access list deleted' })
  return (
    <>
      <PageHeader
        title="Access Lists"
        description="Password protection and IP allow/deny lists."
        actions={
          <Button
            onClick={() => {
              setEdit(undefined)
              setOpen(true)
            }}
          >
            <Plus /> New access list
          </Button>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !lists?.length ? (
        <EmptyState icon={<KeyRound />} title="No access lists" description="Protect admin panels and staging sites with a login or an IP allow list." />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Users</TableHead>
                <TableHead>IP rules</TableHead>
                <TableHead>Satisfy</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {lists.map((l) => (
                <TableRow key={l.id}>
                  <TableCell className="font-medium">{l.name}</TableCell>
                  <TableCell className="text-xs">{(l.users ?? []).map((u) => u.username).join(', ') || '-'}</TableCell>
                  <TableCell className="text-xs">
                    {(l.allow ?? []).length} allow, {(l.deny ?? []).length} deny
                  </TableCell>
                  <TableCell className="text-xs uppercase">{l.satisfy}</TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="Edit"
                      onClick={() => {
                        setEdit(l)
                        setOpen(true)
                      }}
                    >
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(l)}>
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <AccessListDialog open={open} onOpenChange={setOpen} list={edit} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete ${del?.name}?`}
        description="Access lists used by hosts or routes cannot be deleted."
        confirmText="Delete"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
