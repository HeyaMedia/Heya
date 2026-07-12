<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminUsersQuery } from '~/queries/settings'
import type { AdminUser } from '~/queries/settings'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
const { user: me } = useAuth()

const usersData = useQuery(adminUsersQuery())
const users = computed(() => usersData.data.value ?? [])
const loading = computed(() => usersData.isLoading.value)
const { flash } = useFlash()

const showCreate = ref(false)
const newUser = ref({ username: '', email: '', password: '', is_admin: false })
const createErr = ref('')
const creating = ref(false)

const pwModal = ref<AdminUser | null>(null)
const pwValue = ref('')
const pwErr = ref('')
const pwSaving = ref(false)
const showPw = computed({
  get: () => pwModal.value !== null,
  set: (v: boolean) => { if (!v) pwModal.value = null },
})

// Reveal + copy state for the two password inputs (create + reset).
const showCreatePw = ref(false)
const showResetPw = ref(false)
const copied = ref<'create' | 'reset' | null>(null)

// Strong random password from a curated alphabet (ambiguous glyphs like
// 0/O/1/l/I dropped so it survives being read aloud or copied by hand).
//
// Each symbol is used at most once. Chat apps render a matching pair of the
// same symbol as markdown — Discord italicised a '*…*' span once and silently
// ate the two asterisks plus everything between them, so the pasted password
// no longer matched. A lone symbol can't form a pair, so no formatting fires.
// Letters and digits repeat freely; only the markdown-active symbols are
// capped.
function randomPassword(len = 20): string {
  const letters = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789'
  const symbols = '!@#$%*?'
  const alphabet = letters + symbols
  const usedSymbols = new Set<string>()
  let out = ''
  while (out.length < len) {
    const buf = new Uint32Array(1)
    crypto.getRandomValues(buf)
    const ch = alphabet[buf[0]! % alphabet.length]!
    if (symbols.includes(ch)) {
      if (usedSymbols.has(ch)) continue
      usedSymbols.add(ch)
    }
    out += ch
  }
  return out
}

function generateCreatePw() {
  newUser.value.password = randomPassword()
  showCreatePw.value = true
}

function generateResetPw() {
  pwValue.value = randomPassword()
  showResetPw.value = true
}

async function copyPw(text: string, which: 'create' | 'reset') {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    copied.value = which
    setTimeout(() => { if (copied.value === which) copied.value = null }, 1400)
  } catch {
    flash.value = { kind: 'err', text: 'Clipboard not available.' }
  }
}

async function load() {
  try {
    await usersData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load users.' }
  }
}

async function createUser() {
  createErr.value = ''
  if (newUser.value.password.length < 8) {
    createErr.value = 'Password must be at least 8 characters.'
    return
  }
  creating.value = true
  try {
    const u = await $heya('/api/admin/users', {
      method: 'POST',
      body: { ...newUser.value } as any,
    })
    await load()
    flash.value = { kind: 'ok', text: `Created ${u.username}.` }
    showCreate.value = false
    newUser.value = { username: '', email: '', password: '', is_admin: false }
  } catch (e: any) {
    createErr.value = e?.data?.error || e?.message || 'Create failed.'
  } finally {
    creating.value = false
  }
}

async function toggleAdmin(u: AdminUser) {
  if (u.id === me.value?.id) return
  const next = !u.is_admin
  const ok = await confirm({
    title: next ? `Grant admin to ${u.username}?` : `Revoke admin from ${u.username}?`,
    message: next
      ? 'This user will be able to manage libraries, providers, transcoding, and all other admin pages.'
      : 'This user will lose access to every admin page. Their content and sessions are unaffected.',
    destructive: !next,
    confirmLabel: next ? 'Grant admin' : 'Revoke admin',
  })
  if (!ok) return
  try {
    await $heya('/api/admin/users/{id}/role', {
      method: 'PATCH',
      path: { id: u.id },
      body: { is_admin: next } as any,
    })
    await load()
    flash.value = { kind: 'ok', text: `Role updated for ${u.username}.` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Role change failed.' }
  }
}

async function deleteUser(u: AdminUser) {
  if (u.id === me.value?.id) return
  const ok = await confirm({
    title: `Delete ${u.username}?`,
    message: 'Deletes the account and every session. Library data isn\'t touched.',
    destructive: true,
    confirmLabel: 'Delete user',
  })
  if (!ok) return
  try {
    await $heya('/api/admin/users/{id}', { method: 'DELETE', path: { id: u.id } })
    await load()
    flash.value = { kind: 'ok', text: `Deleted ${u.username}.` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Delete failed.' }
  }
}

function openPw(u: AdminUser) {
  pwModal.value = u
  pwValue.value = ''
  pwErr.value = ''
}

async function savePw() {
  if (!pwModal.value) return
  if (pwValue.value.length < 8) {
    pwErr.value = 'Password must be at least 8 characters.'
    return
  }
  pwSaving.value = true
  try {
    await $heya('/api/admin/users/{id}/password', {
      method: 'POST',
      path: { id: pwModal.value.id },
      body: { new_password: pwValue.value } as any,
    })
    flash.value = { kind: 'ok', text: `Password reset for ${pwModal.value.username}.` }
    pwModal.value = null
  } catch (e: any) {
    pwErr.value = e?.message ?? 'Reset failed.'
  } finally {
    pwSaving.value = false
  }
}

const adminCount = computed(() => users.value.filter(u => u.is_admin).length)

function initials(u: { username: string }): string {
  return u.username.slice(0, 2).toUpperCase()
}

</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Users</h2>
      <p class="sv2-page-desc">
        Accounts that can sign in to this server. Admin users see every
        settings page; non-admin users only see the <em>You</em> group.
      </p>
    </header>

    <div class="tiles">
      <MetricTile label="Total users" :value="users.length" icon="users" />
      <MetricTile label="Admins" :value="adminCount" icon="key"
        :tone="adminCount === 0 ? 'bad' : 'good'"
        :sub="adminCount === 0 ? 'no admin — broken state' : `${adminCount} of ${users.length}`" />
      <MetricTile label="Non-admins" :value="users.length - adminCount" icon="user" />
    </div>

    <SettingsSection title="All accounts" icon="users">
      <template #actions>
        <button class="sv2-btn primary" @click="showCreate = true">
          <Icon name="user" :size="12" />
          Add user
        </button>
      </template>

      <div v-if="loading" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>

      <div v-else-if="users.length === 0" class="empty-state">
        <Icon name="info" :size="14" /> No users yet — that's odd, you'd be signed out.
      </div>

      <div v-else class="user-list">
        <div v-for="u in users" :key="u.id" class="user-card" :class="{ self: u.id === me?.id }">
          <div class="user-avatar">{{ initials(u) }}</div>
          <div class="user-body">
            <div class="user-name">
              {{ u.username }}
              <StatusBadge v-if="u.is_admin" state="warn">admin</StatusBadge>
              <StatusBadge v-else state="idle">user</StatusBadge>
              <span v-if="u.id === me?.id" class="you-pill">you</span>
            </div>
            <div class="user-email">
              <Icon name="envelope" :size="11" /> {{ u.email }}
            </div>
            <div class="user-meta">
              <span>ID #{{ u.id }}</span>
              <span>· joined {{ timeAgo(u.created_at) }}</span>
            </div>
          </div>
          <div class="user-actions">
            <button
              class="row-btn"
              :disabled="u.id === me?.id"
              :title="u.id === me?.id ? 'You can\'t reset your own password here — use the Profile page' : `Reset password for ${u.username}`"
              :aria-label="u.id === me?.id ? 'You can\'t reset your own password here — use the Profile page' : `Reset password for ${u.username}`"
              @click="openPw(u)"
            >
              <Icon name="key" :size="14" />
            </button>
            <button
              class="row-btn"
              :disabled="u.id === me?.id"
              :title="u.id === me?.id ? 'You can\'t toggle your own admin flag' : (u.is_admin ? 'Revoke admin' : 'Grant admin')"
              :aria-label="u.id === me?.id ? 'You can\'t toggle your own admin flag' : (u.is_admin ? 'Revoke admin' : 'Grant admin')"
              @click="toggleAdmin(u)"
            >
              <Icon :name="u.is_admin ? 'key' : 'sparkle'" :size="14" />
            </button>
            <button
              class="row-btn danger"
              :disabled="u.id === me?.id"
              :title="u.id === me?.id ? 'You can\'t delete your own account' : `Delete ${u.username}`"
              :aria-label="u.id === me?.id ? 'You can\'t delete your own account' : `Delete ${u.username}`"
              @click="deleteUser(u)"
            >
              <Icon name="trash" :size="14" />
            </button>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Sessions across users" icon="eye"
      description="Want to see every active session and revoke any of them? That lives on the dedicated page.">
      <NuxtLink to="/settings/all-sessions" class="big-link">
        <div class="big-link-icon"><Icon name="eye" :size="20" /></div>
        <div class="big-link-body">
          <div class="big-link-title">Open All sessions</div>
          <div class="big-link-desc">Per-user roster + admin revoke. Shows browser sessions and API tokens together.</div>
        </div>
        <Icon name="chevright" :size="16" class="big-link-chev" />
      </NuxtLink>
    </SettingsSection>

    <SettingsFlash :flash="flash" />

    <AppDialog v-model="showCreate" title="Add user" description="Creates a new account. The user can change their own password and email after signing in." size="md">
      <div class="dialog-form">
        <div class="form-field">
          <label class="form-label" for="user-create-username">Username</label>
          <input id="user-create-username" v-model="newUser.username" class="sv2-input" maxlength="64" autocomplete="off" />
        </div>
        <div class="form-field">
          <label class="form-label" for="user-create-email">Email</label>
          <input id="user-create-email" v-model="newUser.email" class="sv2-input" type="email" maxlength="254" autocomplete="off" />
        </div>
        <div class="form-field">
          <label class="form-label" for="user-create-password">Initial password (≥ 8 chars)</label>
          <div class="pw-group">
            <input
              id="user-create-password"
              v-model="newUser.password"
              class="sv2-input"
              :type="showCreatePw ? 'text' : 'password'"
              minlength="8" maxlength="256" autocomplete="new-password"
              placeholder="At least 8 characters"
            />
            <button type="button" class="pw-btn" :class="{ active: showCreatePw }"
              :title="showCreatePw ? 'Hide' : 'Show'" :aria-label="showCreatePw ? 'Hide password' : 'Show password'" @click="showCreatePw = !showCreatePw">
              <Icon name="eye" :size="14" />
            </button>
            <button type="button" class="pw-btn" :disabled="!newUser.password"
              :title="copied === 'create' ? 'Copied' : 'Copy'" :aria-label="copied === 'create' ? 'Copied' : 'Copy password'" @click="copyPw(newUser.password, 'create')">
              <Icon :name="copied === 'create' ? 'check' : 'clipboard'" :size="14" />
            </button>
            <button type="button" class="pw-gen" @click="generateCreatePw">
              <Icon name="sparkle" :size="12" /> Generate
            </button>
          </div>
        </div>
        <label class="check-row">
          <input v-model="newUser.is_admin" type="checkbox" />
          <span>Grant admin access</span>
        </label>
        <div v-if="createErr" class="form-error">
          <Icon name="warning" :size="13" /> {{ createErr }}
        </div>
      </div>
      <template #footer="{ close }">
        <button class="sv2-btn ghost" @click="close()">Cancel</button>
        <button class="sv2-btn primary" :disabled="creating" @click="createUser">
          <Icon :name="creating ? 'spinner' : 'check'" :size="12" />
          {{ creating ? 'Creating…' : 'Create user' }}
        </button>
      </template>
    </AppDialog>

    <AppDialog v-model="showPw" :title="pwModal ? `Reset password for ${pwModal.username}` : 'Reset password'" description="The user can change it themselves after signing in." size="md">
      <div class="dialog-form">
        <div class="form-field">
          <label class="form-label" for="user-reset-password">New password (≥ 8 chars)</label>
          <div class="pw-group">
            <input
              id="user-reset-password"
              v-model="pwValue"
              class="sv2-input"
              :type="showResetPw ? 'text' : 'password'"
              minlength="8" maxlength="256" autocomplete="new-password"
              placeholder="At least 8 characters"
            />
            <button type="button" class="pw-btn" :class="{ active: showResetPw }"
              :title="showResetPw ? 'Hide' : 'Show'" :aria-label="showResetPw ? 'Hide password' : 'Show password'" @click="showResetPw = !showResetPw">
              <Icon name="eye" :size="14" />
            </button>
            <button type="button" class="pw-btn" :disabled="!pwValue"
              :title="copied === 'reset' ? 'Copied' : 'Copy'" :aria-label="copied === 'reset' ? 'Copied' : 'Copy password'" @click="copyPw(pwValue, 'reset')">
              <Icon :name="copied === 'reset' ? 'check' : 'clipboard'" :size="14" />
            </button>
            <button type="button" class="pw-gen" @click="generateResetPw">
              <Icon name="sparkle" :size="12" /> Generate
            </button>
          </div>
        </div>
        <div v-if="pwErr" class="form-error">
          <Icon name="warning" :size="13" /> {{ pwErr }}
        </div>
      </div>
      <template #footer="{ close }">
        <button class="sv2-btn ghost" @click="close()">Cancel</button>
        <button class="sv2-btn primary" :disabled="pwSaving" @click="savePw">
          <Icon :name="pwSaving ? 'spinner' : 'key'" :size="12" />
          {{ pwSaving ? 'Resetting…' : 'Reset password' }}
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.sv2-page-desc em { color: var(--gold); font-style: normal; }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.user-list { display: flex; flex-direction: column; gap: 8px; }
.user-card {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color 0.15s ease;
}
.user-card:hover { border-color: var(--border-strong); }
.user-card.self {
  border-color: color-mix(in srgb, var(--good) 30%, transparent);
  background: color-mix(in srgb, var(--good) 4%, transparent);
}

.user-avatar {
  width: 40px; height: 40px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: var(--accent-ink);
  font-weight: 700;
  font-size: 13px;
  letter-spacing: 0.04em;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.user-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.user-name {
  display: flex; align-items: center; gap: 8px;
  font-size: 14px; font-weight: 600; color: var(--fg-0);
}
.you-pill {
  display: inline-flex; align-items: center;
  padding: 1px 8px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--good) 12%, transparent);
  color: var(--good);
  font-family: var(--font-mono);
  font-size: 9px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.04em;
}
.user-email {
  display: flex; align-items: center; gap: 5px;
  font-size: 12px; color: var(--fg-2);
}
.user-meta {
  font-family: var(--font-mono); font-size: 11px; color: var(--fg-3);
  display: flex; flex-wrap: wrap; gap: 4px;
}

.user-actions { display: flex; gap: 4px; flex-shrink: 0; }
.row-btn {
  width: 32px; height: 32px;
  border-radius: var(--r-sm);
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--fg-3);
  border: 1px solid transparent;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.row-btn:hover:not(:disabled) {
  color: var(--fg-0);
  background: rgb(var(--ink) / 0.06);
  border-color: var(--border);
}
.row-btn.danger:hover:not(:disabled) {
  color: var(--bad);
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  border-color: color-mix(in srgb, var(--bad) 25%, transparent);
}
.row-btn:disabled { opacity: 0.3; cursor: not-allowed; }

.big-link {
  display: flex; align-items: center; gap: 14px;
  padding: 16px 18px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.12s, background 0.12s;
}
.big-link:hover {
  border-color: var(--gold);
  background: var(--gold-soft);
}
.big-link-icon {
  width: 40px; height: 40px;
  border-radius: var(--r-md);
  background: var(--bg-0);
  color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.big-link-body { flex: 1; }
.big-link-title { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.big-link-desc  { font-size: 12px; color: var(--fg-3); margin-top: 2px; line-height: 1.4; }
.big-link-chev  { color: var(--fg-3); flex-shrink: 0; }

.dialog-form { display: flex; flex-direction: column; gap: 12px; }
.form-field { display: flex; flex-direction: column; gap: 5px; }
.form-label {
  font-family: var(--font-mono);
  font-size: 10px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3);
}
.sv2-input {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  padding: 9px 12px;
  outline: none;
  transition: border-color 0.12s;
}
.sv2-input:focus { border-color: var(--gold); }

.pw-group { display: flex; gap: 6px; align-items: stretch; }
.pw-group .sv2-input { flex: 1; min-width: 0; }
.pw-btn {
  width: 38px; flex-shrink: 0;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--fg-3);
  display: inline-flex; align-items: center; justify-content: center;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.pw-btn:hover:not(:disabled) { color: var(--fg-0); border-color: var(--border-strong); }
.pw-btn.active { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 40%, transparent); }
.pw-btn:disabled { opacity: 0.35; cursor: not-allowed; }
.pw-gen {
  flex-shrink: 0;
  display: inline-flex; align-items: center; gap: 5px;
  padding: 0 12px;
  border: 1px solid color-mix(in srgb, var(--gold) 35%, transparent);
  border-radius: var(--r-sm);
  background: var(--gold-soft);
  color: var(--gold-bright);
  font-size: 12px; font-weight: 600; white-space: nowrap;
  transition: border-color 0.12s, background 0.12s;
}
.pw-gen:hover { border-color: var(--gold); }

.check-row {
  display: flex; align-items: center; gap: 8px;
  font-size: 12.5px; color: var(--fg-1);
  padding: 6px 0;
  cursor: pointer;
}
.check-row input { accent-color: var(--gold); }

.form-error {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px;
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--bad) 25%, transparent);
  border-radius: var(--r-sm);
  color: var(--bad);
  font-size: 12px;
}

/* Phone: minmax(180px) only fits 1 column at 390px — force 2. */
@media (max-width: 720px) {
  .tiles { grid-template-columns: repeat(2, 1fr); }
}
</style>
