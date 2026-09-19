import { Link, NavLink } from 'react-router-dom'

import { useAuth } from '../../hooks/useAuth'

interface Props {
  /**
   * Public poll pages get the brand and nothing else. Someone who followed a
   * shared link came to vote, and turning that moment into a signup prompt is
   * how a voting page stops being a voting page.
   */
  minimal?: boolean
}

export function Header({ minimal = false }: Props) {
  const { user, isLoading, signOut } = useAuth()

  return (
    <header className="header">
      <div className="header__inner">
        <Link to="/" className="header__brand">
          <svg className="header__mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
            <rect x="3" y="17" width="6" height="11" rx="3" fill="currentColor" opacity="0.45" />
            <rect x="13" y="10" width="6" height="18" rx="3" fill="currentColor" />
            <rect x="23" y="14" width="6" height="14" rx="3" fill="currentColor" opacity="0.7" />
          </svg>
          Pulse
        </Link>

        {!minimal && !isLoading && (
          <nav className="header__nav" aria-label="Main">
            {user ? (
              <>
                <span className="header__user">{user.name}</span>
                <NavLink to="/dashboard" className="header__link">
                  My polls
                </NavLink>
                <button type="button" className="button button--secondary button--small" onClick={() => void signOut()}>
                  Sign out
                </button>
              </>
            ) : (
              <>
                <NavLink to="/login" className="header__link">
                  Sign in
                </NavLink>
                <Link to="/register" className="button button--primary button--small">
                  Create a poll
                </Link>
              </>
            )}
          </nav>
        )}
      </div>
    </header>
  )
}
