export type PollStatus = 'active' | 'closed'

export interface PollOption {
  id: string
  text: string
}

export interface Poll {
  id: string
  question: string
  options: PollOption[]
  status: PollStatus
  createdAt: string
  updatedAt: string
  expiresAt?: string
  /** The server's answer to "can this be voted on right now", expiry included. */
  acceptsVotes: boolean
}

/** A dashboard row: a poll plus its vote total. */
export interface PollSummary extends Poll {
  totalVotes: number
}

export interface OptionResult {
  optionId: string
  text: string
  count: number
}

export interface PollResults {
  pollId: string
  results: OptionResult[]
  totalVotes: number
  status: PollStatus
  computedAt: string
  /** The option this browser already chose, if any. */
  yourVote?: string
}

/**
 * The realtime event, matching the backend's events package.
 *
 * It carries counts but no option text: the client already has the text from the
 * poll it loaded, so sending it again would be redundant and would let the two
 * copies disagree.
 */
export interface PollResultsUpdatedEvent {
  type: 'poll_results_updated'
  pollId: string
  results: { optionId: string; count: number }[]
  totalVotes: number
  status: PollStatus
  timestamp: string
}
