import { Link } from 'react-router-dom'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

export default function NotFoundPage() {
  useDocumentTitle('Page not found')
  return (
    <main className="page page--narrow" id="main">
      <h1>Page not found</h1>
      <p className="page__lead">The link may be wrong, or the page may have moved.</p>
      <div>
        <Link to="/" className="button button--secondary">
          Go to the start
        </Link>
      </div>
    </main>
  )
}
