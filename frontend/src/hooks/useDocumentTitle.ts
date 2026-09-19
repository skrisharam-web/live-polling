import { useEffect } from 'react'

const SUFFIX = 'Pulse'

/**
 * Sets the document title for a screen.
 *
 * In a single-page app the title never changes on its own, which leaves a screen
 * reader announcing the same thing after every navigation, browser history full
 * of identical entries, and a row of indistinguishable tabs for anyone watching
 * two polls at once.
 */
export function useDocumentTitle(title?: string) {
  useEffect(() => {
    document.title = title ? `${title} · ${SUFFIX}` : SUFFIX
  }, [title])
}
