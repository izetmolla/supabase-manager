import path from 'node:path'
import { fileURLToPath } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

import { workspaceAliases } from '../../vite.workspace-aliases.mjs'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const frontendRoot = path.resolve(__dirname, '../..')
// The Go service embeds this folder (services/manager/web/embed.go), so the UI ships inside the binary.
const outDir = path.resolve(frontendRoot, '../services/manager/web/dist')

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
      ...workspaceAliases(frontendRoot),
    },
    dedupe: ['react', 'react-dom', 'radix-ui'],
  },
  build: {
    outDir,
    emptyOutDir: true,
    chunkSizeWarningLimit: 3000,
  },
  server: {
    port: 5173,
    // The Host header is kept so the API's same-origin check matches the browser's Origin.
    // Studio is served at the root (/project/<slug>, /_next, ...), so its paths are forwarded too.
    proxy: Object.fromEntries(
      ['/api', '/proxy', '/project/', '/_next', '/favicon/', '/img/', '/monaco-editor'].map((p) => [
        p,
        { target: process.env.VITE_API_PROXY_TARGET ?? 'http://127.0.0.1:8080', ws: p === '/proxy' },
      ]),
    ),
  },
})
