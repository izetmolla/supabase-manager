import { lazy, Suspense } from 'react'
import { Skeleton } from '@workspace/ui/components/skeleton'
import type { CodeDiffProps, CodeEditorProps } from '@/components/code-editor'

const CodeEditor = lazy(() => import('@/components/code-editor'))
const CodeDiff = lazy(() => import('@/components/code-editor').then((m) => ({ default: m.CodeDiff })))

export function LazyEditor(props: CodeEditorProps) {
  return (
    <Suspense fallback={<Skeleton style={{ height: props.height ?? '60vh' }} className="w-full" />}>
      <CodeEditor {...props} />
    </Suspense>
  )
}

export function LazyDiff(props: CodeDiffProps) {
  return (
    <Suspense fallback={<Skeleton style={{ height: props.height ?? '60vh' }} className="w-full" />}>
      <CodeDiff {...props} />
    </Suspense>
  )
}
