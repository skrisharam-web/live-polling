import type { ConnectionState } from '../../hooks/usePollWebSocket'

interface Props {
  status: ConnectionState
  onRetry: () => void
}

/**
 * The realtime connection, stated honestly and quietly.
 *
 * The word is the signal and the dot reinforces it — a coloured dot alone means
 * nothing to a colour-blind viewer, and means nothing to anyone the first time
 * they see it. Nothing here animates: a permanently pulsing element is noise,
 * and this badge should only draw the eye when something is actually wrong.
 */
const LABELS: Record<ConnectionState, string> = {
  connecting: 'Connecting…',
  connected: 'Live',
  reconnecting: 'Reconnecting…',
  disconnected: 'Offline',
}

export function LiveStatusBadge({ status, onRetry }: Props) {
  return (
    <span className={`live-status live-status--${status}`}>
      <span className="live-status__dot" aria-hidden="true" />
      <span>{LABELS[status]}</span>
      {status === 'disconnected' && (
        <button type="button" className="live-status__retry" onClick={onRetry}>
          Refresh results
        </button>
      )}
    </span>
  )
}
