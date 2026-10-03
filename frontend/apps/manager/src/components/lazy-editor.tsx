import { lazy, Suspense } from 'react'
import { Skeleton } from '@workspace/ui/components/skeleton'
import type { CodeEditorProps } from '@/components/code-editor'

const CodeEditor = lazy(() => import('@/components/code-editor'))

export function LazyEditor(props: CodeEditorProps) {
  return (
    <Suspense fallback={<Skeleton style={{ height: props.height ?? '60vh' }} className="w-full" />}>
      <CodeEditor {...props} />
    </Suspense>
  )
}
