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

// Media file size, decimal units: "1.24 GB" / "743 MB" / "512 KB".
export function formatBytes(bytes: number): string {
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(2)} GB`
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(0)} MB`
  return `${(bytes / 1e3).toFixed(0)} KB`
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

// Compact relative age for poster-corner chips: "now", "5m ago", "3h ago",
// "5d ago", "2w ago", "6mo ago", "1y ago". Empty string (chip hidden) for
// missing/invalid input. Accepts ISO strings or pgtype.Timestamptz-shaped
// {Time, Valid} objects, like timeAgo. Not reactive.
export function timeAgoShort(ts?: string | { Time?: string, Valid?: boolean } | null): string {
  const raw = typeof ts === 'string' ? ts : ts?.Valid === false ? undefined : ts?.Time
  if (!raw) return ''
  const t = new Date(raw).getTime()
  if (Number.isNaN(t)) return ''
  const sec = Math.floor((Date.now() - t) / 1000)
  if (sec < 60) return 'now'
  const m = Math.floor(sec / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 7) return `${d}d ago`
  const w = Math.floor(d / 7)
  if (w < 5) return `${w}w ago`
  const mo = Math.floor(d / 30.44)
  if (mo < 12) return `${mo}mo ago`
  return `${Math.floor(d / 365.25)}y ago`
}

// Relative time: "just now" under a minute, then "5m ago" / "3h ago" /
// "12d ago", falling back to a locale date past 30 days. Accepts plain ISO
// strings or pgtype.Timestamptz-shaped {Time, Valid} objects (lists carry
// those); returns "—" for missing/invalid input. Not reactive — pages that
// live-update bind a ticker at the call site (see settings/tasks.vue).
export function timeAgo(ts?: string | { Time?: string, Valid?: boolean } | null): string {
  const raw = typeof ts === 'string' ? ts : ts?.Valid === false ? undefined : ts?.Time
  if (!raw) return '—'
  const t = new Date(raw).getTime()
  if (Number.isNaN(t)) return '—'
  const sec = Math.floor((Date.now() - t) / 1000)
  if (sec < 60) return 'just now'
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  if (sec < 86400 * 30) return `${Math.floor(sec / 86400)}d ago`
  return new Date(raw).toLocaleDateString()
}
