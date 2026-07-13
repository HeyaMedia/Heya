<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import type { components } from '#open-fetch-schemas/heya'
import { librariesQuery } from '~/queries/catalog'
import { userPlaybackSettingsQuery } from '~/queries/settings'
type UserSettings = components['schemas']['UserSettings']
type LibraryView  = components['schemas']['LibraryView']

const { $heya } = useNuxtApp()

const settings = ref<UserSettings | null>(null)
const persistedSettings = ref('')
const settingsData = useQuery(userPlaybackSettingsQuery())
const librariesData = useQuery(librariesQuery())
const libraries = computed(() => (librariesData.data.value ?? []) as LibraryView[])
const loading = computed(() => settingsData.isLoading.value || librariesData.isLoading.value)
const saving = ref(false)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

// 'global' or a library id (as string, since the override map is keyed by id-string).
const activeTab = ref<string>('global')

const SUB_MODES = [
  { value: 'auto',   label: 'Automatic',    desc: 'Show subtitles when the audio language differs from your preference.' },
  { value: 'forced', label: 'Forced only',  desc: 'Only show forced subtitles (translated foreign dialogue).' },
  { value: 'on',     label: 'Always on',    desc: 'Always show subtitles when available.' },
  { value: 'off',    label: 'Off',          desc: 'Never show subtitles.' },
] as const

const QUALITIES = [
  { value: 'auto',   label: 'Automatic (recommended)' },
  { value: 'source', label: 'Source (no transcoding)' },
  { value: '2160',   label: '4K · 2160p' },
  { value: '1080',   label: '1080p' },
  { value: '720',    label: '720p' },
  { value: '480',    label: '480p' },
] as const

const SUB_PRIORITY_LABELS: Record<string, string> = {
  ass: 'ASS (rich formatting)',
  srt: 'SRT',
  subrip: 'SubRip',
  webvtt: 'WebVTT',
  pgs: 'PGS (image-based)',
}

watch(() => settingsData.data.value, value => {
  if (value) {
    settings.value = structuredClone(value)
    // Defensive: backend may omit library_overrides on a fresh account.
    if (settings.value && !settings.value.playback.library_overrides) {
      settings.value.playback.library_overrides = {}
    }
    persistedSettings.value = JSON.stringify(settings.value)
  }
}, { immediate: true })

const hasChanges = computed(() => !!settings.value && JSON.stringify(settings.value) !== persistedSettings.value)
const activeLibrary = computed(() => libraries.value.find(library => String(library.id) === activeTab.value))
const qualityLabel = computed(() => (QUALITIES.find(quality => quality.value === settings.value?.playback.default_quality)?.label ?? 'Automatic').replace(' (recommended)', ''))
const subtitleModeLabel = computed(() => SUB_MODES.find(mode => mode.value === settings.value?.playback.subtitle_mode)?.label ?? 'Automatic')

async function save() {
  if (!settings.value) return
  saving.value = true
  flash.value = null
  try {
    // Strip empty override rows before persisting so the saved shape stays
    // tidy. An override row is "empty" when every field is either undefined
    // or the empty string and the priority list is empty.
    const overrides = settings.value.playback.library_overrides ?? {}
    for (const [id, ov] of Object.entries(overrides)) {
      const empty =
        !ov?.default_audio_language &&
        !ov?.default_subtitle_language &&
        !ov?.subtitle_mode &&
        (!ov?.subtitle_priority || ov.subtitle_priority.length === 0)
      if (empty) delete overrides[id]
    }

    settings.value = await $heya('/api/me/settings', {
      method: 'PUT',
      body: settings.value,
    })
    persistedSettings.value = JSON.stringify(settings.value)
    await settingsData.refetch()
    flash.value = { kind: 'ok', text: 'Playback preferences saved.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to save.' }
  } finally {
    saving.value = false
  }
}

function movePriority(list: string[], idx: number, dir: -1 | 1) {
  const next = idx + dir
  if (next < 0 || next >= list.length) return
  const a = list[idx]
  const b = list[next]
  if (a === undefined || b === undefined) return
  list[idx] = b
  list[next] = a
}

// Override accessor: returns the (possibly empty) override row for the active
// library tab, lazily creating the row if absent. Vue's reactivity picks up
// the new key via the v-model binding.
function getOverride(libId: string) {
  const overrides = settings.value?.playback.library_overrides ?? {}
  if (!overrides[libId]) overrides[libId] = {}
  return overrides[libId]
}

function clearOverride(libId: string) {
  const overrides = settings.value?.playback.library_overrides ?? {}
  delete overrides[libId]
  // re-create empty so the v-model targets still work
  overrides[libId] = {}
  flash.value = { kind: 'ok', text: 'Library overrides cleared — save to persist.' }
}

function overrideCount(libId: string): number {
  const ov = settings.value?.playback.library_overrides?.[libId]
  if (!ov) return 0
  let n = 0
  if (ov.default_audio_language) n++
  if (ov.default_subtitle_language) n++
  if (ov.subtitle_mode) n++
  if (ov.subtitle_priority?.length) n++
  return n
}

const totalOverridesUsed = computed(() => {
  let n = 0
  const map = settings.value?.playback.library_overrides ?? {}
  for (const k of Object.keys(map)) if (overrideCount(k) > 0) n++
  return n
})

function libraryIcon(kind: string): string {
  switch (kind) {
    case 'movie': return 'film'
    case 'tv':
    case 'anime': return 'tv'
    case 'music': return 'music'
    case 'book':  return 'book'
    default:      return 'folder'
  }
}

</script>

<template>
  <div>
    <SettingsContextHero
      title="Playback"
      icon="eq"
      eyebrow="Synced to your account"
      description="Set one reliable playback baseline, then bend the rules only for libraries that need different languages or subtitles."
    >
      <div class="context-fact"><strong>{{ qualityLabel }}</strong><span>Quality</span></div>
      <div class="context-fact"><strong>{{ subtitleModeLabel }}</strong><span>Subtitles</span></div>
      <div class="context-fact"><strong>{{ totalOverridesUsed }}</strong><span>Overrides</span></div>
    </SettingsContextHero>

    <div v-if="loading" class="loading-state">
      <Icon name="spinner" :size="16" /> Loading…
    </div>

    <template v-else-if="settings">
      <div class="tab-bar" role="tablist" aria-label="Playback settings scope">
        <span class="tab-label">Scope</span>
        <button
          id="playback-tab-global"
          class="tab"
          role="tab"
          :aria-selected="activeTab === 'global'"
          :tabindex="activeTab === 'global' ? 0 : -1"
          aria-controls="playback-panel"
          :class="{ active: activeTab === 'global' }"
          @click="activeTab = 'global'"
        >
          <Icon name="settings" :size="13" />
          <span>Global</span>
        </button>
        <button
          v-for="l in libraries"
          :key="l.id"
          :id="`playback-tab-${l.id}`"
          class="tab"
          role="tab"
          :aria-selected="activeTab === String(l.id)"
          :tabindex="activeTab === String(l.id) ? 0 : -1"
          aria-controls="playback-panel"
          :class="{ active: activeTab === String(l.id) }"
          @click="activeTab = String(l.id)"
        >
          <Icon :name="libraryIcon(l.media_type)" :size="13" />
          <span>{{ l.name }}</span>
          <span v-if="overrideCount(String(l.id)) > 0" class="tab-badge">{{ overrideCount(String(l.id)) }}</span>
        </button>
      </div>

      <!-- GLOBAL DEFAULTS -->
      <template v-if="activeTab === 'global'">
        <div id="playback-panel" role="tabpanel" aria-labelledby="playback-tab-global" tabindex="0">
        <SettingsSection title="Languages" icon="translate"
          description="ISO 639-1/2 codes — eng, jpn, fre, etc. Leave empty for 'no preference'.">
          <SettingsField label="Default audio language" v-slot="{ fieldId }">
            <input :id="fieldId" v-model="settings.playback.default_audio_language" class="sv2-input small" placeholder="eng" maxlength="8" />
          </SettingsField>
          <SettingsField label="Default subtitle language" v-slot="{ fieldId }">
            <input :id="fieldId" v-model="settings.playback.default_subtitle_language" class="sv2-input small" placeholder="eng" maxlength="8" />
          </SettingsField>
        </SettingsSection>

        <SettingsSection title="Subtitle mode" icon="subtitles">
          <div class="radio-grid" role="radiogroup" aria-label="Subtitle mode">
            <label
              v-for="m in SUB_MODES"
              :key="m.value"
              class="radio-card"
              :class="{ active: settings.playback.subtitle_mode === m.value }"
            >
              <input type="radio" name="subtitle-mode" :value="m.value" v-model="settings.playback.subtitle_mode" />
              <div class="radio-body">
                <div class="radio-title">{{ m.label }}</div>
                <div class="radio-desc">{{ m.desc }}</div>
              </div>
            </label>
          </div>
        </SettingsSection>

        <SettingsSection title="Subtitle format priority" icon="captions"
          description="When multiple subtitle tracks exist for the same language, prefer formats in this order.">
          <ul class="priority-list">
            <li v-for="(fmt, i) in (settings.playback.subtitle_priority ?? [])" :key="fmt" class="priority-item">
              <span class="priority-rank">{{ i + 1 }}</span>
              <span class="priority-label">{{ SUB_PRIORITY_LABELS[fmt] ?? fmt }}</span>
              <span class="priority-spacer" />
              <button class="priority-btn" :disabled="i === 0" @click="movePriority(settings.playback.subtitle_priority ?? [], i, -1)" title="Move up" aria-label="Move up">
                <Icon name="chevright" :size="12" style="transform: rotate(-90deg)" />
              </button>
              <button class="priority-btn" :disabled="i === (settings.playback.subtitle_priority?.length ?? 0) - 1" @click="movePriority(settings.playback.subtitle_priority ?? [], i, 1)" title="Move down" aria-label="Move down">
                <Icon name="chevright" :size="12" style="transform: rotate(90deg)" />
              </button>
            </li>
          </ul>
        </SettingsSection>

        <SettingsSection title="Quality" icon="film"
          description="Cap the maximum playback quality. 'Source' streams the original file; lower values trigger ffmpeg HLS transcoding.">
          <SettingsField label="Default quality" v-slot="{ fieldId }">
            <select :id="fieldId" v-model="settings.playback.default_quality" class="sv2-select">
              <option v-for="q in QUALITIES" :key="q.value" :value="q.value">{{ q.label }}</option>
            </select>
          </SettingsField>
        </SettingsSection>

        <div v-if="totalOverridesUsed > 0" class="overrides-summary">
          <Icon name="info" :size="13" />
          <span>{{ totalOverridesUsed }} {{ totalOverridesUsed === 1 ? 'library has' : 'libraries have' }} per-library overrides active.</span>
        </div>
        </div>
      </template>

      <!-- LIBRARY OVERRIDES -->
      <template v-else>
        <div id="playback-panel" role="tabpanel" :aria-labelledby="`playback-tab-${activeTab}`" tabindex="0">
        <SettingsSection
          :title="`${activeLibrary?.name ?? 'Library'} overrides`"
          :icon="activeLibrary ? libraryIcon(activeLibrary.media_type) : 'folder'"
          description="Each field falls back to the global default when left blank. Empty fields are stripped on save."
        >
          <template #actions>
            <button
              v-if="overrideCount(activeTab) > 0"
              class="sv2-btn ghost"
              @click="clearOverride(activeTab)"
            >
              <Icon name="trash" :size="12" /> Clear overrides
            </button>
          </template>

          <SettingsField label="Default audio language" v-slot="{ fieldId }">
            <input :id="fieldId" v-model="getOverride(activeTab).default_audio_language" class="sv2-input small" placeholder="(use global)" maxlength="8" />
          </SettingsField>
          <SettingsField label="Default subtitle language" v-slot="{ fieldId }">
            <input :id="fieldId" v-model="getOverride(activeTab).default_subtitle_language" class="sv2-input small" placeholder="(use global)" maxlength="8" />
          </SettingsField>
          <SettingsField label="Subtitle mode" v-slot="{ fieldId }">
            <select :id="fieldId" v-model="getOverride(activeTab).subtitle_mode" class="sv2-select">
              <option value="">Use global default</option>
              <option v-for="m in SUB_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
            </select>
          </SettingsField>
        </SettingsSection>

        <p class="library-note">
          Quality + subtitle format priority are global-only — they apply across every library.
        </p>
        </div>
      </template>

      <div class="save-bar" :class="{ dirty: hasChanges }">
        <div v-if="flash" class="pw-flash" :class="flash.kind">
          <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
          {{ flash.text }}
        </div>
        <div v-else class="save-state">
          <span class="save-dot" />
          {{ hasChanges ? 'You have unsaved playback changes' : 'Playback settings are up to date' }}
        </div>
        <span class="save-spacer" />
        <button class="sv2-btn primary" :disabled="saving || !hasChanges" @click="save">
          <Icon v-if="saving" name="spinner" :size="13" />
          {{ saving ? 'Saving…' : 'Save changes' }}
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.loading-state { display: flex; align-items: center; gap: 8px; color: var(--fg-3); font-size: 13px; padding: 20px 0; }

.tab-bar {
  display: flex;
  gap: 4px;
  margin-bottom: 24px;
  padding-bottom: 0;
  border-bottom: 1px solid var(--border);
  overflow-x: auto;
  scrollbar-width: none;
}
.tab-label {
  align-self: center;
  padding: 0 8px 0 2px;
  color: var(--fg-4);
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  white-space: nowrap;
}
.tab-bar::-webkit-scrollbar { display: none; }
.tab {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 8px 14px;
  margin-bottom: -1px;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--fg-3);
  border-bottom: 2px solid transparent;
  cursor: pointer;
  white-space: nowrap;
  transition: color 0.12s, border-color 0.12s;
}
.tab:hover { color: var(--fg-1); }
.tab.active {
  color: var(--gold);
  border-bottom-color: var(--gold);
}
.tab-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--gold-soft);
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
}
.tab.active .tab-badge { background: var(--gold); color: var(--accent-ink); }

.overrides-summary {
  display: flex; align-items: center; gap: 8px;
  margin-top: 16px;
  padding: 10px 14px;
  background: rgba(140, 160, 255, 0.06);
  border: 1px solid rgba(140, 160, 255, 0.20);
  border-radius: var(--r-sm);
  font-size: 12px;
  color: rgb(140, 160, 255);
}

.library-note {
  margin: 0;
  padding: 10px 14px;
  font-size: 11.5px;
  color: var(--fg-3);
  font-style: italic;
  background: rgb(var(--ink) / 0.02);
  border: 1px dashed var(--border);
  border-radius: var(--r-sm);
}

.radio-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 8px;
}
.radio-card {
  display: flex;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.radio-card:hover { border-color: var(--border-strong); }
.radio-card.active {
  border-color: var(--gold);
  background: var(--gold-soft);
}
.radio-card input { margin-top: 4px; accent-color: var(--gold); }
.radio-body { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.radio-title { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.radio-desc { font-size: 11.5px; color: var(--fg-3); line-height: 1.4; }

.priority-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
.priority-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.priority-rank {
  width: 22px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-3);
  background: var(--bg-0);
  border-radius: var(--r-xs);
}
.priority-label { font-size: 12.5px; color: var(--fg-1); }
.priority-spacer { flex: 1; }
.priority-btn {
  width: 24px; height: 24px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-xs);
  color: var(--fg-3);
  transition: background 0.12s, color 0.12s;
}
.priority-btn:hover:not(:disabled) { background: rgb(var(--ink) / 0.04); color: var(--fg-1); }
.priority-btn:disabled { opacity: 0.3; cursor: not-allowed; }

.sv2-input {
  width: 100%;
  max-width: 380px;
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-mono);
  transition: border-color 0.12s;
}
.sv2-input.small { max-width: 180px; }
.sv2-input:focus { outline: none; border-color: var(--gold); background: var(--bg-1); }

.sv2-select {
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-sans);
  min-width: 280px;
}
.sv2-select:focus { outline: none; border-color: var(--gold); }

.save-bar {
  position: sticky;
  z-index: 5;
  bottom: 56px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-top: 18px;
  background: color-mix(in srgb, var(--bg-1) 90%, transparent);
  box-shadow: 0 10px 34px rgb(0 0 0 / 0.16);
  backdrop-filter: blur(16px);
}
.save-bar.dirty { border-color: color-mix(in srgb, var(--gold) 28%, var(--border)); }
.save-state { display: flex; align-items: center; gap: 8px; color: var(--fg-3); font-size: 11.5px; }
.save-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--good); }
.save-bar.dirty .save-dot { background: var(--gold); box-shadow: 0 0 9px color-mix(in srgb, var(--gold) 55%, transparent); }
.save-spacer { flex: 1; }

.pw-flash {
  padding: 8px 12px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.pw-flash.ok {
  background: color-mix(in srgb, var(--good) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--good) 25%, transparent);
  color: var(--good);
}
.pw-flash.err {
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
  color: var(--bad);
}

.sv2-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 9px 18px;
  border-radius: var(--r-sm);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}

@media (max-width: 720px) {
  .sv2-select { min-width: 0; width: 100%; }
  .save-bar { position: static; bottom: auto; flex-wrap: wrap; box-shadow: none; }
  .save-state, .pw-flash { flex: 1 1 100%; }
}
</style>
