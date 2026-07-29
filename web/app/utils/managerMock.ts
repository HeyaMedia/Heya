// Mock data for the /manager UI stub. Everything on these pages renders from
// this module until the backend exists — pages import it explicitly (utils
// auto-import doesn't resolve cross-module at runtime, and this keeps the
// eventual "swap to real queries" diff obvious).

export type MgrLibraryKey = 'movies' | 'tv' | 'anime' | 'music'

export const MOCK_LIBRARY_OPTIONS: { key: MgrLibraryKey, label: string, icon: string }[] = [
  { key: 'movies', label: 'Movies', icon: 'film' },
  { key: 'tv', label: 'TV', icon: 'tv' },
  { key: 'anime', label: 'Anime', icon: 'tv' },
  { key: 'music', label: 'Music', icon: 'music' },
]

// ── Queue ────────────────────────────────────────────────────────────────

export type MockQueueItem = {
  id: number
  title: string
  subtitle: string
  library: MgrLibraryKey
  quality: string
  sizeGb: number
  doneGb: number
  speed: string
  eta: string
  status: 'downloading' | 'importing' | 'paused' | 'stalled' | 'seeding'
  client: string
  protocol: 'torrent' | 'usenet'
  indexer: string
}

export const MOCK_QUEUE: MockQueueItem[] = [
  { id: 1, title: 'Dune: Part Two', subtitle: '2024 · Remux-2160p', library: 'movies', quality: 'Remux-2160p', sizeGb: 68.4, doneGb: 41.2, speed: '38.2 MB/s', eta: '12m', status: 'downloading', client: 'qBittorrent', protocol: 'torrent', indexer: 'PrivateHD' },
  { id: 2, title: 'Severance', subtitle: 'S02E07 · Chikhai Bardo', library: 'tv', quality: 'WEB-DL 1080p', sizeGb: 4.1, doneGb: 4.1, speed: '—', eta: '—', status: 'importing', client: 'SABnzbd', protocol: 'usenet', indexer: 'DrunkenSlug' },
  { id: 3, title: 'Frieren: Beyond Journey\'s End', subtitle: 'S02E04 · 1080p', library: 'anime', quality: 'WEB-DL 1080p', sizeGb: 1.4, doneGb: 0.6, speed: '9.8 MB/s', eta: '2m', status: 'downloading', client: 'qBittorrent', protocol: 'torrent', indexer: 'AnimeBytes' },
  { id: 4, title: 'The Cure', subtitle: 'Songs of a Lost World · FLAC', library: 'music', quality: 'FLAC', sizeGb: 0.6, doneGb: 0.6, speed: '—', eta: 'ratio 1.4', status: 'seeding', client: 'qBittorrent', protocol: 'torrent', indexer: 'Redacted' },
  { id: 5, title: 'Shōgun', subtitle: 'S01 · Season pack', library: 'tv', quality: 'Bluray-1080p', sizeGb: 92.3, doneGb: 12.1, speed: '0 B/s', eta: '∞', status: 'stalled', client: 'qBittorrent', protocol: 'torrent', indexer: 'TorrentLeech' },
  { id: 6, title: 'Oppenheimer', subtitle: '2023 · IMAX', library: 'movies', quality: 'Bluray-2160p', sizeGb: 54.8, doneGb: 0, speed: 'paused', eta: '—', status: 'paused', client: 'qBittorrent', protocol: 'torrent', indexer: 'PassThePopcorn' },
]

// ── Wanted ───────────────────────────────────────────────────────────────

export type MockWantedItem = {
  id: number
  title: string
  subtitle: string
  library: MgrLibraryKey
  reason: 'missing' | 'cutoff'
  profile: string
  since: string
  lastSearch: string
}

export const MOCK_WANTED: MockWantedItem[] = [
  { id: 1, title: 'Severance', subtitle: 'S02E08 · Sweet Vitriol', library: 'tv', reason: 'missing', profile: 'HD-1080p', since: 'aired 3d ago', lastSearch: '2h ago' },
  { id: 2, title: 'The Brutalist', subtitle: '2024', library: 'movies', reason: 'missing', profile: 'UHD Remux', since: 'released 2w ago', lastSearch: '6h ago' },
  { id: 3, title: 'Sound! Euphonium', subtitle: 'S03 · 4 episodes', library: 'anime', reason: 'missing', profile: 'Anime 1080p', since: 'aired 1w ago', lastSearch: '1d ago' },
  { id: 4, title: 'Interstellar', subtitle: '2014 · currently Bluray-1080p', library: 'movies', reason: 'cutoff', profile: 'UHD Remux', since: 'upgrade eligible', lastSearch: '12h ago' },
  { id: 5, title: 'Boards of Canada', subtitle: 'Geogaddi · currently MP3-320', library: 'music', reason: 'cutoff', profile: 'Lossless', since: 'upgrade eligible', lastSearch: '3d ago' },
  { id: 6, title: 'Dark', subtitle: 'S03E05-E08 · 4 episodes', library: 'tv', reason: 'missing', profile: 'HD-1080p', since: 'gap detected', lastSearch: 'never' },
  { id: 7, title: 'Perfect Days', subtitle: '2023', library: 'movies', reason: 'missing', profile: 'HD-1080p', since: 'added from list 4d ago', lastSearch: '4d ago' },
  { id: 8, title: 'Chainsaw Man', subtitle: 'S01 · currently 720p', library: 'anime', reason: 'cutoff', profile: 'Anime 1080p', since: 'upgrade eligible', lastSearch: '2d ago' },
]

// ── History ──────────────────────────────────────────────────────────────

export type MockHistoryEvent = {
  id: number
  at: string
  event: 'grabbed' | 'imported' | 'upgraded' | 'failed' | 'renamed' | 'deleted'
  title: string
  detail: string
  library: MgrLibraryKey
  via: string
}

export const MOCK_HISTORY: MockHistoryEvent[] = [
  { id: 1, at: '09:41', event: 'imported', title: 'Severance S02E07', detail: 'WEB-DL 1080p · 4.1 GB → /storage/tv/Severance/Season 02', library: 'tv', via: 'SABnzbd' },
  { id: 2, at: '09:38', event: 'grabbed', title: 'Dune: Part Two', detail: 'Remux-2160p · 68.4 GB · score +2840', library: 'movies', via: 'PrivateHD' },
  { id: 3, at: '08:12', event: 'upgraded', title: 'Interstellar', detail: 'Bluray-1080p → Remux-2160p · previous file recycled', library: 'movies', via: 'PassThePopcorn' },
  { id: 4, at: '07:55', event: 'failed', title: 'Shōgun S01 (pack)', detail: 'stalled 6h with no seeders — blocklisted, searching again', library: 'tv', via: 'TorrentLeech' },
  { id: 5, at: '07:02', event: 'imported', title: 'Frieren S02E03', detail: 'WEB-DL 1080p · absolute #31 mapped to S02E03', library: 'anime', via: 'qBittorrent' },
  { id: 6, at: '06:47', event: 'renamed', title: 'The Cure — Songs of a Lost World', detail: '12 tracks renamed to {Artist}/{Album}/{track} pattern', library: 'music', via: 'import' },
  { id: 7, at: '03:30', event: 'grabbed', title: 'Sound! Euphonium S03E10', detail: 'WEB-DL 1080p · 1.4 GB · score +1200', library: 'anime', via: 'AnimeBytes' },
  { id: 8, at: 'yesterday', event: 'imported', title: 'The Cure — Songs of a Lost World', detail: 'FLAC · 0.6 GB · 12/12 tracks matched', library: 'music', via: 'qBittorrent' },
  { id: 9, at: 'yesterday', event: 'deleted', title: 'Interstellar (Bluray-1080p)', detail: 'replaced by upgrade · moved to recycle bin (7d)', library: 'movies', via: 'upgrade' },
  { id: 10, at: 'yesterday', event: 'grabbed', title: 'Perfect Days', detail: 'WEB-DL 1080p · 6.2 GB · score +980', library: 'movies', via: 'DrunkenSlug' },
]

// ── Calendar ─────────────────────────────────────────────────────────────

export type MockCalendarEntry = {
  id: number
  time: string
  title: string
  subtitle: string
  library: MgrLibraryKey
  status: 'downloaded' | 'missing' | 'airing' | 'announced'
}

export type MockCalendarDay = {
  label: string
  date: string
  entries: MockCalendarEntry[]
}

export const MOCK_CALENDAR: MockCalendarDay[] = [
  {
    label: 'Today', date: 'Wed 29 Jul', entries: [
      { id: 1, time: '02:00', title: 'Frieren: Beyond Journey\'s End', subtitle: 'S02E04 · The Land of Gold', library: 'anime', status: 'downloaded' },
      { id: 2, time: '21:00', title: 'Severance', subtitle: 'S02E08 · Sweet Vitriol', library: 'tv', status: 'airing' },
    ],
  },
  {
    label: 'Tomorrow', date: 'Thu 30 Jul', entries: [
      { id: 3, time: '17:00', title: 'Sound! Euphonium', subtitle: 'S03E11', library: 'anime', status: 'announced' },
      { id: 4, time: '—', title: 'Nine Inch Nails', subtitle: 'Tron: Ares (Original Score)', library: 'music', status: 'announced' },
    ],
  },
  {
    label: 'Friday', date: 'Fri 31 Jul', entries: [
      { id: 5, time: '09:00', title: 'The Bear', subtitle: 'S04E01-E10 · full season drop', library: 'tv', status: 'announced' },
      { id: 6, time: '—', title: 'Weapons', subtitle: '2025 · digital release', library: 'movies', status: 'announced' },
    ],
  },
  {
    label: 'Monday', date: 'Mon 4 Aug', entries: [
      { id: 7, time: '21:00', title: 'Industry', subtitle: 'S04E01 · premiere', library: 'tv', status: 'announced' },
      { id: 8, time: '02:00', title: 'Dan Da Dan', subtitle: 'S02E05', library: 'anime', status: 'missing' },
    ],
  },
]

// ── Per-library catalog ──────────────────────────────────────────────────

export type MockCatalogItem = {
  id: number
  title: string
  detail: string
  monitored: boolean
  profile: string
  status: 'ok' | 'missing' | 'unmet'
  statusLabel: string
  size: string
}

export const MOCK_CATALOG: Record<string, MockCatalogItem[]> = {
  movie: [
    { id: 1, title: 'Dune: Part Two', detail: '2024', monitored: true, profile: 'UHD Remux', status: 'ok', statusLabel: 'Remux-2160p', size: '68.4 GB' },
    { id: 2, title: 'Oppenheimer', detail: '2023', monitored: true, profile: 'UHD Remux', status: 'ok', statusLabel: 'Bluray-2160p', size: '54.8 GB' },
    { id: 3, title: 'Interstellar', detail: '2014', monitored: true, profile: 'UHD Remux', status: 'unmet', statusLabel: 'Bluray-1080p · below cutoff', size: '31.2 GB' },
    { id: 4, title: 'The Brutalist', detail: '2024', monitored: true, profile: 'UHD Remux', status: 'missing', statusLabel: 'missing', size: '—' },
    { id: 5, title: 'Perfect Days', detail: '2023', monitored: true, profile: 'HD-1080p', status: 'missing', statusLabel: 'grab in queue', size: '—' },
    { id: 6, title: 'Heat', detail: '1995', monitored: false, profile: 'HD-1080p', status: 'ok', statusLabel: 'Bluray-1080p', size: '18.6 GB' },
  ],
  tv: [
    { id: 1, title: 'Severance', detail: '2 seasons · 19 episodes', monitored: true, profile: 'HD-1080p', status: 'unmet', statusLabel: '18/19 episodes', size: '74.2 GB' },
    { id: 2, title: 'Shōgun', detail: '1 season · 10 episodes', monitored: true, profile: 'HD-1080p', status: 'missing', statusLabel: '0/10 · pack stalled', size: '—' },
    { id: 3, title: 'Dark', detail: '3 seasons · 26 episodes', monitored: true, profile: 'HD-1080p', status: 'unmet', statusLabel: '22/26 episodes', size: '61.0 GB' },
    { id: 4, title: 'The Bear', detail: '3 seasons · 28 episodes', monitored: true, profile: 'HD-1080p', status: 'ok', statusLabel: '28/28 episodes', size: '48.9 GB' },
    { id: 5, title: 'Mad Men', detail: '7 seasons · 92 episodes', monitored: false, profile: 'HD-1080p', status: 'ok', statusLabel: '92/92 episodes', size: '198.4 GB' },
  ],
  anime: [
    { id: 1, title: 'Frieren: Beyond Journey\'s End', detail: '2 seasons · 32 episodes', monitored: true, profile: 'Anime 1080p', status: 'ok', statusLabel: '32/32 · absolute', size: '44.7 GB' },
    { id: 2, title: 'Sound! Euphonium', detail: '3 seasons · 39 episodes', monitored: true, profile: 'Anime 1080p', status: 'missing', statusLabel: '35/39 episodes', size: '52.1 GB' },
    { id: 3, title: 'Chainsaw Man', detail: '1 season · 12 episodes', monitored: true, profile: 'Anime 1080p', status: 'unmet', statusLabel: '12/12 · 720p below cutoff', size: '9.8 GB' },
    { id: 4, title: 'Dan Da Dan', detail: '2 seasons · 16 episodes', monitored: true, profile: 'Anime 1080p', status: 'missing', statusLabel: '15/16 episodes', size: '21.3 GB' },
  ],
  music: [
    { id: 1, title: 'The Cure', detail: '14 albums monitored', monitored: true, profile: 'Lossless', status: 'ok', statusLabel: '14/14 albums', size: '8.2 GB' },
    { id: 2, title: 'Boards of Canada', detail: '5 albums monitored', monitored: true, profile: 'Lossless', status: 'unmet', statusLabel: 'Geogaddi MP3-320', size: '2.9 GB' },
    { id: 3, title: 'Nine Inch Nails', detail: '11 albums monitored', monitored: true, profile: 'Lossless', status: 'missing', statusLabel: '10/11 albums', size: '6.4 GB' },
    { id: 4, title: 'Big Red Machine', detail: '2 albums monitored', monitored: false, profile: 'Lossless', status: 'ok', statusLabel: '2/2 albums', size: '1.1 GB' },
  ],
  book: [
    { id: 1, title: 'Ted Chiang', detail: '2 works monitored', monitored: true, profile: 'EPUB', status: 'ok', statusLabel: '2/2 works', size: '4.1 MB' },
    { id: 2, title: 'Ursula K. Le Guin', detail: '9 works monitored', monitored: true, profile: 'EPUB', status: 'missing', statusLabel: '7/9 works', size: '18.9 MB' },
  ],
}

// ── System: indexers ─────────────────────────────────────────────────────

export type MockIndexer = {
  id: number
  name: string
  protocol: 'torrent' | 'usenet'
  priority: number
  categories: string
  state: 'ok' | 'warn' | 'error'
  stateLabel: string
  latency: string
}

export const MOCK_INDEXERS: MockIndexer[] = [
  { id: 1, name: 'PassThePopcorn', protocol: 'torrent', priority: 1, categories: 'Movies', state: 'ok', stateLabel: 'healthy', latency: '210 ms' },
  { id: 2, name: 'PrivateHD', protocol: 'torrent', priority: 5, categories: 'Movies · TV', state: 'ok', stateLabel: 'healthy', latency: '340 ms' },
  { id: 3, name: 'AnimeBytes', protocol: 'torrent', priority: 1, categories: 'Anime · Music', state: 'ok', stateLabel: 'healthy', latency: '480 ms' },
  { id: 4, name: 'Redacted', protocol: 'torrent', priority: 1, categories: 'Music', state: 'ok', stateLabel: 'healthy', latency: '190 ms' },
  { id: 5, name: 'TorrentLeech', protocol: 'torrent', priority: 25, categories: 'Movies · TV', state: 'warn', stateLabel: 'rate-limited · backing off 15m', latency: '—' },
  { id: 6, name: 'DrunkenSlug', protocol: 'usenet', priority: 10, categories: 'Movies · TV · Music', state: 'ok', stateLabel: 'healthy', latency: '150 ms' },
  { id: 7, name: 'NZBGeek', protocol: 'usenet', priority: 15, categories: 'Movies · TV', state: 'error', stateLabel: 'API key rejected', latency: '—' },
]

// ── System: download clients ─────────────────────────────────────────────

export type MockDownloadClient = {
  id: number
  name: string
  kind: string
  protocol: 'torrent' | 'usenet'
  host: string
  category: string
  state: 'ok' | 'warn' | 'error'
  stateLabel: string
  active: number
}

export const MOCK_CLIENTS: MockDownloadClient[] = [
  { id: 1, name: 'qBittorrent', kind: 'qBittorrent 5.0.3', protocol: 'torrent', host: 'http://knas:8081', category: 'heya', state: 'ok', stateLabel: 'connected', active: 4 },
  { id: 2, name: 'SABnzbd', kind: 'SABnzbd 4.4.1', protocol: 'usenet', host: 'http://knas:8085', category: 'heya', state: 'ok', stateLabel: 'connected', active: 1 },
]

// ── System: quality profiles ─────────────────────────────────────────────

export type MockQualityProfile = {
  id: number
  name: string
  cutoff: string
  upgradesUntil: string
  ladder: { name: string, allowed: boolean }[]
  formats: number
  usedBy: string
}

export const MOCK_PROFILES: MockQualityProfile[] = [
  {
    id: 1, name: 'UHD Remux', cutoff: 'Remux-2160p', upgradesUntil: 'custom format score 5000',
    ladder: [
      { name: 'Remux-2160p', allowed: true },
      { name: 'Bluray-2160p', allowed: true },
      { name: 'WEB-DL 2160p', allowed: true },
      { name: 'Bluray-1080p', allowed: true },
      { name: 'WEB-DL 1080p', allowed: true },
      { name: 'HDTV-1080p', allowed: false },
    ],
    formats: 14, usedBy: 'Movies',
  },
  {
    id: 2, name: 'HD-1080p', cutoff: 'WEB-DL 1080p', upgradesUntil: 'custom format score 2000',
    ladder: [
      { name: 'Bluray-1080p', allowed: true },
      { name: 'WEB-DL 1080p', allowed: true },
      { name: 'WEB-Rip 1080p', allowed: true },
      { name: 'HDTV-1080p', allowed: true },
      { name: 'WEB-DL 720p', allowed: false },
    ],
    formats: 9, usedBy: 'TV · Movies',
  },
  {
    id: 3, name: 'Anime 1080p', cutoff: 'WEB-DL 1080p', upgradesUntil: 'preferred fansub group',
    ladder: [
      { name: 'Bluray-1080p', allowed: true },
      { name: 'WEB-DL 1080p', allowed: true },
      { name: 'WEB-DL 720p', allowed: true },
    ],
    formats: 6, usedBy: 'Anime',
  },
  {
    id: 4, name: 'Lossless', cutoff: 'FLAC', upgradesUntil: 'cutoff met',
    ladder: [
      { name: 'FLAC 24-bit', allowed: true },
      { name: 'FLAC', allowed: true },
      { name: 'MP3-320', allowed: true },
      { name: 'MP3-V0', allowed: false },
    ],
    formats: 3, usedBy: 'Music',
  },
]

// ── System: custom formats ───────────────────────────────────────────────

export type MockCustomFormat = {
  id: number
  name: string
  specs: number
  tags: string[]
  scores: string
  source: 'TRaSH' | 'custom'
}

export const MOCK_FORMATS: MockCustomFormat[] = [
  { id: 1, name: 'Remux Tier 01', specs: 3, tags: ['release group'], scores: 'UHD Remux +1800', source: 'TRaSH' },
  { id: 2, name: 'DV HDR10+', specs: 2, tags: ['HDR', 'Dolby Vision'], scores: 'UHD Remux +1500', source: 'TRaSH' },
  { id: 3, name: 'IMAX Enhanced', specs: 2, tags: ['edition'], scores: 'UHD Remux +800', source: 'TRaSH' },
  { id: 4, name: 'x265 (HD)', specs: 4, tags: ['codec'], scores: 'HD-1080p −10000', source: 'TRaSH' },
  { id: 5, name: 'Repack / Proper', specs: 2, tags: ['repack'], scores: 'all profiles +5', source: 'TRaSH' },
  { id: 6, name: 'Scene groups (LQ)', specs: 6, tags: ['release group'], scores: 'all profiles −1000', source: 'TRaSH' },
  { id: 7, name: 'SubsPlease', specs: 1, tags: ['fansub group'], scores: 'Anime 1080p +500', source: 'custom' },
  { id: 8, name: 'Dual audio (JP+EN)', specs: 2, tags: ['audio'], scores: 'Anime 1080p +300', source: 'custom' },
]

// ── System: import lists ─────────────────────────────────────────────────

export type MockImportList = {
  id: number
  name: string
  kind: string
  target: string
  behavior: string
  lastSync: string
  items: number
  have: number
  state: 'ok' | 'warn'
}

export const MOCK_LISTS: MockImportList[] = [
  { id: 1, name: 'Trakt watchlist', kind: 'Trakt · personal', target: 'Movies + TV', behavior: 'add · monitor · grab missing', lastSync: '25m ago', items: 34, have: 29, state: 'ok' },
  { id: 2, name: 'TMDB: Top Rated 250', kind: 'TMDB · public list', target: 'Movies', behavior: 'add · monitor · grab missing', lastSync: '1h ago', items: 250, have: 212, state: 'ok' },
  { id: 3, name: 'AniList: Watching', kind: 'AniList · personal', target: 'Anime', behavior: 'add · monitor · grab missing', lastSync: '25m ago', items: 12, have: 11, state: 'ok' },
  { id: 4, name: 'MDBList: A24 films', kind: 'MDBList · public', target: 'Movies', behavior: 'add only (no auto-grab)', lastSync: 'failed 3h ago', items: 148, have: 96, state: 'warn' },
]
