import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Loader2, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@workspace/ui/components/tabs'
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
import { PageContainer } from '@/components/layout/layouts'
import { PageHeader } from '@/components/page-header'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useAuth } from '@/hooks/use-auth'
import { api, errorMessage } from '@/lib/api'
import type { AuditLog, Role, User } from '@/lib/types'
import { formatDate, timeAgo } from '@/lib/format'

function UserDialog({
  open,
  onOpenChange,
  user,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  user?: User
}) {
  const qc = useQueryClient()
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('member')
  const editing = !!user

  const save = useMutation({
    mutationFn: () =>
      editing
        ? api.patch<User>(`/users/${user.id}`, { password })
        : api.post<User>('/users', { email, name, password, role }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
      toast.success(editing ? 'Password updated' : 'User created')
      onOpenChange(false)
      setEmail('')
      setName('')
      setPassword('')
      setRole('member')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editing ? `Reset password for ${user.email}` : 'Add team member'}</DialogTitle>
          <DialogDescription>
            {editing ? 'All existing sessions of this user are signed out.' : 'Members can run and configure projects. Admins can also create, delete and manage users.'}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          {!editing && (
            <>
              <div className="grid gap-2">
                <Label htmlFor="u-email">Email</Label>
                <Input id="u-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="u-name">Name</Label>
                <Input id="u-name" value={name} onChange={(e) => setName(e.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label>Role</Label>
                <Select value={role} onValueChange={(v) => setRole(v as Role)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">Member</SelectItem>
                    <SelectItem value="admin">Admin</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </>
          )}
          <div className="grid gap-2">
            <Label htmlFor="u-pw">{editing ? 'New password' : 'Password'}</Label>
            <Input id="u-pw" type="password" minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
            <p className="text-muted-foreground text-xs">At least 8 characters.</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate()} disabled={save.isPending || password.length < 8 || (!editing && !email)}>
            {save.isPending && <Loader2 className="animate-spin" />}
            {editing ? 'Update password' : 'Add user'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function UsersPage() {
  const qc = useQueryClient()
  const { user: me } = useAuth()
  const [addOpen, setAddOpen] = useState(false)
  const [resetUser, setResetUser] = useState<User | undefined>()
  const [deleteUser, setDeleteUser] = useState<User | null>(null)

  const { data: users, isLoading } = useQuery({ queryKey: ['users'], queryFn: () => api.get<User[]>('/users') })
  const { data: audit } = useQuery({ queryKey: ['audit'], queryFn: () => api.get<AuditLog[]>('/audit') })
  const emailById = new Map((users ?? []).map((u) => [u.id, u.email]))

  const changeRole = useMutation({
    mutationFn: ({ id, role }: { id: number; role: Role }) => api.patch<User>(`/users/${id}`, { role }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
      toast.success('Role updated')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <PageContainer>
      <PageHeader
        title="Team"
        description="People who can sign in to this manager."
        actions={
          <Button onClick={() => setAddOpen(true)}>
            <Plus /> Add member
          </Button>
        }
      />
      <Tabs defaultValue="members">
        <TabsList>
          <TabsTrigger value="members">Members</TabsTrigger>
          <TabsTrigger value="audit">Audit log</TabsTrigger>
        </TabsList>
        <TabsContent value="members" className="mt-4">
          <div className="bg-card overflow-hidden rounded-lg border">
            {isLoading ? (
              <Skeleton className="m-4 h-24" />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>User</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Joined</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {users?.map((u) => (
                    <TableRow key={u.id}>
                      <TableCell>
                        <div className="font-medium">{u.name || u.email}</div>
                        <div className="text-muted-foreground text-xs">
                          {u.email}
                          {u.id === me?.id && ' (you)'}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Select value={u.role} onValueChange={(v) => changeRole.mutate({ id: u.id, role: v as Role })} disabled={u.id === me?.id}>
                          <SelectTrigger size="sm" className="w-28">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="member">Member</SelectItem>
                            <SelectItem value="admin">Admin</SelectItem>
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs">{timeAgo(u.created_at)}</TableCell>
                      <TableCell className="text-right">
                        <Button variant="ghost" size="icon-xs" aria-label="Reset password" onClick={() => setResetUser(u)}>
                          <KeyRound />
                        </Button>
                        <Button variant="ghost" size="icon-xs" aria-label="Delete user" disabled={u.id === me?.id} onClick={() => setDeleteUser(u)}>
                          <Trash2 />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
        </TabsContent>
        <TabsContent value="audit" className="mt-4">
          <div className="bg-card overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead>Details</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {audit?.map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className="text-muted-foreground text-xs whitespace-nowrap" title={formatDate(a.created_at)}>
                      {timeAgo(a.created_at)}
                    </TableCell>
                    <TableCell className="text-xs">{emailById.get(a.actor_id) ?? `#${a.actor_id}`}</TableCell>
                    <TableCell className="font-mono text-xs">{a.action}</TableCell>
                    <TableCell className="font-mono text-xs">{a.target}</TableCell>
                    <TableCell className="text-muted-foreground max-w-64 truncate font-mono text-xs">{a.metadata}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </TabsContent>
      </Tabs>

      <UserDialog open={addOpen} onOpenChange={setAddOpen} />
      <UserDialog open={!!resetUser} onOpenChange={(o) => !o && setResetUser(undefined)} user={resetUser} />
      <ConfirmDialog
        open={!!deleteUser}
        onOpenChange={(o) => !o && setDeleteUser(null)}
        title={`Remove ${deleteUser?.email}?`}
        description="They will be signed out and can no longer access the manager."
        confirmText="Remove user"
        destructive
        onConfirm={async () => {
          try {
            await api.delete(`/users/${deleteUser!.id}`)
            qc.invalidateQueries({ queryKey: ['users'] })
            toast.success('User removed')
          } catch (err) {
            toast.error(errorMessage(err))
            throw err
          }
        }}
      />
    </PageContainer>
  )
}
