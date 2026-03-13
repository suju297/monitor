import { Suspense, lazy } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'

const MonitorPage = lazy(() => import('@/pages/MonitorPage'))
const JobsPage = lazy(() => import('@/pages/JobsPage'))
const MailPage = lazy(() => import('@/pages/MailPage'))
const AssistantPage = lazy(() => import('@/pages/AssistantPage'))

function RouteLoadingFallback() {
  return (
    <div className="flex min-h-[100dvh] items-center justify-center bg-[radial-gradient(circle_at_top,_rgba(37,99,235,0.12),_transparent_42%),linear-gradient(180deg,#f8fafc,#eef6ff)] px-6">
      <div className="rounded-full border border-blue-100 bg-white/92 px-4 py-2 text-sm font-medium text-slate-600 shadow-sm">
        Loading workspace...
      </div>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <Suspense fallback={<RouteLoadingFallback />}>
        <Routes>
          <Route path="/" element={<Navigate to="/monitor" replace />} />
          <Route path="/monitor" element={<MonitorPage />} />
          <Route path="/jobs" element={<JobsPage />} />
          <Route path="/mail" element={<MailPage />} />
          <Route path="/assistant" element={<AssistantPage />} />
          <Route path="*" element={<Navigate to="/monitor" replace />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  )
}
