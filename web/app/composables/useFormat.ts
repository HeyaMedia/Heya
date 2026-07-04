// Shared display formatters. Auto-imported everywhere — a component that
// needs different output (thresholds, wording, locale) keeps a local copy,
// which shadows these inside that component's scope.

import { formatTime } from './useHeyaPlayer'

// Track length as m:ss ("3:42"). Empty string for zero/negative input.
export function formatDuration(sec: number): string {
  if (!sec || sec < 0) return ''
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}

// Album/playlist total runtime: m:ss under an hour, "1h 24m" above.
export function formatRunTime(seconds: number): string {
  if (seconds < 3600) return formatTime(seconds)
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

// Media file size, decimal units: "1.24 GB" / "743 MB".
export function formatBytes(bytes: number): string {
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(2)} GB`
  return `${(bytes / 1e6).toFixed(0)} MB`
}

// Adaptive binary-unit size for settings/ops pages: "0 B", "3.4 MB", "12 GB".
// One decimal only when the value is small enough for it to matter.
export function fmtBytes(b?: number | null): string {
  if (!b) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0; let n = b
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${units[i]}`
}

// Rating to one decimal ("7.4"); empty string when unparseable.
export function formatVote(v: unknown): string {
  const n = typeof v === 'number' ? v : parseFloat(String(v))
  return isNaN(n) ? '' : n.toFixed(1)
}

// Date-only string (YYYY-MM-DD) → "May 12, 2024". Anchors to local midnight
// so the rendered day never shifts with the viewer's timezone. Don't feed it
// full ISO timestamps — use formatDateShort for those.
export function formatDate(d: string): string {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

// Long-month variant of formatDate: "January 12, 2024".
export function formatDateLong(d: string): string {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

// Any parseable date/timestamp (e.g. created_at) → "May 12, 2024".
export function formatDateShort(d: string): string {
  if (!d) return ''
  return new Date(d).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

// Full date + time, en-GB medium: "12 May 2024, 14:03:22".
export function formatDateTime(d: string): string {
  return new Date(d).toLocaleString('en-GB', { dateStyle: 'medium', timeStyle: 'medium' })
}

// Relative time: "42s ago" / "5m ago" / "3h ago" / "12d ago"; "never" when
// missing. Not reactive — pages that live-update bind a ticker at the call
// site (see settings/tasks.vue).
export function timeAgo(ts?: string | null): string {
  if (!ts) return 'never'
  const sec = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}
