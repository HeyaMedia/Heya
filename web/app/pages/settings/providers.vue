<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Providers</h2>
      <p class="page-desc">Configure external metadata and subtitle providers</p>
    </div>

    <!-- OpenSubtitles -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="subtitles" :size="14" />
        OpenSubtitles
      </h3>

      <div class="provider-form">
        <div class="form-grid">
          <div class="form-field form-full">
            <label class="form-label">API Key</label>
            <input v-model="os.apiKey" type="text" class="form-input" placeholder="Your OpenSubtitles API key" />
            <span class="form-hint">Required. Get your API key from your opensubtitles.com profile &rarr; API Consumers</span>
          </div>
          <div class="form-field">
            <label class="form-label">Username</label>
            <input v-model="os.username" type="text" class="form-input" placeholder="OpenSubtitles username" />
          </div>
          <div class="form-field">
            <label class="form-label">Password</label>
            <input v-model="os.password" type="password" class="form-input" placeholder="OpenSubtitles password" />
          </div>
        </div>

        <div class="form-actions">
          <button class="btn btn-secondary" :disabled="!canTest || testing" @click="testConnection">
            <Icon v-if="testing" name="loading" :size="14" />
            <Icon v-else name="pulse" :size="14" />
            {{ testing ? 'Testing...' : 'Test Connection' }}
          </button>
          <button class="btn btn-primary" :disabled="!canSave || saving" @click="saveCredentials">
            <Icon name="check" :size="14" />
            {{ saving ? 'Saving...' : 'Save' }}
          </button>
          <span v-if="saved" class="save-confirmation">Saved</span>
          <span v-if="!canSave" class="form-hint">Fill in all fields to enable save</span>
        </div>

        <!-- Status card -->
        <div v-if="testResult" class="status-card" :class="testResult.ok ? 'status-ok' : 'status-error'">
          <div v-if="testResult.ok && testResult.user" class="status-body">
            <div class="status-row">
              <span class="status-label">Status</span>
              <span class="status-val status-good">Connected</span>
            </div>
            <div class="status-row">
              <span class="status-label">Account</span>
              <span class="status-val">
                {{ testResult.user.level }}
                <span v-if="testResult.user.vip" class="vip-badge">VIP</span>
              </span>
            </div>
            <div class="status-row">
              <span class="status-label">Downloads</span>
              <span class="status-val mono">{{ testResult.user.remaining_downloads }} / {{ testResult.user.allowed_downloads }} remaining</span>
            </div>
          </div>
          <div v-else class="status-body">
            <div class="status-row">
              <span class="status-label">Status</span>
              <span class="status-val status-bad">Connection failed</span>
            </div>
            <div v-if="testResult.error" class="status-row">
              <span class="status-label">Error</span>
              <span class="status-val mono">{{ testResult.error }}</span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Future providers -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="globe" :size="14" />
        Other Providers
      </h3>
      <div class="future-cards">
        <div class="future-card">
          <div class="future-icon"><Icon name="film" :size="20" /></div>
          <div class="future-text">
            <div class="future-title">TMDB</div>
            <div class="future-desc">Movie & TV metadata</div>
          </div>
          <span class="future-badge">Coming soon</span>
        </div>
        <div class="future-card">
          <div class="future-icon"><Icon name="tv" :size="20" /></div>
          <div class="future-text">
            <div class="future-title">TVDB</div>
            <div class="future-desc">TV series metadata</div>
          </div>
          <span class="future-badge">Coming soon</span>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
const os = reactive({
  apiKey: '',
  username: '',
  password: '',
})

const testing = ref(false)
const saving = ref(false)
const saved = ref(false)
const testResult = ref<{ ok: boolean; user?: any; error?: string } | null>(null)

const canTest = computed(() => os.apiKey && os.username && os.password)
const canSave = computed(() => os.apiKey && os.username && os.password)

onMounted(async () => {
  try {
    const res = await apiFetch<{ key: string; value: any }>('/api/system-settings/opensubtitles')
    if (res.value) {
      os.apiKey = res.value.api_key || ''
      os.username = res.value.username || ''
      os.password = res.value.password || ''
    }
  } catch { /* not configured yet */ }
})

async function testConnection() {
  testing.value = true
  testResult.value = null
  try {
    const res = await apiFetch<{ ok: boolean; user?: any; error?: string }>('/api/opensubtitles/test', {
      method: 'POST',
      body: JSON.stringify({ api_key: os.apiKey, username: os.username, password: os.password }),
    })
    testResult.value = res
  } catch (e: any) {
    testResult.value = { ok: false, error: e.message || 'Request failed' }
  }
  testing.value = false
}

async function saveCredentials() {
  saving.value = true
  saved.value = false
  try {
    await apiFetch('/api/system-settings/opensubtitles', {
      method: 'PUT',
      body: JSON.stringify({
        value: { api_key: os.apiKey, username: os.username, password: os.password },
      }),
    })
    saved.value = true
    setTimeout(() => { saved.value = false }, 3000)
  } catch { /* empty */ }
  saving.value = false
}
</script>

<style scoped>
.page-header { margin-bottom: 32px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
.section-heading {
  display: flex; align-items: center; gap: 8px;
  font-size: 11px; font-weight: 600; color: var(--fg-3);
  font-family: var(--font-mono); text-transform: uppercase;
  letter-spacing: 0.1em; margin: 0 0 14px; padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.provider-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.form-full {
  grid-column: 1 / -1;
}

.form-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}

.form-input {
  height: 40px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-0);
  font-size: 13px;
  padding: 0 12px;
  outline: none;
  transition: border-color 0.15s;
}
.form-input:focus {
  border-color: var(--gold);
}

.form-hint {
  font-size: 11px;
  color: var(--fg-4);
}

.save-confirmation {
  font-size: 12px;
  font-weight: 600;
  color: var(--good);
}

.form-actions {
  display: flex;
  gap: 10px;
}

.status-card {
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  padding: 16px 20px;
}
.status-ok {
  background: rgba(74, 222, 128, 0.06);
  border-color: rgba(74, 222, 128, 0.2);
}
.status-error {
  background: rgba(217, 107, 107, 0.06);
  border-color: rgba(217, 107, 107, 0.2);
}

.status-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.status-row {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
}

.status-label {
  width: 100px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
  font-family: var(--font-mono);
}

.status-val {
  color: var(--fg-1);
}

.status-good { color: var(--good); font-weight: 600; }
.status-bad { color: var(--bad); font-weight: 600; }

.mono {
  font-family: var(--font-mono);
  font-size: 12px;
}

.vip-badge {
  display: inline-flex;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--gold-soft);
  color: var(--gold-bright);
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-left: 6px;
}

.future-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 12px;
}

.future-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  border-radius: var(--r-md);
  border: 1px dashed var(--border);
  background: rgba(255, 255, 255, 0.015);
}

.future-icon {
  color: var(--fg-4);
}

.future-text { flex: 1; }
.future-title { font-size: 14px; font-weight: 500; color: var(--fg-2); }
.future-desc { font-size: 11px; color: var(--fg-4); margin-top: 2px; }

.future-badge {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-4);
  padding: 3px 8px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.04);
}
</style>
