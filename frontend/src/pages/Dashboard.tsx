import React, { useEffect, useState } from 'react'
import {
  Activity,
  Box,
  Cpu,
  Database,
  Globe,
  HardDrive,
  MemoryStick,
  Plus,
  RefreshCw,
  ShieldCheck,
} from 'lucide-react'
import { api } from '../api/client'
import type { OdooInstance, PanelSettings, SystemInfo } from '../types'
import { StatusBadge } from '../components/UI'

export function Dashboard({
  onNavigate,
  onSelectInstance,
  notify,
}: {
  onNavigate: (page: string) => void
  onSelectInstance: (id: string) => void
  notify: (msg: string, type?: 'success' | 'error') => void
}) {
  const [sysInfo, setSysInfo] = useState<SystemInfo | null>(null)
  const [instances, setInstances] = useState<OdooInstance[]>([])
  const [settings, setSettings] = useState<PanelSettings | null>(null)
  const [rootDomainInput, setRootDomainInput] = useState('')
  const [emailInput, setEmailInput] = useState('')
  const [savingDomain, setSavingDomain] = useState(false)
  const [loading, setLoading] = useState(true)

  const load = async () => {
    setLoading(true)
    try {
      const [s, insts, st] = await Promise.all([
        api.systemInfo(),
        api.listInstances(),
        api.getSettings(),
      ])
      setSysInfo(s)
      setInstances(insts)
      setSettings(st)
      setRootDomainInput(st.rootDomain || '')
      setEmailInput(st.letsEncryptEmail || '')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed loading dashboard', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const handleSaveRootDomain = async (e: React.FormEvent) => {
    e.preventDefault()
    setSavingDomain(true)
    try {
      const updated = await api.updateSettings({
        rootDomain: rootDomainInput,
        letsEncryptEmail: emailInput,
      })
      setSettings(updated)
      notify('Root domain & Nginx config updated! Reloading...')
      setTimeout(() => window.location.reload(), 600)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Failed updating domain', 'error')
    } finally {
      setSavingDomain(false)
    }
  }

  const runningCount = instances.filter((i) => i.status === 'running').length

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Dashboard</h1>
          <p className="text-sm text-slate-500">
            Manage Odoo 17–20 instances, PostgreSQL databases, addons, and SSL domains
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={load}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={() => onNavigate('instances')}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] cursor-pointer"
          >
            <Plus className="h-4 w-4" />
            New Odoo Instance
          </button>
        </div>
      </div>

      {/* Host System Cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-slate-500">
            <span className="text-xs font-medium uppercase">Odoo Instances</span>
            <Box className="h-4 w-4 text-[#714B67]" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-bold text-slate-900">{runningCount}</span>
            <span className="text-xs text-slate-500">/ {instances.length} running</span>
          </div>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-slate-500">
            <span className="text-xs font-medium uppercase">CPU & OS</span>
            <Cpu className="h-4 w-4 text-indigo-600" />
          </div>
          <div className="mt-2 text-2xl font-bold text-slate-900">
            {sysInfo ? `${sysInfo.cpuCores} vCPU` : '—'}
          </div>
          <p className="mt-1 truncate text-xs text-slate-500">
            {sysInfo?.osPretty || 'Detecting...'}
            {sysInfo?.isCos ? ' (Google COS)' : ''}
          </p>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-slate-500">
            <span className="text-xs font-medium uppercase">Host Memory</span>
            <MemoryStick className="h-4 w-4 text-emerald-600" />
          </div>
          <div className="mt-2 text-2xl font-bold text-slate-900">
            {sysInfo && sysInfo.totalRamMb > 0 ? `${sysInfo.totalRamMb} MB` : '—'}
          </div>
          <p className="mt-1 text-xs text-slate-500">
            {sysInfo && sysInfo.availableRamMb > 0 ? `${sysInfo.availableRamMb} MB available` : 'Auto-tuned for Odoo'}
          </p>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
          <div className="flex items-center justify-between text-slate-500">
            <span className="text-xs font-medium uppercase">Docker & Disk</span>
            <HardDrive className="h-4 w-4 text-sky-600" />
          </div>
          <div className="mt-2 flex items-center gap-2">
            <span className="text-2xl font-bold text-slate-900">
              {sysInfo ? `${sysInfo.diskFreeGb} GB` : '—'}
            </span>
            <span className="text-xs text-slate-500">free</span>
          </div>
          <p className="mt-1 text-xs text-slate-500">
            {sysInfo?.dockerConnected ? `Docker ${sysInfo.dockerVersion}` : 'Docker disconnected'}
          </p>
        </div>
      </div>

      {/* CapRover-style Root Domain Configuration Card */}
      <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-xs">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-slate-900">
              <Globe className="h-5 w-5 text-[#714B67]" />
              Root Domain Configuration (CapRover Style)
            </h2>
            <p className="mt-1 text-sm text-slate-500">
              Point a wildcard DNS A-record <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">*.yourdomain.com</code> to
              your server IP. Your panel will be reachable at{' '}
              <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">
                {settings?.panelSubdomain || 'panel'}.{rootDomainInput || 'yourdomain.com'}
              </code>{' '}
              and Odoo apps at <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">&lt;app&gt;.{rootDomainInput || 'yourdomain.com'}</code> — or keep using port 3000 directly.
            </p>
          </div>
          {settings?.panelHasSsl && (
            <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700 border border-emerald-200">
              <ShieldCheck className="h-3.5 w-3.5" />
              Panel HTTPS Active
            </span>
          )}
        </div>

        <form onSubmit={handleSaveRootDomain} className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-3">
          <input
            type="text"
            value={rootDomainInput}
            onChange={(e) => setRootDomainInput(e.target.value)}
            placeholder="e.g. erp.mycompany.com"
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
          />
          <input
            type="email"
            value={emailInput}
            onChange={(e) => setEmailInput(e.target.value)}
            placeholder="Let's Encrypt email (e.g. admin@mycompany.com)"
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
          />
          <div className="flex gap-2">
            <button
              type="submit"
              disabled={savingDomain}
              className="flex-1 rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-60 cursor-pointer"
            >
              {savingDomain ? 'Updating...' : 'Update Root Domain'}
            </button>
            <button
              type="button"
              onClick={() => onNavigate('settings')}
              className="rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
            >
              SSL Setup
            </button>
          </div>
        </form>
      </div>

      {/* Instances Quick Overview */}
      <div className="rounded-xl border border-slate-200 bg-white shadow-xs">
        <div className="flex items-center justify-between border-b border-slate-200 px-5 py-4">
          <h2 className="flex items-center gap-2 text-base font-semibold text-slate-900">
            <Activity className="h-4 w-4 text-[#714B67]" />
            Your Odoo Instances
          </h2>
          <button
            onClick={() => onNavigate('instances')}
            className="text-xs font-medium text-[#714B67] hover:underline cursor-pointer"
          >
            View all &rarr;
          </button>
        </div>

        {instances.length === 0 ? (
          <div className="p-10 text-center">
            <Database className="mx-auto h-10 w-10 text-slate-300" />
            <h3 className="mt-3 text-sm font-semibold text-slate-900">No Odoo instances yet</h3>
            <p className="mt-1 text-xs text-slate-500">
              Deploy your first Odoo 17, 18, 19, or 20 Community instance in one click.
            </p>
            <button
              onClick={() => onNavigate('instances')}
              className="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] cursor-pointer"
            >
              <Plus className="h-4 w-4" />
              Deploy Odoo Instance
            </button>
          </div>
        ) : (
          <div className="divide-y divide-slate-100">
            {instances.map((inst) => (
              <div
                key={inst.id}
                onClick={() => onSelectInstance(inst.id)}
                className="flex flex-wrap items-center justify-between gap-4 px-5 py-3.5 hover:bg-slate-50 cursor-pointer"
              >
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-semibold text-slate-900">{inst.name}</span>
                    <span className="rounded bg-purple-50 px-2 py-0.5 text-xs font-medium text-[#714B67]">
                      Odoo {inst.version}
                    </span>
                    <span className="rounded bg-slate-100 px-2 py-0.5 text-xs text-slate-600">
                      PG: {inst.dbMode}
                    </span>
                  </div>
                  <p className="mt-0.5 text-xs text-slate-500">
                    {inst.primaryUrl || (inst.exposePort ? `Port ${inst.httpPort}` : 'Domain only')} &bull; Image: {inst.image}
                  </p>
                </div>
                <StatusBadge status={inst.status} />
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
