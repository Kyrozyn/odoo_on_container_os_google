import React, { useEffect, useState } from 'react'
import { GitBranch, Loader2, Package, RefreshCw, Sparkles, Trash2, Upload } from 'lucide-react'
import { api } from '../api/client'
import type { AddonModule, AddonRepo } from '../types'
import { Modal } from '../components/UI'

export function Addons({ notify }: { notify: (msg: string, type?: 'success' | 'error') => void }) {
  const [repos, setRepos] = useState<AddonRepo[]>([])
  const [recommended, setRecommended] = useState<AddonModule[]>([])
  const [loading, setLoading] = useState(true)
  const [showGit, setShowGit] = useState(false)
  const [showZip, setShowZip] = useState(false)
  const [busy, setBusy] = useState(false)
  const [repoBusy, setRepoBusy] = useState<string | null>(null)

  // Git form
  const [gitName, setGitName] = useState('')
  const [gitUrl, setGitUrl] = useState('')
  const [gitBranch, setGitBranch] = useState('')
  const [gitToken, setGitToken] = useState('')

  // Zip form
  const [zipName, setZipName] = useState('')
  const [zipFile, setZipFile] = useState<File | null>(null)

  const load = async () => {
    setLoading(true)
    try {
      const [r, rec] = await Promise.all([api.listAddons(), api.listRecommendedAddons()])
      setRepos(Array.isArray(r) ? r : [])
      setRecommended(Array.isArray(rec) ? rec : [])
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading addons', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleClone = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const repo = await api.cloneAddon({
        name: gitName,
        gitUrl,
        branch: gitBranch || undefined,
        token: gitToken || undefined,
      })
      notify(`Cloned "${repo.name}" (${repo.modules.length} Odoo modules detected)!`)
      setShowGit(false)
      setGitName('')
      setGitUrl('')
      setGitBranch('')
      setGitToken('')
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Git clone failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!zipFile) return
    setBusy(true)
    try {
      const repo = await api.uploadAddonZip(zipName, zipFile)
      notify(`Uploaded "${repo.name}" (${repo.modules.length} modules detected)!`)
      setShowZip(false)
      setZipName('')
      setZipFile(null)
      load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Upload failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Addons Library</h1>
          <p className="text-sm text-slate-500">
            Clone Git repositories or upload ZIP bundles &mdash; auto-scans <code className="bg-slate-100 px-1 rounded">__manifest__.py</code>
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => setShowZip(true)}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3.5 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <Upload className="h-4 w-4" /> Upload ZIP
          </button>
          <button
            onClick={() => setShowGit(true)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] cursor-pointer"
          >
            <GitBranch className="h-4 w-4" /> Clone Git Repo
          </button>
        </div>
      </div>

      {recommended.length > 0 && (
        <div className="rounded-xl border border-purple-200 bg-purple-50/40 p-5 space-y-3">
          <div className="flex items-center gap-2">
            <Sparkles className="h-5 w-5 text-[#714B67]" />
            <div>
              <h2 className="text-base font-bold text-slate-900">
                Recommended Addons ({recommended.length} modules)
              </h2>
              <p className="text-xs text-slate-600">
                Koleksi modul bawaan dari folder <code className="bg-white px-1 rounded">addons/</code>. Bisa dipilih secara selektif saat membuat atau mengedit instance Odoo.
              </p>
            </div>
          </div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {recommended.map((m) => (
              <div key={m.relPath} className="rounded-lg border border-purple-200/80 bg-white p-3 text-xs">
                <div className="font-semibold text-slate-900 truncate">{m.name}</div>
                <div className="mt-0.5 font-mono text-[11px] text-[#714B67] truncate">
                  {m.technicalName} {m.version ? `• v${m.version}` : ''}
                </div>
                {m.summary && <p className="mt-1 text-[11px] text-slate-500 line-clamp-2">{m.summary}</p>}
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="space-y-4">
        {repos.map((repo) => (
          <div key={repo.id} className="rounded-xl border border-slate-200 bg-white p-5 shadow-xs">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <Package className="h-5 w-5 text-[#714B67]" />
                  <h3 className="text-base font-bold text-slate-900">{repo.name}</h3>
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs font-medium uppercase text-slate-600">
                    {repo.sourceType}
                  </span>
                  <span className="rounded bg-purple-50 px-2 py-0.5 text-xs font-medium text-[#714B67]">
                    {repo.modules.length} module(s)
                  </span>
                </div>
                {repo.gitUrl && (
                  <p className="mt-1 text-xs text-slate-500">
                    {repo.gitUrl} {repo.gitBranch ? `(${repo.gitBranch})` : ''}
                  </p>
                )}
              </div>

              <div className="flex items-center gap-2">
                {repo.sourceType === 'git' && (
                  <button
                    disabled={!!repoBusy}
                    onClick={async () => {
                      setRepoBusy(`pull:${repo.id}`)
                      try {
                        const r = await api.pullAddon(repo.id)
                        notify(`Pulled "${r.name}" (${r.modules.length} modules)`)
                        load()
                      } catch (err) {
                        notify(err instanceof Error ? err.message : 'Git pull failed', 'error')
                      } finally {
                        setRepoBusy(null)
                      }
                    }}
                    className="inline-flex items-center gap-1 rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 cursor-pointer"
                  >
                    {repoBusy === `pull:${repo.id}` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                    {repoBusy === `pull:${repo.id}` ? 'Pulling...' : 'Git Pull'}
                  </button>
                )}
                <button
                  disabled={!!repoBusy}
                  onClick={async () => {
                    if (!confirm(`Delete addon repo "${repo.name}"?`)) return
                    setRepoBusy(`del:${repo.id}`)
                    try {
                      await api.deleteAddon(repo.id)
                      notify(`Deleted "${repo.name}"`)
                      load()
                    } catch (err) {
                      notify(err instanceof Error ? err.message : 'Delete failed', 'error')
                    } finally {
                      setRepoBusy(null)
                    }
                  }}
                  className="rounded-lg p-1.5 text-rose-500 hover:bg-rose-50 disabled:opacity-50 cursor-pointer"
                >
                  {repoBusy === `del:${repo.id}` ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                </button>
              </div>
            </div>

            {repo.modules.length > 0 && (
              <div className="mt-4 flex flex-wrap gap-2">
                {repo.modules.map((m) => (
                  <div
                    key={m.relPath}
                    title={m.summary || m.name}
                    className="rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-xs"
                  >
                    <span className="font-mono font-semibold text-slate-800">{m.technicalName}</span>
                    {m.version && <span className="ml-1.5 text-slate-400">v{m.version}</span>}
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}

        {repos.length === 0 && !loading && (
          <div className="rounded-xl border border-dashed border-slate-300 bg-white p-12 text-center">
            <p className="text-sm font-medium text-slate-700">No custom addons added yet.</p>
            <p className="mt-1 text-xs text-slate-500">Clone an OCA/custom Git repo or upload a ZIP to attach it to any Odoo instance.</p>
          </div>
        )}
      </div>

      <Modal open={showGit} onClose={() => setShowGit(false)} title="Clone Addons Git Repository">
        <form onSubmit={handleClone} className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Repository Label *</label>
            <input
              type="text"
              required
              value={gitName}
              onChange={(e) => setGitName(e.target.value)}
              placeholder="e.g. oca-web-18"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Git URL *</label>
            <input
              type="url"
              required
              value={gitUrl}
              onChange={(e) => setGitUrl(e.target.value)}
              placeholder="https://github.com/OCA/web.git"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Branch (optional)</label>
              <input
                type="text"
                value={gitBranch}
                onChange={(e) => setGitBranch(e.target.value)}
                placeholder="e.g. 18.0"
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-700">Access Token (private repo)</label>
              <input
                type="password"
                value={gitToken}
                onChange={(e) => setGitToken(e.target.value)}
                placeholder="ghp_..."
                className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setShowGit(false)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">
              Cancel
            </button>
            <button type="submit" disabled={busy} className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white cursor-pointer">
              {busy ? 'Cloning...' : 'Clone Repository'}
            </button>
          </div>
        </form>
      </Modal>

      <Modal open={showZip} onClose={() => setShowZip(false)} title="Upload Addons ZIP Bundle">
        <form onSubmit={handleUpload} className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Bundle Name</label>
            <input
              type="text"
              value={zipName}
              onChange={(e) => setZipName(e.target.value)}
              placeholder="Optional (defaults to zip filename)"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">ZIP File *</label>
            <input
              type="file"
              accept=".zip"
              required
              onChange={(e) => setZipFile(e.target.files?.[0] || null)}
              className="w-full rounded-lg border border-slate-300 p-2 text-sm"
            />
          </div>
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setShowZip(false)} className="rounded-lg border border-slate-300 px-4 py-2 text-sm cursor-pointer">
              Cancel
            </button>
            <button type="submit" disabled={busy} className="rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white cursor-pointer">
              {busy ? 'Uploading...' : 'Upload & Scan'}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
