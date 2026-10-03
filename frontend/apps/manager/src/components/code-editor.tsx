import Editor, { DiffEditor, loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'
import TsWorker from 'monaco-editor/language/typescript/ts.worker?worker'
import { useTheme } from 'next-themes'

self.MonacoEnvironment = {
  getWorker(_id: string, label: string) {
    if (label === 'typescript' || label === 'javascript') return new TsWorker()
    return new EditorWorker()
  },
}
loader.config({ monaco })

monaco.editor.defineTheme('supabase-dark', {
  base: 'vs-dark',
  inherit: true,
  rules: [],
  colors: {
    'editor.background': '#1c1c1c',
    'editor.lineHighlightBackground': '#232323',
    'editorGutter.background': '#1c1c1c',
  },
})

export type EditorLanguage = 'sql' | 'toml' | 'typescript' | 'go' | 'python' | 'swift' | 'dart' | 'json' | 'nginx' | 'plaintext'

export interface CodeEditorProps {
  value: string
  onChange?: (value: string) => void
  language: EditorLanguage
  readOnly?: boolean
  height?: string | number
}

const monacoLanguage: Record<EditorLanguage, string> = {
  sql: 'sql',
  toml: 'ini',
  typescript: 'typescript',
  go: 'go',
  python: 'python',
  swift: 'swift',
  dart: 'dart',
  json: 'json',
  nginx: 'shell',
  plaintext: 'plaintext',
}

export interface CodeDiffProps {
  original: string
  modified: string
  language: EditorLanguage
  height?: string | number
}

export function CodeDiff({ original, modified, language, height = '60vh' }: CodeDiffProps) {
  const { resolvedTheme } = useTheme()
  return (
    <div className="overflow-hidden rounded-md border">
      <DiffEditor
        height={height}
        language={monacoLanguage[language]}
        original={original}
        modified={modified}
        theme={resolvedTheme === 'dark' ? 'supabase-dark' : 'light'}
        options={{
          readOnly: true,
          renderSideBySide: true,
          minimap: { enabled: false },
          fontSize: 12,
          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
          scrollBeyondLastLine: false,
          wordWrap: 'on',
        }}
      />
    </div>
  )
}

export default function CodeEditor({ value, onChange, language, readOnly, height = '60vh' }: CodeEditorProps) {
  const { resolvedTheme } = useTheme()
  return (
    <div className="overflow-hidden rounded-md border">
      <Editor
        height={height}
        language={monacoLanguage[language]}
        value={value}
        onChange={(v) => onChange?.(v ?? '')}
        theme={resolvedTheme === 'dark' ? 'supabase-dark' : 'light'}
        options={{
          readOnly,
          minimap: { enabled: false },
          fontSize: 13,
          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
          scrollBeyondLastLine: false,
          wordWrap: 'on',
          tabSize: 2,
          padding: { top: 12 },
          renderLineHighlight: 'line',
        }}
      />
    </div>
  )
}
