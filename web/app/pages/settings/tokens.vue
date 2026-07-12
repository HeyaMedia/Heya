<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import type { components } from '#open-fetch-schemas/heya'
import { myApiTokensQuery } from '~/queries/settings'
import type { ApiToken } from '~/queries/settings'
type CreateResult = components['schemas']['CreateApiTokenResult']

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const tokensData = useQuery(myApiTokensQuery())
const tokens = computed(() => tokensData.data.value ?? [])
const loading = computed(() => tokensData.isLoading.value)
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
  try {
    await tokensData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load tokens.' }
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
    await load()
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
  } catch {
    flash.value = { kind: 'err', text: 'Clipboard blocked — copy manually.' }
  }
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
    await load()
    flash.value = { kind: 'ok', text: 'Token revoked.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to revoke token.' }
  }
}

// timeAgo comes from useFormat.ts (auto-imported). formatExpiry stays local:
// tokens say "never expires" where sessions say "no expiry".
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
          <button class="token-revoke" title="Revoke" aria-label="Revoke token" @click="revoke(t)">
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
        <SettingsField label="Name" description="Shown in this list. Pick something you'll recognise in 6 months." v-slot="{ fieldId }">
          <input
            :id="fieldId"
            v-model="draftName"
            class="sv2-input"
            placeholder="e.g. backup script · macbook"
            maxlength="64"
            autofocus
          />
        </SettingsField>
        <SettingsField label="Expiry" description="Tokens with no expiry never auto-revoke.">
          <div class="expiry-grid" role="radiogroup" aria-label="Expiry">
            <label v-for="opt in EXPIRY_OPTIONS" :key="opt.value" class="expiry-chip" :class="{ active: draftExpiryDays === opt.value }">
              <input type="radio" name="token-expiry" :value="opt.value" v-model="draftExpiryDays" />
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
.token-revoke:hover { background: color-mix(in srgb, var(--bad) 12%, transparent); color: var(--bad); }

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

@media (pointer: coarse) {
  .expiry-chip { min-height: 44px; box-sizing: border-box; display: inline-flex; align-items: center; }
}

.reveal-warning {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px;
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 30%, transparent);
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
