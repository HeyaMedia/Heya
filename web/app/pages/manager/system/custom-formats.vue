<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import {
  managerCustomFormatsQuery,
  type CustomFormatSpec,
  type ManagerCustomFormatView,
  type ManagerFormatImportResult,
  type ManagerReleaseTestView,
} from '~/queries/manager'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const formatsData = useQuery(managerCustomFormatsQuery())
const formatsAll = computed(() => formatsData.data.value ?? [])
const loading = computed(() => formatsData.isLoading.value)

useLiveRefresh([{
  events: ['manager.changed'],
  keys: [['manager', 'custom-formats']],
  filter: event => (event.payload as { area?: string } | undefined)?.area === 'custom_formats',
}])

// ── Domain filter ────────────────────────────────────────────────────────

const domainFilter = ref('all')
const domains = computed(() => {
  const seen = new Set(formatsAll.value.map(format => format.domain))
  return ['movie', 'tv', 'music', 'book'].filter(domain => seen.has(domain))
})
const formats = computed(() =>
  domainFilter.value === 'all'
    ? formatsAll.value
    : formatsAll.value.filter(format => format.domain === domainFilter.value))

// ── Condition vocabulary ─────────────────────────────────────────────────

const IMPLEMENTATIONS = [
  { value: 'ReleaseTitleSpecification', label: 'Release title (regex)' },
  { value: 'ReleaseGroupSpecification', label: 'Release group (regex)' },
  { value: 'EditionSpecification', label: 'Edition (regex)' },
  { value: 'ResolutionSpecification', label: 'Resolution' },
  { value: 'SourceSpecification', label: 'Source' },
  { value: 'QualityModifierSpecification', label: 'Quality modifier' },
  { value: 'ReleaseTypeSpecification', label: 'Release type' },
  { value: 'LanguageSpecification', label: 'Language' },
  { value: 'SizeSpecification', label: 'Size (GB)' },
  { value: 'YearSpecification', label: 'Year' },
]
const IMPLEMENTATION_SHORT: Record<string, string> = {
  ReleaseTitleSpecification: 'title',
  ReleaseGroupSpecification: 'group',
  EditionSpecification: 'edition',
  ResolutionSpecification: 'resolution',
  SourceSpecification: 'source',
  QualityModifierSpecification: 'modifier',
  ReleaseTypeSpecification: 'type',
  LanguageSpecification: 'language',
  SizeSpecification: 'size',
  YearSpecification: 'year',
  IndexerFlagSpecification: 'flag',
}
const SOURCE_OPTIONS = ['bluray', 'remux', 'webdl', 'webrip', 'tv', 'rawhd', 'dvd', 'cam', 'telesync', 'telecine', 'workprint', 'screener', 'ppv']
  .map(value => ({ value, label: value }))
const MODIFIER_OPTIONS = ['none', 'remux', 'brdisk', 'rawhd', 'regional', 'screener'].map(value => ({ value, label: value }))
const RELEASE_TYPE_OPTIONS = ['single', 'multi', 'season-pack'].map(value => ({ value, label: value }))
const RESOLUTION_OPTIONS = [2160, 1080, 720, 576, 540, 480, 360].map(value => ({ value: String(value), label: `${value}p` }))

const isRegexSpec = (impl: string) => impl.startsWith('ReleaseTitle') || impl.startsWith('ReleaseGroup') || impl.startsWith('Edition')
const isRangeSpec = (impl: string) => impl === 'SizeSpecification' || impl === 'YearSpecification'

function conditionSummary(format: ManagerCustomFormatView): { label: string, negated: boolean }[] {
  const specs = format.specifications ?? []
  return specs.slice(0, 6).map(spec => ({
    label: IMPLEMENTATION_SHORT[spec.implementation] ?? 'other',
    negated: spec.negate,
  }))
}

// ── Editor dialog ────────────────────────────────────────────────────────

type EditSpec = {
  name: string
  implementation: string
  negate: boolean
  required: boolean
  value: string
  except: boolean
  min: string
  max: string
}

const dialogOpen = ref(false)
const editingID = ref<number | null>(null)
const form = reactive({ name: '', domain: 'video', specs: [] as EditSpec[] })
const savingForm = ref(false)
const formError = ref('')

function specToEdit(spec: CustomFormatSpec): EditSpec {
  const fields = (spec.fields ?? {}) as Record<string, unknown>
  return {
    name: spec.name,
    implementation: spec.implementation,
    negate: spec.negate,
    required: spec.required,
    value: fields.value == null ? '' : String(fields.value),
    except: fields.except === true,
    min: fields.min == null ? '' : String(fields.min),
    max: fields.max == null ? '' : String(fields.max),
  }
}

function editToSpec(spec: EditSpec): CustomFormatSpec {
  const fields: Record<string, unknown> = {}
  if (isRangeSpec(spec.implementation)) {
    fields.min = Number(spec.min || 0)
    fields.max = Number(spec.max || 0)
  } else if (spec.implementation === 'ResolutionSpecification') {
    fields.value = Number(spec.value || 0)
  } else {
    fields.value = spec.value
  }
  if (spec.implementation === 'LanguageSpecification' && spec.except) fields.except = true
  return {
    name: spec.name,
    implementation: spec.implementation,
    negate: spec.negate,
    required: spec.required,
    fields: fields as CustomFormatSpec['fields'],
  }
}

function blankSpec(): EditSpec {
  return { name: '', implementation: 'ReleaseTitleSpecification', negate: false, required: true, value: '', except: false, min: '', max: '' }
}

function openCreate() {
  editingID.value = null
  form.name = ''
  form.domain = domainFilter.value === 'all' ? 'movie' : domainFilter.value
  form.specs = [blankSpec()]
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(format: ManagerCustomFormatView) {
  editingID.value = format.id
  form.name = format.name
  form.domain = format.domain
  form.specs = (format.specifications ?? []).map(specToEdit)
  formError.value = ''
  dialogOpen.value = true
}

async function saveForm() {
  savingForm.value = true
  formError.value = ''
  try {
    const body = {
      name: form.name,
      domain: form.domain,
      specifications: form.specs.map(editToSpec),
    }
    if (editingID.value == null) {
      await $heya('/api/manager/custom-formats', { method: 'POST', body })
    } else {
      await $heya(`/api/manager/custom-formats/${editingID.value}`, { method: 'PUT', body })
    }
    dialogOpen.value = false
    await formatsData.refetch()
  } catch (e: any) {
    formError.value = e?.data?.detail ?? e?.message ?? 'Save failed.'
  } finally {
    savingForm.value = false
  }
}

async function removeFormat(format: ManagerCustomFormatView) {
  const ok = await confirm({
    title: 'Delete custom format',
    message: `Delete ${format.name}? Profiles that score it drop the score.`,
    destructive: true,
  })
  if (!ok) return
  try {
    await $heya(`/api/manager/custom-formats/${format.id}`, { method: 'DELETE' })
    await formatsData.refetch()
  } catch (e: any) {
    formError.value = e?.data?.detail ?? e?.message ?? 'Delete failed.'
  }
}

// ── Import dialog ────────────────────────────────────────────────────────

const importOpen = ref(false)
const importMode = ref<'live' | 'paste'>('live')
const importForm = reactive({ kind: 'radarr', base_url: '', api_key: '', json: '', include_profiles: true })
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
        base_url: importMode.value === 'live' ? importForm.base_url : '',
        api_key: importMode.value === 'live' ? importForm.api_key : '',
        json: importMode.value === 'paste' ? importForm.json : '',
        include_profiles: importMode.value === 'live' && importForm.include_profiles,
      },
    }) as ManagerFormatImportResult
    await formatsData.refetch()
  } catch (e: any) {
    importError.value = e?.data?.detail ?? e?.message ?? 'Import failed.'
  } finally {
    importing.value = false
  }
}

// ── Release tester ───────────────────────────────────────────────────────

const testTitle = ref('')
const testTV = ref(false)
const testSizeGB = ref('')
const testing = ref(false)
const testError = ref('')
const testResult = ref<ManagerReleaseTestView | null>(null)

async function runTest() {
  if (!testTitle.value.trim()) return
  testing.value = true
  testError.value = ''
  try {
    testResult.value = await $heya('/api/manager/custom-formats/test-release', {
      method: 'POST',
      body: {
        title: testTitle.value,
        tv: testTV.value,
        size_bytes: Math.round(Number(testSizeGB.value || 0) * 1024 ** 3),
      },
    }) as ManagerReleaseTestView
  } catch (e: any) {
    testError.value = e?.data?.detail ?? e?.message ?? 'Test failed.'
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Custom formats"
      icon="hash"
      eyebrow="Manager · System"
      description="Regex-backed release scoring — the TRaSH Guides model. Formats match parsed releases (group, codec, HDR, edition…) and quality profiles sum their scores to pick the winning release."
    />

    <SettingsSection
      title="Formats"
      icon="hash"
      :description="`${formatsAll.length} formats. Import straight from a running Radarr/Sonarr/Lidarr or paste TRaSH Guides JSON — conditions and semantics carry over unchanged.`"
    >
      <template #actions>
        <div class="cf-actions">
          <button type="button" class="mgr-btn" @click="openImport"><Icon name="cloud-download" :size="14" /> Import</button>
          <button type="button" class="mgr-btn-gold" @click="openCreate"><Icon name="plus" :size="14" /> New format</button>
        </div>
      </template>

      <div v-if="domains.length > 1" class="cf-domains">
        <button
          v-for="domain in ['all', ...domains]"
          :key="domain"
          type="button"
          class="cf-domain-chip"
          :class="{ active: domainFilter === domain }"
          @click="domainFilter = domain"
        >{{ domain }}</button>
      </div>

      <div v-if="loading && !formatsAll.length" class="mgr-loading">
        <Icon name="spinner" :size="16" /> Loading…
      </div>
      <div v-else-if="!formatsAll.length" class="mgr-loading">
        No custom formats yet — import your *arr library or add one by hand.
      </div>

      <div v-else class="cf-table">
        <div class="cf-head">
          <span>Format</span>
          <span>Conditions</span>
          <span>Source</span>
          <span />
        </div>
        <div
          v-for="format in formats"
          :key="format.id"
          class="cf-row"
          role="button"
          tabindex="0"
          @click="openEdit(format)"
          @keydown.enter="openEdit(format)"
          @keydown.space.prevent="openEdit(format)"
        >
          <span class="cf-name">
            {{ format.name }}
            <span v-if="format.trash_id" class="cf-trash-tag">TRaSH</span>
          </span>
          <span class="cf-tags">
            <span
              v-for="(chip, index) in conditionSummary(format)"
              :key="index"
              class="cf-tag"
              :class="{ negated: chip.negated }"
            >{{ chip.negated ? 'not ' : '' }}{{ chip.label }}</span>
            <span v-if="(format.specifications?.length ?? 0) > 6" class="cf-tag">+{{ (format.specifications?.length ?? 0) - 6 }}</span>
          </span>
          <span class="cf-source">{{ format.source || 'manual' }}</span>
          <span class="cf-row-actions" @click.stop>
            <button type="button" class="mgr-btn-icon" :aria-label="`Edit ${format.name}`" @click="openEdit(format)"><Icon name="pencil" :size="13" /></button>
            <button type="button" class="mgr-btn-icon danger" :aria-label="`Delete ${format.name}`" @click="removeFormat(format)"><Icon name="trash" :size="13" /></button>
          </span>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection
      title="Release tester"
      icon="magnifying-glass"
      description="Paste any release name to see how it parses, which formats match, and the score every profile would give it — the exact math the decision engine runs."
    >
      <div class="cf-tester">
        <div class="cf-tester-inputs">
          <input
            v-model="testTitle"
            class="mgr-input mono"
            placeholder="Movie.Name.2023.2160p.UHD.BluRay.REMUX.HDR.HEVC.Atmos-FraMeSToR"
            @keydown.enter="runTest"
          >
          <input v-model="testSizeGB" class="mgr-input cf-tester-size" placeholder="GB" inputmode="decimal">
          <label class="cf-tester-tv"><AppSwitch v-model="testTV" size="sm" aria-label="TV release" /> TV</label>
          <button type="button" class="mgr-btn-gold" :disabled="testing || !testTitle.trim()" @click="runTest">
            <Icon v-if="testing" name="spinner" :size="13" /> Test
          </button>
        </div>
        <p v-if="testError" class="mgr-form-error"><Icon name="warning" :size="12" /> {{ testError }}</p>

        <div v-if="testResult" class="cf-tester-result">
          <div class="cf-tester-parsed">
            <span v-if="testResult.parsed.resolution" class="cf-tag">{{ testResult.parsed.resolution }}p</span>
            <span v-for="source in testResult.parsed.sources ?? []" :key="source" class="cf-tag">{{ source }}</span>
            <span v-if="testResult.parsed.modifier !== 'none'" class="cf-tag gold">{{ testResult.parsed.modifier }}</span>
            <span v-if="testResult.parsed.release_type" class="cf-tag">{{ testResult.parsed.release_type }}</span>
            <span v-for="lang in testResult.parsed.languages ?? []" :key="lang" class="cf-tag">{{ lang }}</span>
            <span v-if="testResult.parsed.group" class="cf-tag">-{{ testResult.parsed.group }}</span>
          </div>

          <div class="cf-tester-matches">
            <span class="cf-tester-label">{{ (testResult.matches?.length ?? 0) || 'No' }} formats match</span>
            <div class="cf-tags">
              <span v-for="match in testResult.matches ?? []" :key="match.format_id" class="cf-tag gold">{{ match.name }}</span>
            </div>
          </div>

          <div v-if="testResult.profiles?.length" class="cf-tester-profiles">
            <div
              v-for="profile in testResult.profiles"
              :key="profile.profile_id"
              class="cf-tester-profile"
              :class="{ rejected: !profile.min_met || !profile.language_ok }"
            >
              <span class="cf-tester-profile-name">{{ profile.name }}</span>
              <span v-if="!profile.language_ok" class="cf-tester-profile-flag">wrong language (wants {{ profile.language }})</span>
              <span v-else-if="!profile.min_met" class="cf-tester-profile-flag">below minimum</span>
              <span class="cf-tester-profile-score">{{ profile.score.toLocaleString() }}</span>
            </div>
          </div>
        </div>
      </div>
    </SettingsSection>

    <!-- ── Editor ──────────────────────────────────────────────────────── -->
    <AppDialog v-model="dialogOpen" :title="editingID == null ? 'New custom format' : 'Edit custom format'" size="lg">
      <div class="mgr-form">
        <div class="cf-form-top">
          <label class="mgr-field cf-form-name">
            <span>Name</span>
            <input v-model="form.name" class="mgr-input" placeholder="x265 (no HDR/DV)">
          </label>
          <label v-if="editingID == null" class="mgr-field">
            <span>Domain</span>
            <AppSelect v-model="form.domain" :options="[{ value: 'movie', label: 'movies' }, { value: 'tv', label: 'tv' }, { value: 'music', label: 'music' }, { value: 'book', label: 'books' }]" />
          </label>
        </div>

        <div class="mgr-field">
          <span>Conditions — same-type conditions OR together (required ones must all hit); different types AND together</span>
          <div class="cf-spec-list">
            <div v-for="(spec, index) in form.specs" :key="index" class="cf-spec">
              <div class="cf-spec-head">
                <AppSelect v-model="spec.implementation" :options="IMPLEMENTATIONS" class="cf-spec-impl" />
                <input v-model="spec.name" class="mgr-input cf-spec-name" placeholder="Condition name">
                <button type="button" class="mgr-btn-icon danger" aria-label="Remove condition" @click="form.specs.splice(index, 1)">
                  <Icon name="trash" :size="13" />
                </button>
              </div>
              <div class="cf-spec-body">
                <input
                  v-if="isRegexSpec(spec.implementation)"
                  v-model="spec.value"
                  class="mgr-input mono"
                  placeholder="regular expression (case-insensitive, .NET syntax)"
                >
                <AppSelect v-else-if="spec.implementation === 'ResolutionSpecification'" v-model="spec.value" :options="RESOLUTION_OPTIONS" />
                <AppSelect v-else-if="spec.implementation === 'SourceSpecification'" v-model="spec.value" :options="SOURCE_OPTIONS" />
                <AppSelect v-else-if="spec.implementation === 'QualityModifierSpecification'" v-model="spec.value" :options="MODIFIER_OPTIONS" />
                <AppSelect v-else-if="spec.implementation === 'ReleaseTypeSpecification'" v-model="spec.value" :options="RELEASE_TYPE_OPTIONS" />
                <template v-else-if="spec.implementation === 'LanguageSpecification'">
                  <input v-model="spec.value" class="mgr-input" placeholder="english / original / any…">
                  <label class="cf-spec-flag"><AppSwitch v-model="spec.except" size="sm" aria-label="Except language" /> except</label>
                </template>
                <template v-else-if="isRangeSpec(spec.implementation)">
                  <input v-model="spec.min" class="mgr-input cf-spec-range" placeholder="min" inputmode="decimal">
                  <input v-model="spec.max" class="mgr-input cf-spec-range" placeholder="max" inputmode="decimal">
                </template>
                <span v-else class="cf-spec-note">Kept for compatibility — never matches yet.</span>
                <label class="cf-spec-flag"><AppSwitch v-model="spec.negate" size="sm" aria-label="Negate" /> negate</label>
                <label class="cf-spec-flag"><AppSwitch v-model="spec.required" size="sm" aria-label="Required" /> required</label>
              </div>
            </div>
          </div>
          <button type="button" class="mgr-btn cf-spec-add" @click="form.specs.push(blankSpec())">
            <Icon name="plus" :size="13" /> Add condition
          </button>
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

    <!-- ── Import ──────────────────────────────────────────────────────── -->
    <AppDialog v-model="importOpen" title="Import custom formats" size="md">
      <div class="mgr-form">
        <div class="cf-import-modes">
          <button type="button" class="cf-domain-chip" :class="{ active: importMode === 'live' }" @click="importMode = 'live'">From a running instance</button>
          <button type="button" class="cf-domain-chip" :class="{ active: importMode === 'paste' }" @click="importMode = 'paste'">Paste JSON</button>
        </div>

        <label class="mgr-field">
          <span>Source app — decides condition mapping and target domain</span>
          <AppSelect v-model="importForm.kind" :options="KIND_OPTIONS" />
        </label>

        <template v-if="importMode === 'live'">
          <label class="mgr-field">
            <span>Base URL</span>
            <input v-model="importForm.base_url" class="mgr-input" placeholder="https://radarr.example.ts.net">
          </label>
          <label class="mgr-field">
            <span>API key</span>
            <input v-model="importForm.api_key" class="mgr-input" type="password" autocomplete="off">
          </label>
          <label class="cf-spec-flag">
            <AppSwitch v-model="importForm.include_profiles" size="sm" aria-label="Import profiles" />
            Also import quality profiles with their format scores
          </label>
        </template>
        <label v-else class="mgr-field">
          <span>Custom format JSON — an arr export, a TRaSH guide file, or an array of either</span>
          <textarea v-model="importForm.json" class="mgr-input mono cf-import-json" rows="8" placeholder='{"name": "…", "specifications": […]}' />
        </label>

        <p v-if="importError" class="mgr-form-error"><Icon name="warning" :size="12" /> {{ importError }}</p>

        <div v-if="importResult" class="cf-import-result">
          <p class="cf-import-line ok">
            <Icon name="check" :size="13" />
            {{ importResult.created }} created · {{ importResult.updated }} updated
            <template v-if="importResult.skipped?.length"> · {{ importResult.skipped.length }} skipped</template>
          </p>
          <p v-for="name in importResult.profiles_created ?? []" :key="name" class="cf-import-line ok">
            <Icon name="check" :size="13" /> Profile imported: {{ name }}
          </p>
          <p v-for="name in importResult.profiles_updated ?? []" :key="`pu-${name}`" class="cf-import-line ok">
            <Icon name="check" :size="13" /> Profile re-synced: {{ name }}
          </p>
          <p v-for="name in importResult.profiles_skipped ?? []" :key="`ps-${name}`" class="cf-import-line warn">
            <Icon name="warning" :size="13" /> Profile skipped (name taken): {{ name }}
          </p>
          <p v-for="(warning, index) in importResult.warnings ?? []" :key="index" class="cf-import-line warn">
            <Icon name="warning" :size="13" /> {{ warning }}
          </p>
        </div>
      </div>
      <template #footer>
        <button type="button" class="mgr-btn" @click="importOpen = false">{{ importResult ? 'Done' : 'Cancel' }}</button>
        <button type="button" class="mgr-btn-gold" :disabled="importing" @click="runImport">
          <Icon v-if="importing" name="spinner" :size="13" /> Import
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.cf-actions { display: flex; gap: 8px; flex-wrap: wrap; }

.cf-domains { display: flex; gap: 6px; margin-bottom: 12px; }

.cf-table { display: flex; flex-direction: column; gap: 6px; }
.cf-head,
.cf-row {
  display: grid;
  grid-template-columns: minmax(180px, 1.4fr) minmax(200px, 2fr) 90px auto;
  gap: 14px;
  align-items: center;
}
.cf-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.cf-row {
  padding: 9px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.cf-row:hover { border-color: color-mix(in srgb, var(--gold) 35%, var(--border)); background: rgb(var(--ink) / 0.03); }
.cf-name {
  display: inline-flex; align-items: center; gap: 8px;
  font-size: 13px; font-weight: 500; color: var(--fg-0);
  min-width: 0;
}
.cf-trash-tag {
  font-family: var(--font-mono);
  font-size: 9px; font-weight: 700;
  letter-spacing: 0.1em;
  color: var(--gold);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  border-radius: 999px;
  padding: 1px 6px;
}
.cf-row-actions { display: flex; gap: 5px; }
.cf-source {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-3);
}

.cf-tester { display: flex; flex-direction: column; gap: 12px; }
.cf-tester-inputs { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.cf-tester-inputs .mgr-input:first-child { flex: 1; min-width: 260px; }
.cf-tester-size { width: 70px; }
.cf-tester-tv {
  display: inline-flex; align-items: center; gap: 7px;
  font-size: 12px; color: var(--fg-2);
}
.cf-tester-result {
  display: flex; flex-direction: column; gap: 12px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.cf-tester-parsed { display: flex; gap: 5px; flex-wrap: wrap; }
.cf-tester-matches { display: flex; flex-direction: column; gap: 7px; }
.cf-tester-label {
  font-family: var(--font-mono);
  font-size: 10px; font-weight: 600;
  letter-spacing: 0.14em; text-transform: uppercase;
  color: var(--fg-3);
}
.cf-tester-profiles { display: flex; flex-direction: column; gap: 4px; }
.cf-tester-profile {
  display: flex; align-items: baseline; gap: 10px;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.04);
  font-size: 12.5px; color: var(--fg-1);
}
.cf-tester-profile.rejected { opacity: 0.65; }
.cf-tester-profile-name { flex: 1; }
.cf-tester-profile-score { font-family: var(--font-mono); font-weight: 600; color: var(--gold); }
.cf-tester-profile.rejected .cf-tester-profile-score { color: var(--bad); }
.cf-tester-profile-flag { font-size: 10.5px; color: var(--bad); }

@media (max-width: 960px) {
  .cf-head { display: none; }
  .cf-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 6px; }
  .cf-tags { grid-column: 1 / -1; }
}
</style>

<!-- Portaled dialog content + shared chip styles (dialogs render outside this
     page's scope; .mgr-form base styles live in the manager layout). -->
<style>
.cf-domain-chip {
  height: 26px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: rgb(var(--ink) / 0.04);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.cf-domain-chip:hover { color: var(--fg-0); }
.cf-domain-chip.active {
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 40%, transparent);
  color: var(--gold-bright);
}

.cf-tags { display: flex; gap: 4px; flex-wrap: wrap; }
.cf-tag {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  padding: 2px 7px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  color: var(--fg-2);
  white-space: nowrap;
}
.cf-tag.negated { border-style: dashed; color: var(--fg-3); }
.cf-tag.gold { color: var(--gold-bright); border-color: color-mix(in srgb, var(--gold) 40%, transparent); background: var(--gold-soft); }

.cf-form-top { display: flex; gap: 12px; align-items: flex-end; }
.cf-form-top .cf-form-name { flex: 1; }

.cf-spec-list { display: flex; flex-direction: column; gap: 8px; }
.cf-spec {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: rgb(var(--ink) / 0.03);
}
.cf-spec-head { display: flex; gap: 8px; align-items: center; }
.cf-spec-impl { min-width: 180px; }
.cf-spec-name { flex: 1; }
.cf-spec-body { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.cf-spec-body > .mgr-input { flex: 1; min-width: 180px; }
.cf-spec-body > .cf-spec-range { flex: 0 0 80px; min-width: 80px; }
.cf-spec-flag {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 11.5px; color: var(--fg-2);
  white-space: nowrap;
}
.cf-spec-note { font-size: 11.5px; color: var(--fg-3); font-style: italic; }
.cf-spec-add { align-self: flex-start; margin-top: 8px; }

.cf-import-modes { display: flex; gap: 6px; }
.cf-import-json { resize: vertical; min-height: 120px; }
.cf-import-result {
  display: flex; flex-direction: column; gap: 4px;
  max-height: 200px;
  overflow-y: auto;
}
.cf-import-line {
  display: flex; align-items: center; gap: 7px;
  margin: 0;
  font-size: 12px;
}
.cf-import-line.ok { color: var(--good); }
.cf-import-line.warn { color: var(--fg-2); }
.cf-import-line.warn svg { color: var(--warn, var(--gold)); }
</style>
