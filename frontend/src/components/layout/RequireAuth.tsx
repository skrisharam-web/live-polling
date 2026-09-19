import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { useAuth } from '../../hooks/useAuth'

/**
 * Gates the owner-only screens.
 *
 * This is a convenience, not a security boundary: every one of these screens
 * calls an endpoint that checks the session itself, and the backend is what
 * actually refuses a stranger. Redirecting here only saves the user from being
 * shown a page that would fail.
 */
export function RequireAuth() {
  const { user, isLoading } = useAuth()
  const location = useLocation()

  if (isLoading) {
    return (
      <main className="page page--narrow" aria-busy="true">
        <div className="skeleton skeleton--title" />
        <span className="sr-only">Checking your session…</span>
      </main>
    )
  }

  if (!user) {
    // Remember where they were headed so signing in can finish the job.
    return <Navigate to="/login" state={{ from: location.pathname }} replace />
  }

  return <Outlet />
}
