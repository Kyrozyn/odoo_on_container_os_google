import React, { useEffect, useState } from 'react'
import { useAutoRefresh } from '../hooks/useAutoRefresh'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  Copy,
  Cpu,
  Database,
  Download,
  HardDrive,
  Loader2,
  Plus,
  RefreshCw,
  RotateCw,
  Save,
  Sliders,
  Sparkles,
  Trash2,
  Zap,
} from 'lucide-react'
import { api } from '../api/client'
import type { DatabaseInfo, OdooInstance, PostgresMetrics, PostgresSetting } from '../types'
import { Modal, StatusBadge } from '../components/UI'
import { CreateDatabaseModal } from '../components/CreateDatabaseModal'

export function DatabaseInstanceDetail({
  instance,
  onBack,
  onChanged,
  notify,
}: {
  instance: OdooInstance
  onBack: () => void
  onChanged: () => void
  notify: (msg: string, type?: 'success' | 'error') => void
}) {
  const [metrics, setMetrics] = useState<PostgresMetrics | null>(null)
  const [dbs, setDbs] = useState<DatabaseInfo[]>([])
  const [settings, setSettings] = useState<PostgresSetting[]>([])
  const [editedValues, setEditedValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [busyAction, setBusyAction] = useState<string | null>(null)
  const [maintOutput, setMaintOutput] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [dupTarget, setDupTarget] = useState<string | null>(null)
  const [dupNewName, setDupNewName] = useState('')

  const loadAll = async () => {
    setLoading(true)
    try {
      const [mRes, dbRes, cfgRes] = await Promise.all([
        api.getPostgresMetrics(instance.id),
        api.listInstanceDatabases(instance.id),
        api.getPostgresConfig(instance.id),
      ])
      setMetrics(mRes)
      setDbs(Array.isArray(dbRes) ? dbRes : [])
      const safeCfg = Array.isArray(cfgRes) ? cfgRes : []
      setSettings(safeCfg)
      const initial: Record<string, string> = {}
      for (const s of safeCfg) {
        initial[s.name] = s.setting
      }
      setEditedValues(initial)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading PostgreSQL details', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadAll()
  }, [instance.id])

  useAutoRefresh(
    async () => {
      const mRes = await api.getPostgresMetrics(instance.id)
      setMetrics(mRes)
    },
    5000,
    !busyAction
  )

  const modifiedUpdates: Record<string, string> = {}
  for (const s of settings) {
    const val = (editedValues[s.name] ?? '').trim()
    if (val !== s.setting) {
      modifiedUpdates[s.name] = val
    }
  }
  const hasChanges = Object.keys(modifiedUpdates).length > 0
  const anyPendingRestart = settings.some((s) => s.pendingRestart)

  const handleSaveConfig = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!hasChanges) return
    setBusyAction('save-config')
    try {
      const updated = await api.updatePostgresConfig(instance.id, modifiedUpdates)
      const safeCfg = Array.isArray(updated) ? updated : []
      setSettings(safeCfg)
      const next: Record<string, string> = {}
      for (const s of safeCfg) {
        next[s.name] = s.setting
      }
      setEditedValues(next)
      const pending = safeCfg.some((s) => s.pendingRestart)
      notify(
        pending
          ? 'PostgreSQL config saved & reloaded. Some parameters require a PostgreSQL restart.'
          : 'PostgreSQL config saved & reloaded!'
      )
      await loadAll()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed saving PostgreSQL config', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleRestartPostgres = async () => {
    const msg =
      instance.dbMode === 'shared'
        ? `Restart shared PostgreSQL container (${instance.dbContainerName})? All shared Odoo instances will briefly lose DB connectivity.`
        : `Restart PostgreSQL container (${instance.dbContainerName})?`
    if (!confirm(msg)) return
    setBusyAction('restart-pg')
    try {
      await api.restartPostgres(instance.id)
      notify('PostgreSQL restarted and ready!')
      await loadAll()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed restarting PostgreSQL', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleMaintenance = async (action: 'vacuum_analyze' | 'terminate_idle', dbName?: string) => {
    setBusyAction(`${action}:${dbName || 'all'}`)
    try {
      const res = await api.runPostgresMaintenance(instance.id, action, dbName)
      setMaintOutput(res.message)
      notify(res.message)
      await loadAll()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Maintenance action failed', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  const handleDuplicate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!dupTarget) return
    setBusyAction('duplicate')
    try {
      await api.duplicateDatabase(instance.id, dupTarget, dupNewName.trim())
      notify(`Duplicated "${dupTarget}" -> "${dupNewName.trim()}"!`)
      setDupTarget(null)
      setDupNewName('')
      await loadAll()
      onChanged()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Duplicate failed', 'error')
    } finally {
      setBusyAction(null)
    }
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <button
            onClick={onBack}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <ArrowLeft className="h-4 w-4" /> Back
          </button>
          <div>
            <div className="flex items-center gap-2.5">
              <h1 className="text-2xl font-bold text-slate-900">{instance.name}</h1>
              <StatusBadge status={instance.dbStatus || instance.status} />
              <span
                className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                  instance.dbMode === 'dedicated'
                    ? 'bg-purple-50 text-[#714B67]'
                    : 'bg-sky-50 text-sky-700'
                }`}
              >
                {instance.dbMode === 'dedicated' ? 'Dedicated PostgreSQL' : 'Shared PostgreSQL'}
              </span>
            </div>
            <p className="text-xs text-slate-500 mt-0.5 font-mono">
              {instance.dbContainerName}
              {metrics?.postgresVersion ? ` • PostgreSQL ${metrics.postgresVersion}` : ''}
              {metrics?.uptime ? ` • Up ${metrics.uptime}` : ''}
            </p>
          </div>
        </div>

        <div className="flex flex-wrap gap-2">
          <button
            onClick={loadAll}
            disabled={loading || !!busyAction}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 cursor-pointer"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
          </button>
          <button
            onClick={handleRestartPostgres}
            disabled={!!busyAction}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm font-semibold cursor-pointer disabled:opacity-50 ${
              anyPendingRestart
                ? 'bg-amber-600 text-white hover:bg-amber-700'
                : 'border border-slate-300 bg-white text-slate-700 hover:bg-slate-50'
            }`}
          >
            {busyAction === 'restart-pg' ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RotateCw className="h-4 w-4" />
            )}
            {anyPendingRestart ? 'Restart PostgreSQL (Pending)' : 'Restart PostgreSQL'}
          </button>
          <button
            onClick={() => setShowCreate(true)}
            disabled={!!busyAction}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-50 cursor-pointer"
          >
            <Plus className="h-4 w-4" /> Create Database
          </button>
        </div>
      </div>

      {/* Shared PostgreSQL Warning */}
      {instance.dbMode === 'shared' && (
        <div className="flex items-start gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 text-xs text-amber-800">
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5 text-amber-600" />
          <div>
            <strong>Shared PostgreSQL Mode:</strong> Instance ini berbagi container{' '}
            <code className="rounded bg-amber-100 px-1 py-0.5 font-mono">{instance.dbContainerName}</code> dengan
            shared instance lainnya. Perubahan parameter konfigurasi atau restart PostgreSQL akan memengaruhi semua
            shared instance.
          </div>
        </div>
      )}

      {/* Metrics Cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-slate-500">
            <span>Container CPU &amp; RAM</span>
            <Cpu className="h-4 w-4 text-[#714B67]" />
          </div>
          <div className="mt-2 text-xl font-bold text-slate-900">
            {metrics ? `${metrics.cpuPercent.toFixed(1)}% CPU` : '—'}
          </div>
          <div className="mt-1 text-xs text-slate-500">
            {metrics
              ? `${metrics.memoryUsageMb.toFixed(0)} / ${metrics.memoryLimitMb.toFixed(0)} MB (${metrics.memoryPercent.toFixed(1)}%)`
              : 'Loading stats...'}
          </div>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-slate-500">
            <span>Connections</span>
            <Activity className="h-4 w-4 text-emerald-600" />
          </div>
          <div className="mt-2 text-xl font-bold text-slate-900">
            {metrics ? `${metrics.totalConnections} / ${metrics.maxConnections}` : '—'}
          </div>
          <div className="mt-1 text-xs text-slate-500">
            {metrics
              ? `${metrics.activeConnections} active • ${metrics.idleConnections} idle`
              : 'Loading connections...'}
          </div>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-slate-500">
            <span>Cache Hit Ratio</span>
            <Zap className="h-4 w-4 text-amber-500" />
          </div>
          <div className="mt-2 text-xl font-bold text-slate-900">
            {metrics ? `${metrics.cacheHitRatio.toFixed(2)}%` : '—'}
          </div>
          <div className="mt-1 text-xs text-slate-500">Target ideal &ge; 99% untuk performa optimal</div>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-slate-500">
            <span>Instance Storage</span>
            <HardDrive className="h-4 w-4 text-sky-600" />
          </div>
          <div className="mt-2 text-xl font-bold text-slate-900">
            {metrics ? metrics.totalSizePretty : '—'}
          </div>
          <div className="mt-1 text-xs text-slate-500">
            {metrics ? `${metrics.databaseCount} database(s)` : 'Loading storage...'}
          </div>
        </div>
      </div>

      {/* Databases in Instance (Card Grid) */}
      <div className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-semibold text-slate-900">
            Databases in {instance.name} ({dbs.length})
          </h2>
          <div className="flex flex-wrap gap-2">
            <button
              disabled={!!busyAction || dbs.length === 0}
              onClick={() => handleMaintenance('vacuum_analyze')}
              className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 cursor-pointer"
            >
              {busyAction === 'vacuum_analyze:all' ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Sparkles className="h-3.5 w-3.5 text-[#714B67]" />
              )}
              VACUUM ANALYZE All
            </button>
            <button
              disabled={!!busyAction}
              onClick={() => handleMaintenance('terminate_idle')}
              className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 cursor-pointer"
            >
              {busyAction === 'terminate_idle:all' ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Zap className="h-3.5 w-3.5 text-amber-600" />
              )}
              Terminate Idle Connections
            </button>
          </div>
        </div>

        {maintOutput && (
          <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-2.5 text-xs text-emerald-800">
            {maintOutput}
          </div>
        )}

        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {dbs.map((db) => (
            <div
              key={db.name}
              className="flex flex-col justify-between rounded-xl border border-slate-200 bg-white p-5 shadow-xs"
            >
              <div>
                <div className="flex items-start justify-between gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-purple-50 text-[#714B67]">
                      <Database className="h-4 w-4" />
                    </div>
                    <div className="min-w-0">
                      <h3 className="truncate text-sm font-bold text-slate-900">{db.name}</h3>
                      <p className="truncate text-xs text-slate-500">Owner: {db.owner}</p>
                    </div>
                  </div>
                  <span className="shrink-0 rounded-md bg-slate-100 px-2 py-0.5 text-xs font-semibold text-slate-700">
                    {db.sizePretty}
                  </span>
                </div>
              </div>

              <div className="mt-4 flex flex-wrap items-center gap-1.5 pt-3 border-t border-slate-100">
                <button
                  disabled={!!busyAction}
                  onClick={async () => {
                    setBusyAction(`backup:${db.name}`)
                    try {
                      await api.createBackup(instance.id, db.name)
                      notify(`Backup queued for ${db.name}! Check Backups page.`)
                    } catch (err) {
                      notify(err instanceof Error ? err.message : 'Backup failed', 'error')
                    } finally {
                      setBusyAction(null)
                    }
                  }}
                  className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 cursor-pointer"
                >
                  {busyAction === `backup:${db.name}` ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : (
                    <Download className="h-3 w-3" />
                  )}
                  Backup
                </button>
                <button
                  disabled={!!busyAction}
                  onClick={() => {
                    setDupTarget(db.name)
                    setDupNewName(`${db.name}_copy`)
                  }}
                  className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 cursor-pointer"
                >
                  <Copy className="h-3 w-3" /> Duplicate
                </button>
                <button
                  disabled={!!busyAction}
                  onClick={() => handleMaintenance('vacuum_analyze', db.name)}
                  className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 cursor-pointer"
                >
                  {busyAction === `vacuum_analyze:${db.name}` ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : (
                    <Sparkles className="h-3 w-3" />
                  )}
                  Vacuum
                </button>
                <button
                  disabled={!!busyAction}
                  onClick={async () => {
                    if (!confirm(`Drop database "${db.name}" permanently?`)) return
                    setBusyAction(`drop:${db.name}`)
                    try {
                      await api.dropDatabase(instance.id, db.name)
                      notify(`Dropped ${db.name}`)
                      await loadAll()
                      onChanged()
                    } catch (err) {
                      notify(err instanceof Error ? err.message : 'Drop failed', 'error')
                    } finally {
                      setBusyAction(null)
                    }
                  }}
                  className="ml-auto inline-flex items-center gap-1 rounded-lg border border-rose-200 px-2.5 py-1 text-xs font-medium text-rose-600 hover:bg-rose-50 disabled:opacity-50 cursor-pointer"
                >
                  {busyAction === `drop:${db.name}` ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : (
                    <Trash2 className="h-3 w-3" />
                  )}
                  Drop
                </button>
              </div>
            </div>
          ))}
          {dbs.length === 0 && !loading && (
            <div className="col-span-full rounded-xl border border-dashed border-slate-300 bg-white p-8 text-center text-xs text-slate-500">
              No databases found in this instance yet.
            </div>
          )}
        </div>
      </div>

      {/* PostgreSQL Configuration Tuning */}
      <form
        onSubmit={handleSaveConfig}
        className="rounded-xl border border-slate-200 bg-white p-5 shadow-xs space-y-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Sliders className="h-4 w-4 text-[#714B67]" />
            <div>
              <h2 className="text-base font-semibold text-slate-900">PostgreSQL Configuration Tuning</h2>
              <p className="text-xs text-slate-500">
                Ubah parameter melalui <code className="rounded bg-slate-100 px-1">ALTER SYSTEM SET</code>. Parameter{' '}
                <code className="rounded bg-slate-100 px-1">postmaster</code> membutuhkan Restart PostgreSQL agar aktif.
              </p>
            </div>
          </div>
          <button
            type="submit"
            disabled={!hasChanges || !!busyAction}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-50 cursor-pointer"
          >
            {busyAction === 'save-config' ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Save className="h-4 w-4" />
            )}
            {busyAction === 'save-config' ? 'Saving...' : `Save & Reload${hasChanges ? ` (${Object.keys(modifiedUpdates).length})` : ''}`}
          </button>
        </div>

        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          {settings.map((s) => {
            const changed = (editedValues[s.name] ?? '') !== s.setting
            return (
              <div
                key={s.name}
                className={`rounded-lg border p-3 space-y-1.5 ${
                  changed ? 'border-[#714B67]/60 bg-purple-50/30' : 'border-slate-200 bg-slate-50/50'
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <label className="font-mono text-xs font-semibold text-slate-900">{s.name}</label>
                  <div className="flex items-center gap-1">
                    {s.unit && (
                      <span className="rounded bg-slate-200/70 px-1.5 py-0.5 text-[10px] font-mono text-slate-600">
                        unit: {s.unit}
                      </span>
                    )}
                    <span
                      className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
                        s.context === 'postmaster'
                          ? 'bg-amber-100 text-amber-800'
                          : 'bg-emerald-100 text-emerald-700'
                      }`}
                    >
                      {s.context === 'postmaster' ? 'Restart' : 'Reload'}
                    </span>
                    {s.pendingRestart && (
                      <span className="rounded bg-rose-100 px-1.5 py-0.5 text-[10px] font-semibold text-rose-700">
                        Pending Restart
                      </span>
                    )}
                  </div>
                </div>
                <input
                  type="text"
                  value={editedValues[s.name] ?? ''}
                  onChange={(e) => setEditedValues((prev) => ({ ...prev, [s.name]: e.target.value }))}
                  className="w-full rounded-lg border border-slate-300 bg-white px-2.5 py-1.5 font-mono text-xs focus:border-[#714B67] focus:outline-none"
                />
                {s.shortDesc && <p className="text-[11px] text-slate-500">{s.shortDesc}</p>}
              </div>
            )
          })}
        </div>
      </form>

      <CreateDatabaseModal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        fixedInstanceId={instance.id}
        onSubmit={async (instanceId, payload) => {
          try {
            await api.createDatabase(instanceId, payload)
            notify(`Odoo database "${payload.name}" initialized!`)
            await loadAll()
            onChanged()
          } catch (err) {
            notify(err instanceof Error ? err.message : 'Failed creating DB', 'error')
            throw err
          }
        }}
      />

      <Modal open={!!dupTarget} onClose={() => setDupTarget(null)} title={`Duplicate "${dupTarget}"`}>
        <form onSubmit={handleDuplicate} className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">New Database Name</label>
            <input
              type="text"
              required
              value={dupNewName}
              onChange={(e) => setDupNewName(e.target.value)}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setDupTarget(null)}
              className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!!busyAction}
              className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white disabled:opacity-50 cursor-pointer"
            >
              Duplicate DB + Filestore
            </button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
