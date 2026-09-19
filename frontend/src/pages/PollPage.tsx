import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError } from '../api/client'
import { getPoll } from '../api/polls'
import { castVote } from '../api/votes'
import { LiveStatusBadge } from '../components/results/LiveStatusBadge'
import { ResultsAnnouncement, ResultsList } from '../components/results/ResultsList'
import { VoteForm } from '../components/polls/VoteForm'
import { usePollResults, resultsKey } from '../hooks/usePollResults'
import { ErrorCode } from '../types/api'
import type { PollResults } from '../types/poll'

/**
 * The public poll: the screen the audience actually uses.
 *
 * Voting and results are one view, not two pages. Submitting a vote replaces the
 * ballot with the standing in place — no navigation, no reload — because the
 * moment after voting is the moment someone most wants to see where things
 * stand.
 */
export default function PollPage() {
  const { pollId } = useParams<{ pollId: string }>()
  const queryClient = useQueryClient()
  const [voteError, setVoteError] = useState<string | null>(null)
  const [alreadyVoted, setAlreadyVoted] = useState(false)

  const pollQuery = useQuery({
    queryKey: ['poll', pollId],
    queryFn: ({ signal }) => getPoll(pollId as string, signal),
    enabled: Boolean(pollId),
  })

  const { results, isLoading, error, connection, refresh } = usePollResults(pollId)

  const voteMutation = useMutation({
    mutationFn: (optionId: string) => castVote(pollId as string, optionId),
    onSuccess: (updated) => {
      setVoteError(null)
      // The response is the new standing, so the view can move straight to it.
      queryClient.setQueryData<PollResults>(resultsKey(pollId as string), updated)
    },
    onError: (cause) => {
      if (cause instanceof ApiError && cause.code === ErrorCode.AlreadyVoted) {
        // Not an error to shout about: they already took part. Show them where
        // things stand and say so plainly.
        setVoteError(null)
        void queryClient.invalidateQueries({ queryKey: resultsKey(pollId as string) })
        setAlreadyVoted(true)
        return
      }
      setVoteError(cause instanceof ApiError ? cause.message : 'Your vote could not be recorded. Please try again.')
    },
  })

  if (pollQuery.isPending || isLoading) {
    return (
      <main className="page page--narrow" id="main" aria-busy="true">
        <div className="skeleton skeleton--title" />
        <div className="skeleton skeleton--bar" />
        <div className="skeleton skeleton--bar" />
        <span className="sr-only">Loading poll…</span>
      </main>
    )
  }

  const failure = pollQuery.error ?? error
  if (failure || !pollQuery.data || !results) {
    const notFound = failure instanceof ApiError && failure.isNotFound
    return (
      <main className="page page--narrow" id="main">
        <h1>{notFound ? 'That poll does not exist' : 'We could not load this poll'}</h1>
        <p className="page__lead">
          {notFound
            ? 'The link may be wrong, or the poll may have been deleted.'
            : 'The server did not answer. Your connection may be down.'}
        </p>
        {!notFound && (
          <button type="button" className="button button--primary" onClick={refresh}>
            Try again
          </button>
        )}
      </main>
    )
  }

  const poll = pollQuery.data.poll
  const hasVoted = Boolean(results.yourVote) || alreadyVoted
  const isClosed = results.status === 'closed'
  const canVote = poll.acceptsVotes && !isClosed && !hasVoted

  return (
    <main className="page page--narrow" id="main">
      <div className="stack">
        <h1 className="poll__question">{poll.question}</h1>
        {isClosed && <span className="badge badge--closed">Voting has closed</span>}
      </div>

      {canVote ? (
        <VoteForm
          poll={poll}
          isSubmitting={voteMutation.isPending}
          error={voteError}
          onSubmit={(optionId) => voteMutation.mutate(optionId)}
        />
      ) : (
        <div className="results">
          {hasVoted && !isClosed && (
            <p className="notice notice--success" role="status">
              {alreadyVoted && !results.yourVote
                ? 'You have already voted in this poll.'
                : 'Your vote was recorded. Thanks for taking part.'}
            </p>
          )}

          <div className="results__header">
            <p className="results__total">
              <strong>{results.totalVotes}</strong> {results.totalVotes === 1 ? 'vote' : 'votes'}
            </p>
            <LiveStatusBadge status={connection} onRetry={refresh} />
          </div>

          <ResultsList results={results} yourVote={results.yourVote} />
          <ResultsAnnouncement results={results} />

          {results.totalVotes === 0 && (
            <p className="results__empty">Waiting for the first vote. Results appear here as they come in.</p>
          )}
        </div>
      )}
    </main>
  )
}
