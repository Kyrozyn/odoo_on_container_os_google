import React, { useEffect, useState } from 'react'
import { Globe, KeyRound, Loader2, RefreshCw, ShieldCheck } from 'lucide-react'
import { api } from '../api/client'
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
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [busyAction, setBusyAction] = useState<'save' | 'ssl' | 'nginx' | 'password' | null>(null)
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

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusyAction('password')
    try {
      await api.changePassword(currentPassword, newPassword)
      setCurrentPassword('')
      setNewPassword('')
      notify('Admin password updated!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Password change failed', 'error')
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
                Disable direct port 3000 access (Panel Domain only)
              </label>
              <p className="ml-6 mt-0.5 text-xs text-slate-500">
                Blocks direct access via IP:3000 and allows access only through the configured Panel Domain. Make sure your Panel Domain is reachable first.
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
            minLength={6}
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder="New password (min 6 chars)"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <button
            type="submit"
            disabled={busy}
            className="inline-flex items-center gap-1.5 rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-60 cursor-pointer"
          >
            {busyAction === 'password' && <Loader2 className="h-4 w-4 animate-spin" />}
            {busyAction === 'password' ? 'Updating...' : 'Update Password'}
          </button>
        </form>
      </div>
    </div>
  )
}
