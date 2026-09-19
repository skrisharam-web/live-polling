import { request } from './client'
import type { PollResults } from '../types/poll'

/**
 * Casting a vote returns the new standing, so the voter sees the outcome of their
 * own vote without a second round trip.
 *
 * There is no voter field: the backend identifies the browser from a signed
 * HTTP-only cookie, which is what makes duplicate prevention meaningful.
 */
export function castVote(pollId: string, optionId: string): Promise<PollResults> {
  return request(`/api/polls/${pollId}/vote`, { method: 'POST', body: { optionId } })
}

export function getResults(pollId: string, signal?: AbortSignal): Promise<PollResults> {
  return request(`/api/polls/${pollId}/results`, { signal })
}
