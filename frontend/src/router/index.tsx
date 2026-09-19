import { createBrowserRouter } from 'react-router-dom'

import { AppLayout } from '../components/layout/AppLayout'
import { RequireAuth } from '../components/layout/RequireAuth'
import CreatePollPage from '../pages/CreatePollPage'
import DashboardPage from '../pages/DashboardPage'
import LandingPage from '../pages/LandingPage'
import LoginPage from '../pages/LoginPage'
import ManagePollPage from '../pages/ManagePollPage'
import NotFoundPage from '../pages/NotFoundPage'
import PollPage from '../pages/PollPage'
import PollResultsPage from '../pages/PollResultsPage'
import RegisterPage from '../pages/RegisterPage'

/**
 * The route table, grouped by who each screen is for: anyone, the audience
 * holding a share link, or the signed-in owner.
 */
export const router = createBrowserRouter([
  {
    element: <AppLayout />,
    children: [
      { path: '/', element: <LandingPage /> },
      { path: '/login', element: <LoginPage /> },
      { path: '/register', element: <RegisterPage /> },

      // Public: the share link is the only credential needed.
      { path: '/polls/:pollId', element: <PollPage /> },
      { path: '/polls/:pollId/results', element: <PollResultsPage /> },

      {
        element: <RequireAuth />,
        children: [
          { path: '/dashboard', element: <DashboardPage /> },
          { path: '/polls/new', element: <CreatePollPage /> },
          { path: '/polls/:pollId/manage', element: <ManagePollPage /> },
        ],
      },

      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
