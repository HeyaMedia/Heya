<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { useQuery, defineQueryOptions } from '@pinia/colada'
import type { ManagerQueueView, ManagerQueueItemView } from '~~/shared/api/types.gen'

const { $heya } = useNuxtApp()

const queueQuery = defineQueryOptions(() => ({
  key: ['manager', 'queue'],
  query: async () => {
    const { $heya: heya } = useNuxtApp()
    return await heya('/api/manager/queue') as ManagerQueueView
  },
  staleTime: 1000 * 10,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

const { data, asyncStatus, error, refetch } = useQuery(queueQuery)

// The queue is live — poll while the page is visible.
const poll = setInterval(() => {
  if (document.visibilityState === 'visible') refetch()
}, 15000)
onUnmounted(() => clearInterval(poll))

// Multi-part grabs (an album fetched as N single-track NZBs, a repeated
// grab) share one release name — collapse them into ONE canonical row; the
// modal lists every underlying entry.
type QueueGroup = {
  key: string
  name: string
  items: ManagerQueueItemView[]
  first: ManagerQueueItemView
  totalMB: number
  doneMB: number
  failures: number
}
function groupItems(items: ManagerQueueItemView[]): QueueGroup[] {
  const byName = new Map<string, ManagerQueueItemView[]>()
  for (const item of items) {
    const key = item.name.toLowerCase()
    if (!byName.has(key)) byName.set(key, [])
    byName.get(key)!.push(item)
  }
  return [...byName.entries()].map(([key, groupItems]) => ({
    key,
    name: groupItems[0]!.name,
    items: groupItems,
    first: groupItems[0]!,
    totalMB: groupItems.reduce((n, i) => n + i.size_mb, 0),
    doneMB: groupItems.reduce((n, i) => n + (i.size_mb - (i.size_left_mb ?? 0)), 0),
    failures: groupItems.filter(i => i.fail_message).length,
  }))
}

const active = computed(() => groupItems((data.value?.items ?? []).filter(i => !i.history)))
const finished = computed(() => groupItems((data.value?.items ?? []).filter(i => i.history)))

const VERDICT_META: Record<string, { label: string, state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  would_accept: { label: 'Heya agrees', state: 'ok' },
  would_reject: { label: 'Heya would reject', state: 'error' },
  unknown_identity: { label: 'unknown to Heya', state: 'idle' },
  unmonitored: { label: 'not monitored', state: 'idle' },
  no_profile: { label: 'no profile', state: 'warn' },
}

const STATE_META: Record<string, { label: string, tone: 'good' | 'warn' | 'bad' }> = {
  missing: { label: 'not in library', tone: 'bad' },
  partial: { label: 'partially on disk', tone: 'warn' },
  have: { label: 'on disk', tone: 'good' },
}

// The one-line answer the modal leads with: what would happen and why.
function headline(item: ManagerQueueItemView): string {
  switch (item.verdict) {
    case 'would_accept':
      if (item.library_state === 'missing') return 'Would import — fills a gap in the library.'
      if (item.library_state === 'partial') return 'Would import — covers units the library is missing.'
      return 'Would import — an upgrade over the current file.'
    case 'would_reject':
      if (item.library_state === 'have') return 'Would reject — the library already has this covered.'
      return 'Would reject.'
    case 'unknown_identity':
      return 'Not recognized — the parsed title matches nothing in the library.'
    case 'unmonitored':
      return 'Recognized, but the matched item is not monitored.'
    case 'no_profile':
      return 'Recognized, but the matched item has no quality profile — nothing to evaluate against.'
    default:
      return ''
  }
}

function pct(item: ManagerQueueItemView): number {
  return Math.min(100, Math.max(0, item.percentage))
}

function fmtSize(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${Math.round(mb)} MB`
}

// ── Detail modal ─────────────────────────────────────────────────────────

const modalOpen = ref(false)
const modalGroup = ref<QueueGroup | null>(null)
function openGroup(group: QueueGroup) {
  modalGroup.value = group
  modalOpen.value = true
}
// Keep the open modal in sync with poll refreshes (deletes, progress).
watch([active, finished], () => {
  const current = modalGroup.value
  if (!current || !modalOpen.value) return
  const next = [...active.value, ...finished.value].find(g => g.key === current.key)
  if (next) modalGroup.value = next
  else modalOpen.value = false
})

// Per-entry file listing (what the finished download actually produced).
type QueueFiles = { path: string, files: { name: string, size_bytes: number }[], error?: string }
const entryFiles = ref(new Map<string, QueueFiles | 'loading' | 'error'>())
async function toggleFiles(item: ManagerQueueItemView) {
  const key = item.nzo_id
  if (entryFiles.value.has(key)) {
    const next = new Map(entryFiles.value)
    next.delete(key)
    entryFiles.value = next
    return
  }
  entryFiles.value = new Map(entryFiles.value).set(key, 'loading')
  try {
    const view = await $heya(`/api/manager/queue/${item.client_id}/${encodeURIComponent(item.nzo_id)}/files`) as QueueFiles
    entryFiles.value = new Map(entryFiles.value).set(key, view)
  } catch {
    entryFiles.value = new Map(entryFiles.value).set(key, 'error')
  }
}

const { confirm } = useConfirm()

// Import: the first real write action — move media files into the matched
// item's folder and hand off to the library scanner.
type ImportResult = { run_id: number, matched_title: string, destination: string, moved: string[], skipped?: string[], scan_queued: boolean }
const importing = ref<string | null>(null)
const importFlash = ref('')
const importError = ref('')
async function importEntry(item: ManagerQueueItemView) {
  if (!await confirm({
    title: 'Import this download?',
    message: `Media files move into ${item.matched_title}'s library folder and a scan runs. This is a real write.`,
  })) return
  importing.value = item.nzo_id
  importFlash.value = ''
  importError.value = ''
  try {
    const res = await $heya(`/api/manager/queue/${item.client_id}/${encodeURIComponent(item.nzo_id)}/import`, {
      method: 'POST',
    }) as ImportResult
    importFlash.value = `Imported ${res.moved.length} file(s) → ${res.destination}${res.scan_queued ? ' · scan queued' : ''} · run #${res.run_id}`
    refetch()
  } catch (e: any) {
    importError.value = e?.data?.detail ?? 'Import failed'
  } finally {
    importing.value = null
  }
}

const deleting = ref<string | null>(null)
async function deleteEntry(item: ManagerQueueItemView) {
  const message = item.history
    ? 'Remove this history record from the download client? Files on disk stay.'
    : 'Cancel this download and delete its partial files?'
  if (!await confirm({ title: 'Remove from client?', message, destructive: true })) return
  deleting.value = item.nzo_id
  try {
    await $heya(`/api/manager/queue/${item.client_id}/${encodeURIComponent(item.nzo_id)}`, {
      method: 'DELETE', query: { history: item.history },
    })
    refetch()
  } finally {
    deleting.value = null
  }
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Queue"
      icon="cloud-download"
      eyebrow="Manager · Dry run"
      description="Everything the download clients are working on — no matter who queued it. Heya recognizes what it can, says whether it fills a gap or upgrades something, and lets you manage the entries."
    />

    <div v-if="error" class="q-empty">
      <Icon name="warning" :size="14" /> Couldn't reach the download clients.
      <button type="button" class="mgr-btn" @click="refetch()">Retry</button>
    </div>
    <div v-else-if="asyncStatus === 'loading' && !data" class="q-empty">
      <span class="mgr-spin" /> Loading queues…
    </div>

    <template v-else-if="data">
      <div v-for="err in data.errors ?? []" :key="err" class="mgr-flash err">{{ err }}</div>

      <div class="q-table" role="table" aria-label="Active downloads">
        <div class="q-head">
          <span>Release</span>
          <span>Heya verdict</span>
          <span class="q-col-progress">Progress</span>
          <span>Status</span>
        </div>
        <button v-for="group in active" :key="group.key" type="button" class="q-row" @click="openGroup(group)">
          <div class="q-release">
            <div class="q-title mono">
              {{ group.name }}
              <span v-if="group.items.length > 1" class="q-count mono">×{{ group.items.length }}</span>
            </div>
            <div class="q-facts">
              <span v-if="group.first.quality" class="mgr-quality">{{ group.first.quality }}</span>
              <span v-if="group.first.library_state" class="q-state" :class="`tone-${STATE_META[group.first.library_state]?.tone}`">{{ STATE_META[group.first.library_state]?.label }}</span>
              <span v-if="group.first.score" class="q-score mono">score <b>{{ group.first.score }}</b></span>
            </div>
            <div class="q-sub">
              <template v-if="group.first.matched_title">
                <NuxtLink v-if="group.first.matched_item_id" :to="`/manager/library/${group.first.matched_library}/${group.first.matched_item_id}`" class="q-matchlink" @click.stop>{{ group.first.matched_title }}</NuxtLink>
                <template v-else>{{ group.first.matched_title }}</template> ·
              </template>
              {{ group.first.client }} · {{ group.first.category || 'no category' }}
            </div>
          </div>
          <div class="q-verdict">
            <StatusBadge :state="VERDICT_META[group.first.verdict]?.state ?? 'idle'">{{ VERDICT_META[group.first.verdict]?.label ?? group.first.verdict }}</StatusBadge>
          </div>
          <div class="q-progress">
            <div class="q-bar"><div class="q-bar-fill" :style="{ width: `${group.totalMB > 0 ? Math.round((group.doneMB / group.totalMB) * 100) : 0}%` }" /></div>
            <span class="q-progress-text mono">{{ fmtSize(group.doneMB) }} / {{ fmtSize(group.totalMB) }}<template v-if="group.first.time_left"> · {{ group.first.time_left }}</template></span>
          </div>
          <StatusBadge :state="group.first.status.toLowerCase() === 'downloading' ? 'ok' : 'idle'">{{ group.first.status.toLowerCase() }}</StatusBadge>
        </button>
      </div>
      <div v-if="active.length === 0" class="q-empty">
        <Icon name="info" :size="14" /> Nothing downloading right now.
      </div>

      <template v-if="finished.length">
        <h2 class="q-section">Recently completed</h2>
        <div class="q-table">
          <button v-for="group in finished" :key="group.key" type="button" class="q-row done" @click="openGroup(group)">
            <div class="q-release">
              <div class="q-title mono">
                {{ group.name }}
                <span v-if="group.items.length > 1" class="q-count mono">×{{ group.items.length }}</span>
              </div>
              <div class="q-facts">
                <span v-if="group.first.quality" class="mgr-quality">{{ group.first.quality }}</span>
                <span v-if="group.first.library_state" class="q-state" :class="`tone-${STATE_META[group.first.library_state]?.tone}`">{{ STATE_META[group.first.library_state]?.label }}</span>
                <span v-if="group.first.score" class="q-score mono">score <b>{{ group.first.score }}</b></span>
              </div>
              <div class="q-sub">
                <template v-if="group.first.matched_title">
                  <NuxtLink v-if="group.first.matched_item_id" :to="`/manager/library/${group.first.matched_library}/${group.first.matched_item_id}`" class="q-matchlink" @click.stop>{{ group.first.matched_title }}</NuxtLink>
                  <template v-else>{{ group.first.matched_title }}</template> ·
                </template>
                {{ group.first.client }} · {{ fmtSize(group.totalMB) }}
                <span v-if="group.failures" class="q-fail">· {{ group.failures }} failed</span>
              </div>
            </div>
            <div class="q-verdict">
              <StatusBadge :state="VERDICT_META[group.first.verdict]?.state ?? 'idle'">{{ VERDICT_META[group.first.verdict]?.label ?? group.first.verdict }}</StatusBadge>
            </div>
            <span class="q-when mono">{{ group.first.completed_at ? new Date(group.first.completed_at * 1000).toLocaleString() : '' }}</span>
            <StatusBadge :state="group.failures ? 'error' : group.first.status.toLowerCase() === 'completed' ? 'ok' : 'idle'">{{ group.failures ? 'failures' : group.first.status.toLowerCase() }}</StatusBadge>
          </button>
        </div>
      </template>
    </template>

    <!-- ── Group detail: the auto-tagger view + entry management ──────── -->
    <AppDialog v-model="modalOpen" :title="modalGroup?.items.length === 1 ? 'Queue entry' : `Queue group · ${modalGroup?.items.length} entries`" size="lg">
      <template v-if="modalGroup">
        <div class="qm-name mono">{{ modalGroup.name }}</div>

        <div class="qm-tagger">
          <div class="qm-tagger-head mono">Auto-tagger</div>
          <div class="qm-headline">
            <StatusBadge :state="VERDICT_META[modalGroup.first.verdict]?.state ?? 'idle'">{{ VERDICT_META[modalGroup.first.verdict]?.label ?? modalGroup.first.verdict }}</StatusBadge>
            <span>{{ headline(modalGroup.first) }}</span>
          </div>
          <div class="qm-rows">
            <div class="qm-row">
              <span class="qm-label mono">Parsed as</span>
              <span class="qm-value mono">{{ modalGroup.first.parsed || '—' }}</span>
            </div>
            <div class="qm-row">
              <span class="qm-label mono">Matched</span>
              <span class="qm-value">
                <NuxtLink v-if="modalGroup.first.matched_item_id" :to="`/manager/library/${modalGroup.first.matched_library}/${modalGroup.first.matched_item_id}`" class="q-matchlink">{{ modalGroup.first.matched_title }}</NuxtLink>
                <template v-else>not recognized in the library</template>
                <span v-if="modalGroup.first.library_state" class="q-state" :class="`tone-${STATE_META[modalGroup.first.library_state]?.tone}`">{{ STATE_META[modalGroup.first.library_state]?.label }}</span>
              </span>
            </div>
            <div v-if="modalGroup.first.quality || modalGroup.first.score" class="qm-row">
              <span class="qm-label mono">Evaluation</span>
              <span class="qm-value qm-eval">
                <span v-if="modalGroup.first.quality" class="mgr-quality">{{ modalGroup.first.quality }}</span>
                <span v-if="modalGroup.first.score" class="q-score mono">score <b>{{ modalGroup.first.score }}</b></span>
                <template v-for="hit in modalGroup.first.format_breakdown ?? []" :key="hit.name">
                  <span class="mgr-cf" :class="{ pos: (hit.score ?? 0) > 0, neg: (hit.score ?? 0) < 0 }">
                    {{ hit.name }}<template v-if="hit.score"> {{ hit.score > 0 ? '+' : '' }}{{ hit.score }}</template>
                  </span>
                </template>
              </span>
            </div>
            <div v-if="modalGroup.first.rejections?.length" class="qm-row">
              <span class="qm-label mono">Rejections</span>
              <span class="qm-value qm-rejections">
                <span v-for="rej in modalGroup.first.rejections" :key="rej.code + rej.message" class="qm-rej">
                  <span class="qm-rej-code mono">{{ rej.code }}</span> {{ rej.message }}
                </span>
              </span>
            </div>
          </div>
        </div>

        <div class="qm-entries">
          <template v-for="item in modalGroup.items" :key="item.nzo_id">
            <div class="qm-entry">
              <div class="qm-entry-body">
                <div class="qm-entry-line mono">
                  {{ item.client }} · {{ item.category || 'no category' }} · {{ fmtSize(item.size_mb) }}
                  <template v-if="item.completed_at"> · {{ new Date(item.completed_at * 1000).toLocaleString() }}</template>
                  <template v-if="!item.history"> · {{ pct(item) }}%<template v-if="item.time_left"> · {{ item.time_left }}</template></template>
                </div>
                <div v-if="item.fail_message" class="qm-entry-fail">{{ item.fail_message }}</div>
              </div>
              <StatusBadge :state="item.fail_message ? 'error' : item.status.toLowerCase() === 'completed' || item.status.toLowerCase() === 'downloading' ? 'ok' : 'idle'">{{ item.status.toLowerCase() }}</StatusBadge>
              <AppTooltip v-if="item.history && !item.fail_message" :label="item.matched_item_id ? `Import into ${item.matched_title}` : 'Not recognized — nothing to import into'">
                <button
                  type="button" class="mgr-btn-icon qm-import"
                  :disabled="!item.matched_item_id || importing !== null"
                  @click="importEntry(item)"
                >
                  <span v-if="importing === item.nzo_id" class="mgr-spin" />
                  <Icon v-else name="download" :size="14" />
                </button>
              </AppTooltip>
              <AppTooltip v-if="item.history" label="Show the files this download produced">
                <button type="button" class="mgr-btn-icon" :class="{ active: entryFiles.has(item.nzo_id) }" @click="toggleFiles(item)">
                  <Icon name="folder" :size="14" />
                </button>
              </AppTooltip>
              <AppTooltip :label="item.history ? 'Remove history record from the client' : 'Cancel download and delete partial files'">
                <button type="button" class="mgr-btn-icon danger" :disabled="deleting !== null" @click="deleteEntry(item)">
                  <span v-if="deleting === item.nzo_id" class="mgr-spin" />
                  <Icon v-else name="trash" :size="14" />
                </button>
              </AppTooltip>
            </div>
            <div v-if="entryFiles.has(item.nzo_id)" class="qm-files">
              <div v-if="entryFiles.get(item.nzo_id) === 'loading'" class="qm-files-loading"><span class="mgr-spin" /> Listing files…</div>
              <div v-else-if="entryFiles.get(item.nzo_id) === 'error'" class="qm-files-err">Couldn't list files.</div>
              <template v-else>
                <div class="qm-files-path mono">{{ (entryFiles.get(item.nzo_id) as QueueFiles).path }}</div>
                <div v-if="(entryFiles.get(item.nzo_id) as QueueFiles).error" class="qm-files-err">{{ (entryFiles.get(item.nzo_id) as QueueFiles).error }}</div>
                <div v-else-if="!(entryFiles.get(item.nzo_id) as QueueFiles).files?.length" class="qm-files-err">Folder is empty (or already imported elsewhere).</div>
                <div v-for="f in (entryFiles.get(item.nzo_id) as QueueFiles).files ?? []" :key="f.name" class="qm-file mono">
                  <span class="qm-file-name">{{ f.name }}</span>
                  <span class="qm-file-size">{{ fmtSize(f.size_bytes / (1024 * 1024)) }}</span>
                </div>
              </template>
            </div>
          </template>
        </div>

        <div v-if="importFlash" class="mgr-flash ok qm-import-flash">{{ importFlash }}</div>
        <div v-if="importError" class="mgr-flash err qm-import-flash">{{ importError }}</div>

        <p class="qm-note">Import moves the media files into the matched item's library folder and queues a scan. Removing an entry acts on the download client itself.</p>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.q-table { display: flex; flex-direction: column; gap: 6px; }
.q-head,
.q-row {
  display: grid;
  grid-template-columns: minmax(260px, 2.4fr) 150px minmax(180px, 1.2fr) 110px;
  gap: 14px;
  align-items: center;
}
.q-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.q-row {
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  align-items: start;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s;
}
.q-row:hover { background: var(--bg-3); border-color: color-mix(in srgb, var(--gold) 30%, var(--border)); }
.q-row.done { opacity: 0.85; }

.q-release { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
/* Full release names, wrapped — never truncated (manager legibility rule). */
.q-title { font-size: 12.5px; color: var(--fg-0); overflow-wrap: anywhere; line-height: 1.45; }
.q-count {
  display: inline-flex;
  padding: 0 7px;
  margin-left: 4px;
  border-radius: 999px;
  font-size: 10.5px;
  font-weight: 700;
  color: var(--gold-bright);
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 45%, transparent);
}
.q-facts { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.q-score { font-size: 10.5px; color: var(--fg-3); }
.q-score b { color: var(--fg-0); font-weight: 700; }
.q-sub { font-size: 11.5px; color: var(--fg-2); }
.q-matchlink { color: var(--gold-bright); text-decoration: none; }
.q-fail { color: var(--bad); }

.q-progress { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.q-bar { height: 5px; border-radius: 999px; background: rgb(var(--ink) / 0.1); overflow: hidden; }
.q-bar-fill { height: 100%; border-radius: 999px; background: var(--gold); transition: width 0.4s; }
.q-progress-text { font-size: 10.5px; color: var(--fg-3); }
.q-when { font-size: 11px; color: var(--fg-3); }

.q-section {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-0);
  margin: 24px 0 12px;
}

.q-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  margin-top: 6px;
}
.mono { font-family: var(--font-mono); }

@media (max-width: 960px) {
  .q-head { display: none; }
  .q-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 8px; }
  .q-progress, .q-when { grid-column: 1 / -1; }
}
</style>

<!-- Modal content is portaled — scoped rules can't reach it. -->
<style>
.q-state {
  display: inline-flex;
  padding: 1px 8px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  border: 1px solid var(--border);
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.05);
  white-space: nowrap;
}
.q-state.tone-good { color: var(--good); border-color: color-mix(in srgb, var(--good) 40%, transparent); background: color-mix(in srgb, var(--good) 9%, transparent); }
.q-state.tone-warn { color: var(--gold-bright); border-color: color-mix(in srgb, var(--gold) 45%, transparent); background: var(--gold-soft); }
.q-state.tone-bad { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); background: color-mix(in srgb, var(--bad) 9%, transparent); }

.qm-name {
  font-family: var(--font-mono);
  font-size: 12.5px;
  color: var(--fg-0);
  overflow-wrap: anywhere;
  line-height: 1.5;
  margin-bottom: 12px;
}
.qm-tagger {
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: rgb(var(--shade) / 0.16);
  padding: 12px 14px;
  margin-bottom: 12px;
}
.qm-tagger-head {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold-bright);
  margin-bottom: 8px;
}
.qm-headline {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  font-size: 13px;
  color: var(--fg-0);
  margin-bottom: 10px;
}
.qm-rows { display: flex; flex-direction: column; }
.qm-row {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 12px;
  padding: 6px 0;
  border-top: 1px solid var(--hair);
  align-items: baseline;
}
.qm-label {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.qm-value { font-size: 12.5px; color: var(--fg-1); overflow-wrap: anywhere; display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
.qm-value.mono { font-family: var(--font-mono); font-size: 11.5px; }
.qm-eval { align-items: center; }
.qm-rejections { flex-direction: column; align-items: flex-start; gap: 3px; }
.qm-rej { font-size: 11.5px; color: var(--fg-2); }
.qm-rej-code { font-family: var(--font-mono); font-size: 10px; color: var(--bad); margin-right: 5px; }

.qm-entries { display: flex; flex-direction: column; gap: 6px; }
.qm-entry {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.qm-entry-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.qm-entry-line { font-family: var(--font-mono); font-size: 11px; color: var(--fg-2); overflow-wrap: anywhere; }
.qm-entry-fail { font-size: 11px; color: var(--bad); }
.qm-note { margin: 10px 2px 0; font-size: 11.5px; color: var(--fg-3); }

.qm-files {
  margin: -2px 0 4px;
  padding: 10px 12px;
  background: rgb(var(--shade) / 0.16);
  border: 1px solid var(--hair);
  border-radius: var(--r-sm);
}
.qm-files-loading,
.qm-files-err { display: flex; align-items: center; gap: 8px; font-size: 11.5px; color: var(--fg-3); }
.qm-files-err { color: var(--bad); }
.qm-files-path {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--fg-3);
  overflow-wrap: anywhere;
  margin-bottom: 6px;
}
.qm-file {
  display: flex;
  align-items: baseline;
  gap: 12px;
  padding: 3px 0;
  border-top: 1px solid var(--hair);
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-1);
}
.qm-file-name { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.qm-file-size { flex-shrink: 0; color: var(--fg-3); }
.mgr-btn-icon.active { color: var(--gold-bright); border-color: color-mix(in srgb, var(--gold) 45%, transparent); }
.mgr-btn-icon.qm-import:not(:disabled) { color: var(--good); border-color: color-mix(in srgb, var(--good) 40%, transparent); }
.qm-import-flash { margin-top: 12px; margin-bottom: 0; }
</style>
