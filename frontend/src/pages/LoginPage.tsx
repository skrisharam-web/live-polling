import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { Field } from '../components/ui/Field'
import { useAuth } from '../hooks/useAuth'
import { useSubmitGuard } from '../hooks/useSubmitGuard'
import { messageFor } from '../utils/errors'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

export default function LoginPage() {
  useDocumentTitle('Sign in')
  const { signIn } = useAuth()
  const navigate = useNavigate()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [isSubmitting, setSubmitting] = useState(false)
  const guard = useSubmitGuard()

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!guard.begin()) return

    setSubmitting(true)
    setError(null)
    try {
      await signIn({ email, password })
      void navigate('/dashboard')
    } catch (cause) {
      // The backend answers every failed sign-in identically, on purpose, so
      // there is one message to show and no field to single out.
      setError(messageFor(cause, 'Incorrect e-mail address or password.'))
    } finally {
      setSubmitting(false)
      guard.end()
    }
  }

  return (
    <main className="page page--narrow" id="main">
      <h1>Sign in</h1>

      <form className="form" onSubmit={(event) => void submit(event)} noValidate>
        {error && (
          <p className="notice notice--error" role="alert">
            {error}
          </p>
        )}

        <Field
          label="E-mail address"
          type="email"
          name="email"
          autoComplete="email"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          required
        />
        <Field
          label="Password"
          type="password"
          name="password"
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          required
        />

        <div className="stack">
          <button type="submit" className="button button--primary" disabled={isSubmitting}>
            {isSubmitting ? 'Signing in…' : 'Sign in'}
          </button>
          <p className="muted">
            No account yet? <Link to="/register">Create one</Link>.
          </p>
        </div>
      </form>
    </main>
  )
}
