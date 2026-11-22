import React from 'react'

export function StatusBadge({ status }: { status: string }) {
  const s = (status || '').toLowerCase()
  let cls = 'bg-slate-100 text-slate-700 border-slate-200'
  let dot = 'bg-slate-400'
  if (s === 'running') {
    cls = 'bg-emerald-50 text-emerald-700 border-emerald-200'
    dot = 'bg-emerald-500'
  } else if (s === 'deploying') {
    cls = 'bg-purple-50 text-[#714B67] border-purple-200'
    dot = 'bg-[#714B67] animate-ping'
  } else if (s === 'restarting') {
    cls = 'bg-amber-50 text-amber-700 border-amber-200'
    dot = 'bg-amber-500 animate-pulse'
  } else if (s === 'exited' || s === 'stopped' || s === 'dead' || s === 'failed') {
    cls = 'bg-rose-50 text-rose-700 border-rose-200'
    dot = 'bg-rose-500'
  }
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium capitalize ${cls}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${dot}`} />
      {status || 'unknown'}
    </span>
  )
}

export function Modal({
  open,
  onClose,
  title,
  children,
  maxWidth = 'max-w-lg',
}: {
  open: boolean
  onClose: () => void
  title: string
  children: React.ReactNode
  maxWidth?: string
}) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-xs">
      <div className={`w-full ${maxWidth} rounded-xl bg-white shadow-2xl border border-slate-200 max-h-[90vh] flex flex-col`}>
        <div className="flex items-center justify-between border-b border-slate-200 px-5 py-4">
          <h3 className="text-base font-semibold text-slate-900">{title}</h3>
          <button
            onClick={onClose}
            className="rounded-lg px-2 py-1 text-slate-400 hover:bg-slate-100 hover:text-slate-600"
          >
            ✕
          </button>
        </div>
        <div className="overflow-y-auto p-5">{children}</div>
      </div>
    </div>
  )
}

export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}
