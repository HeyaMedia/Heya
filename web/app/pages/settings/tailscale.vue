<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Tailscale</h2>
      <p class="page-desc">
        Expose Heya over your tailnet — no port forwarding, no reverse proxy.
        Optional Funnel makes it reachable from the public internet.
      </p>
    </div>

    <section v-if="loading && !status" class="section">
      <div class="loading-card">Loading Tailscale state…</div>
    </section>

    <section v-else-if="!enabled" class="section">
      <div class="empty-card">
        <Icon name="cloud" :size="32" />
        <h3>Tailscale is disabled</h3>
        <p>{{ message || 'Enable Tailscale in heya.yaml under tailscale.enabled and restart the server.' }}</p>
        <pre class="code">tailscale:
  enabled: true
  hostname: heya
  https: true
  funnel: false</pre>
        <p class="hint">
          Set <code>HEYA_TAILSCALE_AUTHKEY</code> in the environment if you want to skip the
          interactive login flow on first start.
        </p>
      </div>
    </section>

    <template v-else-if="status">
      <section class="section">
        <h3 class="section-heading">
          <Icon name="pulse" :size="14" />
          Node status
        </h3>
        <div class="status-grid">
          <div class="status-card" :class="{ ok: status.running, warn: !status.running }">
            <div class="status-label">Backend</div>
            <div class="status-value">{{ status.backend_state || 'Unknown' }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">Hostname</div>
            <div class="status-value">{{ status.hostname }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">MagicDNS</div>
            <div class="status-value mono">{{ status.magic_dns || '—' }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">Tailnet IPv4</div>
            <div class="status-value mono">{{ status.ipv4 || '—' }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">Tailnet IPv6</div>
            <div class="status-value mono">{{ status.ipv6 || '—' }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">HTTPS cert</div>
            <div class="status-value mono">{{ status.cert_domain || '—' }}</div>
          </div>
        </div>
      </section>

      <section v-if="status.login_url" class="section">
        <div class="login-card">
          <Icon name="warning" :size="20" />
          <div>
            <h3>Authentication required</h3>
            <p>Open this URL in your browser and approve the node in your tailnet:</p>
            <a :href="status.login_url" target="_blank" rel="noopener" class="login-link">
              {{ status.login_url }}
            </a>
          </div>
        </div>
      </section>

      <section v-if="status.last_error" class="section">
        <div class="error-card">
          <Icon name="warning" :size="20" />
          <div>
            <h3>Last error</h3>
            <pre class="code">{{ status.last_error }}</pre>
          </div>
        </div>
      </section>

      <section class="section">
        <h3 class="section-heading">
          <Icon name="globe" :size="14" />
          Funnel (public exposure)
        </h3>
        <div class="toggle-row">
          <div class="toggle-text">
            <div class="toggle-title">Expose Heya on the public internet via Funnel</div>
            <div class="toggle-hint">
              Anyone on the internet can reach
              <code v-if="status.cert_domain">{{ status.cert_domain }}</code><code v-else>your MagicDNS name</code>.
              Heya's auth still applies. Requires Funnel to be enabled for your tailnet.
            </div>
          </div>
          <label class="switch">
            <input type="checkbox" :checked="status.funnel" :disabled="saving" @change="toggleFunnel(($event.target as HTMLInputElement).checked)">
            <span class="slider" />
          </label>
        </div>
        <p v-if="funnelNote" class="hint">{{ funnelNote }}</p>
      </section>

      <section class="section">
        <h3 class="section-heading">
          <Icon name="logout" :size="14" />
          Identity
        </h3>
        <div class="actions">
          <button class="btn btn-secondary" :disabled="loggingOut" @click="onLogout">
            <Icon name="logout" :size="14" />
            {{ loggingOut ? 'Logging out…' : 'Log out of tailnet' }}
          </button>
        </div>
        <p class="hint">
          Clears the saved tailnet identity at <code>{{ stateDirHint }}</code>.
          The next <code>heya serve</code> will go through onboarding again.
        </p>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
const { enabled, status, message, loading, refresh, setFunnel, logout, subscribeToEvents } = useTailscale()

const saving = ref(false)
const loggingOut = ref(false)
const funnelNote = ref('')

const stateDirHint = computed(() => 'data/tailscale/')

let unsubscribe: (() => void) | null = null

onMounted(async () => {
  await refresh()
  unsubscribe = subscribeToEvents()
})

onUnmounted(() => {
  unsubscribe?.()
})

async function toggleFunnel(on: boolean) {
  saving.value = true
  try {
    const res = await setFunnel(on)
    funnelNote.value = res.note
    await refresh()
  } finally {
    saving.value = false
  }
}

async function onLogout() {
  if (!confirm('Log out of the tailnet? Heya will re-onboard on next restart.')) return
  loggingOut.value = true
  try {
    await logout()
    await refresh()
  } finally {
    loggingOut.value = false
  }
}
</script>

<style scoped>
.page-header {
  margin-bottom: 24px;
}

.page-title {
  font-size: 24px;
  font-weight: 600;
  margin: 0 0 4px;
}

.page-desc {
  color: var(--fg-3);
  margin: 0;
}

.section {
  margin-bottom: 28px;
}

.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--fg-3);
  margin: 0 0 12px;
}

.loading-card,
.empty-card,
.login-card,
.error-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 24px;
}

.empty-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  color: var(--fg-2);
}

.empty-card h3 {
  margin: 16px 0 8px;
  font-size: 16px;
  color: var(--fg-1);
}

.empty-card p {
  margin: 0 0 12px;
  max-width: 480px;
  font-size: 14px;
}

.login-card,
.error-card {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.login-card {
  border-color: var(--gold);
  background: var(--gold-soft);
}

.error-card {
  border-color: rgba(220, 80, 80, 0.4);
  background: rgba(220, 80, 80, 0.08);
}

.login-card h3,
.error-card h3 {
  margin: 0 0 8px;
  font-size: 14px;
}

.login-link {
  display: inline-block;
  margin-top: 8px;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--gold-bright);
  word-break: break-all;
}

.status-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}

.status-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 16px;
}

.status-card.ok {
  border-color: rgba(120, 200, 120, 0.4);
}

.status-card.warn {
  border-color: rgba(230, 185, 74, 0.4);
}

.status-label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--fg-4);
  margin-bottom: 4px;
}

.status-value {
  font-size: 14px;
  color: var(--fg-1);
  font-weight: 500;
}

.status-value.mono {
  font-family: var(--font-mono);
  font-size: 13px;
  word-break: break-all;
}

.toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 16px 18px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.toggle-text {
  flex: 1;
  min-width: 0;
}

.toggle-title {
  font-size: 14px;
  font-weight: 500;
  margin-bottom: 4px;
}

.toggle-hint {
  font-size: 12px;
  color: var(--fg-3);
  line-height: 1.5;
}

.switch {
  position: relative;
  display: inline-block;
  width: 44px;
  height: 24px;
  flex-shrink: 0;
}

.switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.slider {
  position: absolute;
  inset: 0;
  background: var(--bg-3);
  border-radius: 12px;
  cursor: pointer;
  transition: background 0.15s ease;
}

.slider::before {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--fg-1);
  transition: transform 0.15s ease;
}

.switch input:checked + .slider {
  background: var(--gold);
}

.switch input:checked + .slider::before {
  transform: translateX(20px);
}

.switch input:disabled + .slider {
  opacity: 0.5;
  cursor: not-allowed;
}

.code {
  background: var(--bg-3);
  border-radius: var(--r-sm);
  padding: 12px 14px;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-1);
  margin: 8px 0;
  white-space: pre-wrap;
  word-break: break-word;
  text-align: left;
  max-width: 480px;
  border: 1px solid var(--border);
}

.actions {
  display: flex;
  gap: 12px;
}

.hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--fg-3);
}

code {
  font-family: var(--font-mono);
  font-size: 12px;
  background: var(--bg-3);
  padding: 1px 6px;
  border-radius: 4px;
}
</style>
