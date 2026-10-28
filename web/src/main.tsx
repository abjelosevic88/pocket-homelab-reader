import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/reader.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import Stream from './pages/Stream'
import ArticlePage from './pages/ArticlePage'
import Feeds from './pages/Feeds'
import Highlights from './pages/Highlights'
import Save from './pages/Save'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route path="/save" element={<Gate><ErrorBoundary where="save"><Save /></ErrorBoundary></Gate>} />
      <Route element={<Gate><ErrorBoundary where="reader"><Layout /></ErrorBoundary></Gate>}>
        <Route path="/" element={<Stream key="unread" view="unread" />} />
        <Route path="/all" element={<Stream key="all" view="all" />} />
        <Route path="/today" element={<Stream key="today" view="today" />} />
        <Route path="/saved" element={<Stream key="saved" view="saved" />} />
        <Route path="/archive" element={<Stream key="archive" view="archive" />} />
        <Route path="/starred" element={<Stream key="starred" view="starred" />} />
        <Route path="/history" element={<Stream key="history" view="history" />} />
        <Route path="/feed/:id" element={<Stream key="feed" view="feed" />} />
        <Route path="/folder/:id" element={<Stream key="folder" view="folder" />} />
        <Route path="/tag/:tag" element={<Stream key="tag" view="tag" />} />
        <Route path="/search" element={<Stream key="search" view="search" />} />
        <Route path="/a/:id" element={<ArticlePage />} />
        <Route path="/highlights" element={<Highlights />} />
        <Route path="/feeds/:tab?" element={<Feeds />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
