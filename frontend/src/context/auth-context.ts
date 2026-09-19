import { createContext } from 'react'

import type { Credentials, Registration, User } from '../types/auth'

export interface AuthState {
  user: User | null
  /** True only while we do not yet know whether there is a session. */
  isLoading: boolean
  signIn: (credentials: Credentials) => Promise<void>
  signUp: (registration: Registration) => Promise<void>
  signOut: () => Promise<void>
}

/**
 * Lives in its own module so the provider file exports components only, which is
 * what keeps Fast Refresh working during development.
 */
export const AuthContext = createContext<AuthState | null>(null)
