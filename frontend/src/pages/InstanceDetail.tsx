import React, { useEffect, useRef, useState } from 'react'
import {
  AlertCircle,
  ArrowLeft,
  CheckCircle2,
  Copy,
  Database,
  ExternalLink,
  FileCode,
  Globe,
  Loader2,
  Play,
  Plus,
  Puzzle,
  RefreshCw,
  RotateCw,
  Save,
  ShieldCheck,
  Square,
  Terminal,
  Trash2,
} from 'lucide-react'
import { api } from '../api/client'
import type { AddonModule, AddonRepo, ContainerStats, DatabaseInfo, DeployProgress, DomainConfig, OdooInstance } from '../types'
import { StatusBadge } from '../components/UI'

type Tab = 'overview' | 'domains' | 'databases' | 'addons' | 'config' | 'logs'

export function InstanceDetail({
  instanceId,
  onBack,
  notify,
}: {
  instanceId: string
  onBack: () => void
  notify: (msg: string, type?: 'success' | 'error') => void
}) {
  const [inst, setInst] = useState<OdooInstance | null>(null)
  const [stats, setStats] = useState<ContainerStats | null>(null)
  const [tab, setTab] = useState<Tab>('overview')
  const [dbs, setDbs] = useState<DatabaseInfo[]>([])
  const [allAddons, setAllAddons] = useState<AddonRepo[]>([])
  const [recommendedAddons, setRecommendedAddons] = useState<AddonModule[]>([])
  const [selectedRecs, setSelectedRecs] = useState<string[]>([])
  const [logs, setLogs] = useState('')
  const [confRows, setConfRows] = useState<{ key: string; value: string }[]>([])
  const [bulkConfMode, setBulkConfMode] = useState(false)
  const [bulkConfText, setBulkConfText] = useState('')
  const [customConf, setCustomConf] = useState('')
  const [newDomain, setNewDomain] = useState('')
  const [newDbName, setNewDbName] = useState('')
  const [upgradeDb, setUpgradeDb] = useState('')
  const [upgradeMods, setUpgradeMods] = useState('')
  const [busyAction, setBusyAction] = useState<string | null>(null)
  const busy = busyAction !== null
  const [deploy, setDeploy] = useState<DeployProgress | null>(null)
  const [hideDeployCard, setHideDeployCard] = useState(false)
  const deployLogEndRef = useRef<HTMLDivElement | null>(null)

  const loadInstance = async () => {
    try {
      const data = await api.getInstance(instanceId)
      setInst(data)
      setCustomConf(data.customConf || '')
      setSelectedRecs(data.recommendedAddons || [])
      if (data.status === 'running') {
        api.instanceStats(instanceId).then(setStats).catch(() => {})
      }
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading instance', 'error')
    }
  }

  const refreshDeployStatus = async () => {
    try {
      const d = await api.deployStatus(instanceId)
      setDeploy(d)
      if (d.status === 'deploying') {
        setHideDeployCard(false)
      }
      return d
    } catch {
      return null
    }
  }

  useEffect(() => {
    setHideDeployCard(false)
    loadInstance()
    refreshDeployStatus()
  }, [instanceId])

  useEffect(() => {
    if (!deploy || deploy.status !== 'deploying') return
    const timer = setInterval(async () => {
      const d = await refreshDeployStatus()
      if (d && d.status !== 'deploying') {
        loadInstance()
        if (d.status === 'running') {
          notify('Deployment selesai — Odoo sudah berjalan!')
        } else if (d.status === 'failed') {
          notify(`Deployment gagal: ${d.error || 'lihat log'}`, 'error')
        }
      }
    }, 1500)
    return () => clearInterval(timer)
  }, [deploy?.status, instanceId])

  useEffect(() => {
    deployLogEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [deploy?.logs?.length])

  useEffect(() => {
    if (!inst) return
    if (tab === 'databases') {
      api.listInstanceDatabases(inst.id).then(setDbs).catch(() => setDbs([]))
    } else if (tab === 'addons') {
      api.listAddons().then(setAllAddons).catch(() => {})
      api.listRecommendedAddons().then(setRecommendedAddons).catch(() => {})
      api.listInstanceDatabases(inst.id).then((list) => {
        setDbs(list)
        if (list.length > 0 && !upgradeDb) setUpgradeDb(list[0].name)
      }).catch(() => {})
    } else if (tab === 'config') {
      loadConf(inst.id)
    } else if (tab === 'logs') {
      api.instanceLogs(inst.id, 300).then((r) => setLogs(r.logs)).catch((e) => setLogs(e.message))
    }
  }, [tab, inst?.id])

  if (!inst) {
    return <div className="p-8 text-sm text-slate-500">Loading instance...</div>
  }

  const runAction = async (action: 'start' | 'stop' | 'restart') => {
    setBusyAction(action)
    try {
      if (action === 'start') await api.startInstance(inst.id)
      if (action === 'stop') await api.stopInstance(inst.id)
      if (action === 'restart') await api.restartInstance(inst.id)
      notify(`Container ${action} completed`)
      await loadInstance()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Action failed', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleAddDomain = async (e: React.FormEvent) => {
    e.preventDefault()
    const clean = newDomain
      .trim()
      .toLowerCase()
      .replace(/^https?:\/\//, '')
      .replace(/\/.*$/, '')
    if (!clean) return
    const nextDomains: DomainConfig[] = [...(inst.domains || []), { domain: clean, hasSsl: false, forceHttps: false }]
    setBusyAction('add-domain')
    try {
      await api.updateInstance(inst.id, { domains: nextDomains, redeploy: false })
      setNewDomain('')
      notify(`Domain ${clean} attached and Nginx synced`)
      await loadInstance()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed adding domain', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleRemoveDomain = async (domain: string) => {
    const nextDomains = (inst.domains || []).filter((d) => d.domain !== domain)
    setBusyAction(`rm-domain:${domain}`)
    try {
      await api.updateInstance(inst.id, { domains: nextDomains, redeploy: false })
      notify(`Domain ${domain} removed`)
      await loadInstance()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed removing domain', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleIssueSsl = async (domain: string) => {
    setBusyAction(`ssl:${domain}`)
    try {
      await api.issueInstanceSsl(inst.id, domain)
      notify(`Let's Encrypt SSL enabled for ${domain}!`)
      loadInstance()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'SSL issuance failed', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleCreateDb = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newDbName.trim()) return
    setBusyAction('create-db')
    try {
      await api.createDatabase(inst.id, newDbName.trim())
      setNewDbName('')
      notify(`Database "${newDbName.trim()}" created`)
      const list = await api.listInstanceDatabases(inst.id)
      setDbs(list)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Create DB failed', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleToggleAddon = async (repoId: string) => {
    const current = inst.addonRepoIds || []
    const next = current.includes(repoId) ? current.filter((x) => x !== repoId) : [...current, repoId]
    setBusyAction(`addon:${repoId}`)
    try {
      await api.updateInstance(inst.id, { addonRepoIds: next, redeploy: true })
      notify('Redeployment started...')
      setHideDeployCard(false)
      await refreshDeployStatus()
      loadInstance()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed updating addons', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleUpgradeModules = async (e: React.FormEvent) => {
    e.preventDefault()
    const mods = upgradeMods.split(',').map((s) => s.trim()).filter(Boolean)
    if (!upgradeDb || mods.length === 0) return
    setBusyAction('upgrade')
    try {
      await api.upgradeModules(inst.id, upgradeDb, mods)
      notify(`Upgraded modules (${mods.join(', ')}) on ${upgradeDb}!`)
      setUpgradeMods('')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Module upgrade failed', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const rowsToBulkText = (rows: { key: string; value: string }[]) =>
    rows
      .filter((r) => r.key.trim() !== '')
      .map((r) => `${r.key.trim()} = ${r.value}`)
      .join('\n')

  const bulkTextToRows = (text: string): { key: string; value: string }[] => {
    const out: { key: string; value: string }[] = []
    for (const rawLine of text.split('\n')) {
      const line = rawLine.trim()
      if (!line || line.startsWith('#') || line.startsWith(';') || line === '[options]') continue
      const eq = line.indexOf('=')
      if (eq === -1) continue
      const k = line.slice(0, eq).trim()
      const v = line.slice(eq + 1).trim()
      if (k) out.push({ key: k, value: v })
    }
    return out
  }

  const loadConf = async (id: string) => {
    try {
      const c = await api.instanceConf(id)
      const opts = c.confOptions || {}
      const ordered = Array.isArray(c.orderedKeys) ? c.orderedKeys : []
      const seen = new Set<string>()
      const rows: { key: string; value: string }[] = []
      for (const k of ordered) {
        if (k in opts) {
          rows.push({ key: k, value: opts[k] })
          seen.add(k)
        }
      }
      for (const k of Object.keys(opts).sort()) {
        if (!seen.has(k)) rows.push({ key: k, value: opts[k] })
      }
      setConfRows(rows)
      setBulkConfText(rowsToBulkText(rows))
      setCustomConf(c.customConf || '')
    } catch {
      // ignore
    }
  }

  const toggleBulkMode = (checked: boolean) => {
    if (checked) {
      setBulkConfText(rowsToBulkText(confRows))
    } else {
      setConfRows(bulkTextToRows(bulkConfText))
    }
    setBulkConfMode(checked)
  }

  const buildLiveConfPreview = () => {
    const rows = bulkConfMode ? bulkTextToRows(bulkConfText) : confRows
    const lines = ['[options]', ...rows.filter((r) => r.key.trim()).map((r) => `${r.key.trim()} = ${r.value}`)]
    if (customConf.trim()) {
      lines.push('', '# Additional custom directives', customConf.trim())
    }
    return lines.join('\n')
  }

  const handleSaveConfig = async (redeploy: boolean) => {
    setBusyAction(redeploy ? 'save-conf-redeploy' : 'save-conf')
    try {
      const rows = bulkConfMode ? bulkTextToRows(bulkConfText) : confRows
      const confOptions: Record<string, string> = {}
      for (const r of rows) {
        const k = r.key.trim()
        if (k) confOptions[k] = r.value.trim()
      }
      await api.updateInstance(inst.id, {
        confOptions,
        customConf,
        redeploy,
      })
      notify(redeploy ? 'Config saved & redeployment started...' : 'odoo.conf saved!')
      if (redeploy) {
        setHideDeployCard(false)
        await refreshDeployStatus()
      }
      await loadInstance()
      await loadConf(inst.id)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed saving config', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleResetConf = async () => {
    setBusyAction('reset-conf')
    try {
      await api.updateInstance(inst.id, {
        resetConfOptions: true,
        redeploy: false,
      })
      notify('odoo.conf options reset to defaults!')
      await loadConf(inst.id)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed resetting config', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const copyText = async (txt: string, label: string) => {
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(txt)
      } else {
        const el = document.createElement('textarea')
        el.value = txt
        el.style.position = 'fixed'
        el.style.opacity = '0'
        document.body.appendChild(el)
        el.select()
        document.execCommand('copy')
        document.body.removeChild(el)
      }
      notify(`${label} copied`)
    } catch {
      notify(`Failed to copy ${label}`, 'error')
    }
  }

  const tabs: { id: Tab; label: string; icon: React.ReactNode }[] = [
    { id: 'overview', label: 'Overview', icon: <RefreshCw className="h-4 w-4" /> },
    { id: 'domains', label: 'HTTP & Domains', icon: <Globe className="h-4 w-4" /> },
    { id: 'databases', label: 'Databases', icon: <Database className="h-4 w-4" /> },
    { id: 'addons', label: 'Addons', icon: <Puzzle className="h-4 w-4" /> },
    { id: 'config', label: 'odoo.conf', icon: <FileCode className="h-4 w-4" /> },
    { id: 'logs', label: 'Logs', icon: <Terminal className="h-4 w-4" /> },
  ]

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <button
            onClick={onBack}
            className="rounded-lg border border-slate-300 bg-white p-2 text-slate-600 hover:bg-slate-50 cursor-pointer"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <div>
            <div className="flex items-center gap-2.5">
              <h1 className="text-2xl font-bold text-slate-900">{inst.name}</h1>
              <span className="rounded-md bg-purple-100 px-2 py-0.5 text-xs font-semibold text-[#714B67]">
                Odoo {inst.version}
              </span>
              <StatusBadge status={inst.status} />
            </div>
            <p className="text-xs text-slate-500">
              Container: {inst.containerName} &bull; PostgreSQL: {inst.dbContainerName} ({inst.dbMode})
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {(() => {
            const raw = inst.primaryUrl || (inst.exposePort && inst.httpPort ? `:${inst.httpPort}` : '')
            if (!raw) return null
            const targetUrl = raw.startsWith(':')
              ? `http://${window.location.hostname}${raw}`
              : raw.replace(/^(https?:\/\/)+/i, (m) => (m.toLowerCase().startsWith('https') ? 'https://' : 'http://'))
            return (
              <a
                href={targetUrl}
                target="_blank"
                rel="noopener noreferrer"
                title={targetUrl}
                onClick={(e) => {
                  e.preventDefault()
                  window.open(targetUrl, '_blank', 'noopener,noreferrer')
                }}
                className="inline-flex items-center gap-1.5 rounded-lg border border-indigo-200 bg-indigo-50 px-3 py-2 text-sm font-medium text-indigo-700 hover:bg-indigo-100 cursor-pointer"
              >
                Open Odoo <ExternalLink className="h-3.5 w-3.5" />
              </a>
            )
          })()}
          <button
            disabled={busy}
            onClick={() => runAction(inst.status === 'running' ? 'stop' : 'start')}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-60 cursor-pointer"
          >
            {busyAction === 'stop' || busyAction === 'start' ? (
              <Loader2 className="h-4 w-4 animate-spin text-[#714B67]" />
            ) : inst.status === 'running' ? (
              <Square className="h-4 w-4 text-amber-600" />
            ) : (
              <Play className="h-4 w-4 text-emerald-600" />
            )}
            {busyAction === 'stop'
              ? 'Stopping...'
              : busyAction === 'start'
              ? 'Starting...'
              : inst.status === 'running'
              ? 'Stop'
              : 'Start'}
          </button>
          <button
            disabled={busy}
            onClick={() => runAction('restart')}
            className="inline-flex items-center gap-1.5 rounded-lg bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-60 cursor-pointer"
          >
            {busyAction === 'restart' ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RotateCw className="h-4 w-4" />
            )}
            {busyAction === 'restart' ? 'Restarting...' : 'Restart'}
          </button>
        </div>
      </div>

      {/* Live Deployment Progress & Logs Card */}
      {deploy && deploy.status !== 'idle' && !hideDeployCard && (
        <div
          className={`rounded-xl border p-5 shadow-xs space-y-3 ${
            deploy.status === 'failed'
              ? 'border-rose-300 bg-rose-50/70'
              : deploy.status === 'running'
              ? 'border-emerald-300 bg-emerald-50/60'
              : 'border-purple-300 bg-white'
          }`}
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2.5">
              {deploy.status === 'deploying' && <Loader2 className="h-5 w-5 animate-spin text-[#714B67]" />}
              {deploy.status === 'running' && <CheckCircle2 className="h-5 w-5 text-emerald-600" />}
              {deploy.status === 'failed' && <AlertCircle className="h-5 w-5 text-rose-600" />}
              <div>
                <div className="text-sm font-bold text-slate-900">
                  {deploy.status === 'deploying'
                    ? `Deploying... (${deploy.percent}%)`
                    : deploy.status === 'running'
                    ? 'Deployment Selesai!'
                    : 'Deployment Gagal'}
                </div>
                <div className="text-xs text-slate-600">{deploy.step}</div>
              </div>
            </div>
            {deploy.status !== 'deploying' && (
              <button
                onClick={() => setHideDeployCard(true)}
                className="rounded-lg border border-slate-300 bg-white px-2.5 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50 cursor-pointer"
              >
                Tutup
              </button>
            )}
          </div>

          {/* Progress bar */}
          <div className="h-2 w-full overflow-hidden rounded-full bg-slate-200">
            <div
              className={`h-full transition-all duration-500 ${
                deploy.status === 'failed'
                  ? 'bg-rose-500'
                  : deploy.status === 'running'
                  ? 'bg-emerald-500'
                  : 'bg-[#714B67]'
              }`}
              style={{ width: `${Math.max(5, Math.min(100, deploy.percent || 5))}%` }}
            />
          </div>

          {/* Live deployment logs */}
          <div className="max-h-64 overflow-y-auto rounded-lg bg-slate-950 p-3.5 font-mono text-xs text-emerald-300">
            {(deploy.logs || []).map((line, i) => (
              <div
                key={i}
                className={
                  line.includes('ERROR:') || line.includes('✗')
                    ? 'text-rose-400'
                    : line.includes('==>')
                    ? 'text-sky-300 font-semibold mt-1'
                    : 'text-slate-300'
                }
              >
                {line}
              </div>
            ))}
            <div ref={deployLogEndRef} />
          </div>
        </div>
      )}

      {/* Tabs */}
      <div className="flex flex-wrap gap-1 border-b border-slate-200">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`inline-flex items-center gap-1.5 border-b-2 px-4 py-2.5 text-sm font-medium transition cursor-pointer ${
              tab === t.id
                ? 'border-[#714B67] text-[#714B67]'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            {t.icon}
            {t.label}
          </button>
        ))}
      </div>

      {/* Tab Content */}
      {tab === 'overview' && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
            <h3 className="text-sm font-semibold text-slate-900">Connection & Credentials</h3>
            <div className="space-y-2 text-sm">
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <span className="text-slate-500">Docker Image</span>
                <span className="font-mono text-xs">{inst.image}</span>
              </div>
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <span className="text-slate-500">Master Admin Password</span>
                <button
                  onClick={() => copyText(inst.adminPassword, 'Master password')}
                  className="inline-flex items-center gap-1 font-mono text-xs text-[#714B67] hover:underline cursor-pointer"
                >
                  {inst.adminPassword} <Copy className="h-3 w-3" />
                </button>
              </div>
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <span className="text-slate-500">DB Host / User</span>
                <span className="font-mono text-xs">{inst.dbHost} / {inst.dbUser}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-slate-500">Direct HTTP Port</span>
                <span className="font-mono text-xs">{inst.exposePort ? `0.0.0.0:${inst.httpPort} -> 8069` : 'Internal only'}</span>
              </div>
            </div>
          </div>

          <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
            <h3 className="text-sm font-semibold text-slate-900">Live Container Metrics</h3>
            {stats ? (
              <div className="grid grid-cols-2 gap-4 pt-2">
                <div className="rounded-lg bg-slate-50 p-4">
                  <span className="text-xs text-slate-500">CPU Usage</span>
                  <div className="mt-1 text-2xl font-bold text-slate-900">{stats.cpuPercent}%</div>
                </div>
                <div className="rounded-lg bg-slate-50 p-4">
                  <span className="text-xs text-slate-500">Memory Usage</span>
                  <div className="mt-1 text-2xl font-bold text-slate-900">{stats.memoryUsageMb} MB</div>
                  <div className="text-xs text-slate-400">Limit: {stats.memoryLimitMb} MB</div>
                </div>
              </div>
            ) : (
              <p className="text-sm text-slate-500">Start the container to view live CPU &amp; RAM metrics.</p>
            )}
          </div>
        </div>
      )}

      {tab === 'domains' && (
        <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-5">
          <div>
            <h3 className="text-sm font-semibold text-slate-900">Custom Domains &amp; Let's Encrypt SSL</h3>
            <p className="text-xs text-slate-500">
              Nginx automatically routes HTTP, HTTPS, and Odoo <code className="bg-slate-100 px-1 rounded">/websocket</code> traffic to this container.
            </p>
          </div>

          <form onSubmit={handleAddDomain} className="flex gap-2">
            <input
              type="text"
              value={newDomain}
              disabled={busy}
              onChange={(e) => setNewDomain(e.target.value)}
              placeholder="erp.clientdomain.com"
              className="flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm disabled:bg-slate-50"
            />
            <button
              type="submit"
              disabled={busy || !newDomain.trim()}
              className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-medium text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
            >
              {busyAction === 'add-domain' ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Plus className="h-4 w-4" />
              )}
              {busyAction === 'add-domain' ? 'Connecting & Syncing Nginx...' : 'Connect Domain'}
            </button>
          </form>

          <div className="divide-y divide-slate-100 rounded-lg border border-slate-200">
            {(inst.domains || []).length === 0 ? (
              <div className="p-4 text-center text-xs text-slate-500">No custom domains connected yet.</div>
            ) : (
              inst.domains.map((d) => {
                const cleanHost = d.domain.replace(/^https?:\/\//i, '').replace(/\/.*$/, '')
                const domainUrl = `${d.hasSsl ? 'https' : 'http'}://${cleanHost}`
                return (
                <div key={d.domain} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                  <div>
                    <a
                      href={domainUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={(e) => {
                        e.preventDefault()
                        window.open(domainUrl, '_blank', 'noopener,noreferrer')
                      }}
                      className="inline-flex items-center gap-1.5 font-medium text-indigo-600 hover:underline text-sm cursor-pointer"
                    >
                      {domainUrl}
                      <ExternalLink className="h-3.5 w-3.5" />
                    </a>
                    {d.hasSsl && (
                      <span className="ml-2 inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-0.5 text-xs text-emerald-700">
                        <ShieldCheck className="h-3 w-3" /> SSL Active
                      </span>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      disabled={busy}
                      onClick={() => handleIssueSsl(cleanHost)}
                      className="inline-flex items-center gap-1 rounded-lg border border-emerald-300 bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-700 hover:bg-emerald-100 disabled:opacity-60 cursor-pointer"
                    >
                      {busyAction === `ssl:${cleanHost}` && <Loader2 className="h-3 w-3 animate-spin" />}
                      {busyAction === `ssl:${cleanHost}` ? 'Issuing SSL...' : d.hasSsl ? 'Renew SSL' : 'Enable HTTPS (Certbot)'}
                    </button>
                    <button
                      disabled={busy}
                      onClick={() => handleRemoveDomain(d.domain)}
                      className="rounded-lg p-1.5 text-rose-500 hover:bg-rose-50 disabled:opacity-50 cursor-pointer"
                    >
                      {busyAction === `rm-domain:${d.domain}` ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <Trash2 className="h-4 w-4" />
                      )}
                    </button>
                  </div>
                </div>
                )
              })
            )}
          </div>
        </div>
      )}

      {tab === 'databases' && (
        <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h3 className="text-sm font-semibold text-slate-900">Instance Databases</h3>
            <form onSubmit={handleCreateDb} className="flex gap-2">
              <input
                type="text"
                value={newDbName}
                onChange={(e) => setNewDbName(e.target.value)}
                placeholder="new_db_name"
                className="rounded-lg border border-slate-300 px-3 py-1.5 text-sm"
              />
              <button
                type="submit"
                disabled={busy}
                className="inline-flex items-center gap-1 rounded-lg bg-[#714B67] px-3 py-1.5 text-sm font-medium text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
              >
                {busyAction === 'create-db' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                {busyAction === 'create-db' ? 'Creating...' : 'Create DB'}
              </button>
            </form>
          </div>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-slate-200 text-xs uppercase text-slate-500">
                <tr>
                  <th className="py-2">Database</th>
                  <th className="py-2">Owner</th>
                  <th className="py-2">Size</th>
                  <th className="py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {dbs.map((db) => (
                  <tr key={db.name}>
                    <td className="py-2.5 font-medium text-slate-900">{db.name}</td>
                    <td className="py-2.5 font-mono text-xs text-slate-600">{db.owner}</td>
                    <td className="py-2.5 text-xs text-slate-600">{db.sizePretty}</td>
                    <td className="py-2.5 text-right space-x-2">
                      <button
                        disabled={busy}
                        onClick={async () => {
                          setBusyAction(`backup:${db.name}`)
                          try {
                            await api.createBackup(inst.id, db.name)
                            notify(`Backup created for ${db.name}!`)
                          } catch (err) {
                            notify(err instanceof Error ? err.message : 'Backup failed', 'error')
                          } finally {
                            setBusyAction(null)
                          }
                        }}
                        className="inline-flex items-center gap-1 rounded border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-60 cursor-pointer"
                      >
                        {busyAction === `backup:${db.name}` && <Loader2 className="h-3 w-3 animate-spin" />}
                        {busyAction === `backup:${db.name}` ? 'Backing up...' : 'Backup ZIP'}
                      </button>
                      <button
                        disabled={busy}
                        onClick={async () => {
                          if (!confirm(`Drop database "${db.name}" and its filestore?`)) return
                          setBusyAction(`drop:${db.name}`)
                          try {
                            await api.dropDatabase(inst.id, db.name)
                            notify(`Database ${db.name} dropped`)
                            setDbs(await api.listInstanceDatabases(inst.id))
                          } catch (err) {
                            notify(err instanceof Error ? err.message : 'Drop failed', 'error')
                          } finally {
                            setBusyAction(null)
                          }
                        }}
                        className="inline-flex items-center gap-1 rounded border border-rose-200 px-2.5 py-1 text-xs font-medium text-rose-600 hover:bg-rose-50 disabled:opacity-60 cursor-pointer"
                      >
                        {busyAction === `drop:${db.name}` && <Loader2 className="h-3 w-3 animate-spin" />}
                        {busyAction === `drop:${db.name}` ? 'Dropping...' : 'Drop'}
                      </button>
                    </td>
                  </tr>
                ))}
                {dbs.length === 0 && (
                  <tr>
                    <td colSpan={4} className="py-6 text-center text-xs text-slate-500">
                      No databases found yet. Create one above or via Odoo's /web/database/manager.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {tab === 'addons' && (
        <div className="space-y-4">
          {recommendedAddons.length > 0 && (
            <div className="rounded-xl border border-purple-200 bg-white p-5 space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 className="text-sm font-semibold text-[#714B67]">Recommended Addons</h3>
                  <p className="text-xs text-slate-500">
                    Centang modul rekomendasi yang ingin dipasang ke instance ini, lalu klik Save &amp; Redeploy.
                  </p>
                </div>
                <button
                  disabled={busy}
                  onClick={async () => {
                    setBusyAction('save-recs')
                    try {
                      await api.updateInstance(inst.id, { recommendedAddons: selectedRecs, redeploy: true })
                      notify('Redeployment started...')
                      setHideDeployCard(false)
                      await refreshDeployStatus()
                      loadInstance()
                    } catch (err) {
                      notify(err instanceof Error ? err.message : 'Failed updating recommended addons', 'error')
                    } finally {
                      setBusyAction(null)
                    }
                  }}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
                >
                  {busyAction === 'save-recs' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                  {busyAction === 'save-recs' ? 'Saving...' : `Save & Redeploy (${selectedRecs.length})`}
                </button>
              </div>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
                {recommendedAddons.map((mod) => {
                  const checked = selectedRecs.includes(mod.relPath) || selectedRecs.includes(mod.technicalName)
                  return (
                    <label
                      key={mod.relPath}
                      className={`flex items-start gap-2 rounded-lg border p-2.5 text-xs cursor-pointer ${
                        checked ? 'border-[#714B67] bg-purple-50/40' : 'border-slate-200 hover:bg-slate-50'
                      }`}
                    >
                      <input
                        type="checkbox"
                        className="mt-0.5"
                        checked={checked}
                        onChange={() =>
                          setSelectedRecs((prev) =>
                            checked
                              ? prev.filter((x) => x !== mod.relPath && x !== mod.technicalName)
                              : [...prev, mod.relPath]
                          )
                        }
                      />
                      <div className="min-w-0">
                        <div className="font-semibold text-slate-900 truncate">{mod.name}</div>
                        <div className="font-mono text-[10px] text-slate-500 truncate">
                          {mod.technicalName} {mod.version ? `• v${mod.version}` : ''}
                        </div>
                      </div>
                    </label>
                  )
                })}
              </div>
            </div>
          )}

          <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
            <h3 className="text-sm font-semibold text-slate-900">Attached Addon Repositories</h3>
            <p className="text-xs text-slate-500">
              Toggle repositories to mount into <code className="bg-slate-100 px-1 rounded">/mnt/extra-addons</code>. RunOdoo auto-scans <code className="bg-slate-100 px-1 rounded">__manifest__.py</code> paths.
            </p>
            {allAddons.length === 0 ? (
              <p className="text-xs text-slate-500">No addon repositories in library yet. Add Git repos or upload ZIPs in the Addons page.</p>
            ) : (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {allAddons.map((repo) => {
                  const attached = (inst.addonRepoIds || []).includes(repo.id)
                  return (
                    <div
                      key={repo.id}
                      className={`flex items-center justify-between rounded-lg border p-3.5 ${
                        attached ? 'border-[#714B67] bg-purple-50/40' : 'border-slate-200'
                      }`}
                    >
                      <div>
                        <div className="font-medium text-sm text-slate-900">{repo.name}</div>
                        <div className="text-xs text-slate-500">{repo.modules.length} modules ({repo.sourceType})</div>
                      </div>
                      <button
                        disabled={busy}
                        onClick={() => handleToggleAddon(repo.id)}
                        className={`rounded-lg px-3 py-1.5 text-xs font-semibold cursor-pointer ${
                          attached
                            ? 'bg-rose-50 text-rose-700 border border-rose-200 hover:bg-rose-100'
                            : 'bg-[#714B67] text-white hover:bg-[#5c3c54]'
                        }`}
                      >
                        {attached ? 'Detach' : 'Attach & Redeploy'}
                      </button>
                    </div>
                  )
                })}
              </div>
            )}
          </div>

          <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
            <h3 className="text-sm font-semibold text-slate-900">CLI Module Upgrade (`odoo -u`)</h3>
            <form onSubmit={handleUpgradeModules} className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <input
                type="text"
                value={upgradeDb}
                onChange={(e) => setUpgradeDb(e.target.value)}
                placeholder="Database name"
                className="rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <input
                type="text"
                value={upgradeMods}
                onChange={(e) => setUpgradeMods(e.target.value)}
                placeholder="Modules (comma-separated, e.g. sale,om_theme_lite)"
                className="rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <button
                type="submit"
                disabled={busy}
                className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 cursor-pointer"
              >
                {busy ? 'Upgrading...' : 'Run Upgrade (-u)'}
              </button>
            </form>
          </div>
        </div>
      )}

      {tab === 'config' && (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-5">
          <div className="xl:col-span-3 space-y-4">
            {/* CapRover-style Standard odoo.conf Editor (Key/Value + Bulk Edit) */}
            <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-slate-900">odoo.conf Options</h3>
                  <p className="text-xs text-slate-500">
                    Edit semua parameter <code className="bg-slate-100 px-1 rounded">[options]</code> satu per satu atau centang <strong>Bulk Edit</strong> (ala CapRover).
                  </p>
                </div>
                <div className="flex items-center gap-3">
                  <label className="inline-flex items-center gap-2 rounded-lg border border-slate-300 bg-slate-50 px-3 py-1.5 text-xs font-medium text-slate-700 cursor-pointer select-none">
                    <input
                      type="checkbox"
                      checked={bulkConfMode}
                      onChange={(e) => toggleBulkMode(e.target.checked)}
                    />
                    Bulk Edit
                  </label>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={handleResetConf}
                    className="rounded-lg border border-slate-300 px-2.5 py-1.5 text-xs font-medium text-slate-600 hover:bg-slate-50 disabled:opacity-50 cursor-pointer"
                  >
                    {busyAction === 'reset-conf' ? 'Resetting...' : 'Reset Defaults'}
                  </button>
                </div>
              </div>

              {bulkConfMode ? (
                <div className="space-y-1.5">
                  <p className="text-[11px] text-slate-500">
                    Format: <code className="bg-slate-100 px-1 rounded">key = value</code> (satu per baris). Uncheck <strong>Bulk Edit</strong> untuk kembali ke mode textbox.
                  </p>
                  <textarea
                    rows={16}
                    value={bulkConfText}
                    onChange={(e) => setBulkConfText(e.target.value)}
                    className="w-full rounded-lg border border-slate-300 bg-slate-50 p-3 font-mono text-xs focus:border-[#714B67] focus:bg-white focus:outline-none"
                  />
                </div>
              ) : (
                <div className="space-y-2">
                  <div className="grid grid-cols-12 gap-2 px-1 text-[11px] font-semibold uppercase text-slate-400">
                    <div className="col-span-5">Key</div>
                    <div className="col-span-6">Value</div>
                    <div className="col-span-1" />
                  </div>
                  <div className="max-h-[420px] overflow-y-auto space-y-2 pr-1">
                    {confRows.map((row, idx) => (
                      <div key={idx} className="grid grid-cols-12 items-center gap-2">
                        <input
                          type="text"
                          value={row.key}
                          onChange={(e) => {
                            const val = e.target.value
                            setConfRows((prev) => prev.map((r, i) => (i === idx ? { ...r, key: val } : r)))
                          }}
                          placeholder="option_key"
                          className="col-span-5 rounded-lg border border-slate-300 bg-slate-50 px-2.5 py-1.5 font-mono text-xs focus:border-[#714B67] focus:bg-white focus:outline-none"
                        />
                        <input
                          type="text"
                          value={row.value}
                          onChange={(e) => {
                            const val = e.target.value
                            setConfRows((prev) => prev.map((r, i) => (i === idx ? { ...r, value: val } : r)))
                          }}
                          placeholder="value"
                          className="col-span-6 rounded-lg border border-slate-300 px-2.5 py-1.5 font-mono text-xs focus:border-[#714B67] focus:outline-none"
                        />
                        <button
                          type="button"
                          title="Remove option"
                          onClick={() => setConfRows((prev) => prev.filter((_, i) => i !== idx))}
                          className="col-span-1 flex justify-center rounded-lg p-1.5 text-slate-400 hover:bg-rose-50 hover:text-rose-600 cursor-pointer"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    ))}
                  </div>
                  <button
                    type="button"
                    onClick={() => setConfRows((prev) => [...prev, { key: '', value: '' }])}
                    className="inline-flex items-center gap-1 rounded-lg border border-dashed border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-600 hover:border-[#714B67] hover:text-[#714B67] cursor-pointer"
                  >
                    <Plus className="h-3.5 w-3.5" /> Add Option
                  </button>
                </div>
              )}
            </div>

            {/* Additional Custom Directives */}
            <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
              <div>
                <h3 className="text-sm font-semibold text-slate-900">Additional Custom Directives</h3>
                <p className="text-xs text-slate-500">
                  Tambahkan directive khusus di luar opsi standar (akan ditambahkan di bagian akhir <code className="bg-slate-100 px-1 rounded">odoo.conf</code>).
                </p>
              </div>
              <textarea
                rows={5}
                value={customConf}
                onChange={(e) => setCustomConf(e.target.value)}
                placeholder="Contoh:&#10;dbfilter = ^%h$&#10;log_level = info&#10;without_demo = all"
                className="w-full rounded-lg border border-slate-300 p-3 font-mono text-xs focus:border-[#714B67] focus:outline-none"
              />
              <div className="flex flex-wrap gap-2 pt-1">
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => handleSaveConfig(false)}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-60 cursor-pointer"
                >
                  {busyAction === 'save-conf' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                  {busyAction === 'save-conf' ? 'Saving...' : 'Save Only'}
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => handleSaveConfig(true)}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
                >
                  {busyAction === 'save-conf-redeploy' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                  {busyAction === 'save-conf-redeploy' ? 'Saving & Redeploying...' : 'Save & Redeploy'}
                </button>
              </div>
            </div>
          </div>

          {/* Live Preview */}
          <div className="xl:col-span-2 rounded-xl border border-slate-200 bg-white p-5 space-y-3 h-fit sticky top-6">
            <h3 className="text-sm font-semibold text-slate-900">Live Preview `/etc/odoo/odoo.conf`</h3>
            <pre className="max-h-[560px] overflow-auto rounded-lg bg-slate-900 p-4 font-mono text-xs text-slate-100 whitespace-pre-wrap">
              {buildLiveConfPreview()}
            </pre>
          </div>
        </div>
      )}

      {tab === 'logs' && (
        <div className="rounded-xl border border-slate-200 bg-white p-5 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-slate-900">Container Logs (last 300 lines)</h3>
            <button
              onClick={() => api.instanceLogs(inst.id, 300).then((r) => setLogs(r.logs))}
              className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
            >
              <RefreshCw className="h-3.5 w-3.5" /> Refresh Logs
            </button>
          </div>
          <pre className="max-h-[520px] overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs text-emerald-300 whitespace-pre-wrap">
            {logs || 'No logs available.'}
          </pre>
        </div>
      )}
    </div>
  )
}
