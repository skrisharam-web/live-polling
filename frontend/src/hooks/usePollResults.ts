import { useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { getResults } from '../api/votes'
import type { PollResults, PollResultsUpdatedEvent } from '../types/poll'
import { usePollWebSocket, type ConnectionState } from './usePollWebSocket'

/** The query key for one poll's results, shared by the fetch and the socket. */
export const resultsKey = (pollId: string) => ['results', pollId] as const

interface Options {
  enabled?: boolean
}

interface LiveResults {
  results: PollResults | undefined
  isLoading: boolean
  error: Error | null
  connection: ConnectionState
  /** Reconnect and refetch, for the control shown when the socket has given up. */
  refresh: () => void
}

/**
 * Poll results that keep themselves current.
 *
 * The REST fetch and the WebSocket are not two sources of truth: the socket
 * writes into the same cache entry the fetch populates, so the component reads
 * one value and never has to decide which is fresher. The fetch gives a correct
 * starting point (and a correct one after any gap); the socket keeps it moving.
 */
export function usePollResults(pollId: string | undefined, options: Options = {}): LiveResults {
  const { enabled = true } = options
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: resultsKey(pollId ?? ''),
    queryFn: ({ signal }) => getResults(pollId as string, signal),
    enabled: Boolean(pollId) && enabled,
    // The socket is the update mechanism. Refetching on window focus as well
    // would mean two sources racing to write the same cache entry, and would
    // quietly hide a broken socket behind polling — exactly what this project
    // must not do.
    refetchOnWindowFocus: false,
    staleTime: Infinity,
  })

  const applyEvent = useCallback(
    (event: PollResultsUpdatedEvent) => {
      queryClient.setQueryData<PollResults>(resultsKey(event.pollId), (current) => {
        // The event carries counts but not option text, so the text comes from
        // what we already have. Before the first fetch lands there is nothing to
        // merge into, and the fetch will arrive with the same numbers anyway.
        if (!current) return current

        const counts = new Map(event.results.map((row) => [row.optionId, row.count]))
        return {
          ...current,
          results: current.results.map((option) => ({
            ...option,
            count: counts.get(option.optionId) ?? option.count,
          })),
          totalVotes: event.totalVotes,
          status: event.status,
          computedAt: event.timestamp,
        }
      })
    },
    [queryClient],
  )

  const refetch = useCallback(() => {
    if (!pollId) return
    void queryClient.invalidateQueries({ queryKey: resultsKey(pollId) })
  }, [pollId, queryClient])

  const { status, reconnect } = usePollWebSocket(pollId, {
    onEvent: applyEvent,
    // Anything published while the socket was down is unrecoverable, so the only
    // honest response to a reconnect is to ask the server what the truth is.
    onReconnect: refetch,
    enabled: enabled && Boolean(pollId),
  })

  const refresh = useCallback(() => {
    refetch()
    reconnect()
  }, [refetch, reconnect])

  return {
    results: query.data,
    isLoading: query.isPending && Boolean(pollId) && enabled,
    error: query.error,
    connection: status,
    refresh,
  }
}
