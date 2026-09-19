/**
 * The API's wire format, mirrored from the backend's response package.
 *
 * Every endpoint answers with the same envelope, which is what lets one client
 * function handle every call and one error type describe every failure.
 */

export interface ApiEnvelope<T> {
  success: boolean
  data?: T
  error?: ApiErrorBody
}

export interface ApiErrorBody {
  /** A stable machine-readable code, e.g. ALREADY_VOTED. Safe to branch on. */
  code: string
  /** A message written for a person. Safe to show as-is. */
  message: string
  /** Per-field validation messages, keyed by the request field name. */
  fields?: Record<string, string>
}

/** Error codes the UI reacts to specifically. Any other code falls back to the message. */
export const ErrorCode = {
  Validation: 'VALIDATION_ERROR',
  Unauthorized: 'UNAUTHORIZED',
  Forbidden: 'FORBIDDEN',
  NotFound: 'NOT_FOUND',
  Conflict: 'CONFLICT',
  AlreadyVoted: 'ALREADY_VOTED',
  PollClosed: 'POLL_CLOSED',
  RateLimited: 'RATE_LIMITED',
  Network: 'NETWORK_ERROR',
} as const

export type ErrorCodeValue = (typeof ErrorCode)[keyof typeof ErrorCode]
