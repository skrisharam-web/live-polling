import { ErrorCode, type ApiEnvelope, type ApiErrorBody } from '../types/api'

/**
 * The single place the frontend talks to the backend.
 *
 * Two things are centralised here on purpose. Every request sends credentials,
 * because the session and voter identities live in HTTP-only cookies that
 * JavaScript cannot read or attach by hand. And every failure — an API error, a
 * dead network, a body that is not JSON — arrives at the caller as one
 * ApiError type, so no component has to handle three shapes of bad news.
 */

const BASE_URL = (import.meta.env.VITE_API_URL ?? '').replace(/\/$/, '')

export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly fields?: Record<string, string>

  constructor(body: ApiErrorBody, status: number) {
    super(body.message)
    this.name = 'ApiError'
    this.code = body.code
    this.status = status
    this.fields = body.fields
  }

  /** True when the user simply is not signed in, which is not worth shouting about. */
  get isUnauthorized(): boolean {
    return this.code === ErrorCode.Unauthorized
  }

  get isNotFound(): boolean {
    return this.code === ErrorCode.NotFound
  }

  /** True when the server was unreachable, as opposed to answering with an error. */
  get isNetworkFailure(): boolean {
    return this.code === ErrorCode.Network
  }
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, signal } = options

  let response: Response
  try {
    response = await fetch(`${BASE_URL}${path}`, {
      method,
      // Without this the browser sends no cookies, and every request would look
      // anonymous to the backend.
      credentials: 'include',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    })
  } catch (cause) {
    // fetch only rejects when the request never completed: offline, DNS, CORS.
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw new ApiError(
      { code: ErrorCode.Network, message: 'Could not reach the server. Check your connection and try again.' },
      0,
    )
  }

  let envelope: ApiEnvelope<T> | undefined
  try {
    envelope = (await response.json()) as ApiEnvelope<T>
  } catch {
    envelope = undefined
  }

  if (!response.ok || envelope?.success === false) {
    throw new ApiError(
      envelope?.error ?? { code: 'INTERNAL_ERROR', message: 'Something went wrong. Please try again.' },
      response.status,
    )
  }

  // A successful response with no data (logout, delete) is legitimate.
  return (envelope?.data ?? ({} as T)) as T
}

/** The WebSocket origin, used by the realtime hook. */
export const WS_BASE_URL = (import.meta.env.VITE_WS_URL ?? '').replace(/\/$/, '')
