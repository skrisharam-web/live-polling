import { Link, useRouteError } from 'react-router-dom'

/**
 * The last line of defence: a rendering error anywhere below a route lands here
 * instead of leaving a blank page.
 *
 * It says what happened in plain language and offers a way out. The error itself
 * goes to the console for a developer, never to the screen — a stack trace tells
 * a visitor nothing and can name internals.
 */
export function RouteError() {
  const error = useRouteError()
  console.error('Unhandled route error', error)

  return (
    <main className="page page--narrow" id="main">
      <h1>Something went wrong</h1>
      <p className="page__lead">
        This page could not be displayed. Reloading usually fixes it; if it does not, the poll may
        no longer exist.
      </p>
      <div className="row">
        <button type="button" className="button button--primary" onClick={() => window.location.reload()}>
          Reload the page
        </button>
        <Link to="/" className="button button--secondary">
          Go to the start
        </Link>
      </div>
    </main>
  )
}
