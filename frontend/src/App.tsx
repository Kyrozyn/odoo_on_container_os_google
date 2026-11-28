import React, { useEffect, useState } from 'react'
import {
  Archive,
  Box,
  Container,
  Database,
  LayoutDashboard,
  LogOut,
  Puzzle,
  Server,
  Settings as SettingsIcon,
} from 'lucide-react'
import { api, clearToken, getToken, setToken } from './api/client'
import { Login } from './pages/Login'
import { Dashboard } from './pages/Dashboard'
import { Instances } from './pages/Instances'
import { InstanceDetail } from './pages/InstanceDetail'
import { Databases } from './pages/Databases'
import { Addons } from './pages/Addons'
import { Backups } from './pages/Backups'
import { Containers } from './pages/Containers'
import { Settings } from './pages/Settings'

class ErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { hasError: boolean; message: string }
> {
  constructor(props: { children: React.ReactNode }) {
    super(props)
    this.state = { hasError: false, message: '' }
  }
  static getDerivedStateFromError(err: Error) {
    return { hasError: true, message: err?.message || 'Unexpected render error' }
  }
  render() {
    if (this.state.hasError) {
      return (
        <div className="rounded-xl border border-rose-200 bg-rose-50 p-6 text-rose-800 space-y-3">
          <h2 className="text-base font-bold">Terjadi kesalahan saat merender halaman</h2>
          <p className="font-mono text-xs">{this.state.message}</p>
          <button
            onClick={() => this.setState({ hasError: false, message: '' })}
            className="rounded-lg bg-rose-600 px-3 py-1.5 text-xs font-semibold text-white cursor-pointer"
          >
            Coba Lagi
          </button>
        </div>
      )
    }
    return this.props.children
  }
}

export default function App() {
  const [authed, setAuthed] = useState<boolean>(!!getToken())
  const [forcePasswordChange, setForcePasswordChange] = useState<boolean>(false)
  const [curPass, setCurPass] = useState('')
  const [newPass, setNewPass] = useState('')
  const [confirmPass, setConfirmPass] = useState('')
  const [passErr, setPassErr] = useState('')
  const [savingPass, setSavingPass] = useState(false)
  const [page, setPage] = useState<string>('dashboard')
  const [selectedInstanceId, setSelectedInstanceId] = useState<string | null>(null)
  const [toast, setToast] = useState<{ msg: string; type: 'success' | 'error' } | null>(null)

  const notify = (msg: string, type: 'success' | 'error' = 'success') => {
    setToast({ msg, type })
    setTimeout(() => setToast((t) => (t?.msg === msg ? null : t)), 4000)
  }

  useEffect(() => {
    const onUnauth = () => setAuthed(false)
    window.addEventListener('runodoo:unauthorized', onUnauth)
    if (getToken()) {
      api
        .me()
        .then((me) => setForcePasswordChange(!me.passwordChanged))
        .catch(() => setAuthed(false))
    }
    return () => window.removeEventListener('runodoo:unauthorized', onUnauth)
  }, [])

  if (!authed) {
    return (
      <Login
        onSuccess={(passwordChanged) => {
          setAuthed(true)
          if (passwordChanged === false) {
            setForcePasswordChange(true)
          } else {
            api
              .me()
              .then((me) => setForcePasswordChange(!me.passwordChanged))
              .catch(() => {})
          }
        }}
      />
    )
  }

  if (forcePasswordChange) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-900 p-4">
        <div className="w-full max-w-md rounded-2xl bg-white p-8 shadow-2xl space-y-5">
          <div>
            <h1 className="text-xl font-bold text-slate-900">Change Default Password</h1>
            <p className="mt-1 text-xs text-slate-500">
              Untuk keamanan, Anda wajib mengganti password default sebelum menggunakan panel RunOdoo (minimal 10 karakter).
            </p>
          </div>
          <form
            onSubmit={async (e) => {
              e.preventDefault()
              setPassErr('')
              if (newPass.length < 10) {
                setPassErr('New password must be at least 10 characters.')
                return
              }
              if (newPass !== confirmPass) {
                setPassErr('Password confirmation does not match.')
                return
              }
              setSavingPass(true)
              try {
                const res = await api.changePassword(curPass, newPass)
                if (res.token) setToken(res.token)
                setForcePasswordChange(false)
                setCurPass('')
                setNewPass('')
                setConfirmPass('')
                notify('Admin password updated!')
              } catch (err) {
                setPassErr(err instanceof Error ? err.message : 'Failed updating password')
              } finally {
                setSavingPass(false)
              }
            }}
            className="space-y-3"
          >
            <input
              type="password"
              required
              value={curPass}
              onChange={(e) => setCurPass(e.target.value)}
              placeholder="Current default password"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
            <input
              type="password"
              required
              minLength={10}
              value={newPass}
              onChange={(e) => setNewPass(e.target.value)}
              placeholder="New strong password (min 10 chars)"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
            <input
              type="password"
              required
              minLength={10}
              value={confirmPass}
              onChange={(e) => setConfirmPass(e.target.value)}
              placeholder="Confirm new password"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
            {passErr && (
              <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">
                {passErr}
              </div>
            )}
            <button
              type="submit"
              disabled={savingPass}
              className="w-full rounded-lg bg-[#714B67] py-2.5 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
            >
              {savingPass ? 'Updating...' : 'Set New Admin Password'}
            </button>
          </form>
        </div>
      </div>
    )
  }

  const navItems = [
    { id: 'dashboard', label: 'Dashboard', icon: <LayoutDashboard className="h-4 w-4" /> },
    { id: 'instances', label: 'Odoo Apps', icon: <Box className="h-4 w-4" /> },
    { id: 'databases', label: 'Databases', icon: <Database className="h-4 w-4" /> },
    { id: 'addons', label: 'Addons', icon: <Puzzle className="h-4 w-4" /> },
    { id: 'backups', label: 'Backups', icon: <Archive className="h-4 w-4" /> },
    { id: 'containers', label: 'Containers', icon: <Container className="h-4 w-4" /> },
    { id: 'settings', label: 'Settings', icon: <SettingsIcon className="h-4 w-4" /> },
  ]

  const openInstance = (id: string) => {
    setSelectedInstanceId(id)
    setPage('instance-detail')
  }

  return (
    <div className="min-h-screen flex bg-slate-50">
      {/* Dark Sidebar */}
      <aside className="w-60 shrink-0 bg-slate-900 text-slate-300 flex flex-col">
        <div className="flex items-center gap-2.5 px-5 py-5 border-b border-slate-800">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-[#714B67] text-white shadow-md">
            <Server className="h-5 w-5" />
          </div>
          <div>
            <div className="text-base font-bold text-white leading-tight">RunOdoo</div>
            <div className="text-[11px] text-slate-400">Odoo 17–20 PaaS</div>
          </div>
        </div>

        <nav className="p-3 space-y-1">
          {navItems.map((item) => {
            const active = page === item.id || (page === 'instance-detail' && item.id === 'instances')
            return (
              <button
                key={item.id}
                onClick={() => {
                  setSelectedInstanceId(null)
                  setPage(item.id)
                }}
                className={`w-full flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition cursor-pointer ${
                  active
                    ? 'bg-[#714B67] text-white shadow-xs'
                    : 'text-slate-300 hover:bg-slate-800 hover:text-white'
                }`}
              >
                {item.icon}
                {item.label}
              </button>
            )
          })}

          <div className="pt-2 mt-2 border-t border-slate-800">
            <button
              type="button"
              onClick={() => {
                clearToken()
                setAuthed(false)
              }}
              className="w-full flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium text-slate-300 hover:bg-slate-800 hover:text-white transition cursor-pointer"
            >
              <LogOut className="h-4 w-4" />
              Logout
            </button>
          </div>
        </nav>
      </aside>

      {/* Main Content */}
      <main className="flex-1 overflow-y-auto p-6 lg:p-8">
        <ErrorBoundary key={page + (selectedInstanceId || '')}>
          {page === 'dashboard' && (
            <Dashboard
              onNavigate={(p) => setPage(p)}
              onSelectInstance={openInstance}
              notify={notify}
            />
          )}
          {page === 'instances' && <Instances onSelectInstance={openInstance} notify={notify} />}
          {page === 'instance-detail' && selectedInstanceId && (
            <InstanceDetail
              instanceId={selectedInstanceId}
              onBack={() => setPage('instances')}
              notify={notify}
            />
          )}
          {page === 'databases' && <Databases notify={notify} />}
          {page === 'addons' && <Addons notify={notify} />}
          {page === 'backups' && <Backups notify={notify} />}
          {page === 'containers' && <Containers notify={notify} />}
          {page === 'settings' && <Settings notify={notify} />}
        </ErrorBoundary>
      </main>

      {/* Toast Notification */}
      {toast && (
        <div
          className={`fixed bottom-5 right-5 z-50 rounded-xl px-4 py-3 text-sm font-medium text-white shadow-xl ${
            toast.type === 'error' ? 'bg-rose-600' : 'bg-slate-900'
          }`}
        >
          {toast.msg}
        </div>
      )}
    </div>
  )
}
