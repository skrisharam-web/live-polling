import { request } from './client'
import type { Credentials, Registration, User } from '../types/auth'

export function register(input: Registration): Promise<{ user: User }> {
  return request('/api/auth/register', { method: 'POST', body: input })
}

export function login(input: Credentials): Promise<{ user: User }> {
  return request('/api/auth/login', { method: 'POST', body: input })
}

export function logout(): Promise<Record<string, never>> {
  return request('/api/auth/logout', { method: 'POST', body: {} })
}

/** Answers 401 when there is no session, which the caller treats as "signed out". */
export function me(signal?: AbortSignal): Promise<{ user: User }> {
  return request('/api/auth/me', { signal })
}
