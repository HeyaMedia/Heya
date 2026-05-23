<script setup lang="ts">
import type { PlaybackPreference, MediaLanguagesResponse } from '~~/shared/types'
import type { DropdownOption } from '~/components/ui/Dropdown.vue'

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

const SUB_MODE_OPTIONS: DropdownOption[] = [
  { value: '', label: 'Use default' },
  { value: 'auto', label: 'Auto' },
  { value: 'always', label: 'Always on' },
  { value: 'forced_only', label: 'Forced only' },
  { value: 'off', label: 'Off' },
]

const audioOptions = computed<DropdownOption[]>(() => {
  const list: DropdownOption[] = [{ value: '', label: 'Default' }]
  for (const l of languages.value?.audio_languages || []) {
    list.push({ value: l.code, label: langLabel(l.code), meta: l.count > 1 ? String(l.count) : undefined })
  }
  return list
})

const subOptions = computed<DropdownOption[]>(() => {
  const list: DropdownOption[] = [{ value: '', label: 'Default' }]
  for (const l of languages.value?.subtitle_languages || []) {
    list.push({ value: l.code, label: langLabel(l.code), meta: l.count > 1 ? String(l.count) : undefined })
  }
  return list
})

const hasAudioOptions = computed(() => (languages.value?.audio_languages?.length ?? 0) > 0)
const hasSubOptions = computed(() => (languages.value?.subtitle_languages?.length ?? 0) > 0)

async function loadData() {
  loading.value = true
  try {
    const [langs, prefData] = await Promise.all([
      apiFetch<MediaLanguagesResponse>(`/api/media/${props.mediaItemId}/languages`),
      apiFetch<PlaybackPreference>(`/api/user/playback/${props.mediaItemId}`),
    ])
    languages.value = langs
    pref.value = prefData
  } catch {}
  loading.value = false
}

async function savePref() {
  saving.value = true
  try {
    pref.value = await apiFetch<PlaybackPreference>(`/api/user/playback/${props.mediaItemId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        audio_language: pref.value.audio_language,
        subtitle_language: pref.value.subtitle_language,
        subtitle_mode: pref.value.subtitle_mode,
      }),
    })
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
  <div v-if="!loading && languages && (hasAudioOptions || hasSubOptions)" class="pp" :class="{ 'pp-inline': alwaysOpen }">
    <button v-if="!alwaysOpen" class="pp-toggle" @click="expanded = !expanded">
      <Icon name="translate" :size="14" />
      <span>Audio &amp; Subtitles</span>
      <span v-if="hasCustomPref" class="pp-custom-badge">Custom</span>
      <Icon name="chevdown" :size="12" :style="{ transform: expanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
    </button>

    <Transition name="fold">
      <div v-if="expanded" class="pp-body" :class="{ 'pp-body-inline': alwaysOpen }">
        <div v-if="hasAudioOptions" class="pp-row">
          <label class="pp-label">Audio</label>
          <Dropdown
            v-model="pref.audio_language"
            :options="audioOptions"
            aria-label="Audio language"
            @change="savePref"
          />
        </div>

        <div v-if="hasSubOptions" class="pp-row">
          <label class="pp-label">Subtitles</label>
          <Dropdown
            v-model="pref.subtitle_language"
            :options="subOptions"
            aria-label="Subtitle language"
            @change="savePref"
          />
        </div>

        <div class="pp-row">
          <label class="pp-label">Sub mode</label>
          <Dropdown
            v-model="pref.subtitle_mode"
            :options="SUB_MODE_OPTIONS"
            aria-label="Subtitle mode"
            @change="savePref"
          />
        </div>

        <button v-if="hasCustomPref" class="pp-reset" @click="reset">
          <Icon name="close" :size="11" /> Reset to defaults
        </button>
      </div>
    </Transition>
  </div>
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

.fold-enter-active { transition: all 0.2s ease-out; }
.fold-leave-active { transition: all 0.15s ease-in; }
.fold-enter-from { opacity: 0; max-height: 0; }
.fold-leave-to { opacity: 0; max-height: 0; }

.pp-inline { border: none; margin-top: 0; }
.pp-body-inline { border-top: none; padding: 0; }
</style>
