<script setup lang="ts">
definePageMeta({ layout: 'settings' })

const { $heya } = useNuxtApp()
const { flash } = useFlash()

type ImportState = { status?: string; imported?: number; matched?: number; unmatched?: number; scanned?: number; error?: string }
type ServiceView = { service: 'listenbrainz' | 'lastfm'; username: string; token_set: boolean; scrobble_enabled: boolean; import_state: ImportState }

const services = ref<ServiceView[]>([])
const lbToken = ref('')
const lfUsername = ref('')
const busy = ref(false)
const lfAuthToken = ref('') // in-flight Last.fm connect handshake

const lb = computed(() => services.value.find(s => s.service === 'listenbrainz'))
const lf = computed(() => services.value.find(s => s.service === 'lastfm'))
const importing = computed(() => services.value.some(s => s.import_state?.status === 'running'))

async function load() {
  try {
    const r = await $heya('/api/me/music-services') as { services: ServiceView[] }
    services.value = r.services
    if (!lfUsername.value && lf.value?.username) lfUsername.value = lf.value.username
  } catch { /* poll failure — keep last snapshot */ }
}

let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  load()
  timer = setInterval(load, 3000)
})
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

function importSummary(st?: ImportState): string {
  if (!st?.status || st.status === 'idle') return ''
  if (st.status === 'running') return `Importing… ${st.scanned ?? 0} scanned · ${st.matched ?? 0} matched · ${st.imported ?? 0} new plays`
  if (st.status === 'done') return `Last import: ${st.scanned ?? 0} scanned · ${st.matched ?? 0} matched · ${st.imported ?? 0} new plays`
  return `Import failed: ${st.error ?? 'unknown error'}`
}
</script>

<template>
  <div class="svc-page">
    <SettingsSection
      title="ListenBrainz" icon="music"
      description="Link with your user token (listenbrainz.org → Settings). Import pulls your full listen history into Heya's play history; scrobbling submits everything you play here."
    >
      <div class="svc-row">
        <input
          v-model="lbToken"
          type="password"
          class="svc-input"
          :placeholder="lb?.token_set ? `Token saved — linked as ${lb?.username || '…'}` : 'ListenBrainz user token'"
        >
        <button class="sv2-btn primary" :disabled="busy || !lbToken.trim()" @click="saveService('listenbrainz', { token: lbToken.trim() })">
          Link
        </button>
      </div>
      <div v-if="lb?.token_set" class="svc-controls">
        <label class="svc-toggle">
          <input
            type="checkbox"
            :checked="lb?.scrobble_enabled"
            @change="saveService('listenbrainz', { scrobble_enabled: ($event.target as HTMLInputElement).checked })"
          >
          Scrobble my Heya plays to ListenBrainz
        </label>
        <button class="sv2-btn ghost" :disabled="busy || importing" @click="startImport('listenbrainz')">
          <Icon name="download" :size="13" /> Import listen history
        </button>
      </div>
      <p v-if="importSummary(lb?.import_state)" class="svc-import-state" :class="{ error: lb?.import_state?.status === 'failed' }">
        {{ importSummary(lb?.import_state) }}
      </p>
    </SettingsSection>

    <SettingsSection
      title="Last.fm" icon="music"
      description="History import needs your Last.fm username (public profile). Scrobbling needs the server to have HEYA_LASTFM_API_KEY / HEYA_LASTFM_SECRET set, then connect your account."
    >
      <div class="svc-row">
        <input
          v-model="lfUsername"
          type="text"
          class="svc-input"
          placeholder="Last.fm username"
        >
        <button class="sv2-btn primary" :disabled="busy || !lfUsername.trim()" @click="saveService('lastfm', { username: lfUsername.trim() })">
          Save
        </button>
      </div>
      <div class="svc-controls">
        <template v-if="lf?.token_set">
          <label class="svc-toggle">
            <input
              type="checkbox"
              :checked="lf?.scrobble_enabled"
              @change="saveService('lastfm', { scrobble_enabled: ($event.target as HTMLInputElement).checked })"
            >
            Scrobble my Heya plays to Last.fm
          </label>
        </template>
        <template v-else>
          <button class="sv2-btn ghost" :disabled="busy" @click="lastfmConnect">
            <Icon name="globe" :size="13" /> Connect account
          </button>
          <button v-if="lfAuthToken" class="sv2-btn primary" :disabled="busy" @click="lastfmComplete">
            Finish connecting
          </button>
        </template>
        <button class="sv2-btn ghost" :disabled="busy || importing || !(lf?.username || lfUsername.trim())" @click="startImport('lastfm')">
          <Icon name="download" :size="13" /> Import scrobble history
        </button>
      </div>
      <p v-if="importSummary(lf?.import_state)" class="svc-import-state" :class="{ error: lf?.import_state?.status === 'failed' }">
        {{ importSummary(lf?.import_state) }}
      </p>
    </SettingsSection>

    <SettingsSection title="Why link these?" icon="sparkle"
      description="Imported listens and scrobbles become play history, which powers your taste profile — Mixes for You, recommendations, and the AI music tools all learn from what you actually play and love.">
      <p class="svc-note">Only listens that match tracks in your library are imported (exact MusicBrainz recording match first, artist + title match second). Re-running an import is safe — already-imported listens are skipped.</p>
    </SettingsSection>
  </div>
</template>

<style scoped>
.svc-page { display: flex; flex-direction: column; gap: 18px; }
.svc-row { display: flex; gap: 10px; align-items: center; }
.svc-input {
  flex: 1; padding: 10px 14px;
  background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md);
  color: var(--fg-0); font-size: 13px; outline: none; transition: border-color 0.15s;
}
.svc-input:focus { border-color: var(--gold); }
.svc-input::placeholder { color: var(--fg-4); }
.svc-controls { display: flex; align-items: center; gap: 16px; margin-top: 12px; flex-wrap: wrap; }
.svc-toggle { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-1); cursor: pointer; }
.svc-import-state { margin: 10px 0 0; font-size: 12px; font-family: var(--font-mono); color: var(--fg-2); }
.svc-import-state.error { color: #e06c5c; }
.svc-note { margin: 0; font-size: 13px; line-height: 1.5; color: var(--fg-2); }
</style>
