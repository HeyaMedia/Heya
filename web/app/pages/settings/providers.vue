<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()

type OSValue = {
  api_key?: string
  username?: string
  password?: string
}

const os = reactive<OSValue>({ api_key: '', username: '', password: '' })
const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const testResult = ref<{ ok: boolean; user?: any; error?: string } | null>(null)
const { flash } = useFlash()

const canTest = computed(() => !!(os.api_key && os.username && os.password))
const canSave = computed(() => !!(os.api_key && os.username && os.password))

async function load() {
  try {
    const res = await $heya('/api/system-settings/{key}', { path: { key: 'opensubtitles' } }) as any
    const v = res?.value as OSValue | undefined
    if (v) {
      os.api_key  = v.api_key  ?? ''
      os.username = v.username ?? ''
      os.password = v.password ?? ''
    }
  } catch { /* not configured yet */ } finally {
    loading.value = false
  }
}

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
    flash.value = { kind: 'ok', text: 'OpenSubtitles credentials saved.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Save failed.' }
  } finally {
    saving.value = false
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

onMounted(load)
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Providers</h2>
      <p class="sv2-page-desc">
        External metadata + subtitle services. The Heya Media aggregator
        fronts everything except subtitles, which still need a per-user
        OpenSubtitles account.
      </p>
    </header>

    <SettingsSection title="Heya Media aggregator" icon="database"
      description="The upstream metadata router. All movie / TV / music / book metadata reaches Heya through it — there are no direct outbound clients in this binary."
      lockedBy="HEYA_MEDIA_*">
      <KVTable :rows="[
        { key: 'Base URL',  value: 'https://heya.media', mono: true, copy: true },
        { key: 'Client',    value: 'internal/metadata/heyamedia', mono: true },
        { key: 'Authentication', value: 'API key (env-managed)' },
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
          description="Found under your opensubtitles.com profile → API Consumers.">
          <input v-model="os.api_key" type="text" class="sv2-input" placeholder="Your OpenSubtitles API key" autocomplete="off" />
        </SettingsField>
        <SettingsField label="Username">
          <input v-model="os.username" type="text" class="sv2-input" placeholder="opensubtitles username" autocomplete="username" />
        </SettingsField>
        <SettingsField label="Password">
          <input v-model="os.password" type="password" class="sv2-input" placeholder="opensubtitles password" autocomplete="current-password" />
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

        <div v-if="testResult" class="test-card" :class="testResult.ok ? 'ok' : 'err'">
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

</style>
