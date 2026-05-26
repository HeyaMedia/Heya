<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type AdminUser = components['schemas']['AdminUserView']

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
const { user: me } = useAuth()

const users = ref<AdminUser[]>([])
const loading = ref(true)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

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

async function load() {
  loading.value = true
  try {
    users.value = await $heya('/api/admin/users') ?? []
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load users.' }
  } finally {
    loading.value = false
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
    users.value.push(u)
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
    const updated = await $heya('/api/admin/users/{id}/role', {
      method: 'PATCH',
      path: { id: u.id },
      body: { is_admin: next } as any,
    })
    const idx = users.value.findIndex(x => x.id === u.id)
    if (idx >= 0) users.value[idx] = updated
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
    users.value = users.value.filter(x => x.id !== u.id)
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

function timeAgo(iso: string): string {
  const sec = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (sec < 60) return 'just now'
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  if (sec < 86400 * 30) return `${Math.floor(sec / 86400)}d ago`
  return new Date(iso).toLocaleDateString()
}

onMounted(load)
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
              @click="openPw(u)"
            >
              <Icon name="key" :size="14" />
            </button>
            <button
              class="row-btn"
              :disabled="u.id === me?.id"
              :title="u.id === me?.id ? 'You can\'t toggle your own admin flag' : (u.is_admin ? 'Revoke admin' : 'Grant admin')"
              @click="toggleAdmin(u)"
            >
              <Icon :name="u.is_admin ? 'key' : 'sparkle'" :size="14" />
            </button>
            <button
              class="row-btn danger"
              :disabled="u.id === me?.id"
              :title="u.id === me?.id ? 'You can\'t delete your own account' : `Delete ${u.username}`"
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

    <div v-if="flash" class="sv2-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
      {{ flash.text }}
    </div>

    <AppDialog v-model="showCreate" title="Add user" description="Creates a new account. The user can change their own password and email after signing in." size="md">
      <div class="dialog-form">
        <div class="form-field">
          <label class="form-label">Username</label>
          <input v-model="newUser.username" class="sv2-input" maxlength="64" autocomplete="off" />
        </div>
        <div class="form-field">
          <label class="form-label">Email</label>
          <input v-model="newUser.email" class="sv2-input" type="email" maxlength="254" autocomplete="off" />
        </div>
        <div class="form-field">
          <label class="form-label">Initial password (≥ 8 chars)</label>
          <input v-model="newUser.password" class="sv2-input" type="password" minlength="8" maxlength="256" autocomplete="new-password" />
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
          <label class="form-label">New password (≥ 8 chars)</label>
          <input v-model="pwValue" class="sv2-input" type="password" minlength="8" maxlength="256" autocomplete="new-password" />
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
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }
.sv2-page-desc em { color: var(--gold); font-style: normal; }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.loading-state, .empty-state {
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
  border-color: rgba(111, 191, 124, 0.3);
  background: rgba(111, 191, 124, 0.04);
}

.user-avatar {
  width: 40px; height: 40px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
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
  background: rgba(111, 191, 124, 0.12);
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
  background: rgba(255,255,255,0.06);
  border-color: var(--border);
}
.row-btn.danger:hover:not(:disabled) {
  color: var(--bad);
  background: rgba(217,107,107,0.10);
  border-color: rgba(217,107,107,0.25);
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
  background: rgba(217,107,107,0.10);
  border: 1px solid rgba(217,107,107,0.25);
  border-radius: var(--r-sm);
  color: var(--bad);
  font-size: 12px;
}

.sv2-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 7px 14px;
  border-radius: var(--r-sm);
  font-size: 12px; font-weight: 500;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.sv2-btn.primary { background: var(--gold); color: #1a1408; }
.sv2-btn.primary:hover:not(:disabled) { background: var(--gold-deep); }
.sv2-btn.ghost { border: 1px solid var(--border); background: var(--bg-2); color: var(--fg-2); }
.sv2-btn.ghost:hover:not(:disabled) { border-color: var(--border-strong); color: var(--fg-0); }
.sv2-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.sv2-flash {
  margin-top: 16px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex; align-items: center; gap: 8px;
}
.sv2-flash.ok  { background: rgba(111,191,124,0.10); border: 1px solid rgba(111,191,124,0.25); color: var(--good); }
.sv2-flash.err { background: rgba(217,107,107,0.10); border: 1px solid rgba(217,107,107,0.30); color: var(--bad); }
</style>
