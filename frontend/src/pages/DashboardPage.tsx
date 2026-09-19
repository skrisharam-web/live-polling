import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { listMyPolls } from '../api/polls'
import { useAuth } from '../hooks/useAuth'
import { formatDate, pluralise } from '../utils/formatting'
import { messageFor } from '../utils/errors'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useDelayedFlag } from '../hooks/useDelayedFlag'

/**
 * The signed-in user's polls.
 *
 * A list of rows rather than a grid of cards: these are records to scan, and a
 * card grid would spend most of its pixels on borders and shadows while fitting
 * fewer polls on a phone.
 */
export default function DashboardPage() {
  useDocumentTitle('Your polls')
  const { user } = useAuth()

  const query = useQuery({
    queryKey: ['myPolls'],
    queryFn: ({ signal }) => listMyPolls(signal),
  })

  const showSkeleton = useDelayedFlag(query.isPending)

  return (
    <main className="page" id="main">
      <div className="section-title">
        <h1>Your polls</h1>
        <Link to="/polls/new" className="button button--primary">
          New poll
        </Link>
      </div>

      {query.isPending && showSkeleton && (
        <ul className="poll-list" aria-busy="true">
          {[0, 1, 2].map((row) => (
            <li key={row} className="poll-row">
              <div className="poll-row__main">
                <div className="skeleton skeleton--title" />
              </div>
            </li>
          ))}
          <span className="sr-only">Loading your polls…</span>
        </ul>
      )}

      {query.error && (
        <div className="stack">
          <p className="notice notice--error" role="alert">
            {messageFor(query.error, 'We could not load your polls.')}
          </p>
          <div>
            <button type="button" className="button button--secondary" onClick={() => void query.refetch()}>
              Try again
            </button>
          </div>
        </div>
      )}

      {query.data && query.data.polls.length === 0 && (
        <div className="empty">
          <p className="empty__text">
            No polls yet, {user?.name.split(' ')[0]}. Create your first one and share the link — results
            appear here as people vote.
          </p>
          <Link to="/polls/new" className="button button--primary">
            Create a poll
          </Link>
        </div>
      )}

      {query.data && query.data.polls.length > 0 && (
        <ul className="poll-list">
          {query.data.polls.map((poll) => (
            <li key={poll.id} className="poll-row">
              <div className="poll-row__main">
                <h2 className="poll-row__question">
                  <Link to={`/polls/${poll.id}/results`}>{poll.question}</Link>
                </h2>
                <p className="poll-row__meta">
                  <span className={`badge badge--${poll.status}`}>
                    {poll.status === 'active' ? 'Open' : 'Closed'}
                  </span>
                  <span>{pluralise(poll.totalVotes, 'vote')}</span>
                  <span>·</span>
                  <span>{formatDate(poll.createdAt)}</span>
                </p>
              </div>

              <div className="poll-row__actions">
                <Link to={`/polls/${poll.id}/results`} className="button button--secondary button--small">
                  Results
                </Link>
                <Link to={`/polls/${poll.id}/manage`} className="button button--secondary button--small">
                  Manage
                </Link>
              </div>
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}
