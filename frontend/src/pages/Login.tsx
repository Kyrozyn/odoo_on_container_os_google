import React, { useState } from 'react'
import { Lock, Server, ShieldCheck } from 'lucide-react'
import { api, setToken } from '../api/client'

export function Login({ onSuccess }: { onSuccess: (passwordChanged?: boolean) => void }) {
  const [password, setPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [requiresTotp, setRequiresTotp] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await api.login(password, requiresTotp ? totpCode.trim() : undefined)
      if (res.requiresTotp) {
        setRequiresTotp(true)
        return
      }
      if (res.token) {
        setToken(res.token)
        onSuccess(res.passwordChanged)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-900 via-[#2a1a2e] to-slate-900 p-4">
      <div className="w-full max-w-md rounded-2xl bg-white p-8 shadow-2xl">
        <div className="mb-6 flex flex-col items-center text-center">
          <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-2xl bg-[#714B67] text-white shadow-lg">
            <Server className="h-7 w-7" />
          </div>
          <h1 className="text-2xl font-bold text-slate-900">RunOdoo</h1>
          <p className="mt-1 text-sm text-slate-500">
            PaaS Control Panel for Odoo 17–20
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1.5 block text-sm font-medium text-slate-700">Admin Password</label>
            <div className="relative">
              <Lock className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-slate-400" />
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Enter admin password"
                required
                autoFocus={!requiresTotp}
                className="w-full rounded-lg border border-slate-300 py-2 pl-9 pr-3 text-sm focus:border-[#714B67] focus:outline-none focus:ring-2 focus:ring-[#714B67]/20"
              />
            </div>
          </div>

          {requiresTotp && (
            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-700">2FA Authenticator Code</label>
              <div className="relative">
                <ShieldCheck className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-[#714B67]" />
                <input
                  type="text"
                  inputMode="numeric"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  value={totpCode}
                  onChange={(e) => setTotpCode(e.target.value)}
                  placeholder="6-digit code"
                  required
                  autoFocus
                  className="w-full rounded-lg border border-slate-300 py-2 pl-9 pr-3 font-mono text-sm tracking-widest focus:border-[#714B67] focus:outline-none focus:ring-2 focus:ring-[#714B67]/20"
                />
              </div>
            </div>
          )}

          {error && (
            <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-lg bg-[#714B67] py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-[#5c3c54] disabled:opacity-60 cursor-pointer"
          >
            {loading ? 'Signing in...' : 'Sign In to RunOdoo'}
          </button>
        </form>
      </div>
    </div>
  )
}
