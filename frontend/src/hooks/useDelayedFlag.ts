import { useEffect, useState } from 'react'

/**
 * True only once a condition has held for a while.
 *
 * Loading indicators are for waits people notice. A skeleton that appears and
 * disappears within 150ms reads as a flicker and makes a fast app feel unstable,
 * so nothing is shown until the wait is long enough to be worth acknowledging.
 */
export function useDelayedFlag(active: boolean, delayMs = 300): boolean {
  const [shown, setShown] = useState(false)

  useEffect(() => {
    if (!active) {
      setShown(false)
      return
    }
    const timer = window.setTimeout(() => setShown(true), delayMs)
    return () => window.clearTimeout(timer)
  }, [active, delayMs])

  return shown
}
