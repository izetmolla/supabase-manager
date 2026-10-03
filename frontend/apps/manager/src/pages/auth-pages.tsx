import { useState, type FormEvent, type ReactNode } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { Logo } from '@/components/logo'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'
import { Alert, AlertDescription } from '@workspace/ui/components/alert'
import { FullPageSpinner } from '@/components/layout/layouts'
import { useAuth } from '@/hooks/use-auth'
import { errorMessage } from '@/lib/api'
import { AUTHOR_EMAIL, AUTHOR_MAILTO, AUTHOR_NAME } from '@/lib/author'

function AuthShell({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <div className="grid min-h-screen lg:grid-cols-2">
      <div className="flex flex-col px-8 py-8">
        <div className="flex items-center gap-2">
          <Logo />
          <span className="font-medium">Supabase Manager</span>
        </div>
        <div className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center gap-8">
          <div className="grid gap-1.5">
            <h1 className="text-3xl font-medium tracking-tight">{title}</h1>
            <p className="text-muted-foreground text-sm">{subtitle}</p>
          </div>
          {children}
        </div>
        <p className="text-muted-foreground text-center text-xs">
          Created by <span className="text-foreground font-medium">{AUTHOR_NAME}</span> ·{' '}
          <a href={AUTHOR_MAILTO} className="hover:text-foreground underline underline-offset-4">
            {AUTHOR_EMAIL}
          </a>
        </p>
      </div>
      <div className="bg-sidebar relative hidden overflow-hidden border-l lg:flex lg:items-center lg:justify-center">
        <div className="bg-brand/20 absolute -top-32 -right-32 size-96 rounded-full blur-3xl" />
        <div className="bg-brand/10 absolute -bottom-40 -left-20 size-96 rounded-full blur-3xl" />
        <div className="relative max-w-md px-10">
          <Logo className="mb-8 size-12" />
          <p className="text-2xl leading-snug font-medium">
            Run every Supabase project on this machine from one place.
          </p>
          <p className="text-muted-foreground mt-4 text-sm leading-relaxed">
            Create isolated local stacks with conflict-free ports, manage auth providers and secrets, run migrations and
            follow logs - without editing config.toml by hand.
          </p>
        </div>
      </div>
    </div>
  )
}

function useSubmit(fn: () => Promise<void>) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await fn()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return { error, busy, onSubmit }
}

export function LoginPage() {
  const { user, loading, needsSetup, login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const from = (location.state as { from?: string } | null)?.from || '/projects'
  // Set by the server when a proxied service (Studio, Mailpit) needs a session; those pages
  // are not part of this SPA, so they need a full page load.
  const next = new URLSearchParams(location.search).get('next')
  const external = next && next.startsWith('/') && !next.startsWith('//') ? next : null
  const { error, busy, onSubmit } = useSubmit(async () => {
    await login(email, password)
    if (external) window.location.replace(external)
    else navigate(from, { replace: true })
  })

  if (loading) return <FullPageSpinner />
  if (needsSetup) return <Navigate to="/setup" replace />
  if (user && external) {
    window.location.replace(external)
    return <FullPageSpinner />
  }
  if (user) return <Navigate to={from} replace />

  return (
    <AuthShell title="Welcome back" subtitle="Sign in to your account">
      <form onSubmit={onSubmit} className="grid gap-4">
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        <div className="grid gap-2">
          <Label htmlFor="email">Email</Label>
          <Input id="email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="password">Password</Label>
          <Input id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••••" />
        </div>
        <Button type="submit" size="lg" disabled={busy} className="mt-2">
          {busy && <Loader2 className="animate-spin" />}
          Sign in
        </Button>
      </form>
    </AuthShell>
  )
}

export function SetupPage() {
  const { user, loading, needsSetup, setup } = useAuth()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const { error, busy, onSubmit } = useSubmit(async () => {
    if (password !== confirm) throw new Error('Passwords do not match')
    await setup(name, email, password)
    navigate('/projects', { replace: true })
  })

  if (loading) return <FullPageSpinner />
  if (!needsSetup) return <Navigate to={user ? '/projects' : '/login'} replace />

  return (
    <AuthShell title="Create the admin account" subtitle="This is the first run. The account you create here manages all projects and users.">
      <form onSubmit={onSubmit} className="grid gap-4">
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        <div className="grid gap-2">
          <Label htmlFor="name">Name</Label>
          <Input id="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Jane Doe" />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="email">Email</Label>
          <Input id="email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="password">Password</Label>
          <Input id="password" type="password" required minLength={8} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="confirm">Confirm password</Label>
          <Input id="confirm" type="password" required minLength={8} autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </div>
        <Button type="submit" size="lg" disabled={busy} className="mt-2">
          {busy && <Loader2 className="animate-spin" />}
          Create account
        </Button>
      </form>
    </AuthShell>
  )
}
