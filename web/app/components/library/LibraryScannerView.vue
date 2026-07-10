<script setup lang="ts">
import type { Library } from '~~/shared/types'

type ScanRun = {
  id: number
  library_id: number
  media_type: string
  scanner_version: string
  mode: string
  status: string
  summary: Record<string, any>
  error_message?: string
  started_at?: string
  finished_at?: string
  created_at?: string
}

type ScanFinding = {
  id: number
  scan_run_id?: number
  identity_id?: number
  media_item_id?: number
  library_file_id?: number
  severity: string
  code: string
  rel_path?: string
  message: string
  data: Record<string, any>
  created_at?: string
  identity_key?: string
  identity_title?: string
  identity_year?: string
  media_title?: string
}

type ScanIdentity = {
  id: number
  identity_key: string
  title: string
  year?: string
  confidence: number
  source: string
  review_status: string
  bucket: string
  metadata_provider_id?: string
  media_item_id?: number
  selected_provider_id?: string
  selected_title?: string
  selected_year?: string
  selected_score?: number
  candidate_count: number
  open_finding_count: number
}

type ScanCandidate = {
  id: number
  identity_id: number
  provider_name: string
  provider_id: string
  provider_kind: string
  title: string
  year?: string
  author?: string
  description?: string
  poster_url?: string
  heya_slug?: string
  score?: number
  rank: number
  status: string
  rejection_reason?: string
  external_ids?: Record<string, string>
}

type ScanCandidateDetail = {
  candidate_id: number
  provider_id: string
  provider_name: string
  provider_kind: string
  title: string
  year?: string
  author?: string
  description?: string
  poster_url?: string
  backdrop_url?: string
  heya_slug?: string
  status?: string
  genres?: string[]
  external_ids?: Record<string, string>
  runtime_minutes?: number
  number_of_seasons?: number
  number_of_episodes?: number
  first_air_date?: string
  last_air_date?: string
  networks?: string[]
  isbn?: string
  page_count?: number
  publisher?: string
  publish_date?: string
  language?: string
  subjects?: string[]
}

type BucketCounts = {
  total: number
  matched: number
  needs_review: number
  rejected: number
  unmatched: number
  ignored: number
}

type ScannerView = {
  latest_run?: ScanRun
  bucket_counts?: BucketCounts
  open_findings: ScanFinding[]
  identities: ScanIdentity[]
  candidates?: ScanCandidate[]
}

// Buckets are computed server-side (identity.bucket) so the table and the
// review actions never disagree. An approved-but-not-yet-materialized identity
// reports as `unmatched` (no media_item_id) until a follow-up apply run — the
// UI flags that as "awaiting apply" rather than pretending it is matched.
type Bucket = 'matched' | 'needs_review' | 'unmatched' | 'rejected' | 'ignored'

const props = defineProps<{
  library: Library
}>()

const emit = defineEmits<{
  back: []
}>()

const { $heya } = useNuxtApp()

const includeCandidates = ref(false)
const loading = ref(false)
const error = ref('')
const view = ref<ScannerView | null>(null)
const runs = ref<ScanRun[]>([])
const activeFilter = ref<'all' | Bucket>('all')
const search = ref('')
const expanded = ref<Set<number>>(new Set())
const detailOpen = ref<Set<number>>(new Set())
const candidateDetails = ref<Record<number, ScanCandidateDetail>>({})
const candidateDetailLoading = ref<number | null>(null)
const candidateDetailError = ref<Record<number, string>>({})

// Human labels for the raw finding codes the scanner persists.
const FINDING_LABELS: Record<string, string> = {
  unplanned_media: 'Unplanned media',
  nfo_parse_failed: 'NFO parse failed',
  plexmatch_parse_failed: '.plexmatch parse failed',
  local_identity_issue: 'Local identity issue',
  music_album_issue: 'Music album issue',
  music_track_issue: 'Music track issue',
  music_metadata_mapping: 'Music metadata mapping',
  book_metadata_mapping: 'Book metadata mapping',
  metadata_fetch_failed: 'Metadata fetch failed',
  search_rejected: 'Search rejected',
  search_error: 'Search error',
  search_suspicious: 'Search suspicious',
  title_only_identity: 'Title-only match',
  materialization_blocked: 'Materialize blocked',
  materialization_failed: 'Materialize failed',
  materialization_skipped: 'Materialize skipped',
}

const FILTERS: { key: 'all' | Bucket; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'matched', label: 'Matched' },
  { key: 'needs_review', label: 'Needs review' },
  { key: 'unmatched', label: 'Unmatched' },
  { key: 'rejected', label: 'Rejected' },
  { key: 'ignored', label: 'Ignored' },
]

const BUCKET_META: Record<Bucket, { label: string; state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  matched: { label: 'matched', state: 'ok' },
  needs_review: { label: 'review', state: 'warn' },
  unmatched: { label: 'unmatched', state: 'idle' },
  rejected: { label: 'rejected', state: 'error' },
  ignored: { label: 'ignored', state: 'idle' },
}

function bucketMeta(bucket: string) {
  return BUCKET_META[bucket as Bucket] ?? BUCKET_META.unmatched
}

watch(() => props.library.id, () => {
  activeFilter.value = 'all'
  search.value = ''
  expanded.value = new Set()
  refresh()
}, { immediate: true })

watch(includeCandidates, () => refresh())

const summary = computed(() => view.value?.latest_run?.summary ?? {})
const identities = computed(() => view.value?.identities ?? [])
const findings = computed(() => view.value?.open_findings ?? [])
const candidates = computed(() => view.value?.candidates ?? [])

// Open findings keyed by the identity they attach to. Findings whose
// identity_id never resolved at persist time land in `orphanFindings` instead
// so they aren't silently dropped from the picture.
const findingsByIdentity = computed(() => {
  const map = new Map<number, ScanFinding[]>()
  for (const f of findings.value) {
    if (!f.identity_id) continue
    const list = map.get(f.identity_id) ?? []
    list.push(f)
    map.set(f.identity_id, list)
  }
  return map
})

const orphanFindings = computed(() => findings.value.filter(f => !f.identity_id))

const candidatesByIdentity = computed(() => {
  const map = new Map<number, ScanCandidate[]>()
  for (const c of candidates.value) {
    const list = map.get(c.identity_id) ?? []
    list.push(c)
    map.set(c.identity_id, list)
  }
  for (const list of map.values()) list.sort((a, b) => a.rank - b.rank)
  return map
})

// Counts come from the server's bucket_counts; the client tally is only a
// fallback (keeps working if an older backend omits the field).
const counts = computed<BucketCounts>(() => {
  const bc = view.value?.bucket_counts
  if (bc) return bc
  const t: BucketCounts = { total: 0, matched: 0, needs_review: 0, rejected: 0, unmatched: 0, ignored: 0 }
  for (const i of identities.value) {
    t.total++
    if (i.bucket === 'matched') t.matched++
    else if (i.bucket === 'needs_review') t.needs_review++
    else if (i.bucket === 'rejected') t.rejected++
    else if (i.bucket === 'ignored') t.ignored++
    else t.unmatched++
  }
  return t
})

function bucketCount(key: 'all' | Bucket): number {
  return key === 'all' ? counts.value.total : counts.value[key]
}

// Approved but not yet materialized — has an accepted match but no media item
// until a follow-up apply/scan run attaches files and fetches metadata.
function awaitingApply(identity: ScanIdentity): boolean {
  return identity.review_status === 'accepted' && !identity.media_item_id
}

function canApproveSelectedCandidate(identity: ScanIdentity, candidate: ScanCandidate): boolean {
  return candidate.status === 'selected' && identity.bucket === 'needs_review'
}

const filteredIdentities = computed(() => {
  const q = search.value.trim().toLowerCase()
  return identities.value.filter((i) => {
    if (activeFilter.value !== 'all' && i.bucket !== activeFilter.value) return false
    if (!q) return true
    return (
      i.title.toLowerCase().includes(q) ||
      i.identity_key.toLowerCase().includes(q) ||
      (i.selected_title ?? '').toLowerCase().includes(q)
    )
  })
})

const severityRank: Record<string, number> = { error: 2, warn: 1, info: 0 }

// The single most-severe open finding on an identity, used for the "main
// issue" hint on the row.
function mainFinding(identity: ScanIdentity): ScanFinding | null {
  const fs = findingsByIdentity.value.get(identity.id) ?? []
  if (!fs.length) return null
  return [...fs].sort((a, b) => (severityRank[b.severity] ?? 0) - (severityRank[a.severity] ?? 0))[0]!
}

function findingLabel(code: string): string {
  return FINDING_LABELS[code] ?? code
}

async function refresh(opts: { silent?: boolean } = {}) {
  if (!opts.silent) loading.value = true
  error.value = ''
  try {
    const heya = $heya as any
    const [scanView, runHistory] = await Promise.all([
      heya('/api/libraries/{id}/scanner', {
        path: { id: props.library.id },
        query: { candidates: includeCandidates.value },
      }) as Promise<ScannerView>,
      heya('/api/libraries/{id}/scanner/runs', {
        path: { id: props.library.id },
        query: { limit: 10, offset: 0 },
      }) as Promise<ScanRun[]>,
    ])
    view.value = scanView
    runs.value = runHistory ?? []
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || 'Failed to load scanner state.'
  } finally {
    if (!opts.silent) loading.value = false
  }
}

// Manual review actions. These are review-state transitions, not full
// materialization: approve-candidate marks the candidate selected + clears
// findings, but files/metadata are attached by a later apply run — hence the
// row lands in "unmatched / awaiting apply" until then.
const busyId = ref<number | null>(null)
const actionNote = ref('')
const actionError = ref('')

async function runAction(identity: ScanIdentity, action: string, body: Record<string, any> | undefined, describe: string) {
  busyId.value = identity.id
  actionNote.value = ''
  actionError.value = ''
  try {
    const heya = $heya as any
    await heya(`/api/libraries/{id}/scanner/identities/{identity_id}/${action}`, {
      method: 'POST',
      path: { id: props.library.id, identity_id: identity.id },
      ...(body ? { body } : {}),
    })
    // Refetch silently so identities, candidates, findings and bucket_counts
    // all move together and stay consistent with the server's bucket logic.
    await refresh({ silent: true })
    actionNote.value = describe
  } catch (e: any) {
    actionError.value = e?.data?.error || e?.message || 'Action failed.'
  } finally {
    busyId.value = null
  }
}

function approveCandidate(identity: ScanIdentity, candidate: ScanCandidate) {
  runAction(identity, 'approve-candidate', { candidate_id: candidate.id },
    `Approved “${candidate.title}” for ${identity.title || identity.identity_key} — awaiting apply.`)
}
function rejectIdentity(identity: ScanIdentity) {
  runAction(identity, 'reject', { reason: 'manual_rejected' }, `Rejected ${identity.title || identity.identity_key}.`)
}
function ignoreIdentity(identity: ScanIdentity) {
  runAction(identity, 'ignore', { reason: 'manual_ignored' }, `Ignored ${identity.title || identity.identity_key}.`)
}
function rematchIdentity(identity: ScanIdentity) {
  runAction(identity, 'rematch', undefined, `Reset ${identity.title || identity.identity_key} for re-identify on the next scan.`)
}

// Manual "fix match": live provider search + assign an arbitrary result the
// automated search never surfaced. The dialog posts to .../assign, which
// rides the same approve flow as accepting a scanner-found candidate.
const searchDialogIdentity = ref<ScanIdentity | null>(null)

async function onSearchAssigned(title: string) {
  const identity = searchDialogIdentity.value
  searchDialogIdentity.value = null
  await refresh({ silent: true })
  actionError.value = ''
  actionNote.value = `Matched ${identity?.title || identity?.identity_key || 'identity'} as “${title}” — awaiting apply.`
}

function toggleExpand(id: number) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else {
    next.add(id)
    // Expanding a row is the trigger to pull candidate rows the first time.
    if (!includeCandidates.value) includeCandidates.value = true
  }
  expanded.value = next
}

async function toggleCandidateDetail(identity: ScanIdentity, candidate: ScanCandidate) {
  const next = new Set(detailOpen.value)
  if (next.has(candidate.id)) {
    next.delete(candidate.id)
    detailOpen.value = next
    return
  }
  next.add(candidate.id)
  detailOpen.value = next
  if (candidateDetails.value[candidate.id]) return

  candidateDetailLoading.value = candidate.id
  candidateDetailError.value = { ...candidateDetailError.value, [candidate.id]: '' }
  try {
    const heya = $heya as any
    const detail = await heya('/api/libraries/{id}/scanner/identities/{identity_id}/candidates/{candidate_id}/detail', {
      path: { id: props.library.id, identity_id: identity.id, candidate_id: candidate.id },
    }) as ScanCandidateDetail
    candidateDetails.value = { ...candidateDetails.value, [candidate.id]: detail }
  } catch (e: any) {
    candidateDetailError.value = {
      ...candidateDetailError.value,
      [candidate.id]: e?.data?.error || e?.message || 'Failed to fetch candidate detail.',
    }
  } finally {
    candidateDetailLoading.value = null
  }
}

function formatDate(value?: string): string {
  if (!value) return 'never'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function score(value?: number): string {
  if (value == null) return '—'
  return value.toFixed(2)
}

function summaryNumber(...keys: string[]): number {
  for (const key of keys) {
    const value = summary.value[key]
    if (typeof value === 'number') return value
    if (typeof value === 'string' && value !== '' && !Number.isNaN(Number(value))) return Number(value)
  }
  return 0
}

function selectedMatchLine(identity: ScanIdentity): string {
  const parts: string[] = []
  if (identity.selected_provider_id) parts.push(identity.selected_provider_id)
  if (identity.selected_score != null) parts.push(score(identity.selected_score))
  return parts.join(' · ')
}

function candidateSub(candidate: ScanCandidate): string {
  const parts: string[] = []
  if (candidate.author) parts.push(`by ${candidate.author}`)
  parts.push(candidate.provider_id, `#${candidate.rank}`, score(candidate.score))
  if (candidate.rejection_reason) parts.push(candidate.rejection_reason)
  return parts.join(' · ')
}

function candidateDetailFacts(detail: ScanCandidateDetail): string[] {
  const facts: string[] = []
  if (detail.status) facts.push(detail.status)
  if (detail.number_of_seasons) facts.push(`${detail.number_of_seasons} seasons`)
  if (detail.number_of_episodes) facts.push(`${detail.number_of_episodes} episodes`)
  if (detail.first_air_date) facts.push(`first aired ${detail.first_air_date}`)
  if (detail.networks?.length) facts.push(detail.networks.slice(0, 2).join(', '))
  if (detail.author) facts.push(`by ${detail.author}`)
  if (detail.page_count) facts.push(`${detail.page_count} pages`)
  if (detail.publisher) facts.push(detail.publisher)
  if (detail.publish_date) facts.push(detail.publish_date)
  if (detail.language) facts.push(detail.language.toUpperCase())
  if (detail.isbn) facts.push(`ISBN ${detail.isbn}`)
  return facts
}

function candidateDetailTags(detail: ScanCandidateDetail): string[] {
  return (detail.genres?.length ? detail.genres : detail.subjects)?.slice(0, 8) ?? []
}

function candidateDetailHeyaURL(detail: ScanCandidateDetail): string {
  if (detail.heya_slug) return `https://heya.media/${detail.heya_slug}`
  const parts = detail.provider_id.split(':')
  if (parts.length >= 4 && parts[0] === 'heya') {
    const kind = parts[1]
    const provider = parts[2]
    const value = parts.slice(3).join(':')
    return `https://heya.media/heya_${kind}:${provider}:${value}`
  }
  return 'https://heya.media'
}

function runFiles(run: ScanRun): number {
  const v = run.summary?.files
  return typeof v === 'number' ? v : 0
}
</script>

<template>
  <div class="sv2-view">
    <header class="sv2-head">
      <button class="back-btn" @click="emit('back')">
        <Icon name="back" :size="14" />
        Libraries
      </button>
      <div class="head-title">
        <div class="head-icon" :class="`kind-${library.media_type}`">
          <Icon name="database" :size="16" />
        </div>
        <div class="head-text">
          <h2>{{ library.name }} · Scanner</h2>
          <p class="mono">
            {{ library.media_type }} ·
            <template v-if="view?.latest_run">
              last run {{ view.latest_run.status }} · {{ formatDate(view.latest_run.finished_at || view.latest_run.started_at) }}
            </template>
            <template v-else>no persisted run yet</template>
          </p>
        </div>
      </div>
      <div class="head-actions">
        <button class="sv2-btn ghost" :class="{ active: includeCandidates }" @click="includeCandidates = !includeCandidates">
          <Icon name="search" :size="12" />
          Candidates
        </button>
        <button class="sv2-btn ghost" :disabled="loading" @click="refresh()">
          <Icon :name="loading ? 'spinner' : 'refresh'" :size="12" />
          Refresh
        </button>
      </div>
    </header>

    <div v-if="error" class="sv2-note error">
      <Icon name="warning" :size="13" /> {{ error }}
    </div>

    <div v-else class="sv2-body" :class="{ loading }">
      <div v-if="actionError" class="sv2-note error">
        <Icon name="warning" :size="13" /> {{ actionError }}
      </div>
      <div v-else-if="actionNote" class="sv2-note ok">
        <Icon name="check" :size="13" /> {{ actionNote }}
      </div>

      <div class="sv2-tiles">
        <MetricTile label="Files" :value="summaryNumber('files', 'classified_files')" icon="folder" />
        <MetricTile label="Identities" :value="bucketCount('all')" icon="list" />
        <MetricTile label="Matched" :value="bucketCount('matched')" icon="check" tone="good" />
        <MetricTile label="Needs review" :value="bucketCount('needs_review')" icon="pencil" :tone="bucketCount('needs_review') ? 'warn' : 'neutral'" />
        <MetricTile label="Unmatched" :value="bucketCount('unmatched')" icon="info" :tone="bucketCount('unmatched') ? 'warn' : 'neutral'" />
        <MetricTile label="Rejected" :value="bucketCount('rejected')" icon="close" :tone="bucketCount('rejected') ? 'bad' : 'neutral'" />
        <MetricTile label="Ignored" :value="bucketCount('ignored')" icon="eye" tone="neutral" />
      </div>

      <div v-if="orphanFindings.length" class="sv2-note">
        <Icon name="warning" :size="13" />
        {{ orphanFindings.length }} scan issue{{ orphanFindings.length === 1 ? '' : 's' }} not tied to an identity
        (parse or scan-level problems) — listed under Scan issues below.
      </div>

      <div class="filter-bar">
        <div class="filter-chips">
          <button
            v-for="f in FILTERS"
            :key="f.key"
            class="filter-chip"
            :class="{ active: activeFilter === f.key }"
            @click="activeFilter = f.key"
          >
            {{ f.label }}
            <span class="chip-count">{{ bucketCount(f.key) }}</span>
          </button>
        </div>
        <div class="filter-search">
          <Icon name="search" :size="13" />
          <input v-model="search" placeholder="Filter by title or key…" />
        </div>
      </div>

      <section class="identity-panel">
        <div v-if="identities.length === 0" class="panel-empty">
          No persisted scanner identities for this library.
        </div>
        <div v-else-if="filteredIdentities.length === 0" class="panel-empty">
          No identities match this filter.
        </div>
        <table v-else class="idt">
          <tbody>
            <template v-for="identity in filteredIdentities" :key="identity.id">
              <tr class="idt-row" :class="{ open: expanded.has(identity.id) }" @click="toggleExpand(identity.id)">
                <td class="idt-chev">
                  <button
                    type="button"
                    class="idt-toggle"
                    :aria-expanded="expanded.has(identity.id)"
                    :aria-label="`Toggle candidates for ${identity.title || identity.identity_key}`"
                  >
                    <Icon name="chevright" :size="13" class="chev" :class="{ rot: expanded.has(identity.id) }" />
                  </button>
                </td>
                <td class="idt-status">
                  <StatusBadge :state="bucketMeta(identity.bucket).state">
                    {{ bucketMeta(identity.bucket).label }}
                  </StatusBadge>
                </td>
                <td class="idt-local">
                  <div class="cell-title">
                    {{ identity.title || '(untitled)' }}
                    <span v-if="identity.year" class="dim">({{ identity.year }})</span>
                  </div>
                  <div class="cell-sub mono">{{ identity.identity_key }}</div>
                </td>
                <td class="idt-match">
                  <template v-if="identity.selected_title">
                    <div class="cell-title">
                      <span class="arrow-in">→</span> {{ identity.selected_title }}
                      <span v-if="identity.selected_year" class="dim">({{ identity.selected_year }})</span>
                    </div>
                    <div class="cell-sub mono">{{ selectedMatchLine(identity) }}</div>
                  </template>
                  <div v-else class="cell-title dim">no selected match</div>
                </td>
                <td class="idt-flags">
                  <span class="flag">{{ identity.candidate_count }} cand</span>
                  <span v-if="awaitingApply(identity)" class="flag apply">awaiting apply</span>
                  <span v-else-if="mainFinding(identity)" class="flag issue">
                    {{ findingLabel(mainFinding(identity)!.code) }}
                  </span>
                </td>
                <td class="idt-spacer" />
                <td class="idt-link">
                  <NuxtLink
                    v-if="identity.media_item_id"
                    class="media-link"
                    :to="`/media/${identity.media_item_id}`"
                    :aria-label="`Open ${identity.selected_title || identity.title} detail`"
                    @click.stop
                  >
                    <Icon name="arrow-right" :size="13" />
                  </NuxtLink>
                </td>
              </tr>
              <tr v-if="expanded.has(identity.id)" class="idt-detail-row">
                <td colspan="7">
                  <div class="identity-detail">
                    <div v-if="mainFinding(identity)" class="detail-issue">
                      <Icon name="warning" :size="12" />
                      <span><b>{{ findingLabel(mainFinding(identity)!.code) }}:</b> {{ mainFinding(identity)!.message }}</span>
                    </div>

                    <div class="detail-head">
                      <span>Candidates</span>
                      <span v-if="!includeCandidates" class="dim">loading…</span>
                    </div>

                    <div v-if="!includeCandidates" class="detail-empty">Loading candidate rows…</div>
                    <div v-else-if="(candidatesByIdentity.get(identity.id) ?? []).length === 0" class="detail-empty">
                      No provider candidates recorded for this identity.
                    </div>
                    <div v-else class="candidate-list">
                      <div
                        v-for="candidate in candidatesByIdentity.get(identity.id)"
                        :key="candidate.id"
                        class="candidate-row"
                      >
                        <div class="candidate-rank mono">{{ candidate.rank }}</div>
                        <img
                          v-if="candidate.poster_url"
                          class="candidate-poster"
                          :src="candidate.poster_url"
                          alt=""
                          loading="lazy"
                        >
                        <div class="candidate-main">
                          <div class="candidate-title">
                            {{ candidate.title }}
                            <span v-if="candidate.year" class="dim">({{ candidate.year }})</span>
                          </div>
                          <div class="candidate-sub mono">{{ candidateSub(candidate) }}</div>
                          <div v-if="candidate.description" class="candidate-description">
                            {{ candidate.description }}
                          </div>
                        </div>
                        <StatusBadge :state="candidate.status === 'selected' ? 'ok' : candidate.status === 'review_candidate' ? 'warn' : candidate.status === 'rejected' ? 'error' : 'idle'">
                          {{ candidate.status }}
                        </StatusBadge>
                        <div class="candidate-actions">
                          <button
                            class="mini-btn"
                            :disabled="candidateDetailLoading === candidate.id"
                            @click.stop="toggleCandidateDetail(identity, candidate)"
                          >
                            <Icon :name="candidateDetailLoading === candidate.id ? 'spinner' : 'info'" :size="11" />
                            {{ detailOpen.has(candidate.id) ? 'Hide details' : 'Details' }}
                          </button>
                          <button
                            v-if="canApproveSelectedCandidate(identity, candidate)"
                            class="mini-btn accept"
                            :disabled="busyId === identity.id"
                            @click.stop="approveCandidate(identity, candidate)"
                          >
                            <Icon name="check" :size="11" /> Accept selected
                          </button>
                          <button
                            v-else-if="candidate.status === 'selected'"
                            class="mini-btn selected"
                            disabled
                          >
                            <Icon name="check" :size="11" /> Selected
                          </button>
                          <button
                            v-else
                            class="mini-btn accept"
                            :disabled="busyId === identity.id"
                            @click.stop="approveCandidate(identity, candidate)"
                          >
                            Use this
                          </button>
                        </div>
                        <div v-if="detailOpen.has(candidate.id)" class="candidate-detail">
                          <div v-if="candidateDetailError[candidate.id]" class="candidate-detail-error">
                            {{ candidateDetailError[candidate.id] }}
                          </div>
                          <div v-else-if="candidateDetails[candidate.id]" class="candidate-detail-body">
                            <img
                              v-if="candidateDetails[candidate.id]!.poster_url"
                              class="candidate-detail-poster"
                              :src="candidateDetails[candidate.id]!.poster_url"
                              alt=""
                              loading="lazy"
                            >
                            <div class="candidate-detail-main">
                              <div class="candidate-detail-title">
                                {{ candidateDetails[candidate.id]!.title }}
                                <span v-if="candidateDetails[candidate.id]!.year" class="dim">({{ candidateDetails[candidate.id]!.year }})</span>
                              </div>
                              <div class="candidate-detail-sub mono">
                                {{ candidateDetails[candidate.id]!.provider_id }}
                              </div>
                              <div v-if="candidateDetailFacts(candidateDetails[candidate.id]!).length" class="candidate-detail-facts">
                                <span v-for="fact in candidateDetailFacts(candidateDetails[candidate.id]!)" :key="fact">{{ fact }}</span>
                              </div>
                              <p v-if="candidateDetails[candidate.id]!.description" class="candidate-detail-description">
                                {{ candidateDetails[candidate.id]!.description }}
                              </p>
                              <div v-if="candidateDetailTags(candidateDetails[candidate.id]!).length" class="candidate-detail-genres">
                                <span v-for="tag in candidateDetailTags(candidateDetails[candidate.id]!)" :key="tag">{{ tag }}</span>
                              </div>
                              <div class="candidate-detail-actions">
                                <a
                                  class="mini-btn link"
                                  :href="candidateDetailHeyaURL(candidateDetails[candidate.id]!)"
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  @click.stop
                                >
                                  <Icon name="link" :size="11" /> Open on Heya
                                </a>
                              </div>
                            </div>
                          </div>
                          <div v-else class="detail-empty">Fetching candidate detail…</div>
                        </div>
                      </div>
                    </div>

                    <div class="detail-foot">
                      <button class="mini-btn accept" :disabled="busyId === identity.id" @click.stop="searchDialogIdentity = identity">
                        <Icon name="search" :size="11" /> Search match…
                      </button>
                      <button class="mini-btn" :disabled="busyId === identity.id" @click.stop="rematchIdentity(identity)">
                        <Icon :name="busyId === identity.id ? 'spinner' : 'refresh'" :size="11" /> Reset / re-identify
                      </button>
                      <button
                        class="mini-btn"
                        :disabled="busyId === identity.id || identity.bucket === 'ignored'"
                        @click.stop="ignoreIdentity(identity)"
                      >
                        Ignore
                      </button>
                      <button
                        class="mini-btn danger"
                        :disabled="busyId === identity.id || identity.bucket === 'rejected'"
                        @click.stop="rejectIdentity(identity)"
                      >
                        Reject
                      </button>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </section>

      <!-- Only findings NOT tied to an identity live here — per-identity issues
           are already surfaced inline in the table above (issue pill + expanded
           detail), so this panel is purely the parse/scan-level leftovers. -->
      <section v-if="orphanFindings.length" class="findings-panel">
        <div class="panel-head">
          <h4>Scan issues</h4>
          <span>{{ orphanFindings.length }} not tied to an identity</span>
        </div>
        <div class="finding-list">
          <div v-for="finding in orphanFindings" :key="finding.id" class="finding-row">
            <StatusBadge :state="finding.severity === 'error' ? 'error' : finding.severity === 'warn' ? 'warn' : 'idle'">
              {{ findingLabel(finding.code) }}
            </StatusBadge>
            <div class="finding-main">
              <div class="finding-title">{{ finding.rel_path || finding.code }}</div>
              <div class="finding-msg">{{ finding.message }}</div>
            </div>
          </div>
        </div>
      </section>

      <section class="runs-panel">
        <div class="panel-head">
          <h4>Run history</h4>
          <span>{{ runs.length }} recent runs</span>
        </div>
        <div v-if="runs.length === 0" class="panel-empty">No scanner runs persisted yet.</div>
        <div v-else class="run-list">
          <div v-for="run in runs" :key="run.id" class="run-row">
            <StatusBadge :state="run.status === 'complete' ? 'ok' : 'warn'">{{ run.status }}</StatusBadge>
            <div class="run-main">
              <div class="run-title">#{{ run.id }} · {{ run.mode }} · {{ run.scanner_version }}</div>
              <div class="run-sub mono">{{ formatDate(run.started_at) }}</div>
            </div>
            <div class="run-stats mono">{{ runFiles(run) }} files</div>
          </div>
        </div>
      </section>
    </div>

    <LibraryScannerSearchDialog
      :library-id="library.id"
      :identity="searchDialogIdentity"
      :show="!!searchDialogIdentity"
      @applied="onSearchAssigned"
      @close="searchDialogIdentity = null"
    />
  </div>
</template>

<style scoped>
.sv2-view { display: flex; flex-direction: column; gap: 16px; }

.sv2-head {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}
.back-btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-size: 12px;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.back-btn:hover { color: var(--fg-0); border-color: var(--border-strong); background: rgb(var(--ink) / 0.04); }

.head-title { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; }
.head-icon {
  width: 38px; height: 38px;
  border-radius: var(--r-sm);
  background: var(--gold-soft); color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.head-icon.kind-tv, .head-icon.kind-anime { color: rgb(140,160,255); background: rgba(140,160,255,0.10); }
.head-text { min-width: 0; }
.head-text h2 { margin: 0; font-size: 16px; font-weight: 600; color: var(--fg-0); }
.head-text p { margin: 2px 0 0; font-size: 11px; color: var(--fg-3); text-transform: capitalize; }
.head-actions { display: flex; gap: 8px; flex-shrink: 0; }

.sv2-body { display: flex; flex-direction: column; gap: 18px; }
.sv2-body.loading { opacity: 0.6; pointer-events: none; }

.sv2-tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 8px;
}

.sv2-note {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px;
  border-radius: var(--r-md);
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 25%, transparent);
  color: var(--gold);
  font-size: 12px;
}
.sv2-note.error {
  background: color-mix(in srgb, var(--bad) 6%, transparent);
  border-color: color-mix(in srgb, var(--bad) 28%, transparent);
  color: var(--bad);
}
.sv2-note.ok {
  background: color-mix(in srgb, var(--good) 8%, transparent);
  border-color: color-mix(in srgb, var(--good) 28%, transparent);
  color: var(--good);
}

.filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.filter-chips { display: flex; gap: 6px; flex-wrap: wrap; }
.filter-chip {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 11px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--fg-2);
  font-size: 12px;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.filter-chip:hover { color: var(--fg-0); border-color: var(--border-strong); }
.filter-chip.active { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 35%, transparent); background: var(--gold-soft); }
.chip-count {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--fg-3);
}
.filter-chip.active .chip-count { color: var(--gold); }

.filter-search {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 0 10px;
  height: 32px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--fg-3);
}
.filter-search input {
  background: transparent; border: none; outline: none;
  color: var(--fg-0); font-size: 12px; width: 200px;
}

.identity-panel, .findings-panel, .runs-panel {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}

.panel-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
}
.panel-head h4 {
  margin: 0; font-size: 12px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-1);
}
.panel-head span { font-size: 11px; color: var(--fg-3); }

.panel-empty {
  padding: 20px 16px;
  color: var(--fg-3);
  font-size: 12.5px;
  text-align: center;
}

/* Aligned data table. Reka has no data-table primitive and our KVTable is
   key/value only, so this is a plain semantic <table>. A width:100% spacer
   column soaks up slack, so every other column hugs its content and the
   columns line up across all rows (status, local, match, flags). */
.idt { width: 100%; table-layout: fixed; border-collapse: collapse; }
.idt-row { cursor: pointer; }
.idt-row > td {
  padding: 9px 10px;
  border-top: 1px solid var(--border);
  vertical-align: middle;
  white-space: nowrap;
  transition: background 0.12s;
}
.idt-row:first-child > td { border-top: 0; }
.idt-row:hover > td { background: rgb(var(--ink) / 0.04); }
.idt-row.open > td { background: rgb(var(--ink) / 0.03); }
.idt-row > td:first-child { padding-left: 14px; }
.idt-row > td:last-child { padding-right: 14px; }

.idt-chev { width: 38px; }
.idt-status { width: 110px; }
.idt-local { width: 27%; }
.idt-match { width: 34%; }
.idt-flags { width: 150px; }
.idt-link { width: 38px; text-align: right; }
.idt-spacer { width: auto; padding: 0 !important; }

.idt-toggle {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px; border-radius: var(--r-xs);
  color: var(--fg-3);
}
.idt-toggle:hover { color: var(--fg-1); background: rgb(var(--ink) / 0.06); }
.idt-toggle:focus-visible { outline: 2px solid var(--gold); outline-offset: 1px; }
.chev { transition: transform 0.15s ease; }
.chev.rot { transform: rotate(90deg); }

.cell-title {
  font-size: 13px; font-weight: 600; color: var(--fg-0);
  max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.idt-match .cell-title { font-weight: 500; color: var(--fg-1); }
.cell-sub {
  margin-top: 2px; font-size: 11px; color: var(--fg-3);
  max-width: 100%; overflow: hidden; text-overflow: ellipsis;
}
.arrow-in { color: var(--fg-4); }

.idt-flags .flag + .flag { margin-left: 6px; }
.flag {
  display: inline-block;
  font-family: var(--font-mono); font-size: 10px;
  padding: 2px 7px; border-radius: 999px;
  background: rgb(var(--ink) / 0.04); color: var(--fg-3);
  white-space: nowrap;
}
.flag.issue { background: var(--gold-soft); color: var(--gold); }
.flag.apply { background: rgba(140,160,255,0.12); color: rgb(150,170,255); }
.dim { color: var(--fg-3); }

.media-link {
  width: 26px; height: 26px; border-radius: var(--r-sm);
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--fg-3);
}
.media-link:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.06); }

.idt-detail-row > td {
  padding: 2px 14px 14px 42px;
  background: rgb(var(--ink) / 0.02);
}
.identity-detail {
  display: flex; flex-direction: column; gap: 10px;
}
.detail-issue {
  display: flex; align-items: flex-start; gap: 8px;
  padding: 9px 12px; border-radius: var(--r-sm);
  background: var(--gold-soft); border: 1px solid color-mix(in srgb, var(--gold) 22%, transparent);
  color: var(--fg-1); font-size: 12px; line-height: 1.4;
}
.detail-issue :deep(b) { color: var(--gold); }
.detail-head {
  display: flex; align-items: center; gap: 8px;
  font-family: var(--font-mono); font-size: 10px;
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-3);
}
.detail-empty { font-size: 12px; color: var(--fg-3); padding: 4px 0; }

.candidate-list {
  display: flex; flex-direction: column;
  border: 1px solid var(--border); border-radius: var(--r-sm); overflow: hidden;
}
.candidate-row {
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  padding: 9px 12px;
  border-top: 1px solid var(--border);
  background: var(--bg-1);
}
.candidate-row:first-child { border-top: 0; }
.candidate-rank {
  width: 24px; height: 24px; border-radius: var(--r-xs);
  display: flex; align-items: center; justify-content: center;
  background: var(--bg-0); color: var(--fg-3); font-size: 11px; flex-shrink: 0;
}
.candidate-poster {
  width: 34px; height: 50px; border-radius: var(--r-xs);
  object-fit: cover; background: var(--bg-0); flex-shrink: 0;
}
.candidate-main { flex: 1; min-width: 0; }
.candidate-title { font-size: 12.5px; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.candidate-sub { margin-top: 2px; font-size: 11px; color: var(--fg-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.candidate-description {
  margin-top: 5px; color: var(--fg-2); font-size: 11.5px; line-height: 1.35;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.candidate-actions { display: flex; align-items: center; gap: 5px; flex-shrink: 0; }
.candidate-detail {
  flex: 0 0 100%;
  margin-left: 34px;
  border-top: 1px solid var(--border);
  padding-top: 10px;
}
.candidate-detail-body {
  display: flex; gap: 12px; align-items: flex-start;
}
.candidate-detail-poster {
  width: 72px; height: 108px; border-radius: var(--r-sm);
  object-fit: cover; background: var(--bg-0); flex-shrink: 0;
}
.candidate-detail-main { flex: 1; min-width: 0; }
.candidate-detail-title { font-size: 13px; font-weight: 650; color: var(--fg-0); }
.candidate-detail-sub { margin-top: 2px; font-size: 11px; color: var(--fg-3); }
.candidate-detail-facts, .candidate-detail-genres {
  display: flex; flex-wrap: wrap; gap: 5px; margin-top: 8px;
}
.candidate-detail-facts span, .candidate-detail-genres span {
  border: 1px solid var(--border); border-radius: 999px;
  padding: 2px 7px; font-size: 10.5px; color: var(--fg-2); background: rgb(var(--ink) / 0.03);
}
.candidate-detail-description {
  margin: 8px 0 0; color: var(--fg-1); font-size: 12px; line-height: 1.45;
}
.candidate-detail-actions { display: flex; margin-top: 10px; }
.candidate-detail-error {
  color: var(--bad); font-size: 12px;
}
.mini-btn {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 4px 9px; border-radius: var(--r-xs);
  border: 1px solid var(--border);
  background: var(--bg-2); color: var(--fg-2);
  font-size: 11px;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.mini-btn.link { text-decoration: none; }
.mini-btn:hover:not(:disabled) { color: var(--fg-0); border-color: var(--border-strong); background: rgb(var(--ink) / 0.05); }
.mini-btn:disabled { opacity: 0.45; cursor: not-allowed; }
.mini-btn.accept { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 35%, transparent); }
.mini-btn.accept:hover:not(:disabled) { background: var(--gold-soft); color: var(--gold); border-color: color-mix(in srgb, var(--gold) 50%, transparent); }
.mini-btn.selected { color: var(--good); border-color: color-mix(in srgb, var(--good) 30%, transparent); }
.mini-btn.danger:hover:not(:disabled) { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 35%, transparent); background: color-mix(in srgb, var(--bad) 8%, transparent); }

.detail-foot { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; font-size: 11px; }

.finding-list, .run-list { display: flex; flex-direction: column; }
.finding-row, .run-row {
  display: flex; align-items: flex-start; gap: 10px;
  padding: 10px 14px; border-top: 1px solid var(--border);
}
.finding-row:first-child, .run-row:first-child { border-top: 0; }
.run-row { align-items: center; }
.finding-main, .run-main { flex: 1; min-width: 0; }
.finding-title, .run-title { font-size: 12.5px; font-weight: 600; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.finding-msg, .finding-path, .run-sub { margin-top: 3px; font-size: 11px; color: var(--fg-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.run-stats { color: var(--fg-3); font-size: 11px; flex-shrink: 0; }

.mono { font-family: var(--font-mono); }

.sv2-btn.active { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 35%, transparent); background: var(--gold-soft); }

@media (max-width: 820px) {
  .sv2-tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  /* Table is wider than a phone; the base `.identity-panel { overflow-x: auto }`
     already lets it scroll rather than crushing columns out of alignment. */
  .cell-sub { max-width: 160px; }
  .filter-search input { width: 140px; }
}
</style>
