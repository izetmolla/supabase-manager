import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Download, FileCode2, GitCompare, Loader2, Plus, RefreshCw, RotateCcw, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Badge } from '@workspace/ui/components/badge'
import { Switch } from '@workspace/ui/components/switch'
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
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from '@workspace/ui/components/sheet'
import { EmptyState, PageHeader } from '@/components/page-header'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LazyEditor } from '@/components/lazy-editor'
import { useAuth } from '@/hooks/use-auth'
import { useJobAction, useProject, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { Migration } from '@/lib/types'
import { formatBytes, migrationDate } from '@/lib/format'

function NameDialog({
  open,
  onOpenChange,
  title,
  description,
  label,
  placeholder,
  optional,
  confirmText,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  title: string
  description: string
  label: string
  placeholder: string
  optional?: boolean
  confirmText: string
  onSubmit: (name: string) => Promise<unknown>
}) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    try {
      await onSubmit(name.trim())
      onOpenChange(false)
      setName('')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="name-input">{label}</Label>
          <Input
            id="name-input"
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, '_'))}
            onKeyDown={(e) => e.key === 'Enter' && (optional || name) && submit()}
            placeholder={placeholder}
            className="font-mono"
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || (!optional && !name)}>
            {busy && <Loader2 className="animate-spin" />}
            {confirmText}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MigrationSheet({ file, onClose }: { file: string | null; onClose: () => void }) {
  const slug = useSlug()
  const qc = useQueryClient()
  const [content, setContent] = useState('')
  const { data, isLoading } = useQuery({
    queryKey: ['project', slug, 'migration', file],
    queryFn: () => api.get<{ content: string }>(`/projects/${slug}/migrations/${file}`),
    enabled: !!file,
  })
  useEffect(() => {
    if (data) setContent(data.content)
  }, [data])

  const save = useMutation({
    mutationFn: () => api.put(`/projects/${slug}/migrations/${file}`, { content }),
    onSuccess: () => {
      toast.success('Migration saved')
      qc.invalidateQueries({ queryKey: ['project', slug, 'migrations'] })
      qc.invalidateQueries({ queryKey: ['project', slug, 'migration', file] })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <Sheet open={!!file} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-3xl">
        <SheetHeader className="border-b">
          <SheetTitle className="font-mono text-sm">{file}</SheetTitle>
          <SheetDescription>supabase/migrations/{file}</SheetDescription>
        </SheetHeader>
        <div className="flex-1 overflow-auto p-4">
          {isLoading ? <Skeleton className="h-[70vh]" /> : <LazyEditor language="sql" value={content} onChange={setContent} height="70vh" />}
        </div>
        <SheetFooter className="flex-row justify-end border-t">
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
          <Button onClick={() => save.mutate()} disabled={save.isPending || content === data?.content}>
            {save.isPending && <Loader2 className="animate-spin" />}
            Save
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

export function MigrationsPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { isAdmin } = useAuth()
  const { data: project } = useProject()
  const [newOpen, setNewOpen] = useState(false)
  const [diffOpen, setDiffOpen] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const [noSeed, setNoSeed] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const running = project?.status === 'running'

  const { data: migrations, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['project', slug, 'migrations'],
    queryFn: () => api.get<Migration[]>(`/projects/${slug}/migrations`),
  })
  const up = useJobAction('Apply pending migrations')
  const diff = useJobAction('Schema diff')
  const reset = useJobAction('Reset database')

  const pending = migrations?.filter((m) => m.applied === false).length ?? 0

  return (
    <>
      <PageHeader
        title="Migrations"
        description={
          running
            ? `${migrations?.length ?? 0} local migrations${pending ? `, ${pending} not applied` : ''}`
            : 'Start the project to see which migrations are applied.'
        }
        actions={
          <>
            <Button variant="ghost" size="icon" onClick={() => refetch()} aria-label="Refresh">
              <RefreshCw className={isFetching ? 'animate-spin' : ''} />
            </Button>
            <Button variant="outline" onClick={() => setDiffOpen(true)} disabled={!running}>
              <GitCompare /> Diff schema
            </Button>
            <Button variant="outline" onClick={() => up.mutate({ path: `/projects/${slug}/db/up` })} disabled={!running || up.isPending}>
              <Upload /> Apply pending
            </Button>
            <Button onClick={() => setNewOpen(true)}>
              <Plus /> New migration
            </Button>
          </>
        }
      />

      {isLoading ? (
        <Skeleton className="h-48" />
      ) : !migrations?.length ? (
        <EmptyState
          icon={<FileCode2 />}
          title="No migrations yet"
          description="Create a migration, or make changes in Studio and use 'Diff schema' to capture them as a migration file."
          action={
            <Button onClick={() => setNewOpen(true)}>
              <Plus /> New migration
            </Button>
          }
        />
      ) : (
        <div className="bg-card overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Version</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Size</TableHead>
                <TableHead className="text-right">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {migrations.map((m) => (
                <TableRow key={m.file} className="cursor-pointer" onClick={() => setSelected(m.file)}>
                  <TableCell className="font-mono text-xs">
                    <div>{m.version}</div>
                    <div className="text-muted-foreground">{migrationDate(m.version)}</div>
                  </TableCell>
                  <TableCell className="font-medium">{m.name}</TableCell>
                  <TableCell className="text-muted-foreground text-xs">{formatBytes(m.size)}</TableCell>
                  <TableCell className="text-right">
                    {m.applied === null ? (
                      <Badge variant="outline">unknown</Badge>
                    ) : m.applied ? (
                      <Badge variant="secondary" className="text-brand">applied</Badge>
                    ) : (
                      <Badge variant="secondary" className="text-amber-400">pending</Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {isAdmin && (
        <div className="border-destructive/30 mt-8 flex flex-wrap items-center justify-between gap-4 rounded-lg border px-5 py-4">
          <div className="grid gap-0.5">
            <p className="text-sm font-medium">Reset local database</p>
            <p className="text-muted-foreground text-xs">Drops all data and re-applies every migration and the seed file.</p>
          </div>
          <Button variant="destructive" onClick={() => setResetOpen(true)} disabled={!running}>
            <RotateCcw /> Reset database
          </Button>
        </div>
      )}

      <NameDialog
        open={newOpen}
        onOpenChange={setNewOpen}
        title="New migration"
        description="Creates an empty, timestamped SQL file in supabase/migrations."
        label="Migration name"
        placeholder="create_profiles_table"
        confirmText="Create"
        onSubmit={async (name) => {
          try {
            const res = await api.post<{ file: string }>(`/projects/${slug}/migrations`, { name })
            await qc.invalidateQueries({ queryKey: ['project', slug, 'migrations'] })
            toast.success(`Created ${res.file}`)
            if (res.file) setSelected(res.file)
          } catch (err) {
            toast.error(errorMessage(err))
            throw err
          }
        }}
      />
      <NameDialog
        open={diffOpen}
        onOpenChange={setDiffOpen}
        title="Diff schema"
        description="Compares the local database with your migration files. Leave the name empty to only print the SQL, or enter a name to save it as a new migration."
        label="Save as migration (optional)"
        placeholder="add_todos_policies"
        optional
        confirmText="Run diff"
        onSubmit={(name) =>
          diff.mutateAsync({ path: `/projects/${slug}/db/diff`, body: name ? { file: name } : {} }).then(() =>
            setTimeout(() => qc.invalidateQueries({ queryKey: ['project', slug, 'migrations'] }), 4000),
          )
        }
      />
      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title="Reset the local database?"
        description="All data in this project's local database will be deleted, then migrations and seed data are re-applied."
        typeToConfirm={slug}
        confirmText="Reset database"
        destructive
        onConfirm={async () => {
          await reset.mutateAsync({ path: `/projects/${slug}/db/reset`, body: { confirm: slug, no_seed: noSeed } })
        }}
      >
        <label className="flex items-center gap-3">
          <Switch checked={noSeed} onCheckedChange={setNoSeed} />
          <span className="text-sm">Skip seed data</span>
        </label>
      </ConfirmDialog>
      <MigrationSheet file={selected} onClose={() => setSelected(null)} />
    </>
  )
}

const languages = ['typescript', 'go', 'python', 'swift', 'dart'] as const
type Lang = (typeof languages)[number]
const extensions: Record<Lang, string> = { typescript: 'ts', go: 'go', python: 'py', swift: 'swift', dart: 'dart' }

export function TypesPage() {
  const slug = useSlug()
  const { data: project } = useProject()
  const [lang, setLang] = useState<Lang>('typescript')
  const gen = useMutation({
    mutationFn: () => api.get<{ content: string }>(`/projects/${slug}/types?lang=${lang}`),
    onError: (err) => toast.error(errorMessage(err)),
  })
  const content = gen.data?.content ?? ''

  const download = () => {
    const blob = new Blob([content], { type: 'text/plain' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `database.types.${extensions[lang]}`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  return (
    <>
      <PageHeader
        title="Generated Types"
        description="Generate type definitions from your local database schema with supabase gen types."
        actions={
          <>
            <Select value={lang} onValueChange={(v) => setLang(v as Lang)}>
              <SelectTrigger className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {languages.map((l) => (
                  <SelectItem key={l} value={l}>
                    {l}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button onClick={() => gen.mutate()} disabled={gen.isPending || project?.status !== 'running'}>
              {gen.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              Generate
            </Button>
          </>
        }
      />
      {project?.status !== 'running' && <p className="text-muted-foreground mb-4 text-sm">Start the project to generate types.</p>}
      {content ? (
        <div className="grid gap-3">
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={async () => {
                await navigator.clipboard.writeText(content)
                toast.success('Types copied')
              }}
            >
              <Copy /> Copy
            </Button>
            <Button variant="outline" size="sm" onClick={download}>
              <Download /> Download
            </Button>
          </div>
          <LazyEditor language={lang} value={content} readOnly height="65vh" />
        </div>
      ) : (
        <EmptyState icon={<FileCode2 />} title="No types generated yet" description="Pick a language and click Generate." />
      )}
    </>
  )
}
