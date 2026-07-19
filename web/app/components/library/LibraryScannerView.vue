<script setup lang="ts">
import { useDebounceFn, useIntersectionObserver } from '@vueuse/core'
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
  pipeline_failure_count?: number
  pipeline_error_message?: string
  started_at?: string
  finished_at?: string
  created_at?: string
}

type PipelineFailure = {
  id: number
  identity_key: string
  title: string
  status: string
  stage: string
  error_message: string
  updated_at?: string
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
  created_at?: string
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
  main_finding_code?: string
  main_finding_severity?: string
  main_finding_message?: string
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

type IssueCount = {
  code: string
  severity: string
  count: number
}

type ScannerOverview = {
  latest_run?: ScanRun
  bucket_counts: BucketCounts
  pipeline_failures: PipelineFailure[]
  issue_counts: IssueCount[]
  issue_total: number
}

// Buckets are computed server-side (identity.bucket) so the table and the
// review actions never disagree. An approved-but-not-yet-materialized identity
// reports as `unmatched` (no media_item_id) until a follow-up apply run — the
// UI flags that as "awaiting apply" rather than pretending it is matched.
type Bucket = 'matched' | 'needs_review' | 'unmatched' | 'rejected' | 'ignored'

// The review dataset is served in pages — a production music library carries
// five-figure identity counts and six-figure finding counts, so nothing here
// may ever fetch a whole collection. Identities append onto a windowed
// scroller; candidates and findings load per identity on expand.
const PAGE_SIZE = 100
const ISSUE_PAGE_SIZE = 50
const IDENTITY_FINDINGS_LIMIT = 50

const props = defineProps<{
  library: Library
}>()

const emit = defineEmits<{
  back: []
}>()

const { $heya } = useNuxtApp()

const error = ref('')
const overview = ref<ScannerOverview | null>(null)
const overviewLoading = ref(false)
const runs = ref<ScanRun[]>([])
const forceScanning = ref(false)

const identityItems = ref<ScanIdentity[]>([])
const listLoading = ref(false)
const listLoadingMore = ref(false)
const listHasMore = ref(false)
const listError = ref('')

const activeFilter = ref<'all' | Bucket>('all')
const search = ref('')
const appliedSearch = ref('')

const expanded = ref<Set<number>>(new Set())
const identityCandidates = ref<Record<number, ScanCandidate[]>>({})
const identityFindings = ref<Record<number, ScanFinding[]>>({})
const identityDetailLoading = ref<Set<number>>(new Set())
const detailOpen = ref<Set<number>>(new Set())
const candidateDetails = ref<Record<number, ScanCandidateDetail>>({})
const candidateDetailLoading = ref<number | null>(null)
const candidateDetailError = ref<Record<number, string>>({})

const issueItems = ref<ScanFinding[]>([])
const issueCode = ref('')
const issuesLoading = ref(false)
const issuesLoadingMore = ref(false)
const issuesHaveMore = ref(false)

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

const heya = $heya as any

// --- Fetchers -------------------------------------------------------------

async function fetchOverview(opts: { silent?: boolean } = {}) {
  if (!opts.silent) overviewLoading.value = true
  try {
    overview.value = await heya('/api/libraries/{id}/scanner/overview', {
      path: { id: props.library.id },
    }) as ScannerOverview
    error.value = ''
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || 'Failed to load scanner state.'
  } finally {
    overviewLoading.value = false
  }
}

async function fetchRuns() {
  try {
    runs.value = await heya('/api/libraries/{id}/scanner/runs', {
      path: { id: props.library.id },
      query: { limit: 10, offset: 0 },
    }) as ScanRun[] ?? []
  } catch {
    // Non-fatal: the run history panel shows its empty state.
  }
}

let identityFetchSeq = 0

async function fetchIdentityPage(reset: boolean) {
  const seq = ++identityFetchSeq
  if (reset) listLoading.value = true
  else listLoadingMore.value = true
  listError.value = ''
  try {
    const offset = reset ? 0 : identityItems.value.length
    const page = await heya('/api/libraries/{id}/scanner/identities', {
      path: { id: props.library.id },
      query: {
        limit: PAGE_SIZE,
        offset,
        ...(activeFilter.value !== 'all' ? { bucket: activeFilter.value } : {}),
        ...(appliedSearch.value ? { q: appliedSearch.value } : {}),
      },
    }) as ScanIdentity[] ?? []
    if (seq !== identityFetchSeq) return
    identityItems.value = reset ? page : [...identityItems.value, ...page]
    listHasMore.value = page.length === PAGE_SIZE
  } catch (e: any) {
    if (seq !== identityFetchSeq) return
    listError.value = e?.data?.error || e?.message || 'Failed to load identities.'
  } finally {
    if (seq === identityFetchSeq) {
      listLoading.value = false
      listLoadingMore.value = false
    }
  }
}

function resetIdentities() {
  expanded.value = new Set()
  return fetchIdentityPage(true)
}

function loadMoreIdentities() {
  if (listLoading.value || listLoadingMore.value || !listHasMore.value) return
  fetchIdentityPage(false)
}

let issueFetchSeq = 0

async function fetchIssuePage(reset: boolean) {
  const seq = ++issueFetchSeq
  if (reset) issuesLoading.value = true
  else issuesLoadingMore.value = true
  try {
    const offset = reset ? 0 : issueItems.value.length
    const page = await heya('/api/libraries/{id}/scanner/issues', {
      path: { id: props.library.id },
      query: {
        limit: ISSUE_PAGE_SIZE,
        offset,
        ...(issueCode.value ? { code: issueCode.value } : {}),
      },
    }) as ScanFinding[] ?? []
    if (seq !== issueFetchSeq) return
    issueItems.value = reset ? page : [...issueItems.value, ...page]
    issuesHaveMore.value = page.length === ISSUE_PAGE_SIZE
  } catch {
    if (seq === issueFetchSeq) issuesHaveMore.value = false
  } finally {
    if (seq === issueFetchSeq) {
      issuesLoading.value = false
      issuesLoadingMore.value = false
    }
  }
}

function loadMoreIssues() {
  if (issuesLoading.value || issuesLoadingMore.value || !issuesHaveMore.value) return
  fetchIssuePage(false)
}

async function loadIdentityDetail(identity: ScanIdentity) {
  if (identityDetailLoading.value.has(identity.id)) return
  identityDetailLoading.value = new Set(identityDetailLoading.value).add(identity.id)
  try {
    const [candidates, findings] = await Promise.all([
      heya('/api/libraries/{id}/scanner/identities/{identity_id}/candidates', {
        path: { id: props.library.id, identity_id: identity.id },
      }) as Promise<ScanCandidate[]>,
      identity.open_finding_count > 0
        ? heya('/api/libraries/{id}/scanner/identities/{identity_id}/findings', {
            path: { id: props.library.id, identity_id: identity.id },
            query: { limit: IDENTITY_FINDINGS_LIMIT, offset: 0 },
          }) as Promise<ScanFinding[]>
        : Promise.resolve([] as ScanFinding[]),
    ])
    identityCandidates.value = { ...identityCandidates.value, [identity.id]: candidates ?? [] }
    identityFindings.value = { ...identityFindings.value, [identity.id]: findings ?? [] }
  } catch {
    // Leave the caches unset; the expanded row shows its loading/empty state
    // and a re-expand retries.
  } finally {
    const next = new Set(identityDetailLoading.value)
    next.delete(identity.id)
    identityDetailLoading.value = next
  }
}

async function refreshAll(opts: { silent?: boolean } = {}) {
  identityCandidates.value = {}
  identityFindings.value = {}
  await Promise.all([
    fetchOverview(opts),
    fetchRuns(),
    resetIdentities(),
    fetchIssuePage(true),
  ])
}

watch(() => props.library.id, () => {
  error.value = ''
  overview.value = null
  runs.value = []
  identityItems.value = []
  issueItems.value = []
  issueCode.value = ''
  activeFilter.value = 'all'
  search.value = ''
  appliedSearch.value = ''
  expanded.value = new Set()
  refreshAll()
}, { immediate: true })

watch(activeFilter, () => resetIdentities())

const applySearch = useDebounceFn((value: string) => {
  const trimmed = value.trim()
  if (trimmed === appliedSearch.value) return
  appliedSearch.value = trimmed
  resetIdentities()
}, 350)
watch(search, value => applySearch(value))

watch(issueCode, () => fetchIssuePage(true))

// --- Derived state --------------------------------------------------------

const summary = computed(() => overview.value?.latest_run?.summary ?? {})
const pipelineFailures = computed(() => overview.value?.pipeline_failures ?? [])
const issueCounts = computed(() => overview.value?.issue_counts ?? [])
const issueTotal = computed(() => overview.value?.issue_total ?? 0)

const counts = computed<BucketCounts>(() =>
  overview.value?.bucket_counts
  ?? { total: 0, matched: 0, needs_review: 0, rejected: 0, unmatched: 0, ignored: 0 })

function bucketCount(key: 'all' | Bucket): number {
  return key === 'all' ? counts.value.total : counts.value[key]
}

// The identity list's expected total for the current filter — unknown while a
// text search is applied (the API has no filtered-count endpoint; the footer
// falls back to "N loaded").
const filteredTotal = computed<number | null>(() =>
  appliedSearch.value ? null : bucketCount(activeFilter.value))

// Approved but not yet materialized — has an accepted match but no media item
// until a follow-up apply/scan run attaches files and fetches metadata.
function awaitingApply(identity: ScanIdentity): boolean {
  return identity.review_status === 'accepted' && !identity.media_item_id
}

function canApproveSelectedCandidate(identity: ScanIdentity, candidate: ScanCandidate): boolean {
  return candidate.status === 'selected' && identity.bucket === 'needs_review'
}

function findingLabel(code: string): string {
  return FINDING_LABELS[code] ?? code
}

function issueChipLabel(issue: IssueCount): string {
  return findingLabel(issue.code)
}

// --- Review actions -------------------------------------------------------

const busyId = ref<number | null>(null)
const actionNote = ref('')
const actionError = ref('')
const bulkOpen = ref(false)
const bulkConfidence = ref(0.95)
const bulkBusy = ref(false)
const bulkEligible = ref<number | null>(null)
const bulkEligibleLoading = ref(false)

async function fetchBulkEligible() {
  bulkEligibleLoading.value = true
  try {
    const result = await heya('/api/libraries/{id}/scanner/bulk-eligible', {
      path: { id: props.library.id },
      query: { min_confidence: bulkConfidence.value },
    }) as { eligible: number }
    bulkEligible.value = result.eligible
  } catch {
    bulkEligible.value = null
  } finally {
    bulkEligibleLoading.value = false
  }
}

const fetchBulkEligibleDebounced = useDebounceFn(fetchBulkEligible, 250)
watch(bulkOpen, (open) => { if (open) fetchBulkEligible() })
watch(bulkConfidence, () => { if (bulkOpen.value) fetchBulkEligibleDebounced() })

// Replace the acted-on row with the server's fresh view of it — or drop it
// when its new bucket no longer matches the active filter. Counts move via a
// silent overview refresh; nothing refetches the whole list.
function patchIdentity(updated: ScanIdentity) {
  const idx = identityItems.value.findIndex(item => item.id === updated.id)
  if (idx === -1) return
  if (activeFilter.value !== 'all' && updated.bucket !== activeFilter.value) {
    identityItems.value.splice(idx, 1)
    const next = new Set(expanded.value)
    next.delete(updated.id)
    expanded.value = next
  } else {
    identityItems.value.splice(idx, 1, updated)
  }
}

async function reloadIdentityDetail(identity: ScanIdentity) {
  delete identityCandidates.value[identity.id]
  delete identityFindings.value[identity.id]
  if (expanded.value.has(identity.id)) await loadIdentityDetail(identity)
}

async function runAction(identity: ScanIdentity, action: string, body: Record<string, any> | undefined, describe: string) {
  busyId.value = identity.id
  actionNote.value = ''
  actionError.value = ''
  try {
    const updated = await heya(`/api/libraries/{id}/scanner/identities/{identity_id}/${action}`, {
      method: 'POST',
      path: { id: props.library.id, identity_id: identity.id },
      ...(body ? { body } : {}),
    }) as ScanIdentity
    patchIdentity(updated)
    actionNote.value = describe
    await Promise.all([fetchOverview({ silent: true }), reloadIdentityDetail(updated)])
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

async function bulkApproveSingleCandidates() {
  if (bulkBusy.value) return
  bulkBusy.value = true
  actionNote.value = ''
  actionError.value = ''
  try {
    const result = await heya('/api/libraries/{id}/scanner/bulk-approve-single', {
      method: 'POST',
      path: { id: props.library.id },
      body: { min_confidence: bulkConfidence.value },
    }) as { approved: number }
    // Bulk flips arbitrarily many rows; a targeted patch can't cover it.
    await Promise.all([fetchOverview({ silent: true }), resetIdentities()])
    actionNote.value = `Approved ${result.approved} single-candidate match${result.approved === 1 ? '' : 'es'} at ${score(bulkConfidence.value)} confidence or higher — apply queued.`
    bulkOpen.value = false
  } catch (e: any) {
    actionError.value = e?.data?.error || e?.message || 'Bulk approval failed.'
  } finally {
    bulkBusy.value = false
  }
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

async function forceRescan() {
  if (forceScanning.value) return
  forceScanning.value = true
  actionNote.value = ''
  actionError.value = ''
  try {
    await heya('/api/libraries/{id}/scan', {
      method: 'POST',
      path: { id: props.library.id },
      query: { force: true },
    })
    actionNote.value = `Forced rescan queued for ${props.library.name}. Existing identities will be re-evaluated.`
  } catch (e: any) {
    actionError.value = e?.data?.error || e?.message || 'Failed to queue forced rescan.'
  } finally {
    forceScanning.value = false
  }
}

// Manual "fix match": live provider search + assign an arbitrary result the
// automated search never surfaced. The dialog posts to .../assign, which
// rides the same approve flow as accepting a scanner-found candidate.
const searchDialogIdentity = ref<ScanIdentity | null>(null)

async function onSearchAssigned(title: string) {
  const identity = searchDialogIdentity.value
  searchDialogIdentity.value = null
  actionError.value = ''
  actionNote.value = `Matched ${identity?.title || identity?.identity_key || 'identity'} as “${title}” — awaiting apply.`
  if (!identity) return
  try {
    const updated = await heya('/api/libraries/{id}/scanner/identities/{identity_id}', {
      path: { id: props.library.id, identity_id: identity.id },
    }) as ScanIdentity
    patchIdentity(updated)
    await Promise.all([fetchOverview({ silent: true }), reloadIdentityDetail(updated)])
  } catch {
    // The assign itself succeeded; worst case the row shows stale state until
    // the next refresh.
  }
}

// --- Expand / candidate detail --------------------------------------------

function toggleExpand(identity: ScanIdentity) {
  const next = new Set(expanded.value)
  if (next.has(identity.id)) {
    next.delete(identity.id)
  } else {
    next.add(identity.id)
    if (!identityCandidates.value[identity.id]) loadIdentityDetail(identity)
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

// --- Infinite scroll ------------------------------------------------------

const listSentinel = ref<HTMLElement | null>(null)
useIntersectionObserver(listSentinel, (entries) => {
  if (entries.some(entry => entry.isIntersecting)) loadMoreIdentities()
}, { rootMargin: '400px' })

// --- Formatting -----------------------------------------------------------

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

function formatCount(value: number): string {
  return value.toLocaleString()
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

function candidateDetailProviderURL(detail: ScanCandidateDetail): string {
  return externalProviderUrl(detail.provider_kind, detail.external_ids)
}

function runFiles(run: ScanRun): number {
  const v = run.summary?.files
  return typeof v === 'number' ? v : 0
}

function latestRunStatus(): string {
  const failures = pipelineFailures.value.length
  if (failures > 0) return `${failures} pipeline failure${failures === 1 ? '' : 's'}`
  return overview.value?.latest_run?.status ?? 'not run'
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
            <template v-if="overviewLoading && !overview">loading…</template>
            <template v-else-if="overview?.latest_run">
              last run {{ latestRunStatus() }} · {{ formatDate(overview.latest_run.finished_at || overview.latest_run.started_at) }}
            </template>
            <template v-else>no persisted run yet</template>
          </p>
        </div>
      </div>
      <div class="head-actions">
        <button class="sv2-btn ghost" :disabled="forceScanning" @click="forceRescan">
          <Icon :name="forceScanning ? 'spinner' : 'refresh'" :size="12" />
          {{ forceScanning ? 'Queuing…' : 'Force rescan' }}
        </button>
        <button class="sv2-btn ghost" :disabled="overviewLoading || listLoading" @click="refreshAll()">
          <Icon :name="overviewLoading || listLoading ? 'spinner' : 'refresh'" :size="12" />
          Refresh
        </button>
      </div>
    </header>

    <div v-if="error" class="sv2-note error">
      <Icon name="warning" :size="13" /> {{ error }}
    </div>

    <div v-else class="sv2-body">
      <div v-if="actionError" class="sv2-note error">
        <Icon name="warning" :size="13" /> {{ actionError }}
      </div>
      <div v-else-if="actionNote" class="sv2-note ok">
        <Icon name="check" :size="13" /> {{ actionNote }}
      </div>

      <div v-if="pipelineFailures.length" class="sv2-note error pipeline-summary">
        <Icon name="warning" :size="13" />
        <span>
          <strong>{{ pipelineFailures[0]!.stage }} failed for {{ pipelineFailures[0]!.title || pipelineFailures[0]!.identity_key }}:</strong>
          {{ pipelineFailures[0]!.error_message }}
          <template v-if="pipelineFailures.length > 1"> · {{ pipelineFailures.length - 1 }} more below</template>
        </span>
      </div>

      <div v-if="!overview && overviewLoading" class="sv2-tiles">
        <div v-for="n in 7" :key="n" class="tile-skeleton" />
      </div>
      <div v-else class="sv2-tiles">
        <MetricTile label="Files" :value="summaryNumber('files', 'classified_files')" icon="folder" />
        <MetricTile label="Identities" :value="bucketCount('all')" icon="list" />
        <MetricTile label="Matched" :value="bucketCount('matched')" icon="check" tone="good" />
        <MetricTile label="Needs review" :value="bucketCount('needs_review')" icon="pencil" :tone="bucketCount('needs_review') ? 'warn' : 'neutral'" />
        <MetricTile label="Unmatched" :value="bucketCount('unmatched')" icon="info" :tone="bucketCount('unmatched') ? 'warn' : 'neutral'" />
        <MetricTile label="Rejected" :value="bucketCount('rejected')" icon="close" :tone="bucketCount('rejected') ? 'bad' : 'neutral'" />
        <MetricTile label="Ignored" :value="bucketCount('ignored')" icon="eye" tone="neutral" />
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
            <span class="chip-count">{{ formatCount(bucketCount(f.key)) }}</span>
          </button>
        </div>
        <div class="filter-search">
          <Icon :name="listLoading && appliedSearch ? 'spinner' : 'search'" :size="13" />
          <input v-model="search" placeholder="Filter by title or key…" />
        </div>
        <div class="bulk-accept">
          <button class="sv2-btn ghost" :class="{ active: bulkOpen }" @click="bulkOpen = !bulkOpen">
            <Icon name="check" :size="12" /> Accept confident singles
          </button>
          <div v-if="bulkOpen" class="bulk-popover">
            <div class="bulk-title">Accept one-candidate matches</div>
            <p>Only needs-review identities with exactly one candidate at or above this confidence are changed.</p>
            <label>
              <span>Minimum confidence</span>
              <b class="mono">{{ score(bulkConfidence) }}</b>
            </label>
            <input v-model.number="bulkConfidence" type="range" min="0" max="1" step="0.01">
            <button class="mini-btn accept" :disabled="bulkBusy || bulkEligibleLoading || !bulkEligible" @click="bulkApproveSingleCandidates">
              <Icon :name="bulkBusy || bulkEligibleLoading ? 'spinner' : 'check'" :size="11" />
              {{ bulkBusy ? 'Applying…'
                : bulkEligibleLoading ? 'Counting…'
                : bulkEligible ? `Apply all ${formatCount(bulkEligible)}` : 'Nothing eligible' }}
            </button>
          </div>
        </div>
      </div>

      <section class="identity-panel">
        <div v-if="listError" class="panel-empty">
          {{ listError }}
          <button class="mini-btn retry-btn" @click="fetchIdentityPage(true)">Retry</button>
        </div>
        <div v-else-if="listLoading" class="row-skeletons">
          <div v-for="n in 8" :key="n" class="row-skeleton" />
        </div>
        <div v-else-if="identityItems.length === 0" class="panel-empty">
          {{ activeFilter !== 'all' || appliedSearch
            ? 'No identities match this filter.'
            : 'No persisted scanner identities for this library.' }}
        </div>
        <template v-else>
          <DynamicScroller
            class="identity-scroller"
            :items="identityItems"
            :min-item-size="61"
            key-field="id"
            page-mode
          >
            <template #default="{ item: identity, active }">
              <DynamicScrollerItem
                :item="identity"
                :active="active"
                :size-dependencies="[
                  expanded.has(identity.id),
                  identityCandidates[identity.id]?.length,
                  identityFindings[identity.id]?.length,
                  detailOpen.size,
                  candidateDetailLoading,
                ]"
              >
                <table class="idt">
                  <tbody>
                <tr class="idt-row" :class="{ open: expanded.has(identity.id) }" @click="toggleExpand(identity)">
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
                    <span v-else-if="identity.main_finding_code" class="flag issue">
                      {{ findingLabel(identity.main_finding_code) }}
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
                      <div v-if="identity.main_finding_code" class="detail-issue">
                        <Icon name="warning" :size="12" />
                        <span><b>{{ findingLabel(identity.main_finding_code) }}:</b> {{ identity.main_finding_message }}</span>
                      </div>

                      <template v-if="(identityFindings[identity.id]?.length ?? 0) > 1">
                        <div class="detail-head">
                          <span>Open findings</span>
                          <span v-if="identity.open_finding_count > (identityFindings[identity.id]?.length ?? 0)" class="dim">
                            showing {{ identityFindings[identity.id]!.length }} of {{ formatCount(identity.open_finding_count) }}
                          </span>
                        </div>
                        <div class="identity-finding-list">
                          <div v-for="finding in identityFindings[identity.id]" :key="finding.id" class="identity-finding-row">
                            <StatusBadge :state="finding.severity === 'error' ? 'error' : finding.severity === 'warn' ? 'warn' : 'idle'">
                              {{ findingLabel(finding.code) }}
                            </StatusBadge>
                            <span class="identity-finding-msg">{{ finding.rel_path ? `${finding.rel_path} — ` : '' }}{{ finding.message }}</span>
                          </div>
                        </div>
                      </template>

                      <div class="detail-head">
                        <span>Candidates</span>
                        <span v-if="identityDetailLoading.has(identity.id)" class="dim">loading…</span>
                      </div>

                      <div v-if="identityDetailLoading.has(identity.id) && !identityCandidates[identity.id]" class="detail-empty">
                        Loading candidate rows…
                      </div>
                      <div v-else-if="(identityCandidates[identity.id] ?? []).length === 0" class="detail-empty">
                        No provider candidates recorded for this identity.
                      </div>
                      <div v-else class="candidate-list">
                        <div
                          v-for="candidate in identityCandidates[identity.id]"
                          :key="candidate.id"
                          class="candidate-row"
                        >
                          <div class="candidate-rank mono">{{ candidate.rank }}</div>
                          <LoadingImage
                            v-if="candidate.poster_url"
                            class="candidate-poster"
                            :src="candidate.poster_url"
                            :persistent="candidate.poster_url.includes('/api/v2/images/')"
                            alt=""
                            loading="lazy"
                          />
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
                              <LoadingImage
                                v-if="candidateDetails[candidate.id]!.poster_url"
                                class="candidate-detail-poster"
                                :src="candidateDetails[candidate.id]!.poster_url"
                                :persistent="candidateDetails[candidate.id]!.poster_url?.includes('/api/v2/images/') ?? false"
                                alt=""
                                loading="lazy"
                              />
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
                                <div v-if="candidateDetailProviderURL(candidateDetails[candidate.id]!)" class="candidate-detail-actions">
                                  <a
                                    class="mini-btn link"
                                    :href="candidateDetailProviderURL(candidateDetails[candidate.id]!)"
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    @click.stop
                                  >
                                    <Icon name="link" :size="11" /> Open provider
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
                  </tbody>
                </table>
              </DynamicScrollerItem>
            </template>
          </DynamicScroller>
          <div class="list-foot">
            <span class="list-progress mono">
              {{ formatCount(identityItems.length) }}<template v-if="filteredTotal != null && filteredTotal > identityItems.length"> of {{ formatCount(filteredTotal) }}</template>
              identit{{ identityItems.length === 1 ? 'y' : 'ies' }} loaded
            </span>
            <div ref="listSentinel" class="list-sentinel" aria-hidden="true" />
            <button v-if="listHasMore" class="mini-btn" :disabled="listLoadingMore" @click="loadMoreIdentities">
              <Icon :name="listLoadingMore ? 'spinner' : 'chevdown'" :size="11" />
              {{ listLoadingMore ? 'Loading…' : 'Load more' }}
            </button>
          </div>
        </template>
      </section>

      <!-- Only findings NOT tied to an identity live here — per-identity issues
           are surfaced inline in the table above. The list is paginated and the
           chips come from the overview's aggregate counts: a music library can
           carry six-figure issue counts, which must never render as one list. -->
      <section v-if="issueTotal > 0" class="findings-panel">
        <div class="panel-head">
          <h4>Scan issues</h4>
          <span>{{ formatCount(issueTotal) }} not tied to an identity</span>
        </div>
        <div v-if="issueCounts.length > 1" class="issue-chips">
          <button
            class="filter-chip"
            :class="{ active: issueCode === '' }"
            @click="issueCode = ''"
          >
            All <span class="chip-count">{{ formatCount(issueTotal) }}</span>
          </button>
          <button
            v-for="issue in issueCounts"
            :key="issue.code"
            class="filter-chip"
            :class="{ active: issueCode === issue.code }"
            @click="issueCode = issueCode === issue.code ? '' : issue.code"
          >
            {{ issueChipLabel(issue) }}
            <span class="chip-count">{{ formatCount(issue.count) }}</span>
          </button>
        </div>
        <div v-if="issuesLoading" class="row-skeletons">
          <div v-for="n in 4" :key="n" class="row-skeleton" />
        </div>
        <div v-else-if="issueItems.length === 0" class="panel-empty">No issues match this filter.</div>
        <template v-else>
          <div class="finding-list">
            <div v-for="finding in issueItems" :key="finding.id" class="finding-row">
              <StatusBadge :state="finding.severity === 'error' ? 'error' : finding.severity === 'warn' ? 'warn' : 'idle'">
                {{ findingLabel(finding.code) }}
              </StatusBadge>
              <div class="finding-main">
                <div class="finding-title">{{ finding.rel_path || finding.message }}</div>
                <div v-if="finding.rel_path" class="finding-msg">{{ finding.message }}</div>
              </div>
            </div>
          </div>
          <div v-if="issuesHaveMore" class="list-foot">
            <span class="list-progress mono">{{ formatCount(issueItems.length) }} loaded</span>
            <button class="mini-btn" :disabled="issuesLoadingMore" @click="loadMoreIssues">
              <Icon :name="issuesLoadingMore ? 'spinner' : 'chevdown'" :size="11" />
              {{ issuesLoadingMore ? 'Loading…' : 'Load more' }}
            </button>
          </div>
        </template>
      </section>

      <section v-if="pipelineFailures.length" class="findings-panel pipeline-failures">
        <div class="panel-head">
          <h4>Pipeline failures</h4>
          <span>{{ pipelineFailures.length }} require attention</span>
        </div>
        <div class="finding-list">
          <div v-for="failure in pipelineFailures" :key="failure.id" class="finding-row">
            <StatusBadge state="error">{{ failure.stage }}</StatusBadge>
            <div class="finding-main">
              <div class="finding-title">{{ failure.title || failure.identity_key }}</div>
              <div class="finding-msg failure-message">{{ failure.error_message }}</div>
              <div class="finding-path mono">{{ failure.identity_key }} · {{ formatDate(failure.updated_at) }}</div>
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

.sv2-tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 8px;
}

.tile-skeleton {
  height: 74px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: linear-gradient(100deg, var(--bg-2) 40%, rgb(var(--ink) / 0.05) 50%, var(--bg-2) 60%);
  background-size: 200% 100%;
  animation: sv2-shimmer 1.4s ease-in-out infinite;
}

.row-skeletons { display: flex; flex-direction: column; }
.row-skeleton {
  height: 52px;
  border-top: 1px solid var(--border);
  background: linear-gradient(100deg, transparent 40%, rgb(var(--ink) / 0.04) 50%, transparent 60%);
  background-size: 200% 100%;
  animation: sv2-shimmer 1.4s ease-in-out infinite;
}
.row-skeleton:first-child { border-top: 0; }

@keyframes sv2-shimmer {
  from { background-position: 200% 0; }
  to { background-position: -200% 0; }
}
@media (prefers-reduced-motion: reduce) {
  .tile-skeleton, .row-skeleton { animation: none; }
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
.pipeline-summary { align-items: flex-start; line-height: 1.45; }
.pipeline-summary span { min-width: 0; overflow-wrap: anywhere; }
.pipeline-summary strong { color: var(--bad); }

.filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.bulk-accept { position: relative; margin-left: auto; }
.bulk-popover {
  position: absolute; z-index: 20; top: calc(100% + 7px); right: 0;
  width: min(320px, calc(100vw - 32px)); padding: 13px;
  border: 1px solid var(--border-strong); border-radius: var(--r-md);
  background: var(--bg-1); box-shadow: 0 12px 36px rgb(0 0 0 / 0.28);
}
.bulk-title { color: var(--fg-0); font-size: 12.5px; font-weight: 650; }
.bulk-popover p { margin: 5px 0 12px; color: var(--fg-3); font-size: 11.5px; line-height: 1.4; }
.bulk-popover label { display: flex; justify-content: space-between; color: var(--fg-2); font-size: 11.5px; }
.bulk-popover input[type="range"] { width: 100%; margin: 8px 0 12px; accent-color: var(--gold); }
.bulk-popover .mini-btn { width: 100%; justify-content: center; }
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
.retry-btn { margin-left: 10px; }

.issue-chips {
  display: flex; gap: 6px; flex-wrap: wrap;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}

.list-foot {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  padding: 9px 14px;
  border-top: 1px solid var(--border);
}
.list-progress { font-size: 10.5px; color: var(--fg-3); }
.list-sentinel { flex: 1; height: 1px; }

/* Aligned data table. Reka has no data-table primitive and our KVTable is
   key/value only, so this is a plain semantic <table>. A width:100% spacer
   column soaks up slack, so every other column hugs its content and the
   columns line up across all rows (status, local, match, flags). */
.idt { width: 100%; table-layout: fixed; border-collapse: collapse; }
.identity-scroller :deep(.vue-recycle-scroller__item-wrapper) { overflow: visible; }
.identity-scroller :deep(.vue-recycle-scroller__item-view:not(:first-child)) .idt { border-top: 1px solid var(--border); }
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
.flag.issue {
  background: color-mix(in srgb, var(--gold) 18%, transparent);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  color: var(--fg-0);
  font-weight: 600;
}
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

.identity-finding-list {
  display: flex; flex-direction: column; gap: 5px;
}
.identity-finding-row {
  display: flex; align-items: baseline; gap: 8px;
  font-size: 11.5px; color: var(--fg-2);
  min-width: 0;
}
.identity-finding-msg { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

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
.failure-message { color: var(--bad); white-space: normal; overflow-wrap: anywhere; }
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
