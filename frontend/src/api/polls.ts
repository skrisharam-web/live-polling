import { request } from './client'
import type { Poll, PollSummary } from '../types/poll'

export interface CreatePollInput {
  question: string
  options: string[]
  expiresAt?: string | null
}

export function createPoll(input: CreatePollInput): Promise<{ poll: Poll }> {
  return request('/api/polls', { method: 'POST', body: input })
}

/** Public: anyone holding the share link can read a poll. */
export function getPoll(pollId: string, signal?: AbortSignal): Promise<{ poll: Poll }> {
  return request(`/api/polls/${pollId}`, { signal })
}

/** Owner-only: answers 403 rather than quietly serving the public view. */
export function getOwnedPoll(pollId: string, signal?: AbortSignal): Promise<{ poll: Poll }> {
  return request(`/api/polls/${pollId}/manage`, { signal })
}

export function listMyPolls(signal?: AbortSignal): Promise<{ polls: PollSummary[] }> {
  return request('/api/me/polls', { signal })
}

export function closePoll(pollId: string): Promise<{ poll: Poll }> {
  return request(`/api/polls/${pollId}/close`, { method: 'POST', body: {} })
}

export function deletePoll(pollId: string): Promise<Record<string, never>> {
  return request(`/api/polls/${pollId}`, { method: 'DELETE' })
}
