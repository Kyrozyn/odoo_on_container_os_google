import React, { useEffect, useState } from 'react'
import { Copy, Globe, KeyRound, Loader2, Lock, Network, RefreshCw, ShieldAlert, ShieldCheck } from 'lucide-react'
import { api, setToken } from '../api/client'
import type { PanelSettings } from '../types'

function computePanelDomain(root: string, sub: string): string {
  const cleanRoot = root.trim().toLowerCase().replace(/^https?:\/\//, '').split('/')[0].split(':')[0].replace(/^\.+|\.+$/g, '')
  if (!cleanRoot) return ''
  const cleanSub = sub.trim().toLowerCase().replace(/^\.+|\.+$/g, '')
  if (!cleanSub || cleanSub === '@') return cleanRoot
  return `${cleanSub}.${cleanRoot}`
}

export function Settings({ notify }: { notify: (msg: string, type?: 'success' | 'error') => void }) {
  const [settings, setSettings] = useState<PanelSettings | null>(null)
  const [rootDomain, setRootDomain] = useState('')
  const [panelSubdomain, setPanelSubdomain] = useState('panel')
  const [email, setEmail] = useState('')
  const [forceHttps, setForceHttps] = useState(false)
  const [disableDirectPort, setDisableDirectPort] = useState(false)
  const [panelPort, setPanelPort] = useState('3000')
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [rateLimitEnabled, setRateLimitEnabled] = useState(false)
  const [maxAttempts, setMaxAttempts] = useState('5')
  const [lockoutMinutes, setLockoutMinutes] = useState('10')
  const [totpSetup, setTotpSetup] = useState<{ secret: string; otpauthUri: string } | null>(null)
  const [totpCode, setTotpCode] = useState('')
  const [disable2faPass, setDisable2faPass] = useState('')
  const [busyAction, setBusyAction] = useState<
    'save' | 'ssl' | 'nginx' | 'port' | 'password' | 'security' | '2fa' | 'revoke' | null
  >(null)
  const busy = busyAction !== null

  const load = async () => {
    try {
      const s = await api.getSettings()
      setSettings(s)
      setRootDomain(s.rootDomain || '')
      setPanelSubdomain(s.panelSubdomain || 'panel')
      setEmail(s.letsEncryptEmail || '')
      setForceHttps(s.panelForceHttps || false)
      setDisableDirectPort(s.disableDirectPort || false)
      setPanelPort(s.panelPort || '3000')
      setRateLimitEnabled(s.loginRateLimitEnabled || false)
      setMaxAttempts(String(s.loginMaxAttempts || 5))
      setLockoutMinutes(String(s.loginLockoutMinutes || 10))
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading settings', 'error')
    }
  }

  useEffect(() => {
    load()
  }, [])

  const reloadOrRedirect = (s: PanelSettings) => {
    const targetDomain = computePanelDomain(s.rootDomain, s.panelSubdomain)
    const onTargetHost = targetDomain && window.location.hostname.toLowerCase() === targetDomain.toLowerCase()
    const onDirectPort = window.location.port !== ''

    setTimeout(() => {
      if (s.disableDirectPort && targetDomain && (!onTargetHost || onDirectPort)) {
        const proto = s.panelHasSsl ? 'https' : 'http'
        window.location.href = `${proto}://${targetDomain}${window.location.pathname}`
        return
      }
      if (s.panelForceHttps && s.panelHasSsl && targetDomain && window.location.protocol === 'http:') {
        window.location.href = `https://${targetDomain}${window.location.pathname}`
        return
      }
      window.location.reload()
    }, 700)
  }

  const handleSaveSettings = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusyAction('save')
    try {
      const s = await api.updateSettings({
        rootDomain,
        panelSubdomain,
        letsEncryptEmail: email,
        panelForceHttps: forceHttps,
        disableDirectPort: rootDomain.trim() ? disableDirectPort : false,
      })
      setSettings(s)
      setDisableDirectPort(s.disableDirectPort || false)
      notify('Settings saved and Nginx reloaded! Reloading...')
      reloadOrRedirect(s)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed saving settings', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleEnableSsl = async () => {
    setBusyAction('ssl')
    try {
      await api.updateSettings({
        rootDomain,
        panelSubdomain,
        letsEncryptEmail: email,
        panelForceHttps: forceHttps,
      })
      const res = await api.enablePanelSsl()
      notify(`HTTPS enabled for ${res.domain}! Reloading...`)
      setTimeout(() => {
        if (forceHttps && window.location.protocol === 'http:') {
          window.location.href = `https://${res.domain}${window.location.pathname}`
          return
        }
        window.location.reload()
      }, 700)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed enabling SSL', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleChangePort = async (e: React.FormEvent) => {
    e.preventDefault()
    const portNum = Number(panelPort.trim())
    if (!Number.isInteger(portNum) || portNum < 1 || portNum > 65535) {
      notify('Port must be an integer between 1 and 65535', 'error')
      return
    }
    setBusyAction('port')
    try {
      const res = await api.changePanelPort(portNum)
      if (!res.recreating) {
        notify(`Panel is already running on port ${res.port}`)
        setBusyAction(null)
        return
      }

      const usingDirectPort = window.location.port !== ''
      const nextUrl = usingDirectPort
        ? `${window.location.protocol}//${window.location.hostname}:${res.port}${window.location.pathname}`
        : window.location.href
      const healthUrl = usingDirectPort
        ? `${window.location.protocol}//${window.location.hostname}:${res.port}/api/health`
        : '/api/health'

      notify(`Recreating RunOdoo container on port ${res.port}... Please wait.`)

      for (let i = 0; i < 25; i++) {
        await new Promise((r) => setTimeout(r, 1000))
        try {
          const resp = await fetch(healthUrl, { cache: 'no-store' })
          if (resp.ok) {
            window.location.href = nextUrl
            return
          }
        } catch {
          // Container still restarting
        }
      }

      notify(`Container recreated. Redirecting to port ${res.port}...`)
      window.location.href = nextUrl
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed changing panel port', 'error')
      setBusyAction(null)
    }
  }

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusyAction('password')
    try {
      const res = await api.changePassword(currentPassword, newPassword)
      if (res.token) setToken(res.token)
      setCurrentPassword('')
      setNewPassword('')
      notify('Admin password updated! All other sessions revoked.')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Password change failed', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleSaveSecurity = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusyAction('security')
    try {
      const updated = await api.updateSettings({
        loginRateLimitEnabled: rateLimitEnabled,
        loginMaxAttempts: parseInt(maxAttempts, 10) || 5,
        loginLockoutMinutes: parseInt(lockoutMinutes, 10) || 10,
      })
      setSettings(updated)
      notify('Login security settings saved!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed saving security settings', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleSetup2FA = async () => {
    setBusyAction('2fa')
    try {
      const res = await api.setup2FA()
      setTotpSetup(res)
      setTotpCode('')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed initializing 2FA', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleEnable2FA = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!totpSetup) return
    setBusyAction('2fa')
    try {
      await api.enable2FA(totpSetup.secret, totpCode.trim())
      setTotpSetup(null)
      setTotpCode('')
      await load()
      notify('Two-Factor Authentication (2FA) enabled!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Invalid 2FA code', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleDisable2FA = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusyAction('2fa')
    try {
      const res = await api.disable2FA(disable2faPass)
      if (res.token) setToken(res.token)
      setDisable2faPass('')
      await load()
      notify('2FA disabled.')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed disabling 2FA', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleRevokeSessions = async () => {
    setBusyAction('revoke')
    try {
      const res = await api.revokeSessions()
      if (res.token) setToken(res.token)
      notify('All other active sessions have been logged out!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed revoking sessions', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h1 className="text-2xl font-bold text-slate-900">Settings</h1>
        <p className="text-sm text-slate-500">Configure RunOdoo Root Domain, HTTPS (Certbot), Nginx, and Admin Password</p>
      </div>

      {/* Domain & SSL */}
      <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-xs space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="flex items-center gap-2 text-base font-semibold text-slate-900">
            <Globe className="h-5 w-5 text-[#714B67]" />
            Panel Domain &amp; Nginx Reverse Proxy
          </h2>
          {settings?.panelHasSsl && (
            <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700 border border-emerald-200">
              <ShieldCheck className="h-3.5 w-3.5" /> SSL Active
            </span>
          )}
        </div>

        <form onSubmit={handleSaveSettings} className="space-y-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Root Domain</label>
              <input
                type="text"
                value={rootDomain}
                onChange={(e) => setRootDomain(e.target.value)}
                placeholder="e.g. odoo.example.com"
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Panel Subdomain</label>
              <input
                type="text"
                value={panelSubdomain}
                onChange={(e) => setPanelSubdomain(e.target.value)}
                placeholder="panel (or @ for root)"
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
          </div>

          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Let's Encrypt Email (for SSL certificates)</label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="admin@example.com"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>

          <div className="space-y-2">
            <div>
              <label className="inline-flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
                <input type="checkbox" checked={forceHttps} onChange={(e) => setForceHttps(e.target.checked)} />
                Force HTTPS redirect on Panel Domain
              </label>
              {!settings?.panelHasSsl && (
                <p className="ml-6 mt-0.5 text-xs text-slate-500">
                  Requires active SSL (Enable HTTPS via Certbot) or an external HTTPS proxy (e.g. Cloudflare) to redirect HTTP to HTTPS.
                </p>
              )}
            </div>

            <div>
              <label className={`inline-flex items-center gap-2 text-sm ${!rootDomain.trim() ? 'text-slate-400 cursor-not-allowed' : 'text-slate-700 cursor-pointer'}`}>
                <input
                  type="checkbox"
                  checked={disableDirectPort}
                  disabled={!rootDomain.trim()}
                  onChange={(e) => setDisableDirectPort(e.target.checked)}
                />
                Disable direct port {settings?.panelPort || '3000'} access (Panel Domain only)
              </label>
              <p className="ml-6 mt-0.5 text-xs text-slate-500">
                Blocks direct access via IP:{settings?.panelPort || '3000'} and allows access only through the configured Panel Domain. Make sure your Panel Domain is reachable first.
              </p>
            </div>
          </div>

          <div className="flex flex-wrap gap-2 pt-2">
            <button
              type="submit"
              disabled={busy}
              className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
            >
              {busyAction === 'save' && <Loader2 className="h-4 w-4 animate-spin" />}
              {busyAction === 'save' ? 'Saving...' : 'Save Domain Settings'}
            </button>
            <button
              type="button"
              disabled={busy || !rootDomain}
              onClick={handleEnableSsl}
              className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-300 bg-emerald-50 px-4 py-2 text-sm font-medium text-emerald-700 hover:bg-emerald-100 disabled:opacity-50 cursor-pointer"
            >
              {busyAction === 'ssl' ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
              {busyAction === 'ssl' ? 'Issuing SSL...' : 'Enable HTTPS (Certbot)'}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={async () => {
                setBusyAction('nginx')
                try {
                  await api.syncProxy()
                  notify('Nginx proxy container synced & reloaded!')
                } catch (err) {
                  notify(err instanceof Error ? err.message : 'Nginx sync failed', 'error')
                } finally {
                  setBusyAction(null)
                }
              }}
              className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-60 cursor-pointer"
            >
              {busyAction === 'nginx' ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              {busyAction === 'nginx' ? 'Syncing Nginx...' : 'Ensure / Reload Nginx'}
            </button>
          </div>
        </form>
      </div>

      {/* Panel Port */}
      <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-xs space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="flex items-center gap-2 text-base font-semibold text-slate-900">
            <Network className="h-5 w-5 text-[#714B67]" />
            Panel Port
          </h2>
          <span className="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-700">
            Current: {settings?.panelPort || '3000'}
          </span>
        </div>
        <p className="text-xs text-slate-500">
          Change the host port used to access RunOdoo directly. Saving will recreate the RunOdoo container with the new port mapping (and roll back automatically if the port is unavailable). Make sure the new port is allowed in your firewall.
        </p>
        <form onSubmit={handleChangePort} className="flex flex-wrap items-end gap-3 max-w-md">
          <div className="flex-1 min-w-[160px]">
            <label className="mb-1 block text-xs font-medium text-slate-700">Host Port (1-65535)</label>
            <input
              type="number"
              min={1}
              max={65535}
              required
              value={panelPort}
              onChange={(e) => setPanelPort(e.target.value)}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <button
            type="submit"
            disabled={busy}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
          >
            {busyAction === 'port' && <Loader2 className="h-4 w-4 animate-spin" />}
            {busyAction === 'port' ? 'Updating Port...' : 'Change Panel Port'}
          </button>
        </form>
      </div>

      {/* Change Admin Password */}
      <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-xs space-y-4">
        <h2 className="flex items-center gap-2 text-base font-semibold text-slate-900">
          <KeyRound className="h-5 w-5 text-[#714B67]" />
          Change Panel Admin Password
        </h2>
        <form onSubmit={handleChangePassword} className="space-y-3 max-w-md">
          <input
            type="password"
            required
            value={currentPassword}
            onChange={(e) => setCurrentPassword(e.target.value)}
            placeholder="Current password"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <input
            type="password"
            required
            minLength={10}
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder="New password (min 10 chars)"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <div className="flex flex-wrap gap-2">
            <button
              type="submit"
              disabled={busy}
              className="inline-flex items-center gap-1.5 rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-60 cursor-pointer"
            >
              {busyAction === 'password' && <Loader2 className="h-4 w-4 animate-spin" />}
              {busyAction === 'password' ? 'Updating...' : 'Update Password'}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={handleRevokeSessions}
              className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-60 cursor-pointer"
            >
              {busyAction === 'revoke' && <Loader2 className="h-4 w-4 animate-spin" />}
              Revoke Other Sessions
            </button>
          </div>
        </form>
      </div>

      {/* Login Rate Limiter */}
      <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-xs space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="flex items-center gap-2 text-base font-semibold text-slate-900">
            <ShieldAlert className="h-5 w-5 text-[#714B67]" />
            Login Rate Limiter &amp; Lockout
          </h2>
          <span
            className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${
              rateLimitEnabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-600'
            }`}
          >
            {rateLimitEnabled ? 'Active' : 'Disabled (Default)'}
          </span>
        </div>
        <p className="text-xs text-slate-500">
          Jika diaktifkan, IP yang gagal login melebihi batas akan diblokir sementara untuk mencegah serangan brute-force.
        </p>
        <form onSubmit={handleSaveSecurity} className="space-y-3 max-w-md">
          <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
            <input
              type="checkbox"
              checked={rateLimitEnabled}
              onChange={(e) => setRateLimitEnabled(e.target.checked)}
            />
            Enable login rate limiting &amp; temporary IP lockout
          </label>
          {rateLimitEnabled && (
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="mb-1 block text-xs font-medium text-slate-700">Max Failed Attempts</label>
                <input
                  type="number"
                  min={1}
                  max={50}
                  value={maxAttempts}
                  onChange={(e) => setMaxAttempts(e.target.value)}
                  className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-slate-700">Lockout (minutes)</label>
                <input
                  type="number"
                  min={1}
                  max={1440}
                  value={lockoutMinutes}
                  onChange={(e) => setLockoutMinutes(e.target.value)}
                  className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
                />
              </div>
            </div>
          )}
          <button
            type="submit"
            disabled={busy}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
          >
            {busyAction === 'security' && <Loader2 className="h-4 w-4 animate-spin" />}
            Save Security Settings
          </button>
        </form>
      </div>

      {/* Two-Factor Authentication (2FA / TOTP) */}
      <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-xs space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="flex items-center gap-2 text-base font-semibold text-slate-900">
            <Lock className="h-5 w-5 text-[#714B67]" />
            Two-Factor Authentication (2FA / TOTP)
          </h2>
          <span
            className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${
              settings?.totpEnabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-600'
            }`}
          >
            {settings?.totpEnabled ? 'Enabled' : 'Not Enabled'}
          </span>
        </div>
        <p className="text-xs text-slate-500">
          Tambahkan kode 6-digit dari aplikasi Authenticator (Google Authenticator, Bitwarden, Authy, 1Password) saat login.
        </p>

        {settings?.totpEnabled ? (
          <form onSubmit={handleDisable2FA} className="space-y-3 max-w-md">
            <input
              type="password"
              required
              value={disable2faPass}
              onChange={(e) => setDisable2faPass(e.target.value)}
              placeholder="Enter admin password to disable 2FA"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
            <button
              type="submit"
              disabled={busy}
              className="inline-flex items-center gap-1.5 rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-sm font-semibold text-rose-700 hover:bg-rose-100 disabled:opacity-60 cursor-pointer"
            >
              {busyAction === '2fa' && <Loader2 className="h-4 w-4 animate-spin" />}
              Disable 2FA
            </button>
          </form>
        ) : totpSetup ? (
          <form onSubmit={handleEnable2FA} className="space-y-3 max-w-lg">
            <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 space-y-2 text-xs">
              <div className="font-semibold text-slate-700">1. Masukkan Setup Key ini ke Authenticator App Anda:</div>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded bg-white border border-slate-200 px-2.5 py-1.5 font-mono text-xs text-slate-900 select-all">
                  {totpSetup.secret}
                </code>
                <button
                  type="button"
                  onClick={() => {
                    navigator.clipboard?.writeText(totpSetup.secret)
                    notify('2FA Secret copied!')
                  }}
                  className="inline-flex items-center gap-1 rounded border border-slate-300 bg-white px-2.5 py-1.5 text-xs text-slate-700 hover:bg-slate-100 cursor-pointer"
                >
                  <Copy className="h-3.5 w-3.5" /> Copy
                </button>
              </div>
              <div className="text-[11px] text-slate-500 break-all">
                URI: <code className="font-mono">{totpSetup.otpauthUri}</code>
              </div>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">
                2. Masukkan kode 6-digit dari Authenticator App untuk verifikasi:
              </label>
              <input
                type="text"
                inputMode="numeric"
                pattern="[0-9]{6}"
                maxLength={6}
                required
                value={totpCode}
                onChange={(e) => setTotpCode(e.target.value)}
                placeholder="123456"
                className="w-48 rounded-lg border border-slate-300 px-3 py-2 font-mono text-sm tracking-widest"
              />
            </div>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setTotpSetup(null)}
                className="rounded-lg border border-slate-300 px-4 py-2 text-sm text-slate-700 cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={busy}
                className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
              >
                {busyAction === '2fa' && <Loader2 className="h-4 w-4 animate-spin" />}
                Verify &amp; Activate 2FA
              </button>
            </div>
          </form>
        ) : (
          <button
            type="button"
            disabled={busy}
            onClick={handleSetup2FA}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
          >
            {busyAction === '2fa' && <Loader2 className="h-4 w-4 animate-spin" />}
            Setup 2FA (Authenticator)
          </button>
        )}
      </div>
    </div>
  )
}
