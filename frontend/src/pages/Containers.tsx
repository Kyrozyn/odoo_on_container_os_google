import React, { useEffect, useState } from 'react'
import { Loader2, Play, RefreshCw, RotateCw, Square, Terminal } from 'lucide-react'
import { api } from '../api/client'
import type { ContainerSummary } from '../types'
import { Modal, StatusBadge } from '../components/UI'

export function Containers({ notify }: { notify: (msg: string, type?: 'success' | 'error') => void }) {
  const [containers, setContainers] = useState<ContainerSummary[]>([])
  const [onlyManaged, setOnlyManaged] = useState(true)
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [logModal, setLogModal] = useState<{ name: string; logs: string } | null>(null)

  const load = async () => {
    setLoading(true)
    try {
      setContainers(await api.listContainers(onlyManaged))
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading containers', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [onlyManaged])

  const act = async (id: string, action: 'start' | 'stop' | 'restart') => {
    setBusyId(id)
    try {
      if (action === 'start') await api.startContainer(id)
      if (action === 'stop') await api.stopContainer(id)
      if (action === 'restart') await api.restartContainer(id)
      notify(`Container ${action} succeeded`)
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : `Failed to ${action}`, 'error')
    } finally {
      setBusyId(null)
    }
  }

  const openLogs = async (c: ContainerSummary) => {
    const name = c.Names?.[0]?.replace(/^\//, '') || c.Id.slice(0, 12)
    try {
      const res = await api.containerLogs(c.Id, 250)
      setLogModal({ name, logs: res.logs })
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed reading logs', 'error')
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Containers</h1>
          <p className="text-sm text-slate-500">Inspect and control Docker containers (Odoo, PostgreSQL, Nginx)</p>
        </div>
        <div className="flex items-center gap-3">
          <label className="inline-flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
            <input type="checkbox" checked={onlyManaged} onChange={(e) => setOnlyManaged(e.target.checked)} />
            RunOdoo managed only
          </label>
          <button
            onClick={load}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
          </button>
        </div>
      </div>

      <div className="rounded-xl border border-slate-200 bg-white shadow-xs overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="bg-slate-50 border-b border-slate-200 text-xs uppercase text-slate-500">
              <tr>
                <th className="px-5 py-3">Name</th>
                <th className="px-5 py-3">Image</th>
                <th className="px-5 py-3">Role</th>
                <th className="px-5 py-3">State</th>
                <th className="px-5 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {containers.map((c) => {
                const name = c.Names?.[0]?.replace(/^\//, '') || c.Id.slice(0, 12)
                return (
                  <tr key={c.Id} className="hover:bg-slate-50">
                    <td className="px-5 py-3 font-mono text-xs font-semibold text-slate-900">{name}</td>
                    <td className="px-5 py-3 text-xs text-slate-600">{c.Image}</td>
                    <td className="px-5 py-3 text-xs">
                      <span className="rounded bg-slate-100 px-2 py-0.5 text-slate-600">
                        {c.Labels?.['runodoo.role'] || 'external'}
                      </span>
                    </td>
                    <td className="px-5 py-3">
                      <StatusBadge status={c.State} />
                    </td>
                    <td className="px-5 py-3 text-right space-x-1">
                      {busyId === c.Id ? (
                        <span className="inline-flex items-center gap-1 text-xs text-[#714B67]">
                          <Loader2 className="h-3.5 w-3.5 animate-spin" /> Working...
                        </span>
                      ) : (
                        <>
                          <button title="Logs" onClick={() => openLogs(c)} className="rounded p-1.5 text-slate-600 hover:bg-slate-100 cursor-pointer">
                            <Terminal className="h-4 w-4" />
                          </button>
                          {c.State !== 'running' ? (
                            <button title="Start" onClick={() => act(c.Id, 'start')} className="rounded p-1.5 text-emerald-600 hover:bg-emerald-50 cursor-pointer">
                              <Play className="h-4 w-4" />
                            </button>
                          ) : (
                            <button title="Stop" onClick={() => act(c.Id, 'stop')} className="rounded p-1.5 text-amber-600 hover:bg-amber-50 cursor-pointer">
                              <Square className="h-4 w-4" />
                            </button>
                          )}
                          <button title="Restart" onClick={() => act(c.Id, 'restart')} className="rounded p-1.5 text-slate-600 hover:bg-slate-100 cursor-pointer">
                            <RotateCw className="h-4 w-4" />
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                )
              })}
              {containers.length === 0 && !loading && (
                <tr>
                  <td colSpan={5} className="px-5 py-8 text-center text-xs text-slate-500">
                    No containers found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <Modal open={!!logModal} onClose={() => setLogModal(null)} title={`Logs: ${logModal?.name}`} maxWidth="max-w-4xl">
        <pre className="max-h-[65vh] overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs text-emerald-300 whitespace-pre-wrap">
          {logModal?.logs || 'No logs.'}
        </pre>
      </Modal>
    </div>
  )
}
