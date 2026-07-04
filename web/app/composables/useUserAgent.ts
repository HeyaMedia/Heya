// User-agent presentation helpers shared by the sessions pages
// (settings/sessions.vue, settings/all-sessions.vue). Bare exports —
// auto-imported like the rest of composables/.

// Cheap browser + OS sniff — not perfect, but useful for "is this me?".
export function describeAgent(ua: string): string {
  if (!ua) return 'Unknown device'
  let browser = 'Unknown'
  if (/Edg\//.test(ua)) browser = 'Edge'
  else if (/Chrome\//.test(ua) && !/Chromium/.test(ua)) browser = 'Chrome'
  else if (/Firefox\//.test(ua)) browser = 'Firefox'
  else if (/Safari\//.test(ua) && !/Chrome/.test(ua)) browser = 'Safari'
  else if (/heya-cli/i.test(ua)) browser = 'Heya CLI'
  else if (/curl|wget|HTTPie|Go-http-client|python-requests/i.test(ua)) browser = 'Script'

  let os = 'Unknown OS'
  if (/Mac OS X|Macintosh/.test(ua)) os = 'macOS'
  else if (/Windows NT/.test(ua)) os = 'Windows'
  else if (/Android/.test(ua)) os = 'Android'
  else if (/iPhone|iPad|iPod/.test(ua)) os = 'iOS'
  else if (/Linux/.test(ua)) os = 'Linux'

  return `${browser} · ${os}`
}

// Icon name for a session's user agent. Token-kind sessions pick their own
// icon ("key") at the call site before falling through to this.
export function agentIcon(ua: string): string {
  if (/iPhone|iPad|iPod|Android/.test(ua)) return 'pulse' // no phone icon in catalog yet
  if (/heya-cli|curl|wget|HTTPie|Go-http-client|python-requests/i.test(ua)) return 'wrench'
  return 'cpu'
}

// Session/token expiry wording: "no expiry" / "expired" / "expires in 12d".
export function formatExpiry(iso?: string | null): string {
  if (!iso) return 'no expiry'
  const ms = new Date(iso).getTime() - Date.now()
  if (ms <= 0) return 'expired'
  const d = Math.floor(ms / 86400000)
  if (d < 1) return 'expires today'
  if (d < 30) return `expires in ${d}d`
  if (d < 365) return `expires in ${Math.floor(d / 30)}mo`
  return `expires in ${Math.floor(d / 365)}y`
}
