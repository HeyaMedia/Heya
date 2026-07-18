// Shared vocabulary for the rich track-list columns (plexify-style).
//
// The three flat list endpoints (/api/music/tracks, playlist tracks, rated
// tracks) all carry the same enrichment fields since the ListPlaylistTracks/
// ListUserRatedTracks/ListMusicTracks query expansion — this module owns:
//   1. `RichTrackWire` — the wire shape of those fields (pgtype values
//      MarshalJSON to plain values/null, never {Int32, Valid} wrappers —
//      the generated OpenAPI types lie about this, the handwritten
//      interfaces here are the wire truth, same as PlaylistTrackRow).
//   2. `pickRichFields` — wire row → TrackListRow subset, spread into a
//      page's tlRows mapper.
//   3. Formatters for each field (file size, musical key, sample rate, …).
//   4. `richTrackColumns()` — the optional `kind:'meta'` TrackListColumn
//      defs every adopting page feeds TrackList's column picker.
//
// Nuxt auto-imports `app/utils/**` — call these without import statements.
// (Inside THIS module the cross-util/composable helpers are imported
// explicitly: auto-import injection is for components/pages consuming us,
// not guaranteed between plain .ts modules — formatTrackQuality arrived
// undefined at runtime when left implicit.)

import { formatTrackQuality } from '~/utils/trackQuality'
import { timeAgoShort } from '~/composables/useFormat'
//
// The TrackListRow/TrackListColumn types live HERE (not in TrackList.vue)
// because this plain-.ts module is checked by raw tsc, which can't resolve
// type exports from an SFC. TrackList.vue re-exports them, so components
// keep importing from '~/components/music/TrackList.vue' as before.

export interface TrackListRow {
  id: number
  title: string
  artist: string
  artist_slug?: string
  album: string
  album_slug?: string
  album_year?: string | number | null
  duration: number
  /** false = file removed from disk; row dims and stops accepting clicks. */
  available?: boolean
  poster?: string | null
  /** 0..10 half-star scale; only rendered when a 'rating' column is present. */
  rating?: number
  /** Quality label ("FLAC 24/96", "MP3 320") — see utils/trackQuality.ts.
   *  Rendered under the duration in the phone row and by the optional
   *  desktop 'quality' meta column; omitted entirely when absent. */
  quality?: string | null
  /** Placeholder row in a sparse (random-access paged) list — renders as a
   *  skeleton. Give placeholders unique negative ids for the v-for key. */
  pending?: boolean

  // ── Rich fields (optional 'meta' columns — see below) ──
  track_number?: number
  disc_number?: number
  explicit?: boolean
  genres?: string[]
  label?: string
  release_date?: string
  format?: string
  bitrate_kbps?: number
  sample_rate_hz?: number
  bit_depth?: number
  channels?: number
  size_bytes?: number
  integrated_lufs?: number
  library_added_at?: string
  bpm?: number
  key_root?: number
  key_mode?: number
  /** Full credit string ("A feat. B") — replaces the bare artist in the
   *  title subtitle when it says more than the artist name alone. */
  artists_display?: string
  composer?: string
  play_count?: number
  last_played_at?: string
  /** Page-specific dates surfaced by context columns (playlist "Added",
   *  loved "Loved"). */
  added_at?: string
  rated_at?: string
}

export type TrackListColumnKind =
  | 'index' | 'art' | 'title' | 'artist' | 'album' | 'year' | 'rating' | 'duration' | 'custom' | 'meta'

export interface TrackListColumn {
  key: string
  kind: TrackListColumnKind
  /** Header cell text. Ignored when `headerIcon` is set. */
  label?: string
  /** Header cell renders this icon instead of `label` (songs.vue's clock-over-duration). */
  headerIcon?: string
  /** 'title' only — render a thumb ahead of the text (browse's combined art+title cell). */
  inlineArt?: boolean
  inlineArtSize?: number
  /** 'title' only — how the line under the title renders. */
  subtitle?: 'artist-link' | 'artist-plain' | 'artist-album-year' | 'none'
  /** 'album' only — NuxtLink (default) vs plain text. */
  linkAlbum?: boolean
  /** Grid track for this column. When EVERY column carries a width the grid
   *  is computed from the visible set (column-picker mode); otherwise the
   *  page's literal `gridTemplateColumns` prop is used as-is. */
  width?: string
  /** Column-picker membership: optional columns can be toggled; `defaultOn`
   *  seeds the visible set before any stored preference exists. */
  optional?: boolean
  defaultOn?: boolean
  /** 'meta' only — cell text. Keep it cheap; it runs per row per render. */
  format?: (row: TrackListRow) => string
  /** 'meta' only — cell/header alignment (default left). */
  align?: 'right' | 'left'
  /** Header click sorts by `sortValue` (client-side; TrackList disables the
   *  affordance for sparse lists where unloaded rows would sort wrong). */
  sortable?: boolean
  sortValue?: (row: TrackListRow) => string | number | null | undefined
  /** 'meta' only — hover title attribute (e.g. the full timestamp behind a
   *  date-only cell, so sorting granularity is inspectable). */
  tooltip?: (row: TrackListRow) => string
}

/** Enrichment fields shared by the songs/playlist/loved list endpoints. */
export interface RichTrackWire {
  explicit?: boolean
  album_genres?: string[] | null
  album_label?: string
  album_release_date?: string | null
  format?: string | null
  bitrate_kbps?: number | null
  sample_rate_hz?: number | null
  bit_depth?: number | null
  channels?: number | null
  size_bytes?: number | null
  integrated_lufs?: number | null
  library_added_at?: string | null
  bpm?: number | null
  key_root?: number | null
  key_mode?: number | null
  artists_display?: string
  composer?: string
  play_count?: number
  last_played_at?: string | null
  track_number?: number
  disc_number?: number
}

/** Copies the enrichment fields a wire row carries onto a TrackListRow. */
export function pickRichFields(t: RichTrackWire): Partial<TrackListRow> {
  return {
    explicit: t.explicit,
    genres: t.album_genres ?? undefined,
    label: t.album_label || undefined,
    release_date: t.album_release_date ?? undefined,
    format: t.format ?? undefined,
    bitrate_kbps: t.bitrate_kbps ?? undefined,
    sample_rate_hz: t.sample_rate_hz ?? undefined,
    bit_depth: t.bit_depth ?? undefined,
    channels: t.channels ?? undefined,
    size_bytes: t.size_bytes ?? undefined,
    integrated_lufs: t.integrated_lufs ?? undefined,
    library_added_at: t.library_added_at ?? undefined,
    bpm: t.bpm ?? undefined,
    key_root: t.key_root ?? undefined,
    key_mode: t.key_mode ?? undefined,
    artists_display: t.artists_display || undefined,
    composer: t.composer || undefined,
    play_count: t.play_count,
    last_played_at: t.last_played_at ?? undefined,
    track_number: t.track_number,
    disc_number: t.disc_number,
    quality: formatTrackQuality(t),
  }
}

// ── Field formatters ────────────────────────────────────────────────────

const KEY_NAMES = ['C', 'C♯', 'D', 'D♯', 'E', 'F', 'F♯', 'G', 'G♯', 'A', 'A♯', 'B']
// Camelot wheel codes, indexed by pitch class — mirrors
// internal/sonicanalysis/musictheory.go (0=C..11=B; mode 0=major, 1=minor).
const CAMELOT_MAJOR = ['8B', '3B', '10B', '5B', '12B', '7B', '2B', '9B', '4B', '11B', '6B', '1B']
const CAMELOT_MINOR = ['5A', '12A', '7A', '2A', '9A', '4A', '11A', '6A', '1A', '8A', '3A', '10A']

export function formatMusicalKey(root?: number | null, mode?: number | null): string {
  if (root == null || root < 0 || root > 11) return ''
  const name = KEY_NAMES[root]! + (mode === 1 ? 'm' : '')
  const camelot = (mode === 1 ? CAMELOT_MINOR : CAMELOT_MAJOR)[root]!
  return `${name} · ${camelot}`
}

export function formatFileSize(bytes?: number | null): string {
  if (!bytes || bytes <= 0) return ''
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`
  return `${(bytes / 1024 ** 2).toFixed(1)} MB`
}

export function formatSampleRate(hz?: number | null): string {
  if (!hz || hz <= 0) return ''
  const khz = hz / 1000
  return `${Number.isInteger(khz) ? khz : khz.toFixed(1)} kHz`
}

export function formatChannels(n?: number | null): string {
  if (!n || n <= 0) return ''
  if (n === 1) return 'Mono'
  if (n === 2) return 'Stereo'
  if (n === 6) return '5.1'
  if (n === 8) return '7.1'
  return `${n}ch`
}

export function formatShortDate(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

/** Full local date + time — the hover tooltip behind date-only cells (the
 *  sort runs on the full timestamp, so make it inspectable). */
export function formatFullDateTime(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

// ── Optional column registry ────────────────────────────────────────────

const dash = (s: string) => s || '—'

/**
 * The universal optional columns (`kind:'meta'`, `optional: true`) a rich
 * track list offers through the column picker. Pages append their own
 * context-specific optional columns (playlist "Added", loved "Loved") and
 * mark defaults with `defaultOn`.
 */
const dateNum = (iso?: string | null) => (iso ? Date.parse(iso) || null : null)

/** The optional dedicated Artist column. Separate from richTrackColumns so
 *  pages can slot it in the canonical position (title → artist → album)
 *  rather than wherever the registry spread lands. */
export function artistTrackColumn(): TrackListColumn {
  return { key: 'artist', kind: 'artist', label: 'Artist', width: 'minmax(110px, 0.8fr)', optional: true, sortable: true }
}

export function richTrackColumns(): TrackListColumn[] {
  return [
    { key: 'track_no', kind: 'meta', label: 'Track', width: '52px', align: 'right', optional: true, format: (r) => (r.track_number ? (r.disc_number && r.disc_number > 1 ? `${r.disc_number}-${r.track_number}` : String(r.track_number)) : '—'), sortable: true, sortValue: (r) => (r.disc_number ?? 1) * 1000 + (r.track_number ?? 0) },
    { key: 'year', kind: 'meta', label: 'Year', width: '52px', optional: true, format: (r) => dash(String(r.album_year || '')), sortable: true, sortValue: (r) => Number(r.album_year) || null },
    { key: 'release_date', kind: 'meta', label: 'Released', width: '96px', optional: true, format: (r) => dash(formatShortDate(r.release_date)), sortable: true, sortValue: (r) => dateNum(r.release_date) },
    { key: 'genre', kind: 'meta', label: 'Genre', width: 'minmax(90px, 0.7fr)', optional: true, format: (r) => dash((r.genres ?? []).slice(0, 2).join(', ')), sortable: true, sortValue: (r) => r.genres?.[0] ?? null },
    { key: 'label', kind: 'meta', label: 'Label', width: 'minmax(80px, 0.6fr)', optional: true, format: (r) => dash(r.label ?? ''), sortable: true, sortValue: (r) => r.label ?? null },
    { key: 'composer', kind: 'meta', label: 'Composer', width: 'minmax(90px, 0.7fr)', optional: true, format: (r) => dash(r.composer ?? ''), sortable: true, sortValue: (r) => r.composer ?? null },
    { key: 'plays', kind: 'meta', label: 'Plays', width: '48px', align: 'right', optional: true, format: (r) => (r.play_count ? String(r.play_count) : '—'), sortable: true, sortValue: (r) => r.play_count ?? 0 },
    { key: 'last_played', kind: 'meta', label: 'Played', width: '84px', optional: true, format: (r) => (r.last_played_at ? timeAgoShort(r.last_played_at) : '—'), sortable: true, sortValue: (r) => dateNum(r.last_played_at), tooltip: (r) => formatFullDateTime(r.last_played_at) },
    { key: 'added_library', kind: 'meta', label: 'In Library', width: '96px', optional: true, format: (r) => dash(formatShortDate(r.library_added_at)), sortable: true, sortValue: (r) => dateNum(r.library_added_at), tooltip: (r) => formatFullDateTime(r.library_added_at) },
    { key: 'quality', kind: 'meta', label: 'Quality', width: '86px', optional: true, format: (r) => dash(r.quality ?? ''), sortable: true, sortValue: (r) => (r.bit_depth ?? 0) * 1_000_000 + (r.sample_rate_hz ?? 0) },
    { key: 'format', kind: 'meta', label: 'Format', width: '58px', optional: true, format: (r) => dash((r.format ?? '').toUpperCase()), sortable: true, sortValue: (r) => r.format ?? null },
    { key: 'bitrate', kind: 'meta', label: 'Bitrate', width: '64px', align: 'right', optional: true, format: (r) => (r.bitrate_kbps ? `${r.bitrate_kbps}` : '—'), sortable: true, sortValue: (r) => r.bitrate_kbps ?? null },
    { key: 'sample_rate', kind: 'meta', label: 'Rate', width: '66px', align: 'right', optional: true, format: (r) => dash(formatSampleRate(r.sample_rate_hz)), sortable: true, sortValue: (r) => r.sample_rate_hz ?? null },
    { key: 'bit_depth', kind: 'meta', label: 'Depth', width: '52px', align: 'right', optional: true, format: (r) => (r.bit_depth ? `${r.bit_depth}-bit` : '—'), sortable: true, sortValue: (r) => r.bit_depth ?? null },
    { key: 'channels', kind: 'meta', label: 'Ch', width: '58px', optional: true, format: (r) => dash(formatChannels(r.channels)), sortable: true, sortValue: (r) => r.channels ?? null },
    { key: 'size', kind: 'meta', label: 'Size', width: '70px', align: 'right', optional: true, format: (r) => dash(formatFileSize(r.size_bytes)), sortable: true, sortValue: (r) => r.size_bytes ?? null },
    { key: 'bpm', kind: 'meta', label: 'BPM', width: '46px', align: 'right', optional: true, format: (r) => (r.bpm ? String(Math.round(r.bpm)) : '—'), sortable: true, sortValue: (r) => r.bpm ?? null },
    { key: 'key', kind: 'meta', label: 'Key', width: '76px', optional: true, format: (r) => dash(formatMusicalKey(r.key_root, r.key_mode)), sortable: true, sortValue: (r) => (r.key_root != null ? r.key_root * 2 + (r.key_mode ?? 0) : null) },
    { key: 'lufs', kind: 'meta', label: 'LUFS', width: '58px', align: 'right', optional: true, format: (r) => (r.integrated_lufs != null ? r.integrated_lufs.toFixed(1) : '—'), sortable: true, sortValue: (r) => r.integrated_lufs ?? null },
  ]
}
