import { createBrowserRouter } from 'react-router-dom'

import PollResultsPage from '../pages/PollResultsPage'

/**
 * Routes. The rest of the application's screens land in the next phase; the
 * results view comes first because it is the one that has to prove the realtime
 * path works end to end.
 */
export const router = createBrowserRouter([
  {
    path: '/polls/:pollId/results',
    element: <PollResultsPage />,
  },
])
