import { useEffect, useRef } from 'react'

/**
 * Periodically invokes `callback` while the page is visible, waiting for each
 * invocation to settle before scheduling the next tick (prevents overlapping polls).
 */
export function useAutoRefresh(
  callback: () => Promise<unknown> | void,
  intervalMs = 5000,
  enabled = true
) {
  const cbRef = useRef(callback)
  cbRef.current = callback

  useEffect(() => {
    if (!enabled) return

    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    let running = false

    const schedule = (delay: number) => {
      if (cancelled) return
      if (timer) clearTimeout(timer)
      timer = setTimeout(tick, delay)
    }

    const tick = async () => {
      if (cancelled || document.hidden || running) return
      running = true
      try {
        await cbRef.current()
      } catch {
        // Ignore transient background polling errors
      } finally {
        running = false
        if (!cancelled && !document.hidden) {
          schedule(intervalMs)
        }
      }
    }

    const onVisibility = () => {
      if (!document.hidden && !cancelled) {
        tick()
      } else if (timer) {
        clearTimeout(timer)
        timer = null
      }
    }

    document.addEventListener('visibilitychange', onVisibility)
    if (!document.hidden) {
      schedule(intervalMs)
    }

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [intervalMs, enabled])
}
