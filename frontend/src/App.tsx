import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from 'react-router-dom'

import { ApiError } from './api/client'

import { AuthProvider } from './context/AuthContext'
import { router } from './router'

/**
 * TanStack Query holds server state; the WebSocket writes into the same cache,
 * so a component never has to reconcile two sources.
 *
 * Retries are off for mutations and limited for queries: this application's
 * failures are mostly deliberate answers (404, 409, 422) that retrying cannot
 * improve, and the realtime hook already owns retrying the thing that genuinely
 * benefits from it.
 */
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 404 or a 403 is a decision, not a hiccup: retrying it cannot change
      // the answer and only delays telling the user. Retry once for the failures
      // that a second attempt can actually fix.
      retry: (failureCount, error) => {
        if (error instanceof ApiError && error.status >= 400 && error.status < 500) return false
        return failureCount < 1
      },
      refetchOnWindowFocus: false,
    },
    mutations: { retry: 0 },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </QueryClientProvider>
  )
}
