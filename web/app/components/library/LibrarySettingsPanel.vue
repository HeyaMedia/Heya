<template>
  <div class="sp">
    <div class="sp-section">
      <div class="sp-section-head">
        <div class="sp-section-icon meta"><Icon name="database" :size="13" /></div>
        <div>
          <h4 class="sp-label">Metadata Providers</h4>
          <p class="sp-hint">Sources for matching and fetching metadata</p>
        </div>
      </div>
      <div class="chip-group">
        <button
          v-for="p in metadataProviders"
          :key="p"
          type="button"
          class="sp-chip"
          :class="{ active: local.metadata_providers.includes(p) }"
          @click="toggleChip('metadata_providers', p)"
        >
          <span class="sp-chip-check">
            <Icon :name="local.metadata_providers.includes(p) ? 'check' : 'plus'" :size="10" />
          </span>
          {{ providerLabel(p) }}
        </button>
      </div>
    </div>

    <div v-if="artworkProviders.length" class="sp-section">
      <div class="sp-section-head">
        <div class="sp-section-icon art"><Icon name="star" :size="13" /></div>
        <div>
          <h4 class="sp-label">Artwork Providers</h4>
          <p class="sp-hint">Sources for posters, backdrops, and logos</p>
        </div>
      </div>
      <div class="chip-group">
        <button
          v-for="p in artworkProviders"
          :key="p"
          type="button"
          class="sp-chip"
          :class="{ active: local.artwork_providers.includes(p) }"
          @click="toggleChip('artwork_providers', p)"
        >
          <span class="sp-chip-check">
            <Icon :name="local.artwork_providers.includes(p) ? 'check' : 'plus'" :size="10" />
          </span>
          {{ providerLabel(p) }}
        </button>
      </div>
    </div>

    <div v-if="ratingsProviders.length" class="sp-section">
      <div class="sp-section-head">
        <div class="sp-section-icon rate"><Icon name="star" :size="13" /></div>
        <div>
          <h4 class="sp-label">Ratings Providers</h4>
          <p class="sp-hint">Supplementary ratings from Rotten Tomatoes, Metacritic, etc.</p>
        </div>
      </div>
      <div class="chip-group">
        <button
          v-for="p in ratingsProviders"
          :key="p"
          type="button"
          class="sp-chip"
          :class="{ active: local.ratings_providers.includes(p) }"
          @click="toggleChip('ratings_providers', p)"
        >
          <span class="sp-chip-check">
            <Icon :name="local.ratings_providers.includes(p) ? 'check' : 'plus'" :size="10" />
          </span>
          {{ providerLabel(p) }}
        </button>
      </div>
    </div>

    <div class="sp-section">
      <div class="sp-section-head">
        <div class="sp-section-icon locale"><Icon name="globe" :size="13" /></div>
        <div>
          <h4 class="sp-label">Locale</h4>
          <p class="sp-hint">Preferred language and region for metadata</p>
        </div>
      </div>
      <div class="locale-row">
        <div class="locale-field">
          <span class="locale-field-label">Language</span>
          <div class="select-wrap">
            <select v-model="local.preferred_language" class="sp-select" @change="emitUpdate">
              <option v-for="l in languages" :key="l.code" :value="l.code">{{ l.label }}</option>
            </select>
            <Icon name="chevdown" :size="11" class="select-icon" />
          </div>
        </div>
        <div class="locale-field">
          <span class="locale-field-label">Region</span>
          <div class="select-wrap">
            <select v-model="local.preferred_country" class="sp-select" @change="emitUpdate">
              <option v-for="c in countries" :key="c.code" :value="c.code">{{ c.label }}</option>
            </select>
            <Icon name="chevdown" :size="11" class="select-icon" />
          </div>
        </div>
      </div>
    </div>

    <div class="sp-section">
      <div class="sp-section-head">
        <div class="sp-section-icon opts"><Icon name="eq" :size="13" /></div>
        <div>
          <h4 class="sp-label">Options</h4>
          <p class="sp-hint">Library behavior and file writing</p>
        </div>
      </div>
      <div class="toggle-list">
        <label class="toggle-row" @click.prevent="toggleBool('watch')">
          <div class="toggle-info">
            <span class="toggle-name">Watch for file changes</span>
            <span class="toggle-desc">Automatically scan when files are added or removed</span>
          </div>
          <div class="toggle-switch" :class="{ on: local.watch }">
            <div class="toggle-knob" />
          </div>
        </label>
        <label v-if="mediaType === 'movie'" class="toggle-row" @click.prevent="toggleBool('auto_collections')">
          <div class="toggle-info">
            <span class="toggle-name">Auto collections</span>
            <span class="toggle-desc">Group movies by franchise automatically</span>
          </div>
          <div class="toggle-switch" :class="{ on: local.auto_collections }">
            <div class="toggle-knob" />
          </div>
        </label>
        <label class="toggle-row" @click.prevent="toggleBool('save_nfo')">
          <div class="toggle-info">
            <span class="toggle-name">Write NFO files</span>
            <span class="toggle-desc">Save metadata as NFO files alongside media</span>
          </div>
          <div class="toggle-switch" :class="{ on: local.save_nfo }">
            <div class="toggle-knob" />
          </div>
        </label>
        <label class="toggle-row" @click.prevent="toggleBool('save_images')">
          <div class="toggle-info">
            <span class="toggle-name">Write artwork files</span>
            <span class="toggle-desc">Save poster and backdrop images to media directories</span>
          </div>
          <div class="toggle-switch" :class="{ on: local.save_images }">
            <div class="toggle-knob" />
          </div>
        </label>
      </div>
    </div>

    <div class="sp-section">
      <div class="sp-section-head">
        <div class="sp-section-icon refresh"><Icon name="refresh" :size="13" /></div>
        <div>
          <h4 class="sp-label">Auto-Refresh Metadata</h4>
          <p class="sp-hint">Periodically check for updated metadata</p>
        </div>
      </div>
      <div class="chip-group">
        <button
          v-for="opt in refreshOptions"
          :key="opt.value"
          type="button"
          class="sp-chip"
          :class="{ active: local.metadata_refresh_days === opt.value }"
          @click="setRefresh(opt.value)"
        >{{ opt.label }}</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { LibrarySettings } from '~~/shared/types'

const props = defineProps<{
  modelValue: LibrarySettings
  mediaType: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: LibrarySettings]
}>()

const local = reactive<LibrarySettings>({
  watch: false,
  metadata_providers: [],
  artwork_providers: [],
  ratings_providers: [],
  preferred_language: 'en',
  preferred_country: 'US',
  auto_collections: false,
  metadata_refresh_days: 0,
  save_nfo: false,
  save_images: false,
})

function syncFromProps() {
  const s = props.modelValue
  local.watch = s.watch ?? false
  local.metadata_providers = [...(s.metadata_providers || [])]
  local.artwork_providers = [...(s.artwork_providers || [])]
  local.ratings_providers = [...(s.ratings_providers || [])]
  local.preferred_language = s.preferred_language || 'en'
  local.preferred_country = s.preferred_country || 'US'
  local.auto_collections = s.auto_collections ?? false
  local.metadata_refresh_days = s.metadata_refresh_days ?? 0
  local.save_nfo = s.save_nfo ?? false
  local.save_images = s.save_images ?? false
}

watch(() => props.modelValue, syncFromProps, { immediate: true, deep: true })

function emitUpdate() {
  emit('update:modelValue', { ...toRaw(local), metadata_providers: [...local.metadata_providers], artwork_providers: [...local.artwork_providers], ratings_providers: [...local.ratings_providers] })
}

function toggleChip(field: 'metadata_providers' | 'artwork_providers' | 'ratings_providers', name: string) {
  const idx = local[field].indexOf(name)
  if (idx >= 0) local[field].splice(idx, 1)
  else local[field].push(name)
  emitUpdate()
}

function toggleBool(field: 'watch' | 'auto_collections' | 'save_nfo' | 'save_images') {
  local[field] = !local[field]
  emitUpdate()
}

function setRefresh(days: number) {
  local.metadata_refresh_days = days
  emitUpdate()
}

const metadataProviders = computed(() => {
  switch (props.mediaType) {
    case 'movie': return ['tmdb', 'anidb']
    case 'tv': return ['tmdb', 'tvdb', 'anidb']
    case 'music': return ['musicbrainz']
    case 'book': return ['openlibrary']
    default: return ['tmdb', 'tvdb', 'anidb', 'musicbrainz', 'openlibrary']
  }
})

const artworkProviders = computed(() => {
  if (props.mediaType === 'music' || props.mediaType === 'book') return []
  return ['tmdb', 'fanart.tv']
})

const ratingsProviders = computed(() => {
  if (props.mediaType === 'music' || props.mediaType === 'book') return []
  return ['omdb']
})

const refreshOptions = [
  { label: 'Never', value: 0 },
  { label: '30 days', value: 30 },
  { label: '45 days', value: 45 },
  { label: '90 days', value: 90 },
]

const providerLabels: Record<string, string> = {
  tmdb: 'TMDB', tvdb: 'TVDB', anidb: 'AniDB', musicbrainz: 'MusicBrainz',
  openlibrary: 'OpenLibrary', 'fanart.tv': 'Fanart.tv', omdb: 'OMDb',
}
function providerLabel(name: string) { return providerLabels[name] || name }

const languages = [
  { code: 'en', label: 'English' },
  { code: 'da', label: 'Danish' },
  { code: 'de', label: 'German' },
  { code: 'es', label: 'Spanish' },
  { code: 'fr', label: 'French' },
  { code: 'it', label: 'Italian' },
  { code: 'ja', label: 'Japanese' },
  { code: 'ko', label: 'Korean' },
  { code: 'nl', label: 'Dutch' },
  { code: 'no', label: 'Norwegian' },
  { code: 'pl', label: 'Polish' },
  { code: 'pt', label: 'Portuguese' },
  { code: 'ru', label: 'Russian' },
  { code: 'sv', label: 'Swedish' },
  { code: 'zh', label: 'Chinese' },
]

const countries = [
  { code: 'US', label: 'United States' },
  { code: 'DK', label: 'Denmark' },
  { code: 'GB', label: 'United Kingdom' },
  { code: 'DE', label: 'Germany' },
  { code: 'ES', label: 'Spain' },
  { code: 'FR', label: 'France' },
  { code: 'IT', label: 'Italy' },
  { code: 'JP', label: 'Japan' },
  { code: 'KR', label: 'South Korea' },
  { code: 'NL', label: 'Netherlands' },
  { code: 'NO', label: 'Norway' },
  { code: 'PL', label: 'Poland' },
  { code: 'PT', label: 'Portugal' },
  { code: 'BR', label: 'Brazil' },
  { code: 'RU', label: 'Russia' },
  { code: 'SE', label: 'Sweden' },
  { code: 'AU', label: 'Australia' },
  { code: 'CA', label: 'Canada' },
]
</script>

<style scoped>
.sp { display: flex; flex-direction: column; gap: 24px; }

.sp-section-head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-bottom: 12px;
}

.sp-section-icon {
  width: 28px;
  height: 28px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  margin-top: 1px;
}

.sp-section-icon.meta { background: rgba(255, 255, 255, 0.05); color: var(--fg-2); }
.sp-section-icon.art { background: rgba(140, 160, 255, 0.1); color: rgb(140, 160, 255); }
.sp-section-icon.rate { background: rgba(255, 180, 100, 0.1); color: rgb(255, 180, 100); }
.sp-section-icon.locale { background: rgba(140, 220, 180, 0.1); color: rgb(140, 220, 180); }
.sp-section-icon.opts { background: rgba(200, 140, 255, 0.1); color: rgb(200, 140, 255); }
.sp-section-icon.refresh { background: var(--gold-soft); color: var(--gold); }

.sp-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  margin: 0;
}

.sp-hint {
  font-size: 11px;
  color: var(--fg-3);
  margin: 2px 0 0;
}

/* Chips */
.chip-group { display: flex; flex-wrap: wrap; gap: 6px; }

.sp-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 7px 14px;
  border-radius: 100px;
  font-size: 12px;
  font-weight: 500;
  background: var(--bg-3);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: all 0.15s ease;
}

.sp-chip:hover {
  border-color: var(--fg-3);
  color: var(--fg-1);
}

.sp-chip.active {
  background: var(--gold-soft);
  border-color: rgba(230, 185, 74, 0.4);
  color: var(--gold-bright);
  font-weight: 600;
}

.sp-chip-check {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.06);
  transition: all 0.15s ease;
}

.sp-chip.active .sp-chip-check {
  background: rgba(230, 185, 74, 0.25);
  color: var(--gold);
}

/* Locale */
.locale-row { display: flex; gap: 12px; }
.locale-field { flex: 1; }
.locale-field-label {
  display: block;
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-3);
  margin-bottom: 4px;
}

.select-wrap { position: relative; }

.sp-select {
  width: 100%;
  height: 38px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 30px 0 12px;
  color: var(--fg-0);
  font-size: 13px;
  appearance: none;
  cursor: pointer;
  transition: border-color 0.12s ease;
}

.sp-select:focus { border-color: var(--gold); outline: none; }

.select-icon {
  position: absolute;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--fg-3);
  pointer-events: none;
}

/* Toggle switches */
.toggle-list {
  display: flex;
  flex-direction: column;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}

.toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 12px 14px;
  cursor: pointer;
  transition: background 0.1s ease;
  border-bottom: 1px solid var(--border);
}

.toggle-row:last-child { border-bottom: none; }
.toggle-row:hover { background: rgba(255, 255, 255, 0.02); }

.toggle-info { flex: 1; min-width: 0; }
.toggle-name { display: block; font-size: 13px; font-weight: 500; color: var(--fg-1); }
.toggle-desc { display: block; font-size: 11px; color: var(--fg-3); margin-top: 1px; }

.toggle-switch {
  width: 38px;
  height: 22px;
  border-radius: 12px;
  background: var(--bg-5);
  position: relative;
  transition: background 0.2s ease;
  flex-shrink: 0;
}

.toggle-switch.on { background: var(--gold); }

.toggle-knob {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--fg-0);
  position: absolute;
  top: 3px;
  left: 3px;
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
}

.toggle-switch.on .toggle-knob {
  transform: translateX(16px);
  background: #1a1408;
}
</style>
