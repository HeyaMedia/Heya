<template>
  <div class="sp-stack">
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
        <div class="toggle-row" @click="toggleBool('watch')">
          <div class="toggle-info">
            <span class="toggle-name">Watch for file changes</span>
            <span class="toggle-desc">Automatically scan when files are added or removed</span>
          </div>
          <AppSwitch
            :model-value="local.watch"
            size="sm"
            aria-label="Watch for file changes"
            @click.stop
            @update:model-value="toggleBool('watch')"
          />
        </div>
        <div class="toggle-row" @click="toggleBool('fetch_ratings')">
          <div class="toggle-info">
            <span class="toggle-name">Fetch external ratings</span>
            <span class="toggle-desc">Pull IMDb, TMDB, and other ratings from heya.media</span>
          </div>
          <AppSwitch
            :model-value="local.fetch_ratings"
            size="sm"
            aria-label="Fetch external ratings"
            @click.stop
            @update:model-value="toggleBool('fetch_ratings')"
          />
        </div>
        <div v-if="mediaType === 'movie'" class="toggle-row" @click="toggleBool('auto_collections')">
          <div class="toggle-info">
            <span class="toggle-name">Auto collections</span>
            <span class="toggle-desc">Group movies by franchise automatically</span>
          </div>
          <AppSwitch
            :model-value="local.auto_collections"
            size="sm"
            aria-label="Auto collections"
            @click.stop
            @update:model-value="toggleBool('auto_collections')"
          />
        </div>
        <div class="toggle-row" @click="toggleBool('save_nfo')">
          <div class="toggle-info">
            <span class="toggle-name">Write NFO files</span>
            <span class="toggle-desc">Save metadata as NFO files alongside media</span>
          </div>
          <AppSwitch
            :model-value="local.save_nfo"
            size="sm"
            aria-label="Write NFO files"
            @click.stop
            @update:model-value="toggleBool('save_nfo')"
          />
        </div>
        <div class="toggle-row" @click="toggleBool('save_images')">
          <div class="toggle-info">
            <span class="toggle-name">Write artwork files</span>
            <span class="toggle-desc">Save poster and backdrop images to media directories</span>
          </div>
          <AppSwitch
            :model-value="local.save_images"
            size="sm"
            aria-label="Write artwork files"
            @click.stop
            @update:model-value="toggleBool('save_images')"
          />
        </div>
        <div v-if="mediaType === 'movie' || mediaType === 'tv'" class="toggle-row" @click="toggleBool('enable_trickplay')">
          <div class="toggle-info">
            <span class="toggle-name">Trickplay thumbnails</span>
            <span class="toggle-desc">Generate seek preview sprites for the video player</span>
          </div>
          <AppSwitch
            :model-value="local.enable_trickplay"
            size="sm"
            aria-label="Trickplay thumbnails"
            @click.stop
            @update:model-value="toggleBool('enable_trickplay')"
          />
        </div>
        <div v-if="mediaType === 'movie' || mediaType === 'tv'" class="toggle-row" @click="toggleBool('generate_thumbnails')">
          <div class="toggle-info">
            <span class="toggle-name">Generate missing thumbnails</span>
            <span class="toggle-desc">Extract video frames for extras and episodes without artwork</span>
          </div>
          <AppSwitch
            :model-value="local.generate_thumbnails"
            size="sm"
            aria-label="Generate missing thumbnails"
            @click.stop
            @update:model-value="toggleBool('generate_thumbnails')"
          />
        </div>
      </div>
      <p class="sp-footnote">
        <Icon name="refresh" :size="11" />
        Metadata is fetched from heya.media and auto-refreshes on its own —
        every 14 days while a title is still active, every 180 days once it has
        ended.
      </p>
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
  preferred_language: 'en',
  preferred_country: 'US',
  auto_collections: false,
  fetch_ratings: true,
  save_nfo: false,
  save_images: false,
  enable_trickplay: false,
  generate_thumbnails: true,
})

function syncFromProps() {
  const s = props.modelValue
  local.watch = s.watch ?? false
  local.preferred_language = s.preferred_language || 'en'
  local.preferred_country = s.preferred_country || 'US'
  local.auto_collections = s.auto_collections ?? false
  local.fetch_ratings = s.fetch_ratings ?? true
  local.save_nfo = s.save_nfo ?? false
  local.save_images = s.save_images ?? false
  local.enable_trickplay = s.enable_trickplay ?? false
  local.generate_thumbnails = s.generate_thumbnails ?? true
}

watch(() => props.modelValue, syncFromProps, { immediate: true, deep: true })

function emitUpdate() {
  emit('update:modelValue', { ...toRaw(local) })
}

function toggleBool(field: 'watch' | 'auto_collections' | 'fetch_ratings' | 'save_nfo' | 'save_images' | 'enable_trickplay' | 'generate_thumbnails') {
  local[field] = !local[field]
  emitUpdate()
}

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
.sp-stack {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

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

.sp-section-icon.locale { background: rgba(140, 220, 180, 0.1); color: rgb(140, 220, 180); }
.sp-section-icon.opts { background: rgba(200, 140, 255, 0.1); color: rgb(200, 140, 255); }

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

.sp-footnote {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 10px 2px 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--fg-3);
}
.sp-footnote :deep(svg) { flex-shrink: 0; margin-top: 2px; color: var(--fg-3); }

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
</style>
