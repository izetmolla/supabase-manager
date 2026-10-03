import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Code2, FileCode, Loader2, Plus, RotateCw } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Skeleton } from '@workspace/ui/components/skeleton'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@workspace/ui/components/dialog'
import { PageContainer } from '@/components/layout/layouts'
import { EmptyState, PageHeader } from '@/components/page-header'
import { CopyField } from '@/components/copy-field'
import { useProject, useProjectStatus, useSlug } from '@/hooks/use-projects'
import { api, errorMessage } from '@/lib/api'
import type { EdgeFunction } from '@/lib/types'
import { timeAgo } from '@/lib/format'

export function FunctionsPage() {
  const slug = useSlug()
  const qc = useQueryClient()
  const { data: project } = useProject()
  const { data: status } = useProjectStatus()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')

  const { data: functions, isLoading } = useQuery({
    queryKey: ['project', slug, 'functions'],
    queryFn: () => api.get<EdgeFunction[]>(`/projects/${slug}/functions`),
  })
  const create = useMutation({
    mutationFn: () => api.post<EdgeFunction[]>(`/projects/${slug}/functions`, { name }),
    onSuccess: (fns) => {
      qc.setQueryData(['project', slug, 'functions'], fns)
      toast.success(`Function ${name} created`, { description: `supabase/functions/${name}/index.ts` })
      setOpen(false)
      setName('')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const restart = useMutation({
    mutationFn: () => api.post(`/projects/${slug}/containers/edge_runtime/restart`),
    onSuccess: () => toast.success('Edge runtime restarted'),
    onError: (err) => toast.error(errorMessage(err)),
  })

  const functionsUrl = status?.details?.functions_url
  const fns = (functions ?? []).filter((f) => !f.shared)
  const shared = (functions ?? []).filter((f) => f.shared)

  return (
    <PageContainer>
      <PageHeader
        title="Edge Functions"
        description="Deno functions in supabase/functions. They are served by the edge runtime while the project runs."
        actions={
          <>
            <Button variant="outline" onClick={() => restart.mutate()} disabled={project?.status !== 'running' || restart.isPending}>
              <RotateCw className={restart.isPending ? 'animate-spin' : ''} /> Restart runtime
            </Button>
            <Button onClick={() => setOpen(true)}>
              <Plus /> New function
            </Button>
          </>
        }
      />

      {isLoading ? (
        <Skeleton className="h-40" />
      ) : fns.length === 0 ? (
        <EmptyState
          icon={<Code2 />}
          title="No functions yet"
          description="Create a function to scaffold supabase/functions/<name>/index.ts."
          action={
            <Button onClick={() => setOpen(true)}>
              <Plus /> New function
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {fns.map((f) => (
            <div key={f.name} className="bg-card grid gap-4 rounded-lg border p-5">
              <div className="flex items-start justify-between gap-3">
                <div className="grid gap-0.5">
                  <h3 className="font-medium">{f.name}</h3>
                  <p className="text-muted-foreground text-xs">Modified {timeAgo(f.modified)}</p>
                </div>
                <Code2 className="text-brand size-5" />
              </div>
              <div className="flex flex-wrap gap-1.5">
                {f.files.map((file) => (
                  <span key={file} className="bg-muted text-muted-foreground inline-flex items-center gap-1 rounded px-1.5 py-0.5 font-mono text-[11px]">
                    <FileCode className="size-3" />
                    {file}
                  </span>
                ))}
              </div>
              {functionsUrl && <CopyField label="Endpoint" value={`${functionsUrl}/${f.name}`} />}
            </div>
          ))}
        </div>
      )}

      {shared.length > 0 && (
        <p className="text-muted-foreground mt-6 text-xs">
          Shared folders: {shared.map((s) => s.name).join(', ')}
        </p>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New edge function</DialogTitle>
            <DialogDescription>Runs supabase functions new and creates a hello-world template.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="fn-name">Function name</Label>
            <Input
              id="fn-name"
              value={name}
              onChange={(e) => setName(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, '-'))}
              onKeyDown={(e) => e.key === 'Enter' && name && create.mutate()}
              placeholder="hello-world"
              className="font-mono"
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button onClick={() => create.mutate()} disabled={!name || create.isPending}>
              {create.isPending && <Loader2 className="animate-spin" />}
              Create function
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </PageContainer>
  )
}
