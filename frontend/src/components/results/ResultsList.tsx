import type { PollResults } from '../../types/poll'
import './results.css'

interface Props {
  results: PollResults
  /** The option this browser voted for, marked so a voter can find their choice. */
  yourVote?: string
}

/**
 * The live standing of a poll.
 *
 * Every option is shown, including ones with no votes, and always in the poll's
 * own order. Never reordering is a deliberate choice: a list that re-sorts itself
 * as votes arrive makes the viewer re-find the option they were reading, which is
 * exactly the wrong thing to do to someone watching a close race.
 */
export function ResultsList({ results, yourVote }: Props) {
  const { totalVotes } = results
  const leadingCount = Math.max(0, ...results.results.map((option) => option.count))

  return (
    <ul className="results__list">
      {results.results.map((option) => {
        // The bar uses the exact fraction while the label shows a rounded
        // percentage, so rounding never makes a bar look like it disagrees with
        // its own number.
        const fraction = totalVotes === 0 ? 0 : option.count / totalVotes
        const percentage = Math.round(fraction * 100)
        const isLeading = totalVotes > 0 && option.count === leadingCount
        const isYours = option.optionId === yourVote

        return (
          <li key={option.optionId} className={`result${isLeading ? ' result--leading' : ''}`}>
            <div className="result__label">
              <span className="result__text">
                {option.text}
                {isYours && <span className="sr-only"> — your vote</span>}
              </span>
              <span className="result__numbers">
                <span className="result__count">{option.count}</span>
                {' · '}
                {percentage}%
              </span>
            </div>
            <div className="result__track">
              <div
                className={`result__bar${option.count === 0 ? ' result__bar--empty' : ''}`}
                style={{ width: `${fraction * 100}%` }}
              />
            </div>
          </li>
        )
      })}
    </ul>
  )
}

/**
 * A one-line spoken summary of the results.
 *
 * Screen readers get this instead of the bars themselves: announcing every
 * number on every vote would flood a listener during a burst, while a single
 * summary tells them what changed.
 */
export function ResultsAnnouncement({ results }: { results: PollResults }) {
  const leading = [...results.results].sort((a, b) => b.count - a.count)[0]

  return (
    <p className="sr-only" aria-live="polite" aria-atomic="true">
      {results.totalVotes === 0
        ? 'No votes yet.'
        : `${results.totalVotes} ${results.totalVotes === 1 ? 'vote' : 'votes'}. ${leading.text} is ahead with ${leading.count}.`}
    </p>
  )
}
