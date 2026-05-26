<script setup lang="ts">
import type { PlaybackPreference, MediaLanguagesResponse } from '~~/shared/types'
import type { SelectOption } from '~/components/ui/AppSelect.vue'
import { CollapsibleRoot, CollapsibleTrigger, CollapsibleContent } from 'reka-ui'

const props = defineProps<{ mediaItemId: number; alwaysOpen?: boolean }>()

const loading = ref(true)
const saving = ref(false)
const languages = ref<MediaLanguagesResponse | null>(null)
const pref = ref<PlaybackPreference>({ media_item_id: 0, audio_language: '', subtitle_language: '', subtitle_mode: '' })
const expanded = ref(props.alwaysOpen ?? false)

const hasCustomPref = computed(() => !!(pref.value.audio_language || pref.value.subtitle_language || pref.value.subtitle_mode))

const LANG_LABELS: Record<string, string> = {
  eng: 'English', jpn: 'Japanese', ger: 'German', fre: 'French', spa: 'Spanish',
  ita: 'Italian', por: 'Portuguese', rus: 'Russian', kor: 'Korean', chi: 'Chinese',
  ara: 'Arabic', hin: 'Hindi', dan: 'Danish', swe: 'Swedish', nor: 'Norwegian',
  fin: 'Finnish', dut: 'Dutch', pol: 'Polish', tur: 'Turkish', tha: 'Thai',
  vie: 'Vietnamese', und: 'Unknown', zho: 'Chinese', deu: 'German', fra: 'French',
  nld: 'Dutch', nob: 'Norwegian', ces: 'Czech', hun: 'Hungarian', ron: 'Romanian',
  hrv: 'Croatian', srp: 'Serbian', ukr: 'Ukrainian', heb: 'Hebrew', ind: 'Indonesian',
  may: 'Malay', fil: 'Filipino', lat: 'Latin',
}

function langLabel(code: string) {
  return LANG_LABELS[code] || code.toUpperCase()
}

// Reka's Select treats an empty string as "no value" and falls back to the
// placeholder rather than rendering the matching item. Use a non-empty
// "default" sentinel for the zero-state row across all three selects; the
// loader/saver normalises this back to '' before talking to the API.
const DEFAULT_VALUE = 'default'

const SUB_MODE_OPTIONS: SelectOption[] = [
  { value: DEFAULT_VALUE, label: 'Use default' },
  { value: 'auto', label: 'Auto' },
  { value: 'always', label: 'Always on' },
  { value: 'forced_only', label: 'Forced only' },
  { value: 'off', label: 'Off' },
]

const audioOptions = computed<SelectOption[]>(() => {
  const list: SelectOption[] = [{ value: DEFAULT_VALUE, label: 'Default' }]
  for (const l of languages.value?.audio_languages || []) {
    list.push({ value: l.code, label: langLabel(l.code), meta: l.count > 1 ? String(l.count) : undefined })
  }
  return list
})

const subOptions = computed<SelectOption[]>(() => {
  const list: SelectOption[] = [{ value: DEFAULT_VALUE, label: 'Default' }]
  for (const l of languages.value?.subtitle_languages || []) {
    list.push({ value: l.code, label: langLabel(l.code), meta: l.count > 1 ? String(l.count) : undefined })
  }
  return list
})

const toApi = (v: string) => v === DEFAULT_VALUE ? '' : v
const fromApi = (v: string) => v === '' ? DEFAULT_VALUE : v

// Local v-model values that use the sentinel; the watch above keeps them in
// sync with the API-shaped pref object. We wire them to AppSelect.
const audioLang = computed({
  get: () => fromApi(pref.value.audio_language),
  set: v => { pref.value.audio_language = toApi(v) },
})
const subLang = computed({
  get: () => fromApi(pref.value.subtitle_language),
  set: v => { pref.value.subtitle_language = toApi(v) },
})
const subMode = computed({
  get: () => fromApi(pref.value.subtitle_mode),
  set: v => { pref.value.subtitle_mode = toApi(v) },
})

const hasAudioOptions = computed(() => (languages.value?.audio_languages?.length ?? 0) > 0)
const hasSubOptions = computed(() => (languages.value?.subtitle_languages?.length ?? 0) > 0)

async function loadData() {
  loading.value = true
  try {
    const { $heya } = useNuxtApp()
    const [langs, prefData] = await Promise.all([
      $heya('/api/media/{id}/languages', { path: { id: props.mediaItemId } }) as Promise<MediaLanguagesResponse>,
      $heya('/api/me/playback/{media_id}', { path: { media_id: props.mediaItemId } }) as Promise<PlaybackPreference>,
    ])
    languages.value = langs
    pref.value = prefData
  } catch {}
  loading.value = false
}

async function savePref() {
  saving.value = true
  try {
    const { $heya } = useNuxtApp()
    pref.value = await $heya('/api/me/playback/{media_id}', {
      method: 'PUT',
      path: { media_id: props.mediaItemId },
      body: {
        audio_language: pref.value.audio_language,
        subtitle_language: pref.value.subtitle_language,
        subtitle_mode: pref.value.subtitle_mode,
      } as any,
    }) as PlaybackPreference
  } catch {}
  saving.value = false
}

function reset() {
  pref.value = { media_item_id: props.mediaItemId, audio_language: '', subtitle_language: '', subtitle_mode: '' }
  savePref()
}

onMounted(loadData)
</script>

<template>
  <CollapsibleRoot
    v-if="!loading && languages && (hasAudioOptions || hasSubOptions)"
    v-model:open="expanded"
    class="pp"
    :class="{ 'pp-inline': alwaysOpen }"
    :disabled="alwaysOpen"
  >
    <CollapsibleTrigger v-if="!alwaysOpen" class="pp-toggle">
      <Icon name="translate" :size="14" />
      <span>Audio &amp; Subtitles</span>
      <span v-if="hasCustomPref" class="pp-custom-badge">Custom</span>
      <Icon name="chevdown" :size="12" class="pp-toggle-chev" />
    </CollapsibleTrigger>

    <CollapsibleContent class="pp-collapsible">
      <div class="pp-body" :class="{ 'pp-body-inline': alwaysOpen }">
        <div v-if="hasAudioOptions" class="pp-row">
          <label class="pp-label">Audio</label>
          <AppSelect
            v-model="audioLang"
            :options="audioOptions"
            aria-label="Audio language"
            :custom-baseline="DEFAULT_VALUE"
            @change="savePref"
          />
        </div>

        <div v-if="hasSubOptions" class="pp-row">
          <label class="pp-label">Subtitles</label>
          <AppSelect
            v-model="subLang"
            :options="subOptions"
            aria-label="Subtitle language"
            :custom-baseline="DEFAULT_VALUE"
            @change="savePref"
          />
        </div>

        <div class="pp-row">
          <label class="pp-label">Sub mode</label>
          <AppSelect
            v-model="subMode"
            :options="SUB_MODE_OPTIONS"
            aria-label="Subtitle mode"
            :custom-baseline="DEFAULT_VALUE"
            @change="savePref"
          />
        </div>

        <button v-if="hasCustomPref" class="pp-reset" @click="reset">
          <Icon name="close" :size="11" /> Reset to defaults
        </button>
      </div>
    </CollapsibleContent>
  </CollapsibleRoot>
</template>

<style scoped>
.pp {
  margin-top: 16px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: visible;
}

.pp-toggle {
  display: flex; align-items: center; gap: 8px;
  width: 100%; padding: 10px 14px;
  font-size: 12px; font-weight: 500;
  color: var(--fg-1);
  transition: background 0.12s;
}
.pp-toggle:hover { background: rgba(255,255,255,0.02); }
.pp-toggle > :last-child { margin-left: auto; color: var(--fg-3); }
.pp-toggle-chev { transition: transform 0.2s; }
.pp-toggle[data-state="open"] .pp-toggle-chev { transform: rotate(180deg); }

/* CollapsibleContent reveal — reka exposes `--reka-collapsible-content-height`
   as a CSS var on the content element so we can animate to the resolved
   height without measuring in JS. Same pattern as the music sidebar. */
.pp-collapsible {
  overflow: hidden;
}
.pp-collapsible[data-state="open"] {
  animation: pp-fold-down 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}
.pp-collapsible[data-state="closed"] {
  animation: pp-fold-up 0.18s cubic-bezier(0.4, 0, 1, 1);
}
@keyframes pp-fold-down {
  from { height: 0; opacity: 0; }
  to   { height: var(--reka-collapsible-content-height); opacity: 1; }
}
@keyframes pp-fold-up {
  from { height: var(--reka-collapsible-content-height); opacity: 1; }
  to   { height: 0; opacity: 0; }
}

.pp-custom-badge {
  font-size: 9px; font-weight: 700; font-family: var(--font-mono);
  padding: 1px 6px; border-radius: 3px;
  background: var(--gold-soft); color: var(--gold);
  text-transform: uppercase; letter-spacing: 0.04em;
}

.pp-body {
  padding: 0 14px 14px;
  border-top: 1px solid var(--border);
  display: flex; flex-direction: column; gap: 8px;
}

.pp-row {
  display: grid; grid-template-columns: 76px minmax(0, 1fr);
  align-items: center; gap: 12px;
  margin-top: 8px;
}
.pp-body > .pp-row:first-child,
.pp-body-inline > .pp-row:first-child { margin-top: 0; }

.pp-label {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: rgba(255,255,255,0.55);
}

.pp-reset {
  display: inline-flex; align-items: center; gap: 4px;
  margin-top: 8px;
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  color: rgba(255,255,255,0.4);
  transition: color 0.12s;
  align-self: flex-start;
}
.pp-reset:hover { color: var(--bad); }


.pp-inline { border: none; margin-top: 0; }
.pp-body-inline { border-top: none; padding: 0; }
</style>
