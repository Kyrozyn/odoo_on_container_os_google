import React, { useEffect, useState } from 'react'
import { ExternalLink, Loader2, Play, Plus, RefreshCw, RotateCw, Square, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import type { AddonModule, AddonRepo, DBMode, OdooInstance, OdooVersion, OdooVersionInfo } from '../types'
import { Modal, StatusBadge } from '../components/UI'

export function Instances({
  onSelectInstance,
  notify,
}: {
  onSelectInstance: (id: string) => void
  notify: (msg: string, type?: 'success' | 'error') => void
}) {
  const [instances, setInstances] = useState<OdooInstance[]>([])
  const [versions, setVersions] = useState<OdooVersionInfo[]>([])
  const [addons, setAddons] = useState<AddonRepo[]>([])
  const [recommendedAddons, setRecommendedAddons] = useState<AddonModule[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [creating, setCreating] = useState(false)
  const [cardBusy, setCardBusy] = useState<Record<string, string>>({})

  // Create form state
  const [name, setName] = useState('')
  const [version, setVersion] = useState<OdooVersion>('18')
  const [customImage, setCustomImage] = useState('')
  const [dbMode, setDbMode] = useState<DBMode>('shared')
  const [defaultDbName, setDefaultDbName] = useState('')
  const [adminPassword, setAdminPassword] = useState('')
  const [exposePort, setExposePort] = useState(true)
  const [httpPort, setHttpPort] = useState('')
  const [useRootSubdomain, setUseRootSubdomain] = useState(true)
  const [customDomain, setCustomDomain] = useState('')
  const [selectedAddons, setSelectedAddons] = useState<string[]>([])
  const [selectedRecommended, setSelectedRecommended] = useState<string[]>([])

  const load = async () => {
    setLoading(true)
    try {
      const [insts, vers, ads, recs] = await Promise.all([
        api.listInstances(),
        api.versions(),
        api.listAddons(),
        api.listRecommendedAddons(),
      ])
      setInstances(Array.isArray(insts) ? insts : [])
      setVersions(Array.isArray(vers) ? vers : [])
      setAddons(Array.isArray(ads) ? ads : [])
      setRecommendedAddons(Array.isArray(recs) ? recs : [])
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading instances', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setCreating(true)
    try {
      const created = await api.createInstance({
        name,
        version,
        customImage: customImage || undefined,
        dbMode,
        defaultDbName: defaultDbName || undefined,
        adminPassword: adminPassword || undefined,
        exposePort,
        httpPort: httpPort ? parseInt(httpPort, 10) : 0,
        useRootSubdomain,
        customDomain: customDomain || undefined,
        addonRepoIds: selectedAddons,
        recommendedAddons: selectedRecommended,
      })
      notify(`Deployment started for "${created.name}"!`)
      setShowCreate(false)
      setName('')
      setCustomImage('')
      setDefaultDbName('')
      setAdminPassword('')
      setCustomDomain('')
      setSelectedAddons([])
      setSelectedRecommended([])
      onSelectInstance(created.id)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed creating instance', 'error')
    } finally {
      setCreating(false)
    }
  }

  const doAction = async (e: React.MouseEvent, id: string, action: 'start' | 'stop' | 'restart') => {
    e.stopPropagation()
    setCardBusy((prev) => ({ ...prev, [id]: action }))
    try {
      if (action === 'start') await api.startInstance(id)
      if (action === 'stop') await api.stopInstance(id)
      if (action === 'restart') await api.restartInstance(id)
      notify(`Instance ${action} completed`)
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : `Failed to ${action}`, 'error')
    } finally {
      setCardBusy((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
    }
  }

  const handleDelete = async (e: React.MouseEvent, inst: OdooInstance) => {
    e.stopPropagation()
    if (!confirm(`Delete Odoo instance "${inst.name}"?`)) return
    const deleteData = confirm('Also delete persistent data & filestore from disk?')
    setCardBusy((prev) => ({ ...prev, [inst.id]: 'delete' }))
    try {
      await api.deleteInstance(inst.id, deleteData)
      notify(`Instance "${inst.name}" deleted`)
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Delete failed', 'error')
    } finally {
      setCardBusy((prev) => {
        const next = { ...prev }
        delete next[inst.id]
        return next
      })
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Odoo Apps</h1>
          <p className="text-sm text-slate-500">
            Deploy and manage Odoo 17, 18, 19 &amp; 20 Community instances
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={load}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={() => setShowCreate(true)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] cursor-pointer"
          >
            <Plus className="h-4 w-4" />
            Install New Odoo
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {instances.map((inst) => (
          <div
            key={inst.id}
            onClick={() => onSelectInstance(inst.id)}
            className="group flex flex-col justify-between rounded-xl border border-slate-200 bg-white p-5 shadow-xs transition hover:border-[#714B67]/50 hover:shadow-md cursor-pointer"
          >
            <div>
              <div className="flex items-start justify-between gap-2">
                <div>
                  <h3 className="text-base font-bold text-slate-900 group-hover:text-[#714B67]">
                    {inst.name}
                  </h3>
                  <p className="text-xs text-slate-500">{inst.image}</p>
                </div>
                <StatusBadge status={inst.status} />
              </div>

              <div className="mt-4 flex flex-wrap gap-1.5 text-xs">
                <span className="rounded-md bg-purple-50 px-2 py-0.5 font-medium text-[#714B67]">
                  Odoo {inst.version} Community
                </span>
                <span className="rounded-md bg-slate-100 px-2 py-0.5 text-slate-700">
                  DB: {inst.dbMode}
                </span>
                {inst.exposePort && (
                  <span className="rounded-md bg-sky-50 px-2 py-0.5 text-sky-700">
                    :{inst.httpPort}
                  </span>
                )}
                {inst.addonRepoIds?.length > 0 && (
                  <span className="rounded-md bg-amber-50 px-2 py-0.5 text-amber-700">
                    {inst.addonRepoIds.length} addon repo(s)
                  </span>
                )}
              </div>

              {inst.primaryUrl && (
                <div className="mt-3 text-xs text-slate-600 truncate">
                  URL:{' '}
                  <a
                    href={inst.primaryUrl.startsWith(':') ? `http://${window.location.hostname}${inst.primaryUrl}` : inst.primaryUrl}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(e) => e.stopPropagation()}
                    className="inline-flex items-center gap-1 font-medium text-indigo-600 hover:underline"
                  >
                    {inst.primaryUrl}
                    <ExternalLink className="h-3 w-3" />
                  </a>
                </div>
              )}
            </div>

            <div className="mt-5 flex items-center justify-between border-t border-slate-100 pt-3">
              <div className="flex items-center gap-1">
                {cardBusy[inst.id] ? (
                  <span className="inline-flex items-center gap-1.5 text-xs font-medium text-[#714B67] px-1.5 py-1">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    {cardBusy[inst.id] === 'stop'
                      ? 'Stopping...'
                      : cardBusy[inst.id] === 'start'
                      ? 'Starting...'
                      : cardBusy[inst.id] === 'restart'
                      ? 'Restarting...'
                      : 'Deleting...'}
                  </span>
                ) : (
                  <>
                    {inst.status !== 'running' ? (
                      <button
                        title="Start"
                        onClick={(e) => doAction(e, inst.id, 'start')}
                        className="rounded-lg p-1.5 text-emerald-600 hover:bg-emerald-50 cursor-pointer"
                      >
                        <Play className="h-4 w-4" />
                      </button>
                    ) : (
                      <button
                        title="Stop"
                        onClick={(e) => doAction(e, inst.id, 'stop')}
                        className="rounded-lg p-1.5 text-amber-600 hover:bg-amber-50 cursor-pointer"
                      >
                        <Square className="h-4 w-4" />
                      </button>
                    )}
                    <button
                      title="Restart"
                      onClick={(e) => doAction(e, inst.id, 'restart')}
                      className="rounded-lg p-1.5 text-slate-600 hover:bg-slate-100 cursor-pointer"
                    >
                      <RotateCw className="h-4 w-4" />
                    </button>
                  </>
                )}
              </div>
              <button
                title="Delete"
                disabled={!!cardBusy[inst.id]}
                onClick={(e) => handleDelete(e, inst)}
                className="rounded-lg p-1.5 text-rose-500 hover:bg-rose-50 disabled:opacity-40 cursor-pointer"
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          </div>
        ))}
      </div>

      {instances.length === 0 && !loading && (
        <div className="rounded-xl border border-dashed border-slate-300 bg-white p-12 text-center">
          <p className="text-sm font-medium text-slate-700">No Odoo instances deployed yet.</p>
          <p className="mt-1 text-xs text-slate-500">Click "Install New Odoo" to launch Odoo 17, 18, 19, or 20.</p>
        </div>
      )}

      <Modal open={showCreate} onClose={() => setShowCreate(false)} title="Install New Odoo Community Instance" maxWidth="max-w-2xl">
        <form onSubmit={handleCreate} className="space-y-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Instance Name *</label>
              <input
                type="text"
                required
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                placeholder="e.g. erp-prod"
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Odoo Community Version *</label>
              <select
                value={version}
                onChange={(e) => setVersion(e.target.value as OdooVersion)}
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
              >
                {(versions.length > 0
                  ? versions
                  : [
                      { version: '17', label: 'Odoo 17 Community (LTS)', defaultImage: 'odoo:17.0', status: 'stable' },
                      { version: '18', label: 'Odoo 18 Community', defaultImage: 'odoo:18.0', status: 'stable' },
                      { version: '19', label: 'Odoo 19 Community', defaultImage: 'odoo:19.0', status: 'stable' },
                      { version: '20', label: 'Odoo 20 Community', defaultImage: 'odoo:20.0', status: 'latest' },
                    ]
                ).map((v) => (
                  <option key={v.version} value={v.version}>
                    {v.label} ({v.defaultImage})
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">PostgreSQL Mode</label>
              <select
                value={dbMode}
                onChange={(e) => setDbMode(e.target.value as DBMode)}
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
              >
                <option value="shared">Shared PostgreSQL (Hemat RAM)</option>
                <option value="dedicated">Dedicated PostgreSQL Container (Isolasi Penuh)</option>
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Custom Docker Image (optional)</label>
              <input
                type="text"
                value={customImage}
                onChange={(e) => setCustomImage(e.target.value)}
                placeholder={`Default: odoo:${version}.0`}
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
              />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Initial Database Name (optional)</label>
              <input
                type="text"
                value={defaultDbName}
                onChange={(e) => setDefaultDbName(e.target.value)}
                placeholder="e.g. mycompany_db"
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Master Admin Password (optional)</label>
              <input
                type="text"
                value={adminPassword}
                onChange={(e) => setAdminPassword(e.target.value)}
                placeholder="Auto-generated if empty"
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
              />
            </div>
          </div>

          <div className="rounded-lg border border-slate-200 bg-slate-50 p-3.5 space-y-3">
            <div className="flex flex-wrap items-center gap-6">
              <label className="inline-flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
                <input type="checkbox" checked={exposePort} onChange={(e) => setExposePort(e.target.checked)} />
                Expose Direct HTTP Port
              </label>
              <label className="inline-flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
                <input type="checkbox" checked={useRootSubdomain} onChange={(e) => setUseRootSubdomain(e.target.checked)} />
                Attach Root Subdomain
              </label>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {exposePort && (
                <input
                  type="number"
                  value={httpPort}
                  onChange={(e) => setHttpPort(e.target.value)}
                  placeholder="Port (auto 8069+ if empty)"
                  className="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm"
                />
              )}
              <input
                type="text"
                value={customDomain}
                onChange={(e) => setCustomDomain(e.target.value)}
                placeholder="Custom domain (optional, e.g. erp.client.com)"
                className="rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm"
              />
            </div>
          </div>

          {recommendedAddons.length > 0 && (
            <div className="rounded-lg border border-purple-200 bg-purple-50/40 p-3.5 space-y-2.5">
              <div className="flex items-center justify-between">
                <div>
                  <label className="block text-xs font-semibold text-[#714B67]">
                    Recommended Addons (Opsional — pilih yang ingin di-install)
                  </label>
                  <p className="text-[11px] text-slate-500">
                    Hanya modul yang dicentang yang akan di-mount ke container Odoo ini.
                  </p>
                </div>
                <div className="flex gap-2 text-[11px]">
                  <button
                    type="button"
                    onClick={() => setSelectedRecommended(recommendedAddons.map((m) => m.relPath))}
                    className="font-medium text-[#714B67] hover:underline cursor-pointer"
                  >
                    Select All
                  </button>
                  <button
                    type="button"
                    onClick={() => setSelectedRecommended([])}
                    className="text-slate-500 hover:underline cursor-pointer"
                  >
                    Clear
                  </button>
                </div>
              </div>
              <div className="max-h-44 overflow-y-auto grid grid-cols-1 gap-1.5 sm:grid-cols-2 pr-1">
                {recommendedAddons.map((mod) => {
                  const checked = selectedRecommended.includes(mod.relPath)
                  return (
                    <label
                      key={mod.relPath}
                      className={`flex items-start gap-2 rounded-lg border p-2 text-xs cursor-pointer transition ${
                        checked
                          ? 'border-[#714B67] bg-white shadow-2xs'
                          : 'border-slate-200 bg-white/70 hover:bg-white'
                      }`}
                    >
                      <input
                        type="checkbox"
                        className="mt-0.5"
                        checked={checked}
                        onChange={() =>
                          setSelectedRecommended((prev) =>
                            checked ? prev.filter((x) => x !== mod.relPath) : [...prev, mod.relPath]
                          )
                        }
                      />
                      <div className="min-w-0">
                        <div className="font-semibold text-slate-800 truncate">{mod.name}</div>
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

          {addons.length > 0 && (
            <div>
              <label className="mb-1.5 block text-xs font-medium text-slate-700">Attach Addons Repositories</label>
              <div className="flex flex-wrap gap-2">
                {addons.map((a) => {
                  const checked = selectedAddons.includes(a.id)
                  return (
                    <button
                      type="button"
                      key={a.id}
                      onClick={() =>
                        setSelectedAddons((prev) =>
                          checked ? prev.filter((x) => x !== a.id) : [...prev, a.id]
                        )
                      }
                      className={`rounded-lg border px-3 py-1.5 text-xs font-medium cursor-pointer ${
                        checked
                          ? 'border-[#714B67] bg-purple-50 text-[#714B67]'
                          : 'border-slate-300 bg-white text-slate-600'
                      }`}
                    >
                      {a.name} ({a.modules.length} mods)
                    </button>
                  )
                })}
              </div>
            </div>
          )}

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setShowCreate(false)}
              className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={creating}
              className="rounded-lg bg-[#714B67] px-5 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
            >
              {creating ? 'Starting Deployment...' : 'Deploy Odoo Instance'}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
