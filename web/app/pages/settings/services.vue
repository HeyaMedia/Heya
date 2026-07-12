<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import { musicServicesQuery, type MusicServiceImportState } from '~/queries/settings'

const { $heya } = useNuxtApp()
const { flash } = useFlash()

const servicesData = useQuery(musicServicesQuery())
const services = computed(() => servicesData.data.value ?? [])
const lbToken = ref('')
const lfUsername = ref('')
const busy = ref(false)
const lfAuthToken = ref('') // in-flight Last.fm connect handshake

const lb = computed(() => services.value.find(s => s.service === 'listenbrainz'))
const lf = computed(() => services.value.find(s => s.service === 'lastfm'))
const importing = computed(() => services.value.some(s => s.import_state?.status === 'running'))
const connectedCount = computed(() => services.value.filter(service => service.token_set).length)
const scrobblingCount = computed(() => services.value.filter(service => service.token_set && service.scrobble_enabled).length)
const historySourceCount = computed(() => services.value.filter(service => (service.import_state?.imported ?? 0) > 0).length)

async function load() {
  await servicesData.refetch().catch(() => undefined)
  if (!lfUsername.value && lf.value?.username) lfUsername.value = lf.value.username
}

let timer: ReturnType<typeof setInterval> | null = null
watch(importing, (active) => {
  if (active && !timer) timer = setInterval(load, 3000)
  if (!active && timer) {
    clearInterval(timer)
    timer = null
  }
}, { immediate: true })
watch(lf, value => {
  if (!lfUsername.value && value?.username) lfUsername.value = value.username
}, { immediate: true })
onUnmounted(() => { if (timer) clearInterval(timer) })

async function saveService(service: 'listenbrainz' | 'lastfm', body: Record<string, unknown>) {
  busy.value = true
  try {
    await $heya('/api/me/music-services/{service}', { method: 'PUT', path: { service }, body: body as any })
    if (service === 'listenbrainz') lbToken.value = ''
    await load()
    flash.value = { kind: 'ok', text: 'Saved' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail || 'Save failed' }
  } finally {
    busy.value = false
  }
}

async function startImport(service: 'listenbrainz' | 'lastfm') {
  busy.value = true
  try {
    await $heya('/api/me/music-services/{service}/import', { method: 'POST', path: { service } })
    await load()
    flash.value = { kind: 'ok', text: 'Import started — listens matched to your library appear as play history' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail || 'Import failed to start' }
  } finally {
    busy.value = false
  }
}

async function lastfmConnect() {
  busy.value = true
  try {
    const r = await $heya('/api/me/music-services/lastfm/auth-start', { method: 'POST' }) as { auth_url: string; token: string }
    lfAuthToken.value = r.token
    window.open(r.auth_url, '_blank', 'noopener')
    flash.value = { kind: 'ok', text: 'Approve Heya on Last.fm, then press "Finish connecting"' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail || 'Last.fm connect unavailable' }
  } finally {
    busy.value = false
  }
}

async function lastfmComplete() {
  if (!lfAuthToken.value) return
  busy.value = true
  try {
    await $heya('/api/me/music-services/lastfm/auth-complete', { method: 'POST', body: { token: lfAuthToken.value } })
    lfAuthToken.value = ''
    await load()
    flash.value = { kind: 'ok', text: 'Last.fm connected' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail || 'Last.fm connect failed — did you approve it?' }
  } finally {
    busy.value = false
  }
}

function importSummary(st?: MusicServiceImportState): string {
  if (!st?.status || st.status === 'idle') return ''
  if (st.status === 'running') return `Importing… ${st.scanned ?? 0} scanned · ${st.matched ?? 0} matched · ${st.imported ?? 0} new plays`
  if (st.status === 'done') return `Last import: ${st.scanned ?? 0} scanned · ${st.matched ?? 0} matched · ${st.imported ?? 0} new plays`
  return `Import failed: ${st.error ?? 'unknown error'}`
}
</script>

<template>
  <div class="svc-page">
    <SettingsContextHero
      title="Music services"
      icon="music"
      eyebrow="Your listening history"
      :tone="connectedCount ? 'connected' : 'accent'"
      description="Connect the places that already know what you listen to. Heya can import that history and keep future plays in sync."
    >
      <div class="context-fact"><strong>{{ connectedCount }} / 2</strong><span>Connected</span></div>
      <div class="context-fact"><strong>{{ scrobblingCount }}</strong><span>Scrobbling</span></div>
      <div class="context-fact"><strong>{{ importing ? 'Active' : historySourceCount }}</strong><span>{{ importing ? 'Import' : 'Imported' }}</span></div>
    </SettingsContextHero>

    <div v-if="servicesData.isLoading.value" class="svc-loading">
      <Icon name="spinner" :size="15" /> Checking connected services…
    </div>

    <div v-else class="service-grid">
      <SettingsSection title="ListenBrainz" icon="music" description="Open listening, connected with a personal user token.">
        <template #actions>
          <StatusBadge :state="lb?.token_set ? 'ok' : 'idle'">{{ lb?.token_set ? 'Connected' : 'Not connected' }}</StatusBadge>
        </template>
        <div class="service-intro">
          <div class="service-mark listenbrainz">LB</div>
          <div>
            <strong>{{ lb?.username || 'ListenBrainz account' }}</strong>
            <span>{{ lb?.token_set ? 'Your token is stored securely on the server.' : 'Find your user token under ListenBrainz → Settings.' }}</span>
          </div>
        </div>
        <div class="svc-row">
          <input v-model="lbToken" type="password" class="svc-input" :placeholder="lb?.token_set ? 'Replace saved token…' : 'Paste ListenBrainz user token'">
          <button class="sv2-btn primary" :disabled="busy || !lbToken.trim()" @click="saveService('listenbrainz', { token: lbToken.trim() })">
            {{ lb?.token_set ? 'Update' : 'Connect' }}
          </button>
        </div>
        <div v-if="lb?.token_set" class="svc-controls">
          <div class="svc-toggle">
            <span>Send new Heya plays</span>
            <AppSwitch :model-value="lb?.scrobble_enabled" size="md" aria-label="Scrobble Heya plays to ListenBrainz" @update:model-value="saveService('listenbrainz', { scrobble_enabled: $event })" />
          </div>
          <button class="sv2-btn ghost" :disabled="busy || importing" @click="startImport('listenbrainz')">
            <Icon name="download" :size="13" /> Import history
          </button>
        </div>
        <p v-if="importSummary(lb?.import_state)" class="svc-import-state" :class="{ error: lb?.import_state?.status === 'failed' }">{{ importSummary(lb?.import_state) }}</p>
      </SettingsSection>

      <SettingsSection title="Last.fm" icon="radio" description="Import a public profile, then connect your account to scrobble new plays.">
        <template #actions>
          <StatusBadge :state="lf?.token_set ? 'ok' : 'idle'">{{ lf?.token_set ? 'Connected' : 'Not connected' }}</StatusBadge>
        </template>
        <div class="service-intro">
          <div class="service-mark lastfm">fm</div>
          <div>
            <strong>{{ lf?.username || 'Last.fm account' }}</strong>
            <span>{{ lf?.token_set ? 'Authorised for scrobbling and history.' : 'A username is enough to import public listening history.' }}</span>
          </div>
        </div>
        <div class="svc-row">
          <input v-model="lfUsername" type="text" class="svc-input" placeholder="Last.fm username">
          <button class="sv2-btn primary" :disabled="busy || !lfUsername.trim()" @click="saveService('lastfm', { username: lfUsername.trim() })">Save</button>
        </div>
        <div class="svc-controls">
          <div v-if="lf?.token_set" class="svc-toggle">
            <span>Send new Heya plays</span>
            <AppSwitch :model-value="lf?.scrobble_enabled" size="md" aria-label="Scrobble Heya plays to Last.fm" @update:model-value="saveService('lastfm', { scrobble_enabled: $event })" />
          </div>
          <template v-else>
            <button class="sv2-btn ghost" :disabled="busy" @click="lastfmConnect"><Icon name="globe" :size="13" /> Connect account</button>
            <button v-if="lfAuthToken" class="sv2-btn primary" :disabled="busy" @click="lastfmComplete">Finish connecting</button>
          </template>
          <button class="sv2-btn ghost" :disabled="busy || importing || !(lf?.username || lfUsername.trim())" @click="startImport('lastfm')">
            <Icon name="download" :size="13" /> Import history
          </button>
        </div>
        <p v-if="importSummary(lf?.import_state)" class="svc-import-state" :class="{ error: lf?.import_state?.status === 'failed' }">{{ importSummary(lf?.import_state) }}</p>
      </SettingsSection>
    </div>

    <aside class="history-note">
      <div class="history-note-icon"><Icon name="sparkle" :size="18" /></div>
      <div>
        <strong>One history, better recommendations</strong>
        <p>Imported listens power Mixes for You, recommendations, and music intelligence. Heya only imports plays it can match to your library, and safely skips duplicates when an import is repeated.</p>
      </div>
    </aside>
  </div>
</template>

<style scoped>
.svc-page { display: flex; flex-direction: column; }
.service-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; align-items: start; }
.service-grid :deep(.sv2-section) { height: calc(100% - 16px); }
.svc-loading { display: flex; align-items: center; gap: 8px; padding: 28px; border: 1px solid var(--border); border-radius: var(--r-lg); color: var(--fg-3); font-size: 12.5px; }
.service-intro { display: flex; align-items: center; gap: 11px; margin-bottom: 14px; padding: 11px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-2); }
.service-intro > div:last-child { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.service-intro strong { color: var(--fg-0); font-size: 12.5px; font-weight: 620; }
.service-intro span { color: var(--fg-2); font-size: 11px; line-height: 1.4; }
.service-mark { width: 36px; height: 36px; display: grid; place-items: center; flex: none; border-radius: 10px; color: white; font-size: 11px; font-weight: 800; letter-spacing: -0.03em; }
.service-mark.listenbrainz { background: linear-gradient(135deg, #353070, #eb743b); }
.service-mark.lastfm { background: linear-gradient(135deg, #d92323, #8d0707); }
.svc-row { display: flex; gap: 10px; align-items: center; }
.svc-input { flex: 1; min-width: 0; padding: 10px 14px; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); color: var(--fg-0); font-size: 13px; outline: none; transition: border-color 0.15s; }
.svc-input:focus { border-color: var(--gold); }
.svc-input::placeholder { color: var(--fg-4); }
.svc-controls { display: flex; align-items: center; gap: 9px; margin-top: 12px; flex-wrap: wrap; }
.svc-toggle { display: inline-flex; align-items: center; gap: 9px; padding: 7px 9px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-2); font-size: 12px; color: var(--fg-1); }
.svc-import-state { margin: 10px 0 0; font-size: 11px; line-height: 1.5; font-family: var(--font-mono); color: var(--fg-2); }
.svc-import-state.error { color: var(--bad); }
.history-note { display: flex; align-items: flex-start; gap: 12px; padding: 15px 17px; border: 1px solid color-mix(in srgb, var(--gold) 20%, var(--border)); border-radius: var(--r-md); background: color-mix(in srgb, var(--gold) 5%, var(--bg-1)); }
.history-note-icon { width: 34px; height: 34px; display: grid; place-items: center; flex: none; border-radius: 10px; background: var(--gold-soft); color: var(--gold); }
.history-note strong { color: var(--fg-0); font-size: 12.5px; }
.history-note p { margin: 3px 0 0; color: var(--fg-2); font-size: 11.5px; line-height: 1.5; }
@media (max-width: 940px) {
  .service-grid { grid-template-columns: 1fr; gap: 0; }
  .service-grid :deep(.sv2-section) { height: auto; }
}
@media (max-width: 520px) {
  .svc-row { align-items: stretch; flex-direction: column; }
  .svc-row .sv2-btn, .svc-controls .sv2-btn { justify-content: center; }
  .svc-controls { align-items: stretch; flex-direction: column; }
  .svc-toggle { justify-content: space-between; }
}
</style>
