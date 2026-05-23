<script setup lang="ts">
import type { Library } from '~~/shared/types'
import type { PlaybackSettings, LibraryPlaybackOverride, UserSettingsData } from '~/composables/useUserSettings'

const emit = defineEmits<{ close: [] }>()

const { settings, load, save } = useUserSettings()
const { data: libraries } = await useApi<Library[]>('/api/libraries')

await load()

const draft = reactive<UserSettingsData>(JSON.parse(JSON.stringify(settings.value)))
const activeTab = ref<'global' | string>('global')
const saving = ref(false)

const LANGUAGES = [
  { code: '', label: 'Any / Default' },
  { code: 'eng', label: 'English' },
  { code: 'jpn', label: 'Japanese' },
  { code: 'ger', label: 'German' },
  { code: 'fre', label: 'French' },
  { code: 'spa', label: 'Spanish' },
  { code: 'ita', label: 'Italian' },
  { code: 'por', label: 'Portuguese' },
  { code: 'rus', label: 'Russian' },
  { code: 'kor', label: 'Korean' },
  { code: 'chi', label: 'Chinese' },
  { code: 'ara', label: 'Arabic' },
  { code: 'hin', label: 'Hindi' },
  { code: 'dan', label: 'Danish' },
  { code: 'swe', label: 'Swedish' },
  { code: 'nor', label: 'Norwegian' },
  { code: 'fin', label: 'Finnish' },
  { code: 'dut', label: 'Dutch' },
  { code: 'pol', label: 'Polish' },
  { code: 'tur', label: 'Turkish' },
  { code: 'tha', label: 'Thai' },
  { code: 'vie', label: 'Vietnamese' },
]

const SUB_MODES = [
  { value: 'auto', label: 'Auto', desc: 'Show when audio language differs from subtitle language' },
  { value: 'always', label: 'Always', desc: 'Always show subtitles if available' },
  { value: 'forced_only', label: 'Forced Only', desc: 'Only show forced/sign subtitles' },
  { value: 'off', label: 'Off', desc: 'Never auto-select subtitles' },
]

const QUALITIES = [
  { value: 'auto', label: 'Auto (Original)' },
  { value: '2160p', label: '4K (2160p)' },
  { value: '1440p', label: '1440p' },
  { value: '1080p', label: '1080p' },
  { value: '720p', label: '720p' },
  { value: '480p', label: '480p' },
  { value: '360p', label: '360p' },
]

const SUB_CODECS = [
  { code: 'ass', label: 'ASS / SSA' },
  { code: 'srt', label: 'SRT' },
  { code: 'subrip', label: 'SubRip' },
  { code: 'webvtt', label: 'WebVTT' },
  { code: 'pgs', label: 'PGS (Bitmap)' },
]

function getOverride(libId: string): LibraryPlaybackOverride {
  if (!draft.playback.library_overrides[libId]) {
    draft.playback.library_overrides[libId] = {}
  }
  return draft.playback.library_overrides[libId]
}

function moveCodecUp(list: string[], idx: number) {
  if (idx <= 0) return
  const tmp = list[idx - 1]!
  list[idx - 1] = list[idx]!
  list[idx] = tmp
}

function moveCodecDown(list: string[], idx: number) {
  if (idx >= list.length - 1) return
  const tmp = list[idx + 1]!
  list[idx + 1] = list[idx]!
  list[idx] = tmp
}

function codecLabel(code: string) {
  return SUB_CODECS.find(c => c.code === code)?.label ?? code.toUpperCase()
}

async function handleSave() {
  saving.value = true
  for (const [k, v] of Object.entries(draft.playback.library_overrides)) {
    if (!v.default_audio_language && !v.default_subtitle_language && !v.subtitle_mode && !v.subtitle_priority?.length) {
      delete draft.playback.library_overrides[k]
    }
  }
  await save(JSON.parse(JSON.stringify(draft)))
  saving.value = false
  emit('close')
}

function handleOverlay(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('us-overlay')) emit('close')
}
</script>

<template>
  <Teleport to="body">
    <div class="us-overlay" @click="handleOverlay">
      <div class="us-modal">
        <div class="us-header">
          <span class="us-title">Playback Settings</span>
          <button class="us-close" @click="emit('close')"><Icon name="close" :size="18" /></button>
        </div>

        <div class="us-body">
          <!-- Tab bar -->
          <div class="us-tabs">
            <button class="us-tab" :class="{ active: activeTab === 'global' }" @click="activeTab = 'global'">
              Global
            </button>
            <button
              v-for="lib in (libraries || [])"
              :key="lib.id"
              class="us-tab"
              :class="{ active: activeTab === String(lib.id) }"
              @click="activeTab = String(lib.id)"
            >
              {{ lib.name }}
            </button>
          </div>

          <!-- Global settings -->
          <div v-if="activeTab === 'global'" class="us-panel">
            <div class="us-section">
              <div class="us-section-title">Audio</div>
              <label class="us-field">
                <span class="us-label">Preferred Language</span>
                <select v-model="draft.playback.default_audio_language" class="us-select">
                  <option v-for="l in LANGUAGES" :key="l.code" :value="l.code">{{ l.label }}</option>
                </select>
              </label>
            </div>

            <div class="us-section">
              <div class="us-section-title">Subtitles</div>
              <label class="us-field">
                <span class="us-label">Preferred Language</span>
                <select v-model="draft.playback.default_subtitle_language" class="us-select">
                  <option v-for="l in LANGUAGES" :key="l.code" :value="l.code">{{ l.label }}</option>
                </select>
              </label>
              <label class="us-field">
                <span class="us-label">Subtitle Mode</span>
                <select v-model="draft.playback.subtitle_mode" class="us-select">
                  <option v-for="m in SUB_MODES" :key="m.value" :value="m.value">{{ m.label }} — {{ m.desc }}</option>
                </select>
              </label>
              <div class="us-field">
                <span class="us-label">Format Priority</span>
                <span class="us-hint">Drag to reorder. Higher = preferred when multiple formats exist.</span>
                <div class="us-priority-list">
                  <div v-for="(code, i) in draft.playback.subtitle_priority" :key="code" class="us-priority-item">
                    <span class="us-priority-rank">{{ i + 1 }}</span>
                    <span class="us-priority-name">{{ codecLabel(code) }}</span>
                    <div class="us-priority-btns">
                      <button :disabled="i === 0" @click="moveCodecUp(draft.playback.subtitle_priority, i)"><Icon name="chevleft" :size="12" style="transform: rotate(90deg)" /></button>
                      <button :disabled="i === draft.playback.subtitle_priority.length - 1" @click="moveCodecDown(draft.playback.subtitle_priority, i)"><Icon name="chevleft" :size="12" style="transform: rotate(-90deg)" /></button>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="us-section">
              <div class="us-section-title">Quality</div>
              <label class="us-field">
                <span class="us-label">Default Quality</span>
                <select v-model="draft.playback.default_quality" class="us-select">
                  <option v-for="q in QUALITIES" :key="q.value" :value="q.value">{{ q.label }}</option>
                </select>
              </label>
            </div>
          </div>

          <!-- Library override -->
          <div v-else class="us-panel">
            <div class="us-lib-note">
              Override global defaults for this library. Leave blank to use global settings.
            </div>

            <div class="us-section">
              <div class="us-section-title">Audio</div>
              <label class="us-field">
                <span class="us-label">Preferred Language</span>
                <select v-model="getOverride(activeTab).default_audio_language" class="us-select">
                  <option value="">Use global default</option>
                  <option v-for="l in LANGUAGES.slice(1)" :key="l.code" :value="l.code">{{ l.label }}</option>
                </select>
              </label>
            </div>

            <div class="us-section">
              <div class="us-section-title">Subtitles</div>
              <label class="us-field">
                <span class="us-label">Preferred Language</span>
                <select v-model="getOverride(activeTab).default_subtitle_language" class="us-select">
                  <option value="">Use global default</option>
                  <option v-for="l in LANGUAGES.slice(1)" :key="l.code" :value="l.code">{{ l.label }}</option>
                </select>
              </label>
              <label class="us-field">
                <span class="us-label">Subtitle Mode</span>
                <select v-model="getOverride(activeTab).subtitle_mode" class="us-select">
                  <option value="">Use global default</option>
                  <option v-for="m in SUB_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
                </select>
              </label>
            </div>
          </div>
        </div>

        <div class="us-footer">
          <button class="us-btn-cancel" @click="emit('close')">Cancel</button>
          <button class="us-btn-save" :disabled="saving" @click="handleSave">
            {{ saving ? 'Saving…' : 'Save' }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.us-overlay {
  position: fixed; inset: 0; z-index: 9000;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
}

.us-modal {
  width: 560px; max-width: 95vw; max-height: 85vh;
  background: var(--bg-1);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: 0 24px 80px rgba(0,0,0,0.5);
  display: flex; flex-direction: column;
}

.us-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 20px 24px 16px;
  border-bottom: 1px solid var(--border);
}
.us-title { font-size: 16px; font-weight: 600; color: var(--fg-0); }
.us-close { color: var(--fg-3); transition: color 0.12s; }
.us-close:hover { color: var(--fg-0); }

.us-body { flex: 1; overflow-y: auto; padding: 0 24px 16px; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.08) transparent; }

.us-tabs {
  display: flex; gap: 2px;
  padding: 16px 0 12px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 16px;
  overflow-x: auto;
}

.us-tab {
  padding: 6px 14px; border-radius: var(--r-md);
  font-size: 12px; font-weight: 500;
  color: var(--fg-2); white-space: nowrap;
  transition: color 0.12s, background 0.12s;
}
.us-tab:hover { color: var(--fg-0); background: rgba(255,255,255,0.04); }
.us-tab.active { color: var(--gold); background: var(--gold-soft); }

.us-panel { }

.us-lib-note {
  font-size: 12px; color: var(--fg-3);
  padding: 8px 12px; margin-bottom: 16px;
  background: rgba(255,255,255,0.02);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}

.us-section { margin-bottom: 20px; }
.us-section-title {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.08em; color: var(--fg-3); margin-bottom: 10px;
}

.us-field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 12px; }
.us-label { font-size: 13px; font-weight: 500; color: var(--fg-1); }
.us-hint { font-size: 11px; color: var(--fg-3); }

.us-select {
  appearance: none;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 8px 12px;
  font-size: 13px; color: var(--fg-0);
  outline: none;
  transition: border-color 0.12s;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%23666' stroke-width='2' stroke-linecap='round'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  padding-right: 30px;
}
.us-select:focus { border-color: var(--gold); }
.us-select option { background: var(--bg-2); color: var(--fg-0); }

.us-priority-list { display: flex; flex-direction: column; gap: 4px; }
.us-priority-item {
  display: flex; align-items: center; gap: 10px;
  padding: 6px 10px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.us-priority-rank {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  color: var(--fg-3); width: 16px; text-align: center;
}
.us-priority-name { flex: 1; font-size: 13px; color: var(--fg-0); }
.us-priority-btns { display: flex; gap: 2px; }
.us-priority-btns button {
  width: 22px; height: 22px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-xs);
  color: var(--fg-3);
  transition: color 0.1s, background 0.1s;
}
.us-priority-btns button:hover:not(:disabled) { color: var(--fg-0); background: rgba(255,255,255,0.06); }
.us-priority-btns button:disabled { opacity: 0.2; }

.us-footer {
  display: flex; justify-content: flex-end; gap: 10px;
  padding: 16px 24px;
  border-top: 1px solid var(--border);
}
.us-btn-cancel {
  padding: 8px 18px; border-radius: var(--r-md);
  font-size: 13px; font-weight: 500;
  color: var(--fg-2);
  transition: color 0.12s, background 0.12s;
}
.us-btn-cancel:hover { color: var(--fg-0); background: rgba(255,255,255,0.04); }
.us-btn-save {
  padding: 8px 20px; border-radius: var(--r-md);
  font-size: 13px; font-weight: 600;
  color: #1a1408; background: var(--gold);
  transition: opacity 0.12s;
}
.us-btn-save:hover { opacity: 0.9; }
.us-btn-save:disabled { opacity: 0.5; }
</style>
