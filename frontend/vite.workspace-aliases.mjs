import path from "node:path"

/** Vite resolve aliases for workspace packages (TypeScript paths alone are not enough). */
export function workspaceAliases(frontendRoot) {
  const uiRoot = path.join(frontendRoot, "packages/ui/src")

  return {
    // More specific paths first — `@workspace/ui` alone would break `globals.css`.
    "@workspace/ui/globals.css": path.join(uiRoot, "styles/globals.css"),
    "@workspace/ui": uiRoot,
  }
}
