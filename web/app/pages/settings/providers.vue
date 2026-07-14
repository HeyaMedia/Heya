<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()
import { openSubtitlesSettingsQuery } from '~/queries/settings'
import type { OpenSubtitlesSettings as OSValue } from '~/queries/settings'

const os = reactive<OSValue>({ api_key: '', username: '', password: '' })
const settingsData = useQuery(openSubtitlesSettingsQuery())
const loading = computed(() => settingsData.isLoading.value)
const saving = ref(false)
const testing = ref(false)
const testResult = ref<{ ok: boolean; user?: any; error?: string } | null>(null)
const { flash } = useFlash()

const canTest = computed(() => !!(os.api_key && os.username && os.password))
const canSave = computed(() => !!(os.api_key && os.username && os.password))

async function load() {
  try {
    await settingsData.refetch()
  } catch { /* not configured yet */ }
}

watch(() => settingsData.data.value, value => {
  if (!value) return
  os.api_key = value.api_key ?? ''
  os.username = value.username ?? ''
  os.password = value.password ?? ''
}, { immediate: true })

async function testConnection() {
  testing.value = true
  testResult.value = null
  try {
    const res = await $heya('/api/opensubtitles/test', {
      method: 'POST',
      body: { api_key: os.api_key, username: os.username, password: os.password } as any,
    }) as { ok: boolean; user?: any; error?: string }
    testResult.value = res
  } catch (e: any) {
    testResult.value = { ok: false, error: e?.message ?? 'Request failed' }
  } finally {
    testing.value = false
  }
}

async function save() {
  saving.value = true
  flash.value = null
  try {
    await $heya('/api/system-settings/{key}', {
      method: 'PUT',
      path: { key: 'opensubtitles' },
      body: { value: { api_key: os.api_key, username: os.username, password: os.password } } as any,
    })
    await load()
    flash.value = { kind: 'ok', text: 'OpenSubtitles credentials saved.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Save failed.' }
  } finally {
    saving.value = false
  }
}

// Server-level API key pairs stored in the system-settings KV (env vars
// HEYA_LASTFM_* / HEYA_PODCAST_INDEX_* lock these when set — the PUT then
// fails with the env-var name).
const lastfm = reactive({ api_key: '', secret: '' })
const podcastIndex = reactive({ key: '', secret: '' })
const savingKV = ref(false)

async function loadKV() {
  try {
    const r = await $heya('/api/system-settings/{key}', { path: { key: 'lastfm' } }) as any
    if (r?.value) { lastfm.api_key = r.value.api_key ?? ''; lastfm.secret = r.value.secret ?? '' }
  } catch { /* unset */ }
  try {
    const r = await $heya('/api/system-settings/{key}', { path: { key: 'podcast_index' } }) as any
    if (r?.value) { podcastIndex.key = r.value.key ?? ''; podcastIndex.secret = r.value.secret ?? '' }
  } catch { /* unset */ }
}
onMounted(loadKV)

async function saveKV(key: 'lastfm' | 'podcast_index', value: Record<string, string>) {
  savingKV.value = true
  try {
    await $heya('/api/system-settings/{key}', { method: 'PUT', path: { key }, body: { value } as any })
    flash.value = { kind: 'ok', text: 'Saved.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Save failed (env-locked?).' }
  } finally {
    savingKV.value = false
  }
}

const UPSTREAM_SOURCES = [
  { name: 'TMDB',        scope: 'movies + TV',          icon: 'film' },
  { name: 'TVDB',        scope: 'TV episode data',      icon: 'tv' },
  { name: 'MusicBrainz', scope: 'music catalog',        icon: 'music' },
  { name: 'OpenLibrary', scope: 'books + authors',      icon: 'book' },
  { name: 'Fanart.tv',   scope: 'art assets',           icon: 'image' },
  { name: 'OMDb',        scope: 'aggregated ratings',   icon: 'pulse' },
]

</script>

<template>
  <div>
    <SettingsContextHero
      title="Metadata providers"
      icon="globe"
      eyebrow="Media · External services"
      description="See where metadata comes from and configure credentials for services that require a direct account."
    />

    <SettingsSection title="HeyaMetadata V2" icon="database"
      description="The canonical metadata service. It owns provider reconciliation, durable discovery/resolution, freshness, and canonical identities. Community skip segments are fetched directly by Heya."
      lockedBy="HEYA_METADATA_*">
      <KVTable :rows="[
        { key: 'Base URL',  value: 'HEYA_METADATA_URL (default http://localhost:3030)', mono: true },
        { key: 'Client',    value: 'internal/metadata/heyametadata', mono: true },
        { key: 'Authentication', value: 'HEYA_METADATA_API_KEY (optional, env-managed)' },
      ]" />
      <div class="upstream-row">
        <div v-for="s in UPSTREAM_SOURCES" :key="s.name" class="upstream-chip">
          <Icon :name="s.icon" :size="13" />
          <span class="up-name">{{ s.name }}</span>
          <span class="up-scope">{{ s.scope }}</span>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="OpenSubtitles" icon="subtitles"
      description="Subtitle downloads use your personal OpenSubtitles account so rate limits land where they should. API key + credentials are required.">
      <template #actions>
        <a
          href="https://www.opensubtitles.com/en/profile"
          target="_blank"
          rel="noopener"
          class="link-arrow"
        >Get API key <Icon name="chevright" :size="11" /></a>
      </template>

      <div v-if="loading" class="loading-state">
        <Icon name="spinner" :size="14" /> Loading saved credentials…
      </div>

      <template v-else>
        <SettingsField label="API key"
          description="Found under your opensubtitles.com profile → API Consumers."
          v-slot="{ fieldId }">
          <input :id="fieldId" v-model="os.api_key" type="text" class="sv2-input" placeholder="Your OpenSubtitles API key" autocomplete="off" />
        </SettingsField>
        <SettingsField label="Username" v-slot="{ fieldId }">
          <input :id="fieldId" v-model="os.username" type="text" class="sv2-input" placeholder="opensubtitles username" autocomplete="username" />
        </SettingsField>
        <SettingsField label="Password" v-slot="{ fieldId }">
          <input :id="fieldId" v-model="os.password" type="password" class="sv2-input" placeholder="opensubtitles password" autocomplete="current-password" />
        </SettingsField>

        <div class="actions-bar">
          <button class="sv2-btn ghost" :disabled="!canTest || testing" @click="testConnection">
            <Icon :name="testing ? 'spinner' : 'pulse'" :size="13" />
            {{ testing ? 'Testing…' : 'Test connection' }}
          </button>
          <button class="sv2-btn primary" :disabled="!canSave || saving" @click="save">
            <Icon name="check" :size="13" />
            {{ saving ? 'Saving…' : 'Save credentials' }}
          </button>
          <span class="actions-spacer" />
          <span v-if="!canSave" class="hint-text">All three fields required.</span>
        </div>

        <div v-if="testResult" class="test-card" :class="testResult.ok ? 'ok' : 'err'" role="status" aria-live="polite">
          <div class="test-row">
            <span class="test-key">Status</span>
            <StatusBadge :state="testResult.ok ? 'ok' : 'error'">
              {{ testResult.ok ? 'Connected' : 'Failed' }}
            </StatusBadge>
          </div>
          <template v-if="testResult.ok && testResult.user">
            <div class="test-row">
              <span class="test-key">Account</span>
              <span class="test-val">
                {{ testResult.user.level }}
                <span v-if="testResult.user.vip" class="vip">VIP</span>
              </span>
            </div>
            <div class="test-row">
              <span class="test-key">Downloads</span>
              <span class="test-val mono">
                {{ testResult.user.remaining_downloads }} / {{ testResult.user.allowed_downloads }} remaining
              </span>
            </div>
          </template>
          <div v-else-if="testResult.error" class="test-row">
            <span class="test-key">Error</span>
            <span class="test-val mono err-text">{{ testResult.error }}</span>
          </div>
        </div>
      </template>
    </SettingsSection>

    <SettingsSection title="Last.fm" icon="music"
      description="Server-level application key pair used by everyone's Last.fm links (Settings → Music services). History import needs only the key; connecting accounts for scrobbling needs both. Env vars HEYA_LASTFM_API_KEY / HEYA_LASTFM_SECRET override and lock these.">
      <template #actions>
        <a href="https://www.last.fm/api/account/create" target="_blank" rel="noopener" class="link-arrow">Create API account <Icon name="chevright" :size="11" /></a>
      </template>
      <SettingsField label="API key" v-slot="{ fieldId }">
        <input :id="fieldId" v-model="lastfm.api_key" type="text" class="sv2-input" placeholder="Last.fm API key" autocomplete="off" />
      </SettingsField>
      <SettingsField label="Shared secret" v-slot="{ fieldId }">
        <input :id="fieldId" v-model="lastfm.secret" type="password" class="sv2-input" placeholder="Last.fm shared secret" autocomplete="off" />
      </SettingsField>
      <div class="actions-bar">
        <button class="sv2-btn primary" :disabled="savingKV || !lastfm.api_key" @click="saveKV('lastfm', { api_key: lastfm.api_key, secret: lastfm.secret })">
          <Icon name="check" :size="13" /> Save Last.fm keys
        </button>
      </div>
    </SettingsSection>

    <SettingsSection title="Podcast Index" icon="mic"
      description="API key pair for podcast search + trending (podcastindex.org — free). Env vars HEYA_PODCAST_INDEX_KEY / HEYA_PODCAST_INDEX_SECRET override and lock these.">
      <template #actions>
        <a href="https://api.podcastindex.org/signup" target="_blank" rel="noopener" class="link-arrow">Get API key <Icon name="chevright" :size="11" /></a>
      </template>
      <SettingsField label="API key" v-slot="{ fieldId }">
        <input :id="fieldId" v-model="podcastIndex.key" type="text" class="sv2-input" placeholder="Podcast Index API key" autocomplete="off" />
      </SettingsField>
      <SettingsField label="API secret" v-slot="{ fieldId }">
        <input :id="fieldId" v-model="podcastIndex.secret" type="password" class="sv2-input" placeholder="Podcast Index API secret" autocomplete="off" />
      </SettingsField>
      <div class="actions-bar">
        <button class="sv2-btn primary" :disabled="savingKV || !podcastIndex.key" @click="saveKV('podcast_index', { key: podcastIndex.key, secret: podcastIndex.secret })">
          <Icon name="check" :size="13" /> Save Podcast Index keys
        </button>
      </div>
    </SettingsSection>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.upstream-row {
  display: flex; flex-wrap: wrap; gap: 6px;
  margin-top: 12px;
}
.upstream-chip {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 6px 12px;
  border-radius: 999px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  font-size: 11.5px;
}
.up-name { font-weight: 600; color: var(--fg-1); }
.up-scope { color: var(--fg-3); font-family: var(--font-mono); font-size: 11px; }

.sv2-input {
  width: 100%;
  max-width: 460px;
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  outline: none;
  transition: border-color 0.12s;
}
.sv2-input:focus { border-color: var(--gold); background: var(--bg-1); }

.actions-bar {
  display: flex; align-items: center; gap: 10px;
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.actions-spacer { flex: 1; }
.hint-text { font-size: 11.5px; color: var(--fg-4); font-style: italic; }

.test-card {
  margin-top: 14px;
  padding: 14px 18px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
}
.test-card.ok { background: color-mix(in srgb, var(--good) 6%, transparent); border-color: color-mix(in srgb, var(--good) 25%, transparent); }
.test-card.err { background: color-mix(in srgb, var(--bad) 6%, transparent); border-color: color-mix(in srgb, var(--bad) 25%, transparent); }
.test-row { display: flex; align-items: center; gap: 14px; padding: 4px 0; font-size: 13px; }
.test-key {
  width: 110px; flex-shrink: 0;
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3); font-weight: 600;
}
.test-val { color: var(--fg-1); }
.test-val.mono { font-family: var(--font-mono); font-size: 12px; }
.err-text { color: var(--bad); }
.vip {
  display: inline-flex; padding: 1px 6px;
  border-radius: 4px;
  background: var(--gold-soft); color: var(--gold);
  font-size: 9px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.04em;
  margin-left: 6px;
}

.link-arrow {
  display: inline-flex; align-items: center; gap: 2px;
  font-size: 11px; color: var(--fg-3); text-decoration: none;
}
.link-arrow:hover { color: var(--gold); }

@media (max-width: 520px) {
  .actions-bar { align-items: stretch; flex-direction: column; }
  .actions-spacer { display: none; }
  .actions-bar .sv2-btn { justify-content: center; }
  .test-row { align-items: flex-start; flex-direction: column; gap: 3px; padding: 7px 0; }
  .test-key { width: auto; }
}

</style>
