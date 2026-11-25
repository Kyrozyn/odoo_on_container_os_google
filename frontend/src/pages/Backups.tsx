import React, { useEffect, useRef, useState } from 'react'
import { Archive, Calendar, CheckCircle2, Clock, Download, Loader2, Plus, RotateCcw, Trash2, Upload, XCircle } from 'lucide-react'
import { api } from '../api/client'
import type { BackupJob, BackupRecord, BackupSchedule, DatabaseInfo, OdooInstance } from '../types'
import { formatBytes, Modal } from '../components/UI'

export function Backups({ notify }: { notify: (msg: string, type?: 'success' | 'error') => void }) {
  const [backups, setBackups] = useState<BackupRecord[]>([])
  const [jobs, setJobs] = useState<BackupJob[]>([])
  const [schedules, setSchedules] = useState<BackupSchedule[]>([])
  const [instances, setInstances] = useState<OdooInstance[]>([])
  const [instanceDbs, setInstanceDbs] = useState<DatabaseInfo[]>([])
  const [loadingDbs, setLoadingDbs] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [showUpload, setShowUpload] = useState(false)
  const [showSched, setShowSched] = useState(false)
  const [restoreTarget, setRestoreTarget] = useState<BackupRecord | null>(null)
  const [busy, setBusy] = useState(false)
  const prevActiveRef = useRef(0)

  // Forms
  const [instId, setInstId] = useState('')
  const [dbName, setDbName] = useState('')
  const [restoreDbName, setRestoreDbName] = useState('')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [cronExpr, setCronExpr] = useState('0 2 * * *')
  const [retention, setRetention] = useState(7)

  const load = async () => {
    try {
      const [b, j, sc, insts] = await Promise.all([
        api.listBackups(),
        api.listBackupJobs(),
        api.listSchedules(),
        api.listInstances(),
      ])
      const safeInsts = Array.isArray(insts) ? insts : []
      const safeJobs = Array.isArray(j) ? j : []
      setBackups(Array.isArray(b) ? b : [])
      setJobs(safeJobs)
      setSchedules(Array.isArray(sc) ? sc : [])
      setInstances(safeInsts)
      if (safeInsts.length > 0 && !instId) setInstId(safeInsts[0].id)
      prevActiveRef.current = safeJobs.filter((item) => item.status === 'queued' || item.status === 'running').length
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading backups', 'error')
    }
  }

  useEffect(() => {
    load()
  }, [])

  useEffect(() => {
    if (!instId) {
      setInstanceDbs([])
      return
    }
    let cancelled = false
    setLoadingDbs(true)
    api
      .listInstanceDatabases(instId)
      .then((dbs) => {
        if (cancelled) return
        const safe = Array.isArray(dbs) ? dbs : []
        setInstanceDbs(safe)
        setDbName((prev) => (safe.some((d) => d.name === prev) ? prev : safe[0]?.name || ''))
      })
      .catch(() => {
        if (!cancelled) {
          setInstanceDbs([])
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingDbs(false)
      })
    return () => {
      cancelled = true
    }
  }, [instId])

  useEffect(() => {
    const hasActive = jobs.some((j) => j.status === 'queued' || j.status === 'running')
    if (!hasActive) return
    const timer = setInterval(async () => {
      try {
        const nextJobs = await api.listBackupJobs()
        const safeJobs = Array.isArray(nextJobs) ? nextJobs : []
        setJobs(safeJobs)
        const activeCount = safeJobs.filter((j) => j.status === 'queued' || j.status === 'running').length
        if (activeCount < prevActiveRef.current) {
          const [b, sc] = await Promise.all([api.listBackups(), api.listSchedules()])
          setBackups(Array.isArray(b) ? b : [])
          setSchedules(Array.isArray(sc) ? sc : [])
        }
        prevActiveRef.current = activeCount
      } catch {
        // Ignore transient polling errors
      }
    }, 2000)
    return () => clearInterval(timer)
  }, [jobs])

  const handleCreateBackup = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.createBackup(instId, dbName.trim())
      notify('Backup queued! Check Backup Jobs below for progress.')
      setShowCreate(false)
      setDbName('')
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed queueing backup', 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleUploadRestore = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!uploadFile) return
    setBusy(true)
    try {
      await api.uploadRestoreBackup(instId, dbName.trim(), uploadFile)
      notify(`Restore queued for "${dbName.trim()}"! Check Backup Jobs below for progress.`)
      setShowUpload(false)
      setDbName('')
      setUploadFile(null)
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed queueing restore', 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleRestoreExisting = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!restoreTarget) return
    setBusy(true)
    try {
      await api.restoreBackup(restoreTarget.id, restoreTarget.instanceId, restoreDbName.trim())
      notify(`Restore queued for "${restoreDbName.trim()}"! Check Backup Jobs below for progress.`)
      setRestoreTarget(null)
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed queueing restore', 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleCreateSchedule = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createSchedule({
        instanceId: instId,
        dbName: dbName.trim(),
        cronExpression: cronExpr.trim(),
        retentionCount: retention,
      })
      notify('Scheduled backup created!')
      setShowSched(false)
      setDbName('')
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed creating schedule', 'error')
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Backups &amp; Restore</h1>
          <p className="text-sm text-slate-500">
            Full Odoo database + filestore <code className="bg-slate-100 px-1 rounded">.zip</code> snapshots &amp; scheduled cron backups
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            disabled={instances.length === 0}
            onClick={() => {
              if (!dbName && instanceDbs.length > 0) setDbName(instanceDbs[0].name)
              setShowSched(true)
            }}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <Calendar className="h-4 w-4" /> Schedule Cron
          </button>
          <button
            disabled={instances.length === 0}
            onClick={() => setShowUpload(true)}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <Upload className="h-4 w-4" /> Upload &amp; Restore ZIP
          </button>
          <button
            disabled={instances.length === 0}
            onClick={() => {
              if (!dbName && instanceDbs.length > 0) setDbName(instanceDbs[0].name)
              setShowCreate(true)
            }}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] cursor-pointer"
          >
            <Plus className="h-4 w-4" /> Create Backup
          </button>
        </div>
      </div>

      {/* Backup & Restore Jobs Queue */}
      <div className="rounded-xl border border-slate-200 bg-white shadow-xs overflow-hidden">
        <div className="flex items-center justify-between border-b border-slate-200 px-5 py-3.5">
          <div className="font-semibold text-sm text-slate-900">
            Backup &amp; Restore Jobs ({jobs.length})
          </div>
          {jobs.some((j) => j.status === 'completed' || j.status === 'failed') && (
            <button
              onClick={async () => {
                await api.clearBackupJobs()
                load()
              }}
              className="text-xs font-medium text-slate-500 hover:text-slate-800 cursor-pointer"
            >
              Clear Finished
            </button>
          )}
        </div>
        <div className="divide-y divide-slate-100">
          {jobs.map((j) => (
            <div key={j.id} className="flex flex-wrap items-center justify-between gap-3 px-5 py-3">
              <div className="space-y-0.5">
                <div className="flex items-center gap-2 text-sm font-medium text-slate-900">
                  <span className="uppercase text-[10px] font-semibold rounded bg-slate-100 px-1.5 py-0.5 text-slate-700">
                    {j.type}
                  </span>
                  <span>
                    {j.instanceName} / <span className="font-semibold">{j.dbName}</span>
                  </span>
                  <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-normal text-slate-600">
                    {j.source}
                  </span>
                </div>
                <div className="text-xs text-slate-500">
                  {j.error ? (
                    <span className="text-rose-600">{j.error}</span>
                  ) : (
                    <span>{j.step || j.status}</span>
                  )}
                  {j.fileName && <span className="ml-2 font-mono text-[11px] text-slate-600">({j.fileName})</span>}
                </div>
              </div>
              <div className="flex items-center gap-2">
                {j.status === 'queued' && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-amber-50 border border-amber-200 px-2.5 py-0.5 text-xs font-medium text-amber-700">
                    <Clock className="h-3 w-3" /> Queued
                  </span>
                )}
                {j.status === 'running' && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-blue-50 border border-blue-200 px-2.5 py-0.5 text-xs font-medium text-blue-700">
                    <Loader2 className="h-3 w-3 animate-spin" /> Running
                  </span>
                )}
                {j.status === 'completed' && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 border border-emerald-200 px-2.5 py-0.5 text-xs font-medium text-emerald-700">
                    <CheckCircle2 className="h-3 w-3" /> Completed
                  </span>
                )}
                {j.status === 'failed' && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-rose-50 border border-rose-200 px-2.5 py-0.5 text-xs font-medium text-rose-700">
                    <XCircle className="h-3 w-3" /> Failed
                  </span>
                )}
              </div>
            </div>
          ))}
          {jobs.length === 0 && (
            <div className="px-5 py-6 text-center text-xs text-slate-500">
              No backup or restore jobs in queue.
            </div>
          )}
        </div>
      </div>

      {/* Saved Backups */}
      <div className="rounded-xl border border-slate-200 bg-white shadow-xs overflow-hidden">
        <div className="border-b border-slate-200 px-5 py-3.5 font-semibold text-sm text-slate-900">
          Saved Backup Archives ({backups.length})
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="bg-slate-50 border-b border-slate-200 text-xs uppercase text-slate-500">
              <tr>
                <th className="px-5 py-2.5">Archive</th>
                <th className="px-5 py-2.5">Instance / DB</th>
                <th className="px-5 py-2.5">Size</th>
                <th className="px-5 py-2.5">Created</th>
                <th className="px-5 py-2.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {backups.map((b) => (
                <tr key={b.id} className="hover:bg-slate-50">
                  <td className="px-5 py-3 font-mono text-xs text-slate-800 flex items-center gap-2">
                    <Archive className="h-4 w-4 text-[#714B67]" />
                    {b.fileName}
                    {b.hasFilestore && (
                      <span className="rounded bg-emerald-50 px-1.5 py-0.5 text-[10px] font-sans text-emerald-700">
                        +filestore
                      </span>
                    )}
                  </td>
                  <td className="px-5 py-3 text-xs text-slate-700">
                    {b.instanceName} / <span className="font-semibold">{b.dbName}</span>
                  </td>
                  <td className="px-5 py-3 text-xs text-slate-600">{formatBytes(b.sizeBytes)}</td>
                  <td className="px-5 py-3 text-xs text-slate-500">{new Date(b.createdAt).toLocaleString()}</td>
                  <td className="px-5 py-3 text-right space-x-1.5">
                    <a
                      href={api.downloadBackupUrl(b.id)}
                      className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100"
                    >
                      <Download className="h-3 w-3" /> Download
                    </a>
                    <button
                      onClick={() => {
                        setRestoreTarget(b)
                        setRestoreDbName(b.dbName)
                      }}
                      className="inline-flex items-center gap-1 rounded-lg border border-indigo-200 bg-indigo-50 px-2.5 py-1 text-xs font-medium text-indigo-700 hover:bg-indigo-100 cursor-pointer"
                    >
                      <RotateCcw className="h-3 w-3" /> Restore
                    </button>
                    <button
                      onClick={async () => {
                        if (!confirm(`Delete backup ${b.fileName}?`)) return
                        await api.deleteBackup(b.id)
                        notify('Backup deleted')
                        load()
                      }}
                      className="rounded-lg p-1 text-rose-500 hover:bg-rose-50 cursor-pointer"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </td>
                </tr>
              ))}
              {backups.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-5 py-8 text-center text-xs text-slate-500">
                    No backups yet. Create one manually or set up a cron schedule.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Scheduled Backups */}
      <div className="rounded-xl border border-slate-200 bg-white shadow-xs overflow-hidden">
        <div className="border-b border-slate-200 px-5 py-3.5 font-semibold text-sm text-slate-900">
          Scheduled Cron Backups ({schedules.length})
        </div>
        <div className="divide-y divide-slate-100">
          {schedules.map((sc) => {
            const inst = instances.find((i) => i.id === sc.instanceId)
            return (
              <div key={sc.id} className="flex flex-wrap items-center justify-between gap-3 px-5 py-3">
                <div>
                  <div className="text-sm font-medium text-slate-900">
                    {inst?.name || sc.instanceId} / <span className="font-semibold">{sc.dbName}</span>
                  </div>
                  <div className="text-xs text-slate-500">
                    Cron: <code className="bg-slate-100 px-1 rounded">{sc.cronExpression}</code> &bull; Keep last {sc.retentionCount}
                    {sc.lastStatus && ` • Last run: ${sc.lastStatus}`}
                  </div>
                </div>
                <button
                  onClick={async () => {
                    await api.deleteSchedule(sc.id)
                    notify('Schedule removed')
                    load()
                  }}
                  className="rounded-lg p-1.5 text-rose-500 hover:bg-rose-50 cursor-pointer"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            )
          })}
          {schedules.length === 0 && (
            <div className="p-6 text-center text-xs text-slate-500">No recurring backup schedules configured.</div>
          )}
        </div>
      </div>

      {/* Modals */}
      <Modal open={showCreate} onClose={() => setShowCreate(false)} title="Create Backup (DB + Filestore)">
        <form onSubmit={handleCreateBackup} className="space-y-4">
          <select value={instId} onChange={(e) => setInstId(e.target.value)} className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm">
            {instances.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
          </select>
          <select
            required
            disabled={loadingDbs || instanceDbs.length === 0}
            value={dbName}
            onChange={(e) => setDbName(e.target.value)}
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm disabled:bg-slate-50 disabled:text-slate-500"
          >
            {loadingDbs && <option value="">Loading databases...</option>}
            {!loadingDbs && instanceDbs.length === 0 && <option value="">No databases found for this instance</option>}
            {!loadingDbs && instanceDbs.map((d) => (
              <option key={d.name} value={d.name}>
                {d.name} ({d.sizePretty})
              </option>
            ))}
          </select>
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setShowCreate(false)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">Cancel</button>
            <button type="submit" disabled={busy || loadingDbs || !dbName} className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white disabled:opacity-60 cursor-pointer">{busy ? 'Queueing...' : 'Create Backup'}</button>
          </div>
        </form>
      </Modal>

      <Modal open={showUpload} onClose={() => setShowUpload(false)} title="Upload & Restore Odoo Backup ZIP">
        <form onSubmit={handleUploadRestore} className="space-y-4">
          <select value={instId} onChange={(e) => setInstId(e.target.value)} className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm">
            {instances.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
          </select>
          <input type="text" required value={dbName} onChange={(e) => setDbName(e.target.value)} placeholder="Target Database Name (will be replaced)" className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm" />
          <input type="file" accept=".zip" required onChange={(e) => setUploadFile(e.target.files?.[0] || null)} className="w-full rounded-lg border border-slate-300 p-2 text-sm" />
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setShowUpload(false)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">Cancel</button>
            <button type="submit" disabled={busy} className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white cursor-pointer">{busy ? 'Restoring...' : 'Restore Now'}</button>
          </div>
        </form>
      </Modal>

      <Modal open={!!restoreTarget} onClose={() => setRestoreTarget(null)} title={`Restore ${restoreTarget?.fileName}`}>
        <form onSubmit={handleRestoreExisting} className="space-y-4">
          <p className="text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded-lg p-3">
            Warning: Target database will be dropped and recreated from this archive.
          </p>
          <input type="text" required value={restoreDbName} onChange={(e) => setRestoreDbName(e.target.value)} placeholder="Target database name" className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm" />
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setRestoreTarget(null)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">Cancel</button>
            <button type="submit" disabled={busy} className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white cursor-pointer">{busy ? 'Restoring...' : 'Confirm Restore'}</button>
          </div>
        </form>
      </Modal>

      <Modal open={showSched} onClose={() => setShowSched(false)} title="New Scheduled Backup">
        <form onSubmit={handleCreateSchedule} className="space-y-4">
          <select value={instId} onChange={(e) => setInstId(e.target.value)} className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm">
            {instances.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
          </select>
          <select
            required
            disabled={loadingDbs || instanceDbs.length === 0}
            value={dbName}
            onChange={(e) => setDbName(e.target.value)}
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm disabled:bg-slate-50 disabled:text-slate-500"
          >
            {loadingDbs && <option value="">Loading databases...</option>}
            {!loadingDbs && instanceDbs.length === 0 && <option value="">No databases found for this instance</option>}
            {!loadingDbs && instanceDbs.map((d) => (
              <option key={d.name} value={d.name}>
                {d.name} ({d.sizePretty})
              </option>
            ))}
          </select>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="mb-1 block text-xs text-slate-600">Cron Expression</label>
              <input type="text" required value={cronExpr} onChange={(e) => setCronExpr(e.target.value)} className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-mono" />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-600">Retention Count</label>
              <input type="number" min={1} max={100} value={retention} onChange={(e) => setRetention(parseInt(e.target.value, 10) || 7)} className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setShowSched(false)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">Cancel</button>
            <button type="submit" className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white cursor-pointer">Save Schedule</button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
