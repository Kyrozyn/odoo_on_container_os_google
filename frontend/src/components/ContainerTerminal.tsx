import { useEffect, useRef, useState } from 'react'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { RefreshCw, Terminal as TerminalIcon } from 'lucide-react'
import { getToken } from '../api/client'

export function ContainerTerminal({
  instanceId,
  containerName,
  running,
}: {
  instanceId: string
  containerName: string
  running: boolean
}) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const termRef = useRef<XTerm | null>(null)
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected'>('disconnected')
  const [reconnectSeq, setReconnectSeq] = useState(0)

  useEffect(() => {
    if (!running || !containerRef.current) {
      setStatus('disconnected')
      return
    }

    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      theme: {
        background: '#020617',
        foreground: '#e2e8f0',
        cursor: '#34d399',
      },
      scrollback: 5000,
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(containerRef.current)
    fitAddon.fit()
    termRef.current = term

    const token = getToken() || ''
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const wsUrl = `${proto}://${window.location.host}/api/instances/${instanceId}/terminal?token=${encodeURIComponent(token)}&cols=${term.cols}&rows=${term.rows}`

    setStatus('connecting')
    const ws = new WebSocket(wsUrl)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      setStatus('connected')
      fitAddon.fit()
      term.focus()
    }

    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(ev.data))
      } else if (typeof ev.data === 'string') {
        term.write(ev.data)
      }
    }

    ws.onerror = () => {
      setStatus('disconnected')
    }

    ws.onclose = () => {
      setStatus('disconnected')
      term.write('\r\n\x1b[33m[session closed]\x1b[0m\r\n')
    }

    const dataDisp = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data)
      }
    })

    const resizeDisp = term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols, rows }))
      }
    })

    const ro = new ResizeObserver(() => {
      fitAddon.fit()
    })
    ro.observe(containerRef.current)

    return () => {
      ro.disconnect()
      dataDisp.dispose()
      resizeDisp.dispose()
      ws.close()
      term.dispose()
      termRef.current = null
    }
  }, [instanceId, running, reconnectSeq])

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950 overflow-hidden shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-800 px-4 py-2.5">
        <div className="flex items-center gap-2 text-xs font-mono text-slate-300">
          <TerminalIcon className="h-4 w-4 text-emerald-400" />
          <span>{containerName}</span>
          <span
            className={`rounded px-1.5 py-0.5 text-[11px] ${
              status === 'connected'
                ? 'bg-emerald-500/20 text-emerald-300'
                : status === 'connecting'
                ? 'bg-amber-500/20 text-amber-300'
                : 'bg-slate-800 text-slate-400'
            }`}
          >
            {status === 'connected' ? 'Connected' : status === 'connecting' ? 'Connecting...' : 'Disconnected'}
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            disabled={!running}
            onClick={() => setReconnectSeq((s) => s + 1)}
            className="inline-flex items-center gap-1 rounded border border-slate-700 px-2.5 py-1 text-[11px] text-slate-300 hover:bg-slate-800 hover:text-white disabled:opacity-40 cursor-pointer"
          >
            <RefreshCw className="h-3 w-3" /> Reconnect
          </button>
          <button
            type="button"
            onClick={() => termRef.current?.clear()}
            className="rounded border border-slate-700 px-2.5 py-1 text-[11px] text-slate-400 hover:bg-slate-800 hover:text-white cursor-pointer"
          >
            Clear
          </button>
        </div>
      </div>

      <div ref={containerRef} className="h-[480px] w-full p-2" />
    </div>
  )
}
