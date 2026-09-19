import { useId, type InputHTMLAttributes } from 'react'

interface Props extends Omit<InputHTMLAttributes<HTMLInputElement>, 'id'> {
  label: string
  /** Shown under the label; use it for format or rules, not for errors. */
  hint?: string
  error?: string
}

/**
 * A labelled input.
 *
 * The label is always visible: a placeholder disappears the moment someone
 * starts typing, which leaves them guessing what the field was for — and screen
 * readers treat a placeholder as a hint, not a name. The error is wired through
 * aria-describedby and aria-invalid so it is announced rather than merely seen.
 */
export function Field({ label, hint, error, className = '', ...input }: Props) {
  const id = useId()
  const hintId = `${id}-hint`
  const errorId = `${id}-error`

  const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(' ')

  return (
    <div className="field">
      <label className="field__label" htmlFor={id}>
        {label}
      </label>
      {hint && (
        <span className="field__hint" id={hintId}>
          {hint}
        </span>
      )}
      <input
        {...input}
        id={id}
        className={`field__input ${className}`.trim()}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy || undefined}
      />
      {error && (
        <span className="field__error" id={errorId}>
          {error}
        </span>
      )}
    </div>
  )
}
