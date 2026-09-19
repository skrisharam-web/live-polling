import { useCallback, useMemo, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError } from '../api/client'
import * as authApi from '../api/auth'
import type { User } from '../types/auth'
import { AuthContext, type AuthState } from './auth-context'

const meKey = ['auth', 'me'] as const

/**
 * Authentication state for the whole app.
 *
 * The session itself lives in an HTTP-only cookie, so this context holds no
 * token and has nothing to persist: it asks the server who the user is and
 * caches the answer. That is why signing out is a request rather than clearing
 * local storage — the credential is not ours to delete.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: meKey,
    queryFn: async ({ signal }) => {
      try {
        const { user } = await authApi.me(signal)
        return user
      } catch (error) {
        // Not being signed in is an ordinary answer, not a failure worth
        // retrying or surfacing.
        if (error instanceof ApiError && error.isUnauthorized) return null
        throw error
      }
    },
    retry: false,
    staleTime: 5 * 60 * 1000,
  })

  const setUser = useCallback(
    (user: User | null) => {
      queryClient.setQueryData(meKey, user)
    },
    [queryClient],
  )

  const signInMutation = useMutation({
    mutationFn: authApi.login,
    onSuccess: ({ user }) => setUser(user),
  })

  const signUpMutation = useMutation({
    mutationFn: authApi.register,
    onSuccess: ({ user }) => setUser(user),
  })

  const signOutMutation = useMutation({
    mutationFn: authApi.logout,
    onSuccess: () => {
      setUser(null)
      // Anything fetched as this user is no longer theirs to see.
      void queryClient.removeQueries({ queryKey: ['myPolls'] })
    },
  })

  const value = useMemo<AuthState>(
    () => ({
      user: query.data ?? null,
      isLoading: query.isPending,
      signIn: async (credentials) => {
        await signInMutation.mutateAsync(credentials)
      },
      signUp: async (registration) => {
        await signUpMutation.mutateAsync(registration)
      },
      signOut: async () => {
        await signOutMutation.mutateAsync()
      },
    }),
    [query.data, query.isPending, signInMutation, signUpMutation, signOutMutation],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
