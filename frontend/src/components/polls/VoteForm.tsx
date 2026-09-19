import { useId, useState } from 'react'

import type { Poll } from '../../types/poll'
import './vote-form.css'

interface Props {
  poll: Poll
  isSubmitting: boolean
  error: string | null
  onSubmit: (optionId: string) => void
}

/**
 * The ballot.
 *
 * It is a real radio group: one tab stop, arrow keys move between options, and
 * the whole row is the target rather than a small circle beside it. On a phone —
 * which is where most of these votes happen — that difference is the difference
 * between voting and mis-tapping.
 */
export function VoteForm({ poll, isSubmitting, error, onSubmit }: Props) {
  const [selected, setSelected] = useState<string | null>(null)
  const groupLabelId = useId()

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (selected && !isSubmitting) onSubmit(selected)
  }

  return (
    <form className="vote" onSubmit={submit}>
      <fieldset className="vote__options">
        <legend className="sr-only" id={groupLabelId}>
          {poll.question}
        </legend>

        {poll.options.map((option) => {
          const isSelected = selected === option.id
          return (
            <label key={option.id} className={`vote-option${isSelected ? ' vote-option--selected' : ''}`}>
              <input
                type="radio"
                name="option"
                value={option.id}
                checked={isSelected}
                onChange={() => setSelected(option.id)}
                disabled={isSubmitting}
                className="vote-option__input"
              />
              <span className="vote-option__mark" aria-hidden="true" />
              <span className="vote-option__text">{option.text}</span>
            </label>
          )
        })}
      </fieldset>

      {error && (
        <p className="notice notice--error" role="alert">
          {error}
        </p>
      )}

      <div className="vote__submit">
        <button
          type="submit"
          className="button button--primary button--block"
          disabled={!selected || isSubmitting}
          aria-describedby={selected ? undefined : `${groupLabelId}-help`}
        >
          {isSubmitting ? 'Submitting your vote…' : 'Vote'}
        </button>
        {!selected && (
          <p className="field__hint" id={`${groupLabelId}-help`}>
            Choose an option to vote.
          </p>
        )}
      </div>
    </form>
  )
}
