<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import {
  managerCustomFormatsQuery,
  managerQualityLaddersQuery,
  managerQualityProfilesQuery,
  type ManagerFormatImportResult,
  type ManagerQualityItem,
  type ManagerQualityProfileView,
} from '~/queries/manager'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const profilesData = useQuery(managerQualityProfilesQuery())
const profiles = computed(() => profilesData.data.value ?? [])
const loading = computed(() => profilesData.isLoading.value)

const formatsData = useQuery(managerCustomFormatsQuery())
const formatsAll = computed(() => formatsData.data.value ?? [])

const laddersData = useQuery(managerQualityLaddersQuery())
const ladders = computed(() => laddersData.data.value ?? {})

useLiveRefresh([{
  events: ['manager.changed'],
  keys: [['manager', 'quality-profiles'], ['manager', 'custom-formats']],
  filter: (event) => {
    const area = (event.payload as { area?: string } | undefined)?.area
    return area === 'quality_profiles' || area === 'custom_formats'
  },
}])

const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

const itemLabel = (item: ManagerQualityItem) => item.group || item.quality || ''
const itemKey = (item: ManagerQualityItem) => item.group || item.quality || ''

const DOMAIN_LABELS: Record<string, string> = { movie: 'Movies', tv: 'TV', music: 'Music', book: 'Books' }
const domainGroups = computed(() => {
  const groups: { domain: string, label: string, profiles: ManagerQualityProfileView[] }[] = []
  for (const domain of ['movie', 'tv', 'music', 'book']) {
    const rows = profiles.value.filter(profile => profile.domain === domain)
    if (rows.length) groups.push({ domain, label: DOMAIN_LABELS[domain] ?? domain, profiles: rows })
  }
  return groups
})

function allowedSummary(profile: ManagerQualityProfileView): string {
  const items = profile.items ?? []
  return `${items.filter(item => item.allowed).length} of ${items.length}`
}

// ── Edit dialog ──────────────────────────────────────────────────────────
// Thresholds, language gate, and per-format scores on the left; the ranked
// ladder on the right. The score list is ordered by the scores the profile
// had when the dialog opened — stable while typing, so rows don't jump.

const LANGUAGE_OPTIONS = [
  { value: 'any', label: 'Any — no language filter' },
  { value: 'original', label: 'Original — the item\'s original audio' },
  ...['english', 'danish', 'swedish', 'norwegian', 'finnish', 'german', 'french', 'spanish', 'italian', 'dutch', 'portuguese', 'polish', 'russian', 'ukrainian', 'czech', 'hungarian', 'romanian', 'greek', 'turkish', 'arabic', 'hebrew', 'hindi', 'thai', 'vietnamese', 'chinese', 'japanese', 'korean']
    .map(value => ({ value, label: value.charAt(0).toUpperCase() + value.slice(1) })),
]

const dialogOpen = ref(false)
const editingID = ref<number | null>(null)
const form = reactive({
  name: '',
  domain: 'video',
  items: [] as ManagerQualityItem[],
  cutoff: '',
  language: 'any',
  upgrades_enabled: true,
  min_format_score: 0,
  cutoff_format_score: 0,
  min_upgrade_score: 1,
})
const formScores = reactive(new Map<number, number>())
const initialScores = ref<Record<number, number>>({})
const savingForm = ref(false)
const formError = ref('')
const scoreFilter = ref('')

const cutoffOptions = computed(() =>
  form.items.filter(item => item.allowed).map(item => ({ value: itemKey(item), label: itemLabel(item) })))

// Sorted by the snapshot taken when the dialog opened: positives first
// (highest on top), zero-scored alphabetically, penalties at the bottom.
const editorFormats = computed(() => {
  const snapshot = initialScores.value
  return formatsAll.value
    .filter(format => format.domain === form.domain)
    .slice()
    .sort((a, b) => {
      const scoreA = snapshot[a.id] ?? 0
      const scoreB = snapshot[b.id] ?? 0
      if (scoreA !== scoreB) return scoreB - scoreA
      return a.name.localeCompare(b.name)
    })
})

const visibleFormats = computed(() => {
  const needle = scoreFilter.value.trim().toLowerCase()
  if (!needle) return editorFormats.value
  return editorFormats.value.filter(format => format.name.toLowerCase().includes(needle))
})

const DOMAIN_OPTIONS = [
  { value: 'movie', label: 'movies' },
  { value: 'tv', label: 'tv' },
  { value: 'music', label: 'music' },
  { value: 'book', label: 'books' },
]

function applyLadderTemplate(domain: string) {
  form.items = (ladders.value[domain] ?? []).map(item => ({ ...item }))
  form.cutoff = form.items.find(item => item.allowed)?.quality ?? ''
}

function onDomainChange(domain: unknown) {
  const next = String(domain ?? 'video')
  form.domain = next
  applyLadderTemplate(next)
}

function openCreate() {
  editingID.value = null
  form.name = ''
  form.domain = 'movie'
  form.language = 'any'
  form.upgrades_enabled = true
  form.min_format_score = 0
  form.cutoff_format_score = 0
  form.min_upgrade_score = 1
  applyLadderTemplate('movie')
  formScores.clear()
  initialScores.value = {}
  scoreFilter.value = ''
  formError.value = ''
  dialogOpen.value = true
}

async function cloneProfile(profile: ManagerQualityProfileView) {
  try {
    await $heya(`/api/manager/quality-profiles/${profile.id}/clone`, { method: 'POST' })
    await profilesData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Clone failed.' }
  }
}

// ── Import from a running arr ────────────────────────────────────────────
// Same endpoint as the custom-formats import — profiles need the formats
// they score, so both always come over together.

const importOpen = ref(false)
const importForm = reactive({ kind: 'radarr', base_url: '', api_key: '' })
const importing = ref(false)
const importError = ref('')
const importResult = ref<ManagerFormatImportResult | null>(null)

const KIND_OPTIONS = [
  { value: 'radarr', label: 'Radarr (movies)' },
  { value: 'sonarr', label: 'Sonarr (TV)' },
  { value: 'lidarr', label: 'Lidarr (music)' },
]

function openImport() {
  importError.value = ''
  importResult.value = null
  importOpen.value = true
}

async function runImport() {
  importing.value = true
  importError.value = ''
  importResult.value = null
  try {
    importResult.value = await $heya('/api/manager/custom-formats/import', {
      method: 'POST',
      body: {
        kind: importForm.kind,
        base_url: importForm.base_url,
        api_key: importForm.api_key,
        include_profiles: true,
      },
    }) as ManagerFormatImportResult
    await Promise.all([profilesData.refetch(), formatsData.refetch()])
  } catch (e: any) {
    importError.value = e?.data?.detail ?? e?.message ?? 'Import failed.'
  } finally {
    importing.value = false
  }
}

function openEdit(profile: ManagerQualityProfileView) {
  editingID.value = profile.id
  form.name = profile.name
  form.domain = profile.domain
  form.items = (profile.items ?? []).map(item => ({ ...item }))
  form.cutoff = profile.cutoff
  form.language = profile.language || 'any'
  form.upgrades_enabled = profile.upgrades_enabled
  form.min_format_score = profile.min_format_score
  form.cutoff_format_score = profile.cutoff_format_score
  form.min_upgrade_score = profile.min_upgrade_score
  formScores.clear()
  const snapshot: Record<number, number> = {}
  for (const score of profile.format_scores ?? []) {
    formScores.set(score.format_id, score.score)
    snapshot[score.format_id] = score.score
  }
  initialScores.value = snapshot
  scoreFilter.value = ''
  formError.value = ''
  dialogOpen.value = true
}

function scoreFor(formatID: number): number {
  return formScores.get(formatID) ?? 0
}

function setScore(formatID: number, raw: string) {
  const value = Math.trunc(Number(raw))
  if (!raw.trim() || Number.isNaN(value) || value === 0) {
    formScores.delete(formatID)
  } else {
    formScores.set(formatID, value)
  }
}

async function saveForm() {
  savingForm.value = true
  formError.value = ''
  try {
    const body = {
      name: form.name,
      domain: form.domain,
      items: form.items,
      cutoff: form.cutoff,
      language: form.language,
      upgrades_enabled: form.upgrades_enabled,
      min_format_score: Math.trunc(Number(form.min_format_score) || 0),
      cutoff_format_score: Math.trunc(Number(form.cutoff_format_score) || 0),
      min_upgrade_score: Math.trunc(Number(form.min_upgrade_score) || 1),
      format_scores: [...formScores.entries()].map(([format_id, score]) => ({ format_id, score })),
    }
    if (editingID.value == null) {
      await $heya('/api/manager/quality-profiles', { method: 'POST', body })
    } else {
      await $heya(`/api/manager/quality-profiles/${editingID.value}`, { method: 'PUT', body })
    }
    dialogOpen.value = false
    await profilesData.refetch()
  } catch (e: any) {
    formError.value = e?.data?.detail ?? e?.message ?? 'Save failed.'
  } finally {
    savingForm.value = false
  }
}

async function removeProfile(profile: ManagerQualityProfileView) {
  const ok = await confirm({
    title: 'Delete quality profile',
    message: `Delete ${profile.name}?`,
    destructive: true,
  })
  if (!ok) return
  try {
    await $heya(`/api/manager/quality-profiles/${profile.id}`, { method: 'DELETE' })
    await profilesData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Delete failed.' }
  }
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Quality profiles"
      icon="eq"
      eyebrow="Manager · System"
      description="What the decision engine is allowed to grab and when it stops upgrading: a ranked quality ladder, an optional language gate, and custom-format scores that break ties inside a quality."
    />

    <div v-if="flash" class="mgr-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" /> {{ flash.text }}
    </div>

    <SettingsSection
      title="Profiles"
      icon="eq"
      description="Click a profile to open the full editor — ladder, thresholds, language, and every format score."
    >
      <template #actions>
        <div class="qp-actions">
          <button type="button" class="mgr-btn" @click="openImport"><Icon name="cloud-download" :size="14" /> Import</button>
          <button type="button" class="mgr-btn-gold" @click="openCreate"><Icon name="plus" :size="14" /> New profile</button>
        </div>
      </template>

      <div v-if="loading && !profiles.length" class="mgr-loading">
        <Icon name="spinner" :size="16" /> Loading…
      </div>

      <div v-else class="qp-groups">
        <div v-for="group in domainGroups" :key="group.domain" class="qp-group">
          <div class="qp-group-head">{{ group.label }}</div>
          <div class="qp-list">
            <div class="qp-cols">
              <span>Profile</span>
              <span>Cutoff</span>
              <span>Qualities</span>
              <span>Format scoring</span>
              <span class="num">In use</span>
              <span />
            </div>
            <div
              v-for="profile in group.profiles"
              :key="profile.id"
              class="qp-row"
              role="button"
              tabindex="0"
              @click="openEdit(profile)"
              @keydown.enter="openEdit(profile)"
              @keydown.space.prevent="openEdit(profile)"
            >
              <span class="qp-row-name">
                {{ profile.name }}
                <span v-if="profile.source" class="qp-chip">{{ profile.source }}</span>
                <span v-if="profile.language && profile.language !== 'any'" class="qp-chip lang">{{ profile.language }}</span>
                <span v-if="!profile.upgrades_enabled" class="qp-chip muted">no upgrades</span>
              </span>
              <span class="qp-row-cutoff">{{ profile.cutoff }}</span>
              <span class="qp-row-qual">{{ allowedSummary(profile) }}</span>
              <span class="qp-row-scores">
                <template v-if="profile.format_scores?.length">
                  {{ profile.format_scores.length }} scored · min {{ profile.min_format_score.toLocaleString() }} · until {{ profile.cutoff_format_score.toLocaleString() }}
                </template>
                <template v-else>—</template>
              </span>
              <span class="qp-row-inuse num">{{ profile.in_use_count }}</span>
              <span class="qp-row-actions" @click.stop>
                <AppTooltip label="Clone">
                  <button type="button" class="mgr-btn-icon" :aria-label="`Clone ${profile.name}`" @click="cloneProfile(profile)"><Icon name="copy" :size="13" /></button>
                </AppTooltip>
                <button type="button" class="mgr-btn-icon" :aria-label="`Edit ${profile.name}`" @click="openEdit(profile)"><Icon name="pencil" :size="13" /></button>
                <button
                  type="button"
                  class="mgr-btn-icon danger"
                  :disabled="profile.in_use_count > 0"
                  :aria-label="`Delete ${profile.name}`"
                  @click="removeProfile(profile)"
                ><Icon name="trash" :size="13" /></button>
              </span>
            </div>
          </div>
        </div>
      </div>
    </SettingsSection>

    <AppDialog v-model="importOpen" title="Import quality profiles" size="sm">
      <div class="mgr-form">
        <p class="qpi-blurb">
          Pulls the app's quality profiles — ladder, cutoffs, thresholds, and every
          tuned format score — along with the custom formats they depend on.
          Re-importing from the same app syncs your earlier import in place.
        </p>
        <label class="mgr-field">
          <span>Source app</span>
          <AppSelect v-model="importForm.kind" :options="KIND_OPTIONS" />
        </label>
        <label class="mgr-field">
          <span>Base URL</span>
          <input v-model="importForm.base_url" class="mgr-input" placeholder="https://radarr.example.ts.net">
        </label>
        <label class="mgr-field">
          <span>API key</span>
          <input v-model="importForm.api_key" class="mgr-input" type="password" autocomplete="off">
        </label>

        <p v-if="importError" class="mgr-form-error"><Icon name="warning" :size="12" /> {{ importError }}</p>

        <div v-if="importResult" class="qpi-result">
          <p class="qpi-line ok">
            <Icon name="check" :size="13" />
            Formats: {{ importResult.created }} new · {{ importResult.updated }} synced
          </p>
          <p v-for="name in importResult.profiles_created ?? []" :key="name" class="qpi-line ok">
            <Icon name="check" :size="13" /> Profile imported: {{ name }}
          </p>
          <p v-for="name in importResult.profiles_updated ?? []" :key="`u-${name}`" class="qpi-line ok">
            <Icon name="check" :size="13" /> Profile re-synced: {{ name }}
          </p>
          <p v-for="name in importResult.profiles_skipped ?? []" :key="`s-${name}`" class="qpi-line warn">
            <Icon name="warning" :size="13" /> Profile skipped (name taken): {{ name }}
          </p>
        </div>
      </div>
      <template #footer>
        <button type="button" class="mgr-btn" @click="importOpen = false">{{ importResult ? 'Done' : 'Cancel' }}</button>
        <button type="button" class="mgr-btn-gold" :disabled="importing || !importForm.base_url" @click="runImport">
          <Icon v-if="importing" name="spinner" :size="13" /> Import
        </button>
      </template>
    </AppDialog>

    <AppDialog v-model="dialogOpen" :title="editingID == null ? 'New quality profile' : 'Edit quality profile'" size="lg">
      <div class="mgr-form qpx">
        <div class="qpx-cols">
          <div class="qpx-left">
            <div class="qpx-scores-row" :class="{ two: editingID == null }">
              <label class="mgr-field" :style="editingID == null ? undefined : { gridColumn: '1 / -1' }">
                <span>Name</span>
                <input v-model="form.name" class="mgr-input" placeholder="4K — Danish audio">
              </label>
              <label v-if="editingID == null" class="mgr-field">
                <span>Domain</span>
                <AppSelect :model-value="form.domain" :options="DOMAIN_OPTIONS" @update:model-value="onDomainChange" />
              </label>
            </div>

            <div class="qpx-upgrades">
              <AppSwitch v-model="form.upgrades_enabled" size="sm" aria-label="Upgrades enabled" />
              <span>Upgrade existing files until the cutoffs are met</span>
            </div>

            <div class="qpx-scores-row two">
              <label class="mgr-field">
                <span>Upgrade until</span>
                <AppSelect v-model="form.cutoff" :options="cutoffOptions" />
              </label>
              <label class="mgr-field">
                <span>Language</span>
                <AppSelect v-model="form.language" :options="LANGUAGE_OPTIONS" />
              </label>
            </div>

            <div class="qpx-scores-row">
              <label class="mgr-field">
                <span>Minimum score</span>
                <input v-model.number="form.min_format_score" class="mgr-input" inputmode="numeric">
              </label>
              <label class="mgr-field">
                <span>Upgrade until score</span>
                <input v-model.number="form.cutoff_format_score" class="mgr-input" inputmode="numeric">
              </label>
              <label class="mgr-field">
                <span>Min increment</span>
                <input v-model.number="form.min_upgrade_score" class="mgr-input" inputmode="numeric">
              </label>
            </div>
            <p class="qpx-hint">
              A release must score at least the minimum to be grabbed, and must be in the
              profile's language (or any, or the item's original audio). Once the current
              file reaches the upgrade-until score, no further format upgrades are fetched.
            </p>

            <div class="mgr-field">
              <div class="qpx-formats-head">
                <span class="qpx-formats-label">Custom format scores</span>
                <span class="qpx-formats-note">sorted by score</span>
                <input v-model="scoreFilter" class="mgr-input qpx-formats-filter" placeholder="Filter…">
              </div>
              <div v-if="!editorFormats.length" class="qpx-hint">
                No custom formats in the {{ form.domain }} domain yet — import or create them on the Custom formats page.
              </div>
              <div v-else class="qpx-format-list">
                <label v-for="format in visibleFormats" :key="format.id" class="qpx-format-row">
                  <span class="qpx-format-name" :class="{ scored: scoreFor(format.id) !== 0 }">{{ format.name }}</span>
                  <input
                    class="mgr-input qpx-format-score"
                    :class="{ negative: scoreFor(format.id) < 0, positive: scoreFor(format.id) > 0 }"
                    :value="scoreFor(format.id) || ''"
                    placeholder="0"
                    inputmode="numeric"
                    @input="setScore(format.id, ($event.target as HTMLInputElement).value)"
                  >
                </label>
              </div>
            </div>
          </div>

          <div class="qpx-right">
            <div class="mgr-field">
              <span>Qualities — higher is preferred; grouped qualities tie; only checked qualities are wanted</span>
              <ul class="qp-edit-ladder">
                <li v-for="item in form.items" :key="itemKey(item)" class="qp-edit-row">
                  <AppSwitch v-model="item.allowed" size="sm" :aria-label="`Allow ${itemLabel(item)}`" />
                  <div class="qp-edit-labels">
                    <span class="qp-edit-quality" :class="{ off: !item.allowed }">{{ itemLabel(item) }}</span>
                    <span v-if="item.group" class="qp-edit-members">{{ (item.qualities ?? []).join(' · ') }}</span>
                  </div>
                  <span v-if="itemKey(item) === form.cutoff" class="qp-cutoff-tag">cutoff</span>
                </li>
              </ul>
            </div>
          </div>
        </div>

        <p v-if="formError" class="mgr-form-error"><Icon name="warning" :size="12" /> {{ formError }}</p>
      </div>
      <template #footer>
        <button type="button" class="mgr-btn" @click="dialogOpen = false">Cancel</button>
        <button type="button" class="mgr-btn-gold" :disabled="savingForm" @click="saveForm">
          <Icon v-if="savingForm" name="spinner" :size="13" /> Save
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.qp-groups { display: flex; flex-direction: column; gap: 22px; }
.qp-group { display: flex; flex-direction: column; gap: 8px; }
.qp-group-head {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}

.qp-list { display: flex; flex-direction: column; gap: 5px; }
.qp-cols,
.qp-row {
  display: grid;
  grid-template-columns: minmax(200px, 1.8fr) minmax(110px, 1fr) 80px minmax(180px, 1.4fr) 52px auto;
  gap: 14px;
  align-items: center;
}
.qp-cols {
  padding: 0 14px 2px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.qp-row {
  padding: 10px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.qp-row:hover { border-color: color-mix(in srgb, var(--gold) 35%, var(--border)); background: rgb(var(--ink) / 0.03); }

.qp-row-name {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex-wrap: wrap;
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  min-width: 0;
}
.qp-chip {
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  padding: 1px 7px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: rgb(var(--ink) / 0.05);
  color: var(--fg-2);
}
.qp-chip.lang {
  color: var(--gold-bright);
  border-color: color-mix(in srgb, var(--gold) 40%, transparent);
  background: var(--gold-soft);
}
.qp-chip.muted { color: var(--fg-3); border-style: dashed; }

.qp-row-cutoff { font-family: var(--font-mono); font-size: 11.5px; color: var(--gold); }
.qp-row-qual { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); }
.qp-row-scores { font-size: 11.5px; color: var(--fg-2); }
.qp-row-inuse { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-3); }
.num { text-align: right; }
.qp-row-actions { display: flex; gap: 5px; }

@media (max-width: 960px) {
  .qp-cols { display: none; }
  .qp-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 6px; }
  .qp-row-cutoff { grid-column: 1; }
  .qp-row-qual, .qp-row-inuse { display: none; }
  .qp-row-scores { grid-column: 1 / -1; }
  .qp-row-actions { grid-row: 1; grid-column: 2; }
}
</style>

<!-- Portaled dialog additions (base .mgr-form styles live in the layout). -->
<style>
.qp-actions { display: flex; gap: 8px; flex-wrap: wrap; }

.qpi-blurb {
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
  color: var(--fg-2);
}
.qpi-result {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 220px;
  overflow-y: auto;
}
.qpi-line {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 0;
  font-size: 12px;
}
.qpi-line.ok { color: var(--good); }
.qpi-line.warn { color: var(--fg-2); }

.qpx-cols {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: 22px;
  align-items: start;
}
@media (max-width: 860px) {
  .qpx-cols { grid-template-columns: minmax(0, 1fr); }
}

.qpx-left, .qpx-right { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

.qpx-upgrades {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12.5px;
  color: var(--fg-1);
}

.qpx-scores-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.qpx-scores-row.two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.qpx-hint {
  margin: 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--fg-3);
}

.qpx-formats-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.qpx-formats-label {
  font-size: 12px;
  font-weight: 580;
  color: var(--fg-1);
  white-space: nowrap;
}
.qpx-formats-note {
  flex: 1;
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--fg-3);
  white-space: nowrap;
}
.qpx-formats-filter { width: 150px; height: 28px; font-size: 12px; }

.qpx-format-list {
  display: flex;
  flex-direction: column;
  max-height: 320px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: rgb(var(--shade) / 0.18);
  padding: 4px;
}
.qpx-format-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 6px 4px 10px;
  border-radius: var(--r-sm);
}
.qpx-format-row:not(:first-child) { margin-top: 1px; }
.qpx-format-row:hover { background: rgb(var(--ink) / 0.05); }
.qpx-format-name {
  flex: 1;
  font-size: 12px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.qpx-format-name.scored { color: var(--fg-0); font-weight: 500; }
.qpx-format-score {
  width: 76px;
  height: 28px;
  text-align: right;
  font-family: var(--font-mono);
  font-size: 12px;
}
.qpx-format-score.positive { color: var(--gold-bright); }
.qpx-format-score.negative { color: var(--bad); }

.qp-edit-ladder {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 480px;
  overflow-y: auto;
  padding-right: 4px;
}
.qp-edit-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 8px;
  border-radius: var(--r-sm);
}
.qp-edit-row:hover { background: rgb(var(--ink) / 0.04); }
.qp-edit-labels { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.qp-edit-quality { font-family: var(--font-mono); font-size: 12px; color: var(--fg-0); }
.qp-edit-quality.off { color: var(--fg-3); text-decoration: line-through; }
.qp-edit-members {
  font-size: 10px;
  color: var(--fg-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.qp-cutoff-tag {
  margin-left: auto;
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--gold);
}
</style>
