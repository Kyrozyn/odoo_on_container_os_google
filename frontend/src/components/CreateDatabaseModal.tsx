import React, { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { Modal } from './UI'
import type { OdooInstance } from '../types'

export interface CreateOdooDBPayload {
  name: string
  login: string
  password: string
  phone: string
  lang: string
  countryCode: string
  demo: boolean
}

const LANGUAGES = [
  { code: 'en_US', label: 'English (US)' },
  { code: 'id_ID', label: 'Indonesian / Bahasa Indonesia' },
  { code: 'ms_MY', label: 'Malay / Bahasa Melayu' },
  { code: 'en_GB', label: 'English (UK)' },
  { code: 'fr_FR', label: 'French' },
  { code: 'de_DE', label: 'German' },
  { code: 'es_ES', label: 'Spanish' },
  { code: 'ja_JP', label: 'Japanese' },
  { code: 'zh_CN', label: 'Chinese (Simplified)' },
  { code: 'ar_001', label: 'Arabic' },
]

const COUNTRIES = [
  { code: '', label: '— None / Not set —' },
  { code: 'id', label: 'Indonesia (ID)' },
  { code: 'sg', label: 'Singapore (SG)' },
  { code: 'my', label: 'Malaysia (MY)' },
  { code: 'us', label: 'United States (US)' },
  { code: 'gb', label: 'United Kingdom (GB)' },
  { code: 'au', label: 'Australia (AU)' },
  { code: 'jp', label: 'Japan (JP)' },
  { code: 'de', label: 'Germany (DE)' },
  { code: 'fr', label: 'France (FR)' },
  { code: 'nl', label: 'Netherlands (NL)' },
  { code: 'in', label: 'India (IN)' },
]

export function CreateDatabaseModal({
  open,
  onClose,
  instances,
  fixedInstanceId,
  onSubmit,
}: {
  open: boolean
  onClose: () => void
  instances?: OdooInstance[]
  fixedInstanceId?: string
  onSubmit: (instanceId: string, payload: CreateOdooDBPayload) => Promise<void>
}) {
  const [selectedInst, setSelectedInst] = useState(fixedInstanceId || instances?.[0]?.id || '')
  const [name, setName] = useState('')
  const [login, setLogin] = useState('admin')
  const [password, setPassword] = useState('')
  const [phone, setPhone] = useState('')
  const [lang, setLang] = useState('en_US')
  const [countryCode, setCountryCode] = useState('id')
  const [demo, setDemo] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const targetInstId = fixedInstanceId || selectedInst || instances?.[0]?.id || ''

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!targetInstId) return
    setSubmitting(true)
    try {
      await onSubmit(targetInstId, {
        name: name.trim(),
        login: login.trim(),
        password,
        phone: phone.trim(),
        lang,
        countryCode,
        demo,
      })
      setName('')
      setPassword('')
      setPhone('')
      setDemo(false)
      onClose()
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal open={open} onClose={() => !submitting && onClose()} title="Create Odoo Database" maxWidth="max-w-lg">
      <form onSubmit={handleSubmit} className="space-y-4">
        {!fixedInstanceId && instances && (
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Target Odoo Instance *</label>
            <select
              value={targetInstId}
              disabled={submitting}
              onChange={(e) => setSelectedInst(e.target.value)}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            >
              {instances.map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name} (Odoo {i.version} — {i.status})
                </option>
              ))}
            </select>
          </div>
        )}

        <div>
          <label className="mb-1 block text-xs font-medium text-slate-700">Database Name *</label>
          <input
            type="text"
            required
            disabled={submitting}
            value={name}
            onChange={(e) => setName(e.target.value.replace(/[^a-zA-Z0-9_-]/g, ''))}
            placeholder="e.g. prod_erp"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
          />
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Admin Email / Login *</label>
            <input
              type="text"
              required
              disabled={submitting}
              value={login}
              onChange={(e) => setLogin(e.target.value)}
              placeholder="admin@company.com"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Admin Password *</label>
            <input
              type="password"
              required
              disabled={submitting}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Password untuk login Odoo"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
            />
          </div>
        </div>

        <div>
          <label className="mb-1 block text-xs font-medium text-slate-700">Phone Number (optional)</label>
          <input
            type="text"
            disabled={submitting}
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            placeholder="e.g. +628123456789"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-[#714B67] focus:outline-none"
          />
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Language</label>
            <select
              value={lang}
              disabled={submitting}
              onChange={(e) => setLang(e.target.value)}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            >
              {LANGUAGES.map((l) => (
                <option key={l.code} value={l.code}>
                  {l.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-700">Country</label>
            <select
              value={countryCode}
              disabled={submitting}
              onChange={(e) => setCountryCode(e.target.value)}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            >
              {COUNTRIES.map((c) => (
                <option key={c.code} value={c.code}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>
        </div>

        <label className="flex items-start gap-2.5 rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs text-slate-700 cursor-pointer">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={demo}
            disabled={submitting}
            onChange={(e) => setDemo(e.target.checked)}
          />
          <span>
            <strong>Demo data</strong> — centang untuk mengisi data contoh (hanya untuk testing/evaluasi, jangan untuk production).
          </span>
        </label>

        {submitting && (
          <div className="flex items-center gap-2 rounded-lg border border-purple-200 bg-purple-50 px-3 py-2.5 text-xs text-[#714B67]">
            <Loader2 className="h-4 w-4 animate-spin shrink-0" />
            <span>Odoo sedang menginisialisasi database &amp; modul dasar (biasanya 15–45 detik)...</span>
          </div>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <button
            type="button"
            disabled={submitting}
            onClick={onClose}
            className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={submitting}
            className="inline-flex items-center gap-1.5 rounded-lg bg-[#714B67] px-4 py-2 text-sm font-semibold text-white hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
          >
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            {submitting ? 'Creating Database...' : 'Create Database'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
