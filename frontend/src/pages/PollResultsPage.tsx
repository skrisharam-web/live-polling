import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { getPoll } from '../api/polls'
import { ApiError } from '../api/client'
import { LiveStatusBadge } from '../components/results/LiveStatusBadge'
import { ResultsAnnouncement, ResultsList } from '../components/results/ResultsList'
import { usePollResults } from '../hooks/usePollResults'

/**
 * The live results view.
 *
 * The bars are the page. Everything else — the question, the total, the
 * connection badge — is deliberately secondary, because this screen exists to be
 * watched rather than read.
 */
export default function PollResultsPage() {
  const { pollId } = useParams<{ pollId: string }>()

  const pollQuery = useQuery({
    queryKey: ['poll', pollId],
    queryFn: ({ signal }) => getPoll(pollId as string, signal),
    enabled: Boolean(pollId),
  })

  const { results, isLoading, error, connection, refresh } = usePollResults(pollId)

  if (pollQuery.isPending || isLoading) {
    return (
      <main className="page page--narrow" aria-busy="true">
        {/* A skeleton shaped like the real thing, so nothing jumps when it loads. */}
        <div className="skeleton skeleton--title" />
        <div className="skeleton skeleton--bar" />
        <div className="skeleton skeleton--bar" />
        <span className="sr-only">Loading results…</span>
      </main>
    )
  }

  const failure = pollQuery.error ?? error
  if (failure || !results || !pollQuery.data) {
    const notFound = failure instanceof ApiError && failure.isNotFound
    return (
      <main className="page page--narrow">
        <h1>{notFound ? 'That poll does not exist' : 'We could not load these results'}</h1>
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

  return (
    <main className="page page--narrow">
      <div className="results">
        <header className="results__heading">
          <h1 className="results__question">{poll.question}</h1>
          {results.status === 'closed' && <span className="badge badge--closed">Voting has closed</span>}
        </header>

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
    </main>
  )
}
