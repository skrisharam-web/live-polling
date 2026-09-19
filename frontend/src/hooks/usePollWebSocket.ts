import { useCallback, useEffect, useRef, useState } from 'react'

import { WS_BASE_URL } from '../api/client'
import type { PollResultsUpdatedEvent } from '../types/poll'

/**
 * The connection's state, as the UI shows it.
 *
 * These four are deliberately distinct. "connecting" is the first attempt and
 * deserves no alarm; "reconnecting" means we had a connection and lost it, which
 * is worth telling the viewer because the numbers on screen may be stale;
 * "disconnected" means we have given up and the viewer needs a way to act.
 */
export type ConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'disconnected'

/** Reconnection schedule: 1s, 2s, 4s, 8s, 16s, then 16s until we stop. */
const BASE_DELAY_MS = 1_000
const MAX_DELAY_MS = 16_000
const MAX_ATTEMPTS = 8

/** A normal close initiated by us; the browser must not treat it as a failure. */
const CLOSE_GOING_AWAY = 1000

interface Options {
  /** Called for every valid event, including the snapshot sent on connect. */
  onEvent: (event: PollResultsUpdatedEvent) => void
  /**
   * Called after a connection is re-established.
   *
   * Events that arrived while the socket was down are gone — Pub/Sub keeps
   * nothing for absent subscribers — so the results on screen may be stale by an
   * unknown amount. Refetching once on reconnect closes that gap, and is the
   * reason a dropped message never has to be recovered.
   */
  onReconnect?: () => void
  /** Set false to hold the connection closed (no poll id yet, page hidden, tests). */
  enabled?: boolean
}

interface Realtime {
  status: ConnectionState
  /** Reconnect now, for the manual control shown when we have given up. */
  reconnect: () => void
}

/**
 * Keeps a WebSocket open to one poll and hands every update to the caller.
 *
 * The hook owns reconnection rather than the component, because getting it wrong
 * is how a realtime feature turns into an accidental denial-of-service against
 * your own backend: a browser that retries instantly, on every tab, the moment a
 * deploy restarts the server. Hence exponential backoff with jitter, a cap on
 * attempts, and a deliberate stop.
 */
export function usePollWebSocket(pollId: string | undefined, options: Options): Realtime {
  const { enabled = true } = options

  const [status, setStatus] = useState<ConnectionState>(enabled && pollId ? 'connecting' : 'disconnected')

  // Callbacks live in a ref so that a caller re-creating them on every render
  // does not tear down and rebuild the socket — which would look to the server
  // like a client reconnecting dozens of times a second.
  const callbacks = useRef(options)
  callbacks.current = options

  const socketRef = useRef<WebSocket | null>(null)
  const retryTimer = useRef<number | null>(null)
  const attempts = useRef(0)
  const everConnected = useRef(false)
  // Set while the effect is tearing down, so a close event from our own cleanup
  // is not mistaken for the connection dropping.
  const closingDeliberately = useRef(false)

  const clearRetry = useCallback(() => {
    if (retryTimer.current !== null) {
      window.clearTimeout(retryTimer.current)
      retryTimer.current = null
    }
  }, [])

  /**
   * Backoff with jitter. The jitter matters more than the backoff: without it,
   * every browser watching a poll reconnects at the same instant after a restart,
   * and the stampede knocks the server over again.
   */
  const nextDelay = useCallback(() => {
    const exponential = Math.min(BASE_DELAY_MS * 2 ** attempts.current, MAX_DELAY_MS)
    const jitter = exponential * 0.25 * (Math.random() * 2 - 1)
    return Math.max(BASE_DELAY_MS / 2, Math.round(exponential + jitter))
  }, [])

  const connect = useCallback(() => {
    if (!pollId || !enabled) return

    clearRetry()
    closingDeliberately.current = false

    let socket: WebSocket
    try {
      socket = new WebSocket(`${WS_BASE_URL}/ws/polls/${pollId}`)
    } catch {
      // A malformed URL is not worth retrying; nothing will change.
      setStatus('disconnected')
      return
    }
    socketRef.current = socket

    setStatus(everConnected.current ? 'reconnecting' : 'connecting')

    socket.onopen = () => {
      const wasReconnect = everConnected.current
      everConnected.current = true
      attempts.current = 0
      setStatus('connected')

      if (wasReconnect) {
        callbacks.current.onReconnect?.()
      }
    }

    socket.onmessage = (message) => {
      try {
        const event = JSON.parse(message.data as string) as PollResultsUpdatedEvent
        // Only events this client understands, and only for the poll it asked
        // about. The socket is a public endpoint; nothing unrecognised is rendered.
        if (event?.type !== 'poll_results_updated' || event.pollId !== pollId) return
        callbacks.current.onEvent(event)
      } catch {
        // A malformed frame is ignored rather than allowed to break the view.
      }
    }

    socket.onerror = () => {
      // onerror is always followed by onclose, which owns the retry decision.
    }

    socket.onclose = () => {
      socketRef.current = null
      if (closingDeliberately.current) return

      if (attempts.current >= MAX_ATTEMPTS) {
        // Stopping is a deliberate choice: something is wrong that retrying will
        // not fix, and a browser hammering a dead server helps nobody. The UI
        // offers a manual retry instead.
        setStatus('disconnected')
        return
      }

      const delay = nextDelay()
      attempts.current += 1
      setStatus('reconnecting')
      retryTimer.current = window.setTimeout(connect, delay)
    }
  }, [pollId, enabled, clearRetry, nextDelay])

  const reconnect = useCallback(() => {
    attempts.current = 0
    connect()
  }, [connect])

  useEffect(() => {
    if (!pollId || !enabled) {
      setStatus('disconnected')
      return
    }

    attempts.current = 0
    everConnected.current = false
    connect()

    return () => {
      closingDeliberately.current = true
      clearRetry()
      socketRef.current?.close(CLOSE_GOING_AWAY)
      socketRef.current = null
    }
  }, [pollId, enabled, connect, clearRetry])

  /**
   * Two browser signals are worth acting on immediately rather than waiting out
   * a backoff delay: coming back online, and the tab becoming visible again.
   * Phones suspend sockets on background tabs, so without this a viewer who
   * switches apps comes back to a frozen result.
   */
  useEffect(() => {
    if (!pollId || !enabled) return

    const retryNow = () => {
      if (socketRef.current || document.hidden || !navigator.onLine) return
      attempts.current = 0
      connect()
    }

    window.addEventListener('online', retryNow)
    document.addEventListener('visibilitychange', retryNow)
    return () => {
      window.removeEventListener('online', retryNow)
      document.removeEventListener('visibilitychange', retryNow)
    }
  }, [pollId, enabled, connect])

  return { status, reconnect }
}
