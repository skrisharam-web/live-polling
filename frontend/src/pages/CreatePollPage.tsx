import { useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'

import { createPoll } from '../api/polls'
import { Field } from '../components/ui/Field'
import { ShareLink } from '../components/polls/ShareLink'
import { useFocusFirstError } from '../hooks/useFocusFirstError'
import { useSubmitGuard } from '../hooks/useSubmitGuard'
import { fieldErrors, messageFor } from '../utils/errors'
import { shareUrl } from '../utils/formatting'
import type { Poll } from '../types/poll'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

const MIN_OPTIONS = 2
const MAX_OPTIONS = 10

/**
 * Creating a poll.
 *
 * The question comes first and is the largest control on the page, because it is
 * the only thing the person actually has to think about. Everything else is
 * mechanics.
 */
export default function CreatePollPage() {
  useDocumentTitle('Create a poll')
  const queryClient = useQueryClient()

  const [question, setQuestion] = useState('')
  const [options, setOptions] = useState(['', ''])
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<Poll | null>(null)

  const optionRefs = useRef<(HTMLInputElement | null)[]>([])
  const guard = useSubmitGuard()

  // A question error focuses the question; an options error focuses the first
  // option, which is where the fix starts.
  useFocusFirstError(errors, ['question'])

  const mutation = useMutation({
    mutationFn: createPoll,
    onSettled: () => guard.end(),
    onSuccess: ({ poll }) => {
      setCreated(poll)
      setErrors({})
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['myPolls'] })
    },
    onError: (cause) => {
      const fields = fieldErrors(cause)
      setErrors(fields)
      setError(Object.keys(fields).length === 0 ? messageFor(cause) : null)
    },
  })

  const updateOption = (index: number, value: string) =>
    setOptions((current) => current.map((option, i) => (i === index ? value : option)))

  const addOption = () => {
    if (options.length >= MAX_OPTIONS) return
    setOptions((current) => [...current, ''])
    // Focus the new field: someone adding an option intends to type in it.
    window.setTimeout(() => optionRefs.current[options.length]?.focus(), 0)
  }

  const removeOption = (index: number) => {
    if (options.length <= MIN_OPTIONS) return
    setOptions((current) => current.filter((_, i) => i !== index))
  }

  // Enter in the last option adds another, so a list can be typed without
  // reaching for the mouse.
  const onOptionKeyDown = (index: number) => (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== 'Enter') return
    event.preventDefault()
    if (index === options.length - 1) addOption()
    else optionRefs.current[index + 1]?.focus()
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    // The ref flips synchronously; mutation.isPending would still read false for
    // a second click in the same tick.
    if (!guard.begin()) return
    mutation.mutate({ question, options })
  }

  // The moment the product delivers: the link exists and is ready to share.
  if (created) {
    return (
      <main className="page page--narrow" id="main">
        <h1>Your poll is live</h1>
        <p className="page__lead">{created.question}</p>

        <ShareLink url={shareUrl(created.id)} label="Share this link with your audience" />

        <div className="row">
          <Link to={`/polls/${created.id}/results`} className="button button--primary">
            Watch the results
          </Link>
          <Link to={`/polls/${created.id}/manage`} className="button button--secondary">
            Manage poll
          </Link>
        </div>
      </main>
    )
  }

  return (
    <main className="page page--narrow" id="main">
      <h1>Create a poll</h1>

      <form className="form" onSubmit={submit} noValidate>
        {error && (
          <p className="notice notice--error" role="alert">
            {error}
          </p>
        )}

        <Field
          label="Question"
          name="question"
          className="field__input--question"
          placeholder="Which release do we ship first?"
          value={question}
          onChange={(event) => setQuestion(event.target.value)}
          error={errors.question}
          autoFocus
          required
        />

        <div className="field">
          <span className="field__label" id="options-label">
            Options
          </span>
          <span className="field__hint">Between {MIN_OPTIONS} and {MAX_OPTIONS}. Each one must be different.</span>

          <ol className="option-list" aria-labelledby="options-label">
            {options.map((option, index) => (
              <li key={index} className="option-list__item">
                <input
                  ref={(element) => {
                    optionRefs.current[index] = element
                  }}
                  className="field__input"
                  value={option}
                  onChange={(event) => updateOption(index, event.target.value)}
                  onKeyDown={onOptionKeyDown(index)}
                  aria-label={`Option ${index + 1}`}
                  placeholder={index === 0 ? 'The one with tests' : ''}
                />
                <button
                  type="button"
                  className="button button--secondary button--small"
                  onClick={() => removeOption(index)}
                  disabled={options.length <= MIN_OPTIONS}
                  aria-disabled={options.length <= MIN_OPTIONS}
                  title={options.length <= MIN_OPTIONS ? `A poll needs at least ${MIN_OPTIONS} options` : undefined}
                >
                  Remove
                  <span className="sr-only"> option {index + 1}</span>
                </button>
              </li>
            ))}
          </ol>

          {errors.options && (
            <span className="field__error" role="alert">
              {errors.options}
            </span>
          )}

          <div>
            <button
              type="button"
              className="button button--secondary button--small"
              onClick={addOption}
              disabled={options.length >= MAX_OPTIONS}
              aria-disabled={options.length >= MAX_OPTIONS}
            >
              Add option
            </button>
            {options.length >= MAX_OPTIONS && (
              <span className="field__hint"> A poll can have at most {MAX_OPTIONS} options.</span>
            )}
          </div>
        </div>

        <button type="submit" className="button button--primary" disabled={mutation.isPending}>
          {mutation.isPending ? 'Creating poll…' : 'Create poll'}
        </button>
      </form>
    </main>
  )
}
