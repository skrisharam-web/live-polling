import { useEffect } from 'react'

/**
 * Moves focus to the first field the server rejected.
 *
 * Without this, submitting a long form and getting it back with an error leaves
 * the person's focus on the submit button, scrolled past the problem — and a
 * screen reader user with no indication that anything is wrong at all. Focusing
 * the field both announces the error and puts the cursor where the fix goes.
 */
export function useFocusFirstError(errors: Record<string, string>, order: string[]) {
  useEffect(() => {
    const first = order.find((name) => errors[name])
    if (!first) return

    const field = document.querySelector<HTMLElement>(`[name="${first}"]`)
    field?.focus()
    field?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }, [errors, order])
}
