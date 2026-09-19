import { useCallback, useRef } from 'react'

/**
 * Stops a form being submitted twice by a fast double tap.
 *
 * Guarding on a piece of state — `if (isSubmitting) return` — looks right and is
 * not enough: the handler closes over the value from its render, and two clicks
 * a few milliseconds apart can both run before React has re-rendered with the
 * flag set. A ref is updated synchronously, so the second call sees the first
 * one's decision immediately.
 *
 * This matters here because the duplicate is not harmless: two submissions of
 * the create form are two polls, with two different share links.
 */
export function useSubmitGuard(): { begin: () => boolean; end: () => void } {
  const running = useRef(false)

  const begin = useCallback(() => {
    if (running.current) return false
    running.current = true
    return true
  }, [])

  const end = useCallback(() => {
    running.current = false
  }, [])

  return { begin, end }
}
