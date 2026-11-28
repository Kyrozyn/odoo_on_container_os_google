import React, { useEffect, useState } from 'react'
import { ArrowRight, Copy, Database, Download, Loader2, Plus, RefreshCw, Server, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import type { DatabaseInfo, OdooInstance } from '../types'
import { Modal, StatusBadge } from '../components/UI'
import { CreateDatabaseModal } from '../components/CreateDatabaseModal'
import { DatabaseInstanceDetail } from './DatabaseInstanceDetail'

type DbTab = 'all' | 'instances'

function formatTotalBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function Databases({ notify }: { notify: (msg: string, type?: 'success' | 'error') => void }) {
  const [tab, setTab] = useState<DbTab>('all')
  const [selectedInstId, setSelectedInstId] = useState<string | null>(null)
  const [dbs, setDbs] = useState<DatabaseInfo[]>([])
  const [instances, setInstances] = useState<OdooInstance[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [dupTarget, setDupTarget] = useState<{ instId: string; src: string } | null>(null)
  const [dupNewName, setDupNewName] = useState('')
  const [busyKey, setBusyKey] = useState<string | null>(null)

  const load = async () => {
    setLoading(true)
    try {
      const [dbList, instList] = await Promise.all([api.listAllDatabases(), api.listInstances()])
      setDbs(Array.isArray(dbList) ? dbList : [])
      setInstances(Array.isArray(instList) ? instList : [])
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading databases', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const selectedInstance = selectedInstId ? instances.find((i) => i.id === selectedInstId) : null
  if (selectedInstance) {
    return (
      <DatabaseInstanceDetail
        instance={selectedInstance}
        onBack={() => setSelectedInstId(null)}
        onChanged={load}
        notify={notify}
      />
    )
  }

  const handleDuplicate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!dupTarget) return
    setBusyKey('duplicate')
    try {
      await api.duplicateDatabase(dupTarget.instId, dupTarget.src, dupNewName.trim())
      notify(`Duplicated "${dupTarget.src}" -> "${dupNewName.trim()}"!`)
      setDupTarget(null)
      setDupNewName('')
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Duplicate failed', 'error')
    } finally {
      setBusyKey(null)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Databases</h1>
          <p className="text-sm text-slate-500">Manage PostgreSQL databases across Shared &amp; Dedicated instances</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={load}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
          </button>
          <button
            disabled={instances.length === 0}
            onClick={() => setShowCreate(true)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-50 cursor-pointer"
          >
            <Plus className="h-4 w-4" /> Create Database
          </button>
        </div>
      </div>

      {/* 2 Tabs: All Databases & By Instance */}
      <div className="flex gap-1 border-b border-slate-200">
        <button
          onClick={() => setTab('all')}
          className={`inline-flex items-center gap-1.5 border-b-2 px-4 py-2.5 text-sm font-medium transition cursor-pointer ${
            tab === 'all'
              ? 'border-[#714B67] text-[#714B67]'
              : 'border-transparent text-slate-500 hover:text-slate-800'
          }`}
        >
          <Database className="h-4 w-4" />
          All Databases ({dbs.length})
        </button>
        <button
          onClick={() => setTab('instances')}
          className={`inline-flex items-center gap-1.5 border-b-2 px-4 py-2.5 text-sm font-medium transition cursor-pointer ${
            tab === 'instances'
              ? 'border-[#714B67] text-[#714B67]'
              : 'border-transparent text-slate-500 hover:text-slate-800'
          }`}
        >
          <Server className="h-4 w-4" />
          By Instance ({instances.length})
        </button>
      </div>

      {tab === 'all' && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {dbs.map((db) => (
            <div
              key={`${db.containerName}-${db.name}`}
              className="flex flex-col justify-between rounded-xl border border-slate-200 bg-white p-5 shadow-xs transition hover:border-[#714B67]/40 hover:shadow-sm"
            >
              <div>
                <div className="flex items-start justify-between gap-2">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-purple-50 text-[#714B67]">
                      <Database className="h-4 w-4" />
                    </div>
                    <div className="min-w-0">
                      <h3 className="truncate text-base font-bold text-slate-900">{db.name}</h3>
                      <p className="truncate text-xs text-slate-500 font-mono">{db.containerName}</p>
                    </div>
                  </div>
                  <span className="shrink-0 rounded-md bg-slate-100 px-2 py-0.5 text-xs font-semibold text-slate-700">
                    {db.sizePretty}
                  </span>
                </div>

                <div className="mt-4 flex flex-wrap gap-1.5 text-xs">
                  {db.instanceName && (
                    <button
                      type="button"
                      onClick={() => {
                        if (db.instanceId) {
                          setSelectedInstId(db.instanceId)
                        }
                      }}
                      className="rounded-md bg-purple-50 px-2 py-0.5 font-medium text-[#714B67] hover:bg-purple-100 cursor-pointer"
                    >
                      Instance: {db.instanceName}
                    </button>
                  )}
                  <span className="rounded-md bg-slate-100 px-2 py-0.5 text-slate-700">
                    Owner: {db.owner}
                  </span>
                </div>
              </div>

              {db.instanceId && (
                <div className="mt-4 flex flex-wrap items-center gap-1.5 pt-3 border-t border-slate-100">
                  <button
                    disabled={!!busyKey}
                    onClick={async () => {
                      setBusyKey(`backup:${db.name}`)
                      try {
                        await api.createBackup(db.instanceId!, db.name)
                        notify(`Backup queued for ${db.name}! Check Backups page.`)
                      } catch (err) {
                        notify(err instanceof Error ? err.message : 'Backup failed', 'error')
                      } finally {
                        setBusyKey(null)
                      }
                    }}
                    className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 cursor-pointer"
                  >
                    {busyKey === `backup:${db.name}` ? (
                      <Loader2 className="h-3 w-3 animate-spin" />
                    ) : (
                      <Download className="h-3 w-3" />
                    )}
                    {busyKey === `backup:${db.name}` ? 'Backing up...' : 'Backup'}
                  </button>
                  <button
                    disabled={!!busyKey}
                    onClick={() => {
                      setDupTarget({ instId: db.instanceId!, src: db.name })
                      setDupNewName(`${db.name}_copy`)
                    }}
                    className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 cursor-pointer"
                  >
                    <Copy className="h-3 w-3" /> Duplicate
                  </button>
                  <button
                    disabled={!!busyKey}
                    onClick={async () => {
                      if (!confirm(`Drop database "${db.name}" permanently?`)) return
                      setBusyKey(`drop:${db.name}`)
                      try {
                        await api.dropDatabase(db.instanceId!, db.name)
                        notify(`Dropped ${db.name}`)
                        load()
                      } catch (err) {
                        notify(err instanceof Error ? err.message : 'Drop failed', 'error')
                      } finally {
                        setBusyKey(null)
                      }
                    }}
                    className="ml-auto inline-flex items-center gap-1 rounded-lg border border-rose-200 px-2.5 py-1 text-xs font-medium text-rose-600 hover:bg-rose-50 disabled:opacity-50 cursor-pointer"
                  >
                    {busyKey === `drop:${db.name}` ? (
                      <Loader2 className="h-3 w-3 animate-spin" />
                    ) : (
                      <Trash2 className="h-3 w-3" />
                    )}
                    {busyKey === `drop:${db.name}` ? 'Dropping...' : 'Drop'}
                  </button>
                </div>
              )}
            </div>
          ))}
          {dbs.length === 0 && !loading && (
            <div className="col-span-full rounded-xl border border-dashed border-slate-300 bg-white p-10 text-center text-xs text-slate-500">
              No databases found. Deploy an Odoo instance first or create a database.
            </div>
          )}
        </div>
      )}

      {tab === 'instances' && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {instances.map((inst) => {
            const instDbs = dbs.filter((d) => d.instanceId === inst.id)
            const totalBytes = instDbs.reduce((acc, d) => acc + (d.sizeBytes || 0), 0)
            return (
              <div
                key={inst.id}
                onClick={() => setSelectedInstId(inst.id)}
                className="group flex flex-col justify-between rounded-xl border border-slate-200 bg-white p-5 shadow-xs transition hover:border-[#714B67]/50 hover:shadow-md cursor-pointer"
              >
                <div>
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <h3 className="text-base font-bold text-slate-900 group-hover:text-[#714B67]">
                        {inst.name}
                      </h3>
                      <p className="text-xs text-slate-500 font-mono">{inst.dbContainerName}</p>
                    </div>
                    <StatusBadge status={inst.dbStatus || inst.status} />
                  </div>

                  <div className="mt-4 flex flex-wrap gap-1.5 text-xs">
                    <span className="rounded-md bg-purple-50 px-2 py-0.5 font-medium text-[#714B67]">
                      Odoo {inst.version}
                    </span>
                    <span
                      className={`rounded-md px-2 py-0.5 font-medium ${
                        inst.dbMode === 'dedicated'
                          ? 'bg-emerald-50 text-emerald-700'
                          : 'bg-sky-50 text-sky-700'
                      }`}
                    >
                      DB: {inst.dbMode}
                    </span>
                    <span className="rounded-md bg-slate-100 px-2 py-0.5 text-slate-700">
                      {instDbs.length} DB(s)
                    </span>
                    <span className="rounded-md bg-slate-100 px-2 py-0.5 text-slate-700">
                      {formatTotalBytes(totalBytes)}
                    </span>
                  </div>
                </div>

                <div className="mt-4 flex items-center justify-between pt-3 border-t border-slate-100 text-xs font-medium text-[#714B67]">
                  <span>Metrics, Postgres Config &amp; Maintenance</span>
                  <ArrowRight className="h-3.5 w-3.5 transition-transform group-hover:translate-x-0.5" />
                </div>
              </div>
            )
          })}
          {instances.length === 0 && !loading && (
            <div className="col-span-full rounded-xl border border-dashed border-slate-300 bg-white p-10 text-center text-xs text-slate-500">
              No Odoo instances deployed yet.
            </div>
          )}
        </div>
      )}

      <CreateDatabaseModal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        instances={instances}
        onSubmit={async (instanceId, payload) => {
          try {
            await api.createDatabase(instanceId, payload)
            notify(`Odoo database "${payload.name}" initialized!`)
            await load()
          } catch (err) {
            notify(err instanceof Error ? err.message : 'Failed creating DB', 'error')
            throw err
          }
        }}
      />

      <Modal open={!!dupTarget} onClose={() => setDupTarget(null)} title={`Duplicate "${dupTarget?.src}"`}>
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
            <button type="button" onClick={() => setDupTarget(null)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">
              Cancel
            </button>
            <button type="submit" disabled={!!busyKey} className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white disabled:opacity-50 cursor-pointer">
              Duplicate DB + Filestore
            </button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
