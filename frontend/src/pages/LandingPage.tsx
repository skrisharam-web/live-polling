import { Link } from 'react-router-dom'

import { useAuth } from '../hooks/useAuth'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

/**
 * The landing page says what Pulse does in a sentence and gets out of the way.
 *
 * No hero, no feature grid, no illustration: the only thing anyone arrives here
 * to do is start a poll, so that is the only thing with any weight on the page.
 */
export default function LandingPage() {
  useDocumentTitle('Live polling')
  const { user } = useAuth()

  return (
    <main className="page page--narrow landing" id="main">
      <h1>Ask a question. Watch the answers arrive.</h1>
      <p className="page__lead">
        Create a poll, share the link, and see the results move as people vote — on every screen
        watching, at the same moment, with nothing to refresh.
      </p>

      <div className="row">
        <Link to={user ? '/polls/new' : '/register'} className="button button--primary">
          Create a poll
        </Link>
        {!user && (
          <Link to="/login" className="button button--secondary">
            Sign in
          </Link>
        )}
      </div>

      <p className="muted landing__note">
        Voting needs no account. Only creating and managing a poll does.
      </p>
    </main>
  )
}
