<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

type NamingSettings = {
  movie: string
  tv: string
  daily_tv: string
  anime: string
  music: string
  music_multi: string
}
type NamingToken = { token: string, description: string, example: string }
type NamingView = { settings: NamingSettings, tokens: NamingToken[], examples: Record<string, string> }
type NamingKind = 'movie' | 'tv' | 'music'

const { $heya } = useNuxtApp()
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const activeKind = ref<NamingKind>('movie')
const tokenSearch = ref('')
const view = ref<NamingView | null>(null)
const form = reactive<NamingSettings>({ movie: '', tv: '', daily_tv: '', anime: '', music: '', music_multi: '' })

const sections: { key: keyof NamingSettings, kind: NamingKind, label: string, detail: string }[] = [
  { key: 'movie', kind: 'movie', label: 'Movie format', detail: 'The path and filename below the selected movie library root.' },
  { key: 'tv', kind: 'tv', label: 'Standard episode format', detail: 'Season and episode-numbered television.' },
  { key: 'daily_tv', kind: 'tv', label: 'Daily episode format', detail: 'Date-numbered television.' },
  { key: 'anime', kind: 'tv', label: 'Anime episode format', detail: 'Season, episode, and absolute numbering.' },
  { key: 'music', kind: 'music', label: 'Track format', detail: 'Single-medium releases.' },
  { key: 'music_multi', kind: 'music', label: 'Multi-disc track format', detail: 'Releases containing multiple media/discs.' },
]

const visibleSections = computed(() => sections.filter(section => section.kind === activeKind.value))
const visibleTokens = computed(() => {
  const query = tokenSearch.value.trim().toLowerCase()
  return (view.value?.tokens ?? []).filter(token => !query || `${token.token} ${token.description}`.toLowerCase().includes(query))
})
const dirty = computed(() => view.value != null && sections.some(section => form[section.key] !== view.value!.settings[section.key]))

async function load() {
  loading.value = true
  try {
    view.value = await $heya('/api/manager/file-naming') as NamingView
    Object.assign(form, view.value.settings)
  } catch (e: any) {
    error.value = e?.data?.detail ?? e?.message ?? 'Could not load file naming settings.'
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    view.value = await $heya('/api/manager/file-naming', { method: 'PUT', body: { ...form } }) as NamingView
    Object.assign(form, view.value.settings)
    saved.value = true
  } catch (e: any) {
    error.value = e?.data?.detail ?? e?.message ?? 'Could not save file naming settings.'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <SettingsContextHero
      title="File naming"
      icon="file-naming"
      eyebrow="Manager · Import output"
      description="Define the exact folders and filenames Heya will produce. Queue imports use these templates for both their preview and the real move."
    />

    <div v-if="loading" class="naming-empty"><span class="mgr-spin" /> Loading templates…</div>
    <template v-else-if="view">
      <div class="naming-overview">
        <div><strong>Arr-style templates</strong><span>Compatible labels and conditional wrappers</span></div>
        <div><strong>Preview first</strong><span>Every move is shown in Queue before import</span></div>
        <div><strong>Original extension</strong><span>Preserved automatically and not part of the template</span></div>
      </div>

      <SettingsSection title="Templates" icon="file-naming">
        <div class="naming-tabs" role="tablist" aria-label="Media type">
          <button v-for="tab in ([['movie', 'Movies', 'film'], ['tv', 'TV', 'tv'], ['music', 'Music', 'music']] as const)" :key="tab[0]" class="naming-tab" :class="{ active: activeKind === tab[0] }" @click="activeKind = tab[0]">
            <Icon :name="tab[2]" :size="14" /> {{ tab[1] }}
          </button>
        </div>
        <div class="naming-list">
          <div v-for="section in visibleSections" :key="section.key" class="naming-card">
            <div class="naming-card-head">
              <div>
                <div class="naming-label">{{ section.label }}</div>
                <div class="naming-detail">{{ section.detail }}</div>
              </div>
            </div>
            <textarea v-model="form[section.key]" class="sv2-input naming-template mono" rows="3" spellcheck="false" :aria-label="section.label" />
            <div class="naming-example">
              <span class="naming-example-label mono">Example</span>
              <code>{{ view.examples[section.key] }}</code>
            </div>
          </div>
        </div>
        <div class="naming-actions">
          <span v-if="saved && !dirty" class="naming-saved"><Icon name="check" :size="12" /> Saved</span>
          <span v-else-if="dirty" class="naming-unsaved">Unsaved changes</span>
          <button class="mgr-btn primary" :disabled="saving || !dirty" @click="save">
            <span v-if="saving" class="mgr-spin" />
            <Icon v-else name="check" :size="12" />
            Save templates
          </button>
        </div>
      </SettingsSection>

      <SettingsSection title="Available labels" icon="hash">
        <div class="naming-token-head">
          <p class="naming-help">Labels are case-insensitive. A label with no value disappears. Punctuation inside the same braces is conditional too—for example <code>{-Release Group}</code>.</p>
          <label class="naming-search"><Icon name="search" :size="13" /><input v-model="tokenSearch" placeholder="Filter labels…"></label>
        </div>
        <div class="token-grid">
          <div v-for="token in visibleTokens" :key="token.token" class="token-card">
            <code>{{ token.token }}</code>
            <span>{{ token.description }}</span>
            <small class="mono">{{ token.example }}</small>
          </div>
        </div>
      </SettingsSection>
    </template>

    <div v-if="error" class="mgr-flash err">{{ error }}</div>
  </div>
</template>

<style scoped>
.naming-empty { display: flex; align-items: center; gap: 8px; padding: 16px; color: var(--fg-3); }
.naming-overview { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; margin: 0 0 12px; }
.naming-overview > div { display: grid; gap: 3px; padding: 12px 14px; border: 1px solid var(--hair); border-radius: var(--r-md); background: var(--bg-2); }
.naming-overview strong { font-size: 11.5px; color: var(--fg-1); }
.naming-overview span { font-size: 10.5px; line-height: 1.35; color: var(--fg-3); }
.naming-tabs { display: flex; gap: 4px; margin-bottom: 12px; padding: 4px; width: fit-content; border: 1px solid var(--hair); border-radius: var(--r-md); background: var(--bg-2); }
.naming-tab { display: inline-flex; align-items: center; gap: 6px; padding: 7px 12px; border: 0; border-radius: var(--r-sm); color: var(--fg-3); background: transparent; cursor: pointer; font: inherit; font-size: 11.5px; }
.naming-tab:hover { color: var(--fg-1); }
.naming-tab.active { color: var(--fg-0); background: var(--bg-4); box-shadow: inset 0 0 0 1px var(--hair); }
.naming-list { display: grid; gap: 10px; }
.naming-card { padding: 14px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-2); }
.naming-card-head { display: flex; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.naming-label { font-size: 13.5px; font-weight: 650; color: var(--fg-0); }
.naming-detail { margin-top: 2px; font-size: 11.5px; color: var(--fg-3); }
.naming-template { width: 100%; min-height: 76px; resize: vertical; font-size: 11.5px; line-height: 1.5; }
.naming-example { display: grid; grid-template-columns: 70px minmax(0, 1fr); gap: 8px; margin-top: 8px; align-items: baseline; }
.naming-example-label { font-size: 9px; letter-spacing: .12em; text-transform: uppercase; color: var(--fg-3); }
.naming-example code, .naming-help code, .token-card code { font-family: var(--font-mono); color: var(--gold-bright); overflow-wrap: anywhere; }
.naming-example code { font-size: 10.5px; }
.naming-actions { display: flex; justify-content: flex-end; align-items: center; gap: 10px; margin-top: 12px; }
.naming-saved { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; color: var(--good); }
.naming-unsaved { font-size: 11px; color: var(--gold-bright); }
.naming-token-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.naming-help { margin: 0 0 12px; font-size: 12px; line-height: 1.5; color: var(--fg-2); }
.naming-token-head .naming-help { margin-bottom: 0; }
.naming-search { display: flex; align-items: center; gap: 7px; min-width: 210px; padding: 7px 9px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-2); color: var(--fg-3); }
.naming-search:focus-within { border-color: var(--gold-dim); }
.naming-search input { width: 100%; border: 0; outline: 0; background: transparent; color: var(--fg-1); font: inherit; font-size: 11px; }
.token-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 8px; }
.token-card { display: grid; grid-template-columns: minmax(140px, auto) 1fr; gap: 4px 10px; padding: 10px 12px; border: 1px solid var(--hair); border-radius: var(--r-sm); background: var(--bg-2); }
.token-card code { font-size: 10.5px; }
.token-card span { font-size: 11px; color: var(--fg-2); }
.token-card small { grid-column: 1 / -1; font-size: 9.5px; color: var(--fg-3); }
.mono { font-family: var(--font-mono); }
@media (max-width: 760px) {
  .naming-overview { grid-template-columns: 1fr; }
  .naming-token-head { display: grid; }
  .naming-search { min-width: 0; }
}
</style>
