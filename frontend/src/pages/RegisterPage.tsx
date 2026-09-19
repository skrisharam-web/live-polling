import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { Field } from '../components/ui/Field'
import { useAuth } from '../hooks/useAuth'
import { fieldErrors, messageFor } from '../utils/errors'

export default function RegisterPage() {
  const { signUp } = useAuth()
  const navigate = useNavigate()

  const [form, setForm] = useState({ name: '', email: '', password: '' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [error, setError] = useState<string | null>(null)
  const [isSubmitting, setSubmitting] = useState(false)

  const update = (key: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) =>
    setForm((current) => ({ ...current, [key]: event.target.value }))

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (isSubmitting) return

    setSubmitting(true)
    setError(null)
    setErrors({})
    try {
      await signUp(form)
      void navigate('/polls/new')
    } catch (cause) {
      // The backend is the authority on what is valid, so its per-field messages
      // go onto the fields they belong to rather than into one banner.
      const fields = fieldErrors(cause)
      setErrors(fields)
      if (Object.keys(fields).length === 0) {
        setError(messageFor(cause))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="page page--narrow" id="main">
      <h1>Create an account</h1>
      <p className="page__lead">You need one to create and manage polls. Voting never does.</p>

      <form className="form" onSubmit={(event) => void submit(event)} noValidate>
        {error && (
          <p className="notice notice--error" role="alert">
            {error}
          </p>
        )}

        <Field
          label="Name"
          name="name"
          autoComplete="name"
          value={form.name}
          onChange={update('name')}
          error={errors.name}
          required
        />
        <Field
          label="E-mail address"
          type="email"
          name="email"
          autoComplete="email"
          value={form.email}
          onChange={update('email')}
          error={errors.email}
          required
        />
        <Field
          label="Password"
          type="password"
          name="password"
          autoComplete="new-password"
          hint="At least 8 characters."
          value={form.password}
          onChange={update('password')}
          error={errors.password}
          required
        />

        <div className="stack">
          <button type="submit" className="button button--primary" disabled={isSubmitting}>
            {isSubmitting ? 'Creating your account…' : 'Create account'}
          </button>
          <p className="muted">
            Already have one? <Link to="/login">Sign in</Link>.
          </p>
        </div>
      </form>
    </main>
  )
}
