// The generic contract the calendar components render. Consumers (the
// manager today, the public library sections later) map their own API data
// into entries — fetching, filtering, link targets, and aggregation all
// belong to the consuming page, keeping these components presentation-only.

export type CalendarBadgeState = 'ok' | 'warn' | 'error' | 'idle'

/** Small descriptive markers beside the title — "Season finale",
    "Series premiere", an album type. Distinct from the badge, which carries
    the file/availability STATE; tags describe the event itself. */
export type CalendarEntryTag = { label: string, tone: 'gold' | 'good' | 'bad' | 'neutral' }

export interface CalendarEntry {
  /** Stable unique id (the API's opaque event id, or a consumer-built key). */
  id: string
  /** Calendar date, YYYY-MM-DD. Entries are grouped and placed by this. */
  date: string
  title: string
  subtitle?: string
  /** Router target; rows/chips render as links when set. */
  to?: string
  /** Icon name for the kind glyph (tv / film / music / book …). */
  icon?: string
  /** Small trailing pill naming the source collection, when the consumer
      mixes several (the manager's cross-library view does). */
  library?: { id: number, label: string }
  badge?: { label: string, state: CalendarBadgeState }
  tags?: CalendarEntryTag[]
}

/** Parse a YYYY-MM-DD calendar date in LOCAL time. `new Date('YYYY-MM-DD')`
    parses as UTC midnight, which shifts the day for anyone west of UTC. */
export function parseCalendarDate(date: string): Date {
  const [y, m, d] = date.split('-').map(Number)
  return new Date(y!, (m ?? 1) - 1, d ?? 1)
}

export function toCalendarDate(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}
