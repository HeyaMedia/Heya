<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import type { components } from '#open-fetch-schemas/heya'
type ApiToken = components['schemas']['ApiTokenView']
type CreateResult = components['schemas']['CreateApiTokenResult']

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const tokens = ref<ApiToken[]>([])
const loading = ref(true)
const { flash } = useFlash()

// Create dialog state
const showCreate = ref(false)
const draftName = ref('')
const draftExpiryDays = ref(0)
const creating = ref(false)

// Plaintext reveal state — only shown immediately after creation. Wiped
// when the user closes the dialog; never re-fetchable.
const revealed = ref<CreateResult | null>(null)
const copied = ref(false)

const EXPIRY_OPTIONS = [
  { value: 0,    label: 'Never expires' },
  { value: 7,    label: '7 days' },
  { value: 30,   label: '30 days' },
  { value: 90,   label: '90 days' },
  { value: 365,  label: '1 year' },
] as const

async function load() {
  loading.value = true
  try {
    tokens.value = await $heya('/api/me/api-tokens')
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load tokens.' }
  } finally {
    loading.value = false
  }
}

function openCreate() {
  draftName.value = ''
  draftExpiryDays.value = 0
  revealed.value = null
  showCreate.value = true
}

async function create() {
  if (!draftName.value.trim()) return
  creating.value = true
  flash.value = null
  try {
    const result = await $heya('/api/me/api-tokens', {
      method: 'POST',
      body: { name: draftName.value.trim(), expires_in_days: draftExpiryDays.value },
    })
    revealed.value = result
    // Prepend the new token (without plaintext) to the list.
    tokens.value.unshift({
      id: result.id,
      name: result.name,
      created_at: result.created_at,
      last_seen_at: result.last_seen_at,
      expires_at: result.expires_at,
    })
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to mint token.' }
  } finally {
    creating.value = false
  }
}

async function copyToken() {
  if (!revealed.value) return
  try {
    await navigator.clipboard.writeText(revealed.value.token)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {}
}

function closeCreate() {
  showCreate.value = false
  revealed.value = null
  copied.value = false
}

async function revoke(t: ApiToken) {
  const ok = await confirm({
    title: `Revoke "${t.name}"?`,
    message: 'Any script using this token will start getting 401 immediately. You can\'t recover a revoked token — mint a fresh one if you need to.',
    destructive: true,
    confirmLabel: 'Revoke',
  })
  if (!ok) return
  try {
    await $heya('/api/me/api-tokens/{id}', { method: 'DELETE', path: { id: t.id } })
    tokens.value = tokens.value.filter(x => x.id !== t.id)
    flash.value = { kind: 'ok', text: 'Token revoked.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to revoke token.' }
  }
}

function timeAgo(iso: string): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '—'
  const s = Math.floor((Date.now() - t) / 1000)
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  return `${d}d ago`
}

function formatExpiry(iso?: string | null): string {
  if (!iso) return 'never expires'
  const ms = new Date(iso).getTime() - Date.now()
  if (ms <= 0) return 'expired'
  const d = Math.floor(ms / 86400000)
  if (d < 1) return 'expires today'
  if (d < 30) return `expires in ${d}d`
  if (d < 365) return `expires in ${Math.floor(d / 30)}mo`
  return `expires in ${Math.floor(d / 365)}y`
}

function wasUsed(t: ApiToken): boolean {
  // last_seen_at defaults to creation time, so use a small window heuristic.
  return new Date(t.last_seen_at).getTime() > new Date(t.created_at).getTime() + 5000
}

onMounted(load)
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">API tokens</h2>
      <p class="sv2-page-desc">
        Personal long-lived tokens for scripts, the CLI, and integrations.
        They carry your user's permissions and keep working through password
        changes — revoke individually when you're done with one.
      </p>
    </header>

    <SettingsSection title="Active tokens" icon="key">
      <template #actions>
        <button class="sv2-btn primary" @click="openCreate">
          <Icon name="plus" :size="13" />
          New token
        </button>
      </template>

      <div v-if="loading" class="empty-state">
        <Icon name="spinner" :size="14" /> Loading…
      </div>

      <div v-else-if="tokens.length === 0" class="empty-state">
        <Icon name="key" :size="14" />
        No API tokens yet. Create one and use it as <code>Authorization: Bearer &lt;token&gt;</code>.
      </div>

      <div v-else class="token-list">
        <div v-for="t in tokens" :key="t.id" class="token-card">
          <div class="token-icon"><Icon name="key" :size="16" /></div>
          <div class="token-body">
            <div class="token-name">{{ t.name }}</div>
            <div class="token-meta">
              <span>Created {{ timeAgo(t.created_at) }}</span>
              <span v-if="wasUsed(t)">· last used {{ timeAgo(t.last_seen_at) }}</span>
              <span v-else>· never used</span>
              <span>· {{ formatExpiry(t.expires_at) }}</span>
            </div>
          </div>
          <button class="token-revoke" title="Revoke" @click="revoke(t)">
            <Icon name="trash" :size="13" />
          </button>
        </div>
      </div>

      <SettingsFlash :flash="flash" />
    </SettingsSection>

    <AppDialog
      v-model="showCreate"
      :title="revealed ? 'Token created' : 'Create API token'"
      :description="revealed ? 'Copy it now — you won\'t see it again.' : 'Give it a recognisable name so you know which integration the token belongs to.'"
      size="md"
      :closable="true"
      @update:model-value="(v: boolean) => { if (!v) closeCreate() }"
    >
      <template v-if="!revealed">
        <SettingsField label="Name" description="Shown in this list. Pick something you'll recognise in 6 months.">
          <input
            v-model="draftName"
            class="sv2-input"
            placeholder="e.g. backup script · macbook"
            maxlength="64"
            autofocus
          />
        </SettingsField>
        <SettingsField label="Expiry" description="Tokens with no expiry never auto-revoke.">
          <div class="expiry-grid">
            <label v-for="opt in EXPIRY_OPTIONS" :key="opt.value" class="expiry-chip" :class="{ active: draftExpiryDays === opt.value }">
              <input type="radio" :value="opt.value" v-model="draftExpiryDays" />
              {{ opt.label }}
            </label>
          </div>
        </SettingsField>
      </template>

      <template v-else>
        <div class="reveal-warning">
          <Icon name="warning" :size="14" />
          <span>This token is shown <strong>once</strong>. Copy it now and store it somewhere safe.</span>
        </div>
        <div class="reveal-box">
          <code class="reveal-token">{{ revealed.token }}</code>
          <button class="reveal-copy" @click="copyToken">
            <Icon :name="copied ? 'check' : 'clipboard'" :size="13" />
            {{ copied ? 'Copied' : 'Copy' }}
          </button>
        </div>
        <p class="reveal-hint">
          Use it as: <code>Authorization: Bearer {{ revealed.token.slice(0, 6) }}…{{ revealed.token.slice(-4) }}</code>
        </p>
      </template>

      <template #footer>
        <template v-if="!revealed">
          <button class="sv2-btn ghost" @click="closeCreate">Cancel</button>
          <button class="sv2-btn primary" :disabled="!draftName.trim() || creating" @click="create">
            <Icon v-if="creating" name="spinner" :size="13" />
            {{ creating ? 'Creating…' : 'Create token' }}
          </button>
        </template>
        <template v-else>
          <button class="sv2-btn primary" @click="closeCreate">Done</button>
        </template>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 13px;
  padding: 20px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.empty-state code { font-family: var(--font-mono); font-size: 11.5px; background: var(--bg-0); padding: 2px 6px; border-radius: var(--r-xs); }

.token-list { display: flex; flex-direction: column; gap: 8px; }
.token-card {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.token-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
  flex-shrink: 0;
}
.token-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.token-name { font-size: 14px; font-weight: 500; color: var(--fg-0); }
.token-meta { font-size: 11.5px; color: var(--fg-3); display: flex; flex-wrap: wrap; gap: 6px; }

.token-revoke {
  width: 28px; height: 28px;
  border-radius: var(--r-sm);
  color: var(--fg-3);
  display: flex; align-items: center; justify-content: center;
  transition: background 0.12s, color 0.12s;
  flex-shrink: 0;
}
.token-revoke:hover { background: rgba(217, 107, 107, 0.12); color: var(--bad); }

.expiry-grid { display: flex; flex-wrap: wrap; gap: 8px; }
.expiry-chip {
  padding: 7px 14px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  font-size: 12px;
  color: var(--fg-2);
  cursor: pointer;
  background: var(--bg-2);
  transition: border-color 0.12s, background 0.12s, color 0.12s;
}
.expiry-chip:hover { border-color: var(--border-strong); }
.expiry-chip.active { border-color: var(--gold); background: var(--gold-soft); color: var(--gold); }
.expiry-chip input { display: none; }

.reveal-warning {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px;
  background: var(--gold-soft);
  border: 1px solid rgba(230, 185, 74, 0.30);
  border-radius: var(--r-sm);
  font-size: 12px;
  color: var(--gold);
  margin-bottom: 12px;
}
.reveal-box {
  display: flex; gap: 8px; align-items: center;
  padding: 12px 14px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  margin-bottom: 8px;
}
.reveal-token {
  flex: 1; min-width: 0;
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-0);
  word-break: break-all;
}
.reveal-copy {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 6px 10px;
  border-radius: var(--r-xs);
  border: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--fg-2);
  font-size: 11px;
  flex-shrink: 0;
}
.reveal-copy:hover { color: var(--fg-0); border-color: var(--border-strong); }
.reveal-hint { font-size: 11.5px; color: var(--fg-3); margin: 4px 0 0; }
.reveal-hint code { font-family: var(--font-mono); background: var(--bg-0); padding: 2px 6px; border-radius: var(--r-xs); }

.sv2-input {
  width: 100%;
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-sans);
  transition: border-color 0.12s;
}
.sv2-input:focus { outline: none; border-color: var(--gold); background: var(--bg-1); }

</style>
