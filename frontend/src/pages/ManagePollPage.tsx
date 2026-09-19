import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError } from '../api/client'
import { closePoll, deletePoll, getOwnedPoll } from '../api/polls'
import { LiveStatusBadge } from '../components/results/LiveStatusBadge'
import { ResultsAnnouncement, ResultsList } from '../components/results/ResultsList'
import { ShareLink } from '../components/polls/ShareLink'
import { usePollResults } from '../hooks/usePollResults'
import { messageFor } from '../utils/errors'
import { formatDate, shareUrl } from '../utils/formatting'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useDelayedFlag } from '../hooks/useDelayedFlag'

/**
 * Managing a poll you own.
 *
 * The share link is first because sharing is what an owner comes here to do most
 * often. Closing and deleting are consequential, so both confirm in place rather
 * than firing on a single click — and never through a browser confirm() dialog,
 * which cannot be styled, read well on a phone, or explain itself.
 */
export default function ManagePollPage() {
  const { pollId } = useParams<{ pollId: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [confirming, setConfirming] = useState<'close' | 'delete' | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const pollQuery = useQuery({
    queryKey: ['ownedPoll', pollId],
    queryFn: ({ signal }) => getOwnedPoll(pollId as string, signal),
    enabled: Boolean(pollId),
    retry: false,
  })

  useDocumentTitle(pollQuery.data ? `${pollQuery.data.poll.question} — manage` : 'Loading poll')

  const { results, connection, refresh } = usePollResults(pollId, { enabled: !pollQuery.isError })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['ownedPoll', pollId] })
    void queryClient.invalidateQueries({ queryKey: ['poll', pollId] })
    void queryClient.invalidateQueries({ queryKey: ['myPolls'] })
  }

  const closeMutation = useMutation({
    mutationFn: () => closePoll(pollId as string),
    onSuccess: () => {
      setConfirming(null)
      setActionError(null)
      invalidate()
    },
    onError: (cause) => setActionError(messageFor(cause, 'The poll could not be closed.')),
  })

  const deleteMutation = useMutation({
    mutationFn: () => deletePoll(pollId as string),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['myPolls'] })
      void navigate('/dashboard')
    },
    onError: (cause) => setActionError(messageFor(cause, 'The poll could not be deleted.')),
  })

  const isBusy = pollQuery.isPending
  const showSkeleton = useDelayedFlag(isBusy)

  if (isBusy) {
    // Nothing is rendered for a wait too short to notice; a skeleton that
    // flashes in and out makes a fast page feel unstable.
    if (!showSkeleton) return <main className="page page--narrow" id="main" aria-busy="true" />

    return (
      <main className="page page--narrow" id="main" aria-busy="true">
        <div className="skeleton skeleton--title" />
        <div className="skeleton skeleton--bar" />
        <span className="sr-only">Loading poll…</span>
      </main>
    )
  }

  if (pollQuery.error || !pollQuery.data) {
    const error = pollQuery.error
    const forbidden = error instanceof ApiError && error.code === 'FORBIDDEN'
    const notFound = error instanceof ApiError && error.isNotFound

    return (
      <main className="page page--narrow" id="main">
        <h1>{forbidden ? 'This poll is not yours' : notFound ? 'That poll does not exist' : 'We could not load this poll'}</h1>
        <p className="page__lead">
          {forbidden
            ? 'Only the person who created a poll can manage it.'
            : notFound
              ? 'The link may be wrong, or the poll may have been deleted.'
              : 'The server did not answer. Your connection may be down.'}
        </p>
        <div>
          <Link to="/dashboard" className="button button--secondary">
            Back to your polls
          </Link>
        </div>
      </main>
    )
  }

  const poll = pollQuery.data.poll
  const isClosed = poll.status === 'closed'

  return (
    <main className="page page--narrow" id="main">
      <div className="stack">
        <h1 className="poll__question">{poll.question}</h1>
        <p className="poll-row__meta">
          <span className={`badge badge--${poll.status}`}>{isClosed ? 'Closed' : 'Open'}</span>
          <span>Created {formatDate(poll.createdAt)}</span>
        </p>
      </div>

      <ShareLink url={shareUrl(poll.id)} />

      {results && (
        <div className="results">
          <div className="results__header">
            <p className="results__total">
              <strong>{results.totalVotes}</strong> {results.totalVotes === 1 ? 'vote' : 'votes'}
            </p>
            <LiveStatusBadge status={connection} onRetry={refresh} />
          </div>
          <ResultsList results={results} />
          <ResultsAnnouncement results={results} />
        </div>
      )}

      {actionError && (
        <p className="notice notice--error" role="alert">
          {actionError}
        </p>
      )}

      <section className="stack manage__actions" aria-labelledby="manage-actions">
        <h2 id="manage-actions">Manage</h2>

        {!isClosed &&
          (confirming === 'close' ? (
            <div className="stack confirm">
              <p>Close this poll? Voting stops immediately and the results stay visible.</p>
              <div className="row">
                <button
                  type="button"
                  className="button button--primary"
                  onClick={() => closeMutation.mutate()}
                  disabled={closeMutation.isPending}
                >
                  {closeMutation.isPending ? 'Closing…' : 'Close poll'}
                </button>
                <button type="button" className="button button--secondary" onClick={() => setConfirming(null)}>
                  Keep it open
                </button>
              </div>
            </div>
          ) : (
            <div>
              <button type="button" className="button button--secondary" onClick={() => setConfirming('close')}>
                Close poll
              </button>
            </div>
          ))}

        {confirming === 'delete' ? (
          <div className="stack confirm">
            <p>Delete this poll and its {results?.totalVotes ?? 0} votes? This cannot be undone.</p>
            <div className="row">
              <button
                type="button"
                className="button button--danger"
                onClick={() => deleteMutation.mutate()}
                disabled={deleteMutation.isPending}
              >
                {deleteMutation.isPending ? 'Deleting…' : 'Delete permanently'}
              </button>
              <button type="button" className="button button--secondary" onClick={() => setConfirming(null)}>
                Cancel
              </button>
            </div>
          </div>
        ) : (
          <div>
            <button type="button" className="button button--danger" onClick={() => setConfirming('delete')}>
              Delete poll
            </button>
          </div>
        )}
      </section>
    </main>
  )
}
