/** Formats a timestamp as a short, locale-aware date. */
export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  })
}

/** "1 vote" / "4 votes" — small, but it is the difference between polished and not. */
export function pluralise(count: number, singular: string, plural = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : plural}`
}

/** The absolute URL an audience uses to reach a poll. */
export function shareUrl(pollId: string): string {
  return `${window.location.origin}/polls/${pollId}`
}
