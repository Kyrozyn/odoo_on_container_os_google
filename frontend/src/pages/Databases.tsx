import React, { useEffect, useState } from 'react'
import { Copy, Database, Download, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import type { DatabaseInfo, OdooInstance } from '../types'
import { Modal } from '../components/UI'

export function Databases({ notify }: { notify: (msg: string, type?: 'success' | 'error') => void }) {
  const [dbs, setDbs] = useState<DatabaseInfo[]>([])
  const [instances, setInstances] = useState<OdooInstance[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [selectedInst, setSelectedInst] = useState('')
  const [dbName, setDbName] = useState('')
  const [dupTarget, setDupTarget] = useState<{ instId: string; src: string } | null>(null)
  const [dupNewName, setDupNewName] = useState('')

  const load = async () => {
    setLoading(true)
    try {
      const [dbList, instList] = await Promise.all([api.listAllDatabases(), api.listInstances()])
      setDbs(dbList)
      setInstances(instList)
      if (instList.length > 0 && !selectedInst) setSelectedInst(instList[0].id)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading databases', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.createDatabase(selectedInst, dbName.trim())
      notify(`Database "${dbName.trim()}" created!`)
      setDbName('')
      setShowCreate(false)
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed creating DB', 'error')
    }
  }

  const handleDuplicate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!dupTarget) return
    try {
      await api.duplicateDatabase(dupTarget.instId, dupTarget.src, dupNewName.trim())
      notify(`Duplicated "${dupTarget.src}" -> "${dupNewName.trim()}"!`)
      setDupTarget(null)
      setDupNewName('')
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Duplicate failed', 'error')
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

      <div className="rounded-xl border border-slate-200 bg-white shadow-xs overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="bg-slate-50 border-b border-slate-200 text-xs uppercase text-slate-500">
              <tr>
                <th className="px-5 py-3">Database Name</th>
                <th className="px-5 py-3">Odoo Instance</th>
                <th className="px-5 py-3">PostgreSQL Container</th>
                <th className="px-5 py-3">Size</th>
                <th className="px-5 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {dbs.map((db) => (
                <tr key={`${db.containerName}-${db.name}`} className="hover:bg-slate-50">
                  <td className="px-5 py-3.5 font-semibold text-slate-900 flex items-center gap-2">
                    <Database className="h-4 w-4 text-[#714B67]" />
                    {db.name}
                  </td>
                  <td className="px-5 py-3.5 text-slate-700">{db.instanceName || '—'}</td>
                  <td className="px-5 py-3.5 font-mono text-xs text-slate-500">{db.containerName}</td>
                  <td className="px-5 py-3.5 text-xs text-slate-600">{db.sizePretty}</td>
                  <td className="px-5 py-3.5 text-right space-x-1.5">
                    {db.instanceId && (
                      <>
                        <button
                          onClick={async () => {
                            try {
                              await api.createBackup(db.instanceId!, db.name)
                              notify(`Backup created for ${db.name}! Check Backups page.`)
                            } catch (err) {
                              notify(err instanceof Error ? err.message : 'Backup failed', 'error')
                            }
                          }}
                          className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 cursor-pointer"
                        >
                          <Download className="h-3 w-3" /> Backup
                        </button>
                        <button
                          onClick={() => {
                            setDupTarget({ instId: db.instanceId!, src: db.name })
                            setDupNewName(`${db.name}_copy`)
                          }}
                          className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 cursor-pointer"
                        >
                          <Copy className="h-3 w-3" /> Duplicate
                        </button>
                        <button
                          onClick={async () => {
                            if (!confirm(`Drop database "${db.name}" permanently?`)) return
                            await api.dropDatabase(db.instanceId!, db.name)
                            notify(`Dropped ${db.name}`)
                            load()
                          }}
                          className="inline-flex items-center gap-1 rounded-lg border border-rose-200 px-2.5 py-1 text-xs font-medium text-rose-600 hover:bg-rose-50 cursor-pointer"
                        >
                          <Trash2 className="h-3 w-3" /> Drop
                        </button>
                      </>
                    )}
                  </td>
                </tr>
              ))}
              {dbs.length === 0 && !loading && (
                <tr>
                  <td colSpan={5} className="px-5 py-10 text-center text-xs text-slate-500">
                    No databases found. Deploy an Odoo instance first or create a database.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <Modal open={showCreate} onClose={() => setShowCreate(false)} title="Create PostgreSQL Database">
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Target Odoo Instance</label>
            <select
              value={selectedInst}
              onChange={(e) => setSelectedInst(e.target.value)}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            >
              {instances.map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name} (Odoo {i.version} — {i.dbMode})
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Database Name</label>
            <input
              type="text"
              required
              value={dbName}
              onChange={(e) => setDbName(e.target.value)}
              placeholder="e.g. prod_erp"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setShowCreate(false)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">
              Cancel
            </button>
            <button type="submit" className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white cursor-pointer">
              Create
            </button>
          </div>
        </form>
      </Modal>

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
            <button type="submit" className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white cursor-pointer">
              Duplicate DB + Filestore
            </button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
