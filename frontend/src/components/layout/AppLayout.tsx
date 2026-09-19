import { Outlet, useMatch } from 'react-router-dom'

import { Header } from './Header'

/**
 * The shell every page renders inside. The skip link is first in the DOM so a
 * keyboard user can jump past the header instead of tabbing through it on every
 * page.
 */
export function AppLayout() {
  // Both matchers run on every render: `??` would short-circuit the second one,
  // and a hook that is sometimes called changes the hook order between renders.
  const votingPage = useMatch('/polls/:pollId')
  const resultsPage = useMatch('/polls/:pollId/results')

  // The public voting and results pages are for the audience, not for account
  // holders, so they get the minimal header.
  const isPublicPollPage = Boolean(votingPage ?? resultsPage)

  return (
    <>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <Header minimal={isPublicPollPage} />
      <Outlet />
    </>
  )
}
