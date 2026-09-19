import { ApiError } from '../api/client'

/**
 * Turns any thrown value into a message worth showing.
 *
 * Every API failure already carries a human-readable message from the backend,
 * so the job here is mostly to stop an unexpected JavaScript error leaking its
 * internals into the interface.
 */
export function messageFor(error: unknown, fallback = 'Something went wrong. Please try again.'): string {
  if (error instanceof ApiError) return error.message
  return fallback
}

/** Per-field validation messages, when the backend supplied them. */
export function fieldErrors(error: unknown): Record<string, string> {
  return error instanceof ApiError ? (error.fields ?? {}) : {}
}
