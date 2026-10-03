import { useEffect, useState } from 'react'
import { Cable, Loader2, Pencil, Plus, Trash2 } from 'lucide-react'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Skeleton } from '@workspace/ui/components/skeleton'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@workspace/ui/components/select'
import { Switch } from '@workspace/ui/components/switch'
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
import { PM, useInstances, usePMMutation, useStreams, useUpstreams } from '@/hooks/use-proxy-manager'
import { api } from '@/lib/api'
import type { ProxyStream } from '@/lib/types'
import { Field, InstancePicker, NumberInput, ToggleRow } from './shared'

const blank = (): Omit<ProxyStream, 'id'> => ({ name: '', protocol: 'tcp', listen_port: 0, upstream_id: 0, instance_ids: [], enabled: true })

function StreamDialog({ open, onOpenChange, stream }: { open: boolean; onOpenChange: (o: boolean) => void; stream?: ProxyStream }) {
  const { data: upstreams = [] } = useUpstreams()
  const [f, setF] = useState(blank())
  useEffect(() => {
    if (open) setF(stream ? { ...stream } : blank())
  }, [open, stream])
  const set = (patch: Partial<ProxyStream>) => setF((cur) => ({ ...cur, ...patch }))
  const save = usePMMutation(() => (stream ? api.put(`${PM}/streams/${stream.id}`, f) : api.post(`${PM}/streams`, f)), {
    success: 'Stream saved',
    onSuccess: () => onOpenChange(false),
  })
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{stream ? 'Edit stream' : 'New TCP/UDP stream'}</DialogTitle>
          <DialogDescription>Forward a raw TCP or UDP port (databases, SMTP, game servers) to an upstream.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid grid-cols-3 gap-4">
            <Field label="Name">
              <Input value={f.name} onChange={(e) => set({ name: e.target.value })} placeholder="postgres" />
            </Field>
            <Field label="Protocol">
              <Select value={f.protocol} onValueChange={(v) => set({ protocol: v as 'tcp' | 'udp' })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="tcp">TCP</SelectItem>
                  <SelectItem value="udp">UDP</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label="Listen port">
              <NumberInput value={f.listen_port} onChange={(v) => set({ listen_port: v })} placeholder="5432" />
            </Field>
          </div>
          <Field label="Upstream">
            <Select value={f.upstream_id ? String(f.upstream_id) : ''} onValueChange={(v) => set({ upstream_id: Number(v) })}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Choose an upstream" />
              </SelectTrigger>
              <SelectContent>
                {upstreams.map((u) => (
                  <SelectItem key={u.id} value={String(u.id)}>
                    {u.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="Served by" hint="Listens on the bind IP of each selected instance.">
            <InstancePicker value={f.instance_ids} onChange={(v) => set({ instance_ids: v })} />
          </Field>
          <ToggleRow label="Enabled" checked={f.enabled} onChange={(v) => set({ enabled: v })} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => save.mutate(undefined)} disabled={save.isPending || !f.listen_port || !f.upstream_id}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save stream
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function StreamsPage() {
  const { data: streams, isLoading } = useStreams()
  const { data: upstreams = [] } = useUpstreams()
  const { data: instances = [] } = useInstances()
  const [open, setOpen] = useState(false)
  const [edit, setEdit] = useState<ProxyStream>()
  const [del, setDel] = useState<ProxyStream | null>(null)
  const save = usePMMutation((s: ProxyStream) => api.put(`${PM}/streams/${s.id}`, s))
  const remove = usePMMutation((id: number) => api.delete(`${PM}/streams/${id}`), { success: 'Stream deleted' })
  const upName = new Map(upstreams.map((u) => [u.id, u.name]))
  const instName = new Map(instances.map((i) => [i.id, i.name]))

  return (
    <>
      <PageHeader
        title="Streams"
        description="Layer 4 TCP and UDP forwarding."
        actions={
          <Button
            onClick={() => {
              setEdit(undefined)
              setOpen(true)
            }}
          >
            <Plus /> New stream
          </Button>
        }
      />
      {isLoading ? (
        <Skeleton className="h-24" />
      ) : !streams?.length ? (
        <EmptyState icon={<Cable />} title="No streams" description="Expose a database or any TCP/UDP service through a proxy instance." />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Listen</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Upstream</TableHead>
                <TableHead>Instances</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {streams.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="font-mono text-xs">
                    {s.listen_port}/{s.protocol}
                  </TableCell>
                  <TableCell>{s.name}</TableCell>
                  <TableCell className="text-xs">{upName.get(s.upstream_id) ?? '-'}</TableCell>
                  <TableCell className="text-xs">{(s.instance_ids ?? []).map((i) => instName.get(i) ?? `#${i}`).join(', ') || <span className="text-amber-500">none</span>}</TableCell>
                  <TableCell>
                    <Switch checked={s.enabled} onCheckedChange={(v) => save.mutate({ ...s, enabled: v })} />
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="Edit"
                      onClick={() => {
                        setEdit(s)
                        setOpen(true)
                      }}
                    >
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon-xs" aria-label="Delete" onClick={() => setDel(s)}>
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <StreamDialog open={open} onOpenChange={setOpen} stream={edit} />
      <ConfirmDialog
        open={!!del}
        onOpenChange={(o) => !o && setDel(null)}
        title={`Delete stream ${del?.listen_port}/${del?.protocol}?`}
        description="The port stops being forwarded after the next deploy."
        confirmText="Delete"
        destructive
        onConfirm={async () => {
          await remove.mutateAsync(del!.id)
        }}
      />
    </>
  )
}
