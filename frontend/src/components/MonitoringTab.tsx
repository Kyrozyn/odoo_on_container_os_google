import React, { useEffect, useRef, useState } from 'react'
import {
  Activity,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  Globe,
  KeyRound,
  Loader2,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Save,
  ShieldCheck,
  Square,
  Trash2,
} from 'lucide-react'
import { api } from '../api/client'
import type { DomainConfig, MonitoringStatus, OdooInstance, PanelSettings } from '../types'
import { StatusBadge } from './UI'

function resolveGrafanaUrl(raw: string): string {
  if (!raw) return ''
  if (raw.startsWith(':')) {
    return `http://${window.location.hostname}${raw}`
  }
  return raw.replace(/^(https?:\/\/)+/i, (m) =>
    m.toLowerCase().startsWith('https') ? 'https://' : 'http://'
  )
}

export function MonitoringTab({
  instance,
  settings,
  notify,
}: {
  instance: OdooInstance
  settings: PanelSettings | null
  notify: (msg: string, type?: 'success' | 'error') => void
}) {
  const [status, setStatus] = useState<MonitoringStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)

  // Install form state
  const [instExposePort, setInstExposePort] = useState(true)
  const [instPort, setInstPort] = useState('3001')
  const [instRootSub, setInstRootSub] = useState(!!settings?.rootDomain)
  const [instCustomDomain, setInstCustomDomain] = useState('')
  const [instUser, setInstUser] = useState('admin')
  const [instPass, setInstPass] = useState('')
  const [instAnon, setInstAnon] = useState(false)
  const [installLog, setInstallLog] = useState('')
  const installLogRef = useRef<HTMLPreElement | null>(null)

  // Domain & Port edit state
  const [exposePort, setExposePort] = useState(true)
  const [httpPort, setHttpPort] = useState('3001')
  const [useRootSub, setUseRootSub] = useState(false)
  const [allowAnon, setAllowAnon] = useState(false)
  const [newDomain, setNewDomain] = useState('')

  // Account edit state
  const [accUser, setAccUser] = useState('admin')
  const [accPass, setAccPass] = useState('')
  const [showPass, setShowPass] = useState(false)

  const syncFormFromStatus = (st: MonitoringStatus) => {
    setExposePort(st.config.exposePort)
    setHttpPort(String(st.config.httpPort || 3001))
    setInstPort(String(st.config.httpPort || 3001))
    setUseRootSub(st.config.useRootSubdomain)
    setAllowAnon(st.config.allowAnonymous)
    setAccUser(st.config.grafanaUser || 'admin')
    setAccPass(st.config.grafanaPassword || '')
  }

  const load = async () => {
    setLoading(true)
    try {
      const res = await api.monitoringStatus(instance.id)
      setStatus(res)
      syncFormFromStatus(res)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading monitoring status', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [instance.id])

  useEffect(() => {
    if (installLogRef.current) {
      installLogRef.current.scrollTop = installLogRef.current.scrollHeight
    }
  }, [installLog])

  const handleInstall = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy('install')
    setInstallLog('')
    try {
      const domains: DomainConfig[] = instCustomDomain.trim()
        ? [{ domain: instCustomDomain.trim(), hasSsl: false, forceHttps: false }]
        : []
      await api.monitoringInstallStream(
        instance.id,
        {
          exposePort: instExposePort,
          httpPort: parseInt(instPort, 10) || 3001,
          useRootSubdomain: instRootSub,
          domains,
          grafanaUser: instUser.trim() || 'admin',
          grafanaPassword: instPass.trim(),
          allowAnonymous: instAnon,
        },
        (chunk) => setInstallLog((prev) => prev + chunk)
      )
      await load()
      notify('Prometheus & Grafana stack installed and running!')
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed installing monitoring stack'
      setInstallLog((prev) => prev + `\nERROR: ${msg}\n`)
      notify(msg, 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleControl = async (
    target: 'prometheus' | 'grafana' | 'pg_exporter' | 'all',
    action: 'start' | 'stop' | 'restart'
  ) => {
    setBusy(`${target}:${action}`)
    try {
      const res = await api.monitoringControl(instance.id, target, action)
      setStatus(res)
      notify(`Monitoring (${target}) ${action} succeeded`)
    } catch (err) {
      notify(err instanceof Error ? err.message : `Failed ${action}`, 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleResync = async () => {
    setBusy('resync')
    try {
      const res = await api.monitoringResync(instance.id)
      setStatus(res)
      notify('Dashboards & Prometheus scrape targets re-synced!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed re-syncing monitoring', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleSaveAccess = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy('save-access')
    try {
      const res = await api.monitoringUpdateConfig(instance.id, {
        exposePort,
        httpPort: parseInt(httpPort, 10) || 3001,
        useRootSubdomain: useRootSub,
        allowAnonymous: allowAnon,
      })
      setStatus(res)
      syncFormFromStatus(res)
      notify('Grafana access & port settings updated!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed updating config', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleAddDomain = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!status || !newDomain.trim()) return
    setBusy('add-domain')
    try {
      const nextDomains: DomainConfig[] = [
        ...(status.config.domains || []),
        { domain: newDomain.trim(), hasSsl: false, forceHttps: false },
      ]
      const res = await api.monitoringUpdateConfig(instance.id, { domains: nextDomains })
      setStatus(res)
      setNewDomain('')
      notify('Grafana domain added & Nginx synced!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed adding domain', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleRemoveDomain = async (idx: number) => {
    if (!status) return
    setBusy(`rm-domain:${idx}`)
    try {
      const nextDomains = (status.config.domains || []).filter((_, i) => i !== idx)
      const res = await api.monitoringUpdateConfig(instance.id, { domains: nextDomains })
      setStatus(res)
      notify('Domain removed')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed removing domain', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleIssueSSL = async (domain: string) => {
    setBusy(`ssl:${domain}`)
    try {
      const res = await api.monitoringIssueSSL(instance.id, domain)
      setStatus(res)
      notify(`SSL certificate issued for ${domain}!`)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed issuing SSL', 'error')
    } finally {
      setBusy(null)
    }
  }

  const handleUpdateAccount = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy('save-account')
    try {
      const res = await api.monitoringUpdateAccount(instance.id, accUser, accPass)
      setStatus(res)
      syncFormFromStatus(res)
      notify('Grafana admin account updated!')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed updating Grafana account', 'error')
    } finally {
      setBusy(null)
    }
  }

  if (loading && !status) {
    return (
      <div className="rounded-xl border border-slate-200 bg-white p-8 flex items-center justify-center gap-2 text-sm text-slate-500">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading monitoring status...
      </div>
    )
  }

  const isInstalled =
    status?.config.installed &&
    (status.grafanaStatus !== 'not_found' || status.prometheusStatus !== 'not_found')

  if (!isInstalled) {
    return (
      <form onSubmit={handleInstall} className="rounded-xl border border-slate-200 bg-white p-6 space-y-5 max-w-2xl">
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-purple-50 text-[#714B67]">
            <Activity className="h-5 w-5" />
          </div>
          <div>
            <h3 className="text-base font-bold text-slate-900">Install Advanced Monitoring for {instance.name}</h3>
            <p className="text-xs text-slate-500 mt-0.5">
              Menginstall stack khusus untuk <strong>{instance.name}</strong> (
              <code className="bg-slate-100 px-1 rounded">runodoo-prom-{instance.name}</code>,{' '}
              <code className="bg-slate-100 px-1 rounded">runodoo-grafana-{instance.name}</code>, dan{' '}
              <code className="bg-slate-100 px-1 rounded">runodoo-pgexp-{instance.name}</code>) dengan 2 dashboard otomatis:
              <strong> Odoo Apps Monitoring</strong> &amp; <strong>PostgreSQL Database Monitoring</strong>.
            </p>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Grafana Admin Username</label>
            <input
              type="text"
              required
              value={instUser}
              onChange={(e) => setInstUser(e.target.value)}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">
              Grafana Admin Password <span className="text-slate-400">(kosong = auto-generate)</span>
            </label>
            <input
              type="text"
              value={instPass}
              onChange={(e) => setInstPass(e.target.value)}
              placeholder="Auto-generated if blank"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-mono"
            />
          </div>
        </div>

        <div className="rounded-lg border border-slate-200 bg-slate-50 p-4 space-y-3">
          <div className="text-xs font-semibold text-slate-700">Domain &amp; Port Access</div>
          <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
            <input type="checkbox" checked={instExposePort} onChange={(e) => setInstExposePort(e.target.checked)} />
            Expose Grafana port directly
          </label>
          {instExposePort && (
            <div>
              <label className="mb-1 block text-xs text-slate-600">Host Port</label>
              <input
                type="number"
                value={instPort}
                onChange={(e) => setInstPort(e.target.value)}
                className="w-40 rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm font-mono"
              />
            </div>
          )}
          {settings?.rootDomain && (
            <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
              <input type="checkbox" checked={instRootSub} onChange={(e) => setInstRootSub(e.target.checked)} />
              Enable Root Subdomain (<code className="font-mono text-xs">grafana.{instance.name}.{settings.rootDomain}</code>)
            </label>
          )}
          <div>
            <label className="mb-1 block text-xs text-slate-600">Custom Domain (opsional)</label>
            <input
              type="text"
              value={instCustomDomain}
              onChange={(e) => setInstCustomDomain(e.target.value)}
              placeholder="monitoring.domainanda.com"
              className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
            />
          </div>
          <label className="flex items-center gap-2 text-xs text-slate-600 cursor-pointer">
            <input type="checkbox" checked={instAnon} onChange={(e) => setInstAnon(e.target.checked)} />
            Allow anonymous read-only viewer access
          </label>
        </div>

        <button
          type="submit"
          disabled={!!busy}
          className="inline-flex items-center gap-2 rounded-lg bg-[#714B67] px-5 py-2.5 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
        >
          {busy === 'install' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
          {busy === 'install' ? 'Pulling images & installing stack...' : 'Install Prometheus & Grafana'}
        </button>

        {installLog && (
          <div className="space-y-1.5">
            <div className="text-xs font-semibold text-slate-700">Installation Output</div>
            <pre
              ref={installLogRef}
              className="max-h-72 overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs text-emerald-300 whitespace-pre-wrap"
            >
              {installLog}
            </pre>
          </div>
        )}
      </form>
    )
  }

  const grafanaBase = resolveGrafanaUrl(status!.primaryUrl)
  const odooDashUrl = grafanaBase
    ? `${grafanaBase}/d/runodoo-odoo-apps/runodoo-odoo-apps-monitoring?var-instance=${encodeURIComponent(instance.name)}`
    : ''
  const pgDashUrl = grafanaBase
    ? `${grafanaBase}/d/runodoo-postgresql/runodoo-postgresql-database-monitoring?var-instance=${encodeURIComponent(instance.name)}`
    : ''

  return (
    <div className="space-y-4">
      {/* Stack Status & Quick Dashboard Links */}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h3 className="text-sm font-semibold text-slate-900">Monitoring Stack ({instance.name})</h3>
              <p className="text-xs text-slate-500">Dedicated Prometheus, Grafana &amp; PostgreSQL Exporter</p>
            </div>
            <div className="flex gap-1.5">
              <button
                type="button"
                disabled={!!busy}
                onClick={handleResync}
                className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 cursor-pointer"
              >
                {busy === 'resync' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                Re-sync Dashboards &amp; Targets
              </button>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-2 text-xs">
            {[
              { key: 'grafana' as const, label: 'Grafana', st: status!.grafanaStatus },
              { key: 'prometheus' as const, label: 'Prometheus', st: status!.prometheusStatus },
              { key: 'pg_exporter' as const, label: 'PG Exporter', st: status!.pgExporterStatus },
            ].map((item) => (
              <div key={item.key} className="rounded-lg border border-slate-200 bg-slate-50 p-2.5 space-y-2">
                <div className="flex items-center justify-between gap-1">
                  <span className="font-semibold text-slate-800">{item.label}</span>
                  <StatusBadge status={item.st} />
                </div>
                <div className="flex gap-1">
                  {item.st !== 'running' ? (
                    <button
                      type="button"
                      disabled={!!busy}
                      onClick={() => handleControl(item.key, 'start')}
                      className="rounded border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-700 cursor-pointer"
                    >
                      Start
                    </button>
                  ) : (
                    <button
                      type="button"
                      disabled={!!busy}
                      onClick={() => handleControl(item.key, 'stop')}
                      className="rounded border border-amber-200 bg-amber-50 px-2 py-0.5 text-[11px] text-amber-700 cursor-pointer"
                    >
                      <Square className="h-2.5 w-2.5 inline mr-0.5" />Stop
                    </button>
                  )}
                  <button
                    type="button"
                    disabled={!!busy}
                    onClick={() => handleControl(item.key, 'restart')}
                    className="rounded border border-slate-200 bg-white px-2 py-0.5 text-[11px] text-slate-700 cursor-pointer"
                  >
                    <RotateCw className="h-2.5 w-2.5 inline mr-0.5" />Restart
                  </button>
                </div>
              </div>
            ))}
          </div>

          {grafanaBase ? (
            <div className="flex flex-wrap gap-2 pt-1">
              <a
                href={odooDashUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-3.5 py-2 text-xs font-semibold text-white hover:bg-[#5c3c54]"
              >
                <ExternalLink className="h-3.5 w-3.5" /> Open Odoo Apps Dashboard ({instance.name})
              </a>
              <a
                href={pgDashUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1.5 rounded-lg border border-[#714B67]/40 bg-purple-50 px-3.5 py-2 text-xs font-semibold text-[#714B67] hover:bg-purple-100"
              >
                <ExternalLink className="h-3.5 w-3.5" /> Open PostgreSQL Dashboard ({instance.name})
              </a>
            </div>
          ) : (
            <p className="text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded-lg p-2.5">
              Belum ada port atau domain yang diaktifkan untuk Grafana. Aktifkan di panel Access &amp; Domains di bawah.
            </p>
          )}
        </div>

        {/* Grafana Account Settings */}
        <form onSubmit={handleUpdateAccount} className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
          <div className="flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-[#714B67]" />
            <div>
              <h3 className="text-sm font-semibold text-slate-900">Grafana Account Settings</h3>
              <p className="text-xs text-slate-500">Kredensial login administrator Grafana</p>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-slate-600">Admin Username</label>
              <input
                type="text"
                required
                value={accUser}
                onChange={(e) => setAccUser(e.target.value)}
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-600">Admin Password</label>
              <div className="flex gap-1">
                <input
                  type={showPass ? 'text' : 'password'}
                  required
                  value={accPass}
                  onChange={(e) => setAccPass(e.target.value)}
                  className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-mono"
                />
                <button
                  type="button"
                  onClick={() => setShowPass((v) => !v)}
                  className="rounded-lg border border-slate-300 px-2.5 text-slate-600 hover:bg-slate-50 cursor-pointer"
                >
                  {showPass ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    navigator.clipboard?.writeText(accPass)
                    notify('Grafana password copied!')
                  }}
                  className="rounded-lg border border-slate-300 px-2.5 text-slate-600 hover:bg-slate-50 cursor-pointer"
                >
                  <Copy className="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>

          <button
            type="submit"
            disabled={!!busy}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-xs font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
          >
            {busy === 'save-account' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            Update Grafana Account
          </button>
        </form>
      </div>

      {/* Domains, Port & SSL */}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <form onSubmit={handleSaveAccess} className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
          <div className="flex items-center gap-2">
            <Globe className="h-4 w-4 text-[#714B67]" />
            <h3 className="text-sm font-semibold text-slate-900">Grafana Port &amp; Root Subdomain</h3>
          </div>

          <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
            <input type="checkbox" checked={exposePort} onChange={(e) => setExposePort(e.target.checked)} />
            Expose port directly
          </label>
          {exposePort && (
            <div>
              <label className="mb-1 block text-xs text-slate-600">HTTP Port</label>
              <input
                type="number"
                value={httpPort}
                onChange={(e) => setHttpPort(e.target.value)}
                className="w-40 rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-mono"
              />
            </div>
          )}

          {settings?.rootDomain && (
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-slate-200 bg-slate-50 p-3">
              <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
                <input type="checkbox" checked={useRootSub} onChange={(e) => setUseRootSub(e.target.checked)} />
                <span className="font-mono text-xs">grafana.{instance.name}.{settings.rootDomain}</span>
              </label>
              {status!.config.useRootSubdomain && (
                status!.config.rootSubdomainSsl ? (
                  <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2.5 py-0.5 text-xs font-medium text-emerald-700">
                    <ShieldCheck className="h-3.5 w-3.5" /> SSL Active
                  </span>
                ) : (
                  <button
                    type="button"
                    disabled={!!busy}
                    onClick={() => handleIssueSSL(`grafana.${instance.name}.${settings.rootDomain}`)}
                    className="inline-flex items-center gap-1 rounded-lg border border-[#714B67] px-2.5 py-1 text-xs font-medium text-[#714B67] hover:bg-purple-50 cursor-pointer"
                  >
                    {busy === `ssl:grafana.${instance.name}.${settings.rootDomain}` ? <Loader2 className="h-3 w-3 animate-spin" /> : <ShieldCheck className="h-3 w-3" />}
                    Issue SSL
                  </button>
                )
              )}
            </div>
          )}

          <label className="flex items-center gap-2 text-xs text-slate-600 cursor-pointer">
            <input type="checkbox" checked={allowAnon} onChange={(e) => setAllowAnon(e.target.checked)} />
            Allow anonymous read-only viewer access
          </label>

          <button
            type="submit"
            disabled={!!busy}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-xs font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
          >
            {busy === 'save-access' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            Save Access Settings
          </button>
        </form>

        {/* Custom Domains */}
        <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
          <h3 className="text-sm font-semibold text-slate-900">Grafana Custom Domains &amp; SSL</h3>
          <form onSubmit={handleAddDomain} className="flex gap-2">
            <input
              type="text"
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
              placeholder="grafana.example.com"
              className="flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
            <button
              type="submit"
              disabled={!!busy || !newDomain.trim()}
              className="inline-flex items-center gap-1 rounded-lg bg-[#714B67] px-3.5 py-2 text-xs font-semibold text-white disabled:opacity-50 cursor-pointer"
            >
              <Plus className="h-3.5 w-3.5" /> Add
            </button>
          </form>

          <div className="space-y-2">
            {(status!.config.domains || []).map((d, idx) => (
              <div key={d.domain} className="flex items-center justify-between gap-2 rounded-lg border border-slate-200 px-3 py-2 text-xs">
                <span className="font-mono font-semibold text-slate-800">{d.domain}</span>
                <div className="flex items-center gap-1.5">
                  {d.hasSsl ? (
                    <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700 font-medium">
                      <ShieldCheck className="h-3 w-3" /> SSL
                    </span>
                  ) : (
                    <button
                      type="button"
                      disabled={!!busy}
                      onClick={() => handleIssueSSL(d.domain)}
                      className="rounded border border-[#714B67] px-2 py-0.5 text-[#714B67] hover:bg-purple-50 cursor-pointer"
                    >
                      {busy === `ssl:${d.domain}` ? 'Issuing...' : 'Issue SSL'}
                    </button>
                  )}
                  <button
                    type="button"
                    disabled={!!busy}
                    onClick={() => handleRemoveDomain(idx)}
                    className="rounded p-1 text-slate-400 hover:bg-rose-50 hover:text-rose-600 cursor-pointer"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            ))}
            {(status!.config.domains || []).length === 0 && (
              <p className="text-xs text-slate-400">No custom domains attached yet.</p>
            )}
          </div>
        </div>
      </div>

      {/* Prometheus Scrape Targets */}
      <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
        <h3 className="text-sm font-semibold text-slate-900">Prometheus Scrape Targets ({status!.targets.length})</h3>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {status!.targets.map((t, i) => (
            <div key={`${t.job}-${t.instance}-${i}`} className="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-xs">
              <div className="flex items-center justify-between gap-2">
                <span className="font-semibold text-slate-800">{t.job} / {t.instance || 'runodoo'}</span>
                <span
                  className={`rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase ${
                    t.health === 'up' ? 'bg-emerald-100 text-emerald-700' : 'bg-rose-100 text-rose-700'
                  }`}
                >
                  {t.health}
                </span>
              </div>
              {t.lastError && <p className="mt-1 text-[11px] text-rose-600 truncate">{t.lastError}</p>}
            </div>
          ))}
          {status!.targets.length === 0 && (
            <p className="text-xs text-slate-400">No scrape targets reported yet (wait ~15s after start).</p>
          )}
        </div>
      </div>
    </div>
  )
}
