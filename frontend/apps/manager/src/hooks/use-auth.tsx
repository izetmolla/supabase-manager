import { createContext, useCallback, useContext, useEffect, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { User } from '@/lib/types'

interface AuthContextValue {
  user: User | null
  loading: boolean
  needsSetup: boolean
  isAdmin: boolean
  login: (email: string, password: string) => Promise<void>
  setup: (name: string, email: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()

  const setupQuery = useQuery({
    queryKey: ['auth', 'setup'],
    queryFn: () => api.get<{ needs_setup: boolean }>('/auth/setup'),
    staleTime: Infinity,
  })
  const meQuery = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: () => api.get<User>('/auth/me').catch(() => null),
    enabled: setupQuery.data?.needs_setup === false,
    staleTime: 5 * 60 * 1000,
  })

  useEffect(() => {
    const onExpired = () => qc.setQueryData(['auth', 'me'], null)
    window.addEventListener('auth:expired', onExpired)
    return () => window.removeEventListener('auth:expired', onExpired)
  }, [qc])

  const login = useCallback(
    async (email: string, password: string) => {
      const user = await api.post<User>('/auth/login', { email, password })
      qc.setQueryData(['auth', 'me'], user)
    },
    [qc],
  )

  const setup = useCallback(
    async (name: string, email: string, password: string) => {
      const user = await api.post<User>('/auth/setup', { name, email, password })
      qc.setQueryData(['auth', 'setup'], { needs_setup: false })
      qc.setQueryData(['auth', 'me'], user)
    },
    [qc],
  )

  const logout = useCallback(async () => {
    await api.post('/auth/logout').catch(() => undefined)
    qc.clear()
    qc.setQueryData(['auth', 'setup'], { needs_setup: false })
    qc.setQueryData(['auth', 'me'], null)
  }, [qc])

  const user = meQuery.data ?? null
  const value: AuthContextValue = {
    user,
    loading: setupQuery.isLoading || (setupQuery.data?.needs_setup === false && meQuery.isLoading),
    needsSetup: setupQuery.data?.needs_setup ?? false,
    isAdmin: user?.role === 'admin',
    login,
    setup,
    logout,
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider')
  return ctx
}
