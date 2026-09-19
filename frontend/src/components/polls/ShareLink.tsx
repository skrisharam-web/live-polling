import { useEffect, useRef, useState } from 'react'

interface Props {
  url: string
  label?: string
}

/**
 * The share link, treated as the object it is: shown in full, selectable, and
 * copyable in one press.
 *
 * The confirmation replaces the button's own label rather than appearing as a
 * toast somewhere else on screen — the feedback belongs where the action was,
 * and a toast that fades after two seconds is a message some people never read.
 */
export function ShareLink({ url, label = 'Share this link' }: Props) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | null>(null)

  useEffect(() => () => {
    if (timer.current !== null) window.clearTimeout(timer.current)
  }, [])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url)
    } catch {
      // Clipboard access can be refused (permissions, insecure origin). The URL
      // is on screen and selectable, so there is still a way through.
      return
    }
    setCopied(true)
    if (timer.current !== null) window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setCopied(false), 3000)
  }

  return (
    <div className="share">
      <span className="field__label">{label}</span>
      <div className="share__row">
        <span className="share__url">{url}</span>
        <button type="button" className="button button--secondary" onClick={() => void copy()}>
          {copied ? 'Copied' : 'Copy link'}
        </button>
      </div>
      <span className="sr-only" aria-live="polite">
        {copied ? 'Link copied to the clipboard' : ''}
      </span>
    </div>
  )
}
