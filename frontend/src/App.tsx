import { BrowserRouter, Routes, Route, Navigate, Outlet, useLocation } from 'react-router-dom'
import { ConfigProvider, App as AntApp, Spin, theme } from 'antd'
import { Suspense, lazy, useEffect, useState } from 'react'
import { useAuthStore } from './stores/auth'

const LoginPage = lazy(() => import('./pages/LoginPage'))
const HomePage = lazy(() => import('./pages/HomePage'))
const NotesList = lazy(() => import('./pages/NotesList'))
const NoteEditor = lazy(() => import('./pages/NoteEditor'))

function AuthGuard() {
  const { isAuthenticated, check } = useAuthStore()
  const [checking, setChecking] = useState(true)
  const location = useLocation()

  useEffect(() => {
    check().finally(() => setChecking(false))
  }, [location.pathname])

  if (checking) return null
  if (!isAuthenticated) return <Navigate to="/login" replace />
  return <Outlet />
}

export default function App() {
  return (
    <ConfigProvider
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: {
          fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans SC", sans-serif',
          borderRadius: 8,
          colorPrimary: '#4f46e5',
          colorLink: '#4f46e5',
        },
      }}
    >
      <AntApp>
        <BrowserRouter>
          <Suspense fallback={<PageLoading />}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route element={<AuthGuard />}>
                <Route path="/" element={<HomePage />}>
                  <Route index element={<NotesList />} />
                  <Route path="note/:title" element={<NoteEditor />} />
                </Route>
              </Route>
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Suspense>
        </BrowserRouter>
      </AntApp>
    </ConfigProvider>
  )
}

function PageLoading() {
  return (
    <div style={{ height: '100vh', display: 'grid', placeItems: 'center' }}>
      <Spin />
    </div>
  )
}
