<script setup lang="ts">
definePageMeta({ layout: 'settings' })

const { user } = useAuth()
const { $heya } = useNuxtApp()

const memberSince = computed(() => {
  // /api/auth/me's UserView doesn't currently expose created_at, so this is
  // a placeholder. Wire it through in PR 4 (Users page needs it too).
  return '—'
})

const currentPwd = ref('')
const newPwd = ref('')
const confirmPwd = ref('')
const saving = ref(false)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

const newPwdTooShort = computed(() => newPwd.value.length > 0 && newPwd.value.length < 8)
const mismatch = computed(() => confirmPwd.value.length > 0 && newPwd.value !== confirmPwd.value)
const canSubmit = computed(() =>
  currentPwd.value.length > 0 &&
  newPwd.value.length >= 8 &&
  newPwd.value === confirmPwd.value &&
  !saving.value,
)

async function changePassword() {
  if (!canSubmit.value) return
  saving.value = true
  flash.value = null
  try {
    await $heya('/api/me/password', {
      method: 'PUT',
      body: { current_password: currentPwd.value, new_password: newPwd.value },
    })
    flash.value = { kind: 'ok', text: 'Password updated. Other devices stay signed in until you revoke them on the Sessions tab.' }
    currentPwd.value = ''
    newPwd.value = ''
    confirmPwd.value = ''
  } catch (err: any) {
    const status = err?.response?.status
    if (status === 401) {
      flash.value = { kind: 'err', text: 'Current password is incorrect.' }
    } else {
      flash.value = { kind: 'err', text: err?.data?.detail ?? err?.message ?? 'Failed to change password.' }
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Profile</h2>
      <p class="sv2-page-desc">Identity and password for your account.</p>
    </header>

    <SettingsSection title="Identity" icon="user">
      <div class="profile-card">
        <div class="profile-avatar">
          <span>{{ user?.username?.slice(0, 2).toUpperCase() }}</span>
        </div>
        <div class="profile-info">
          <div class="profile-name">{{ user?.username }}</div>
          <div class="profile-email">{{ user?.email }}</div>
          <div class="profile-meta">
            <StatusBadge :state="user?.is_admin ? 'warn' : 'idle'">
              {{ user?.is_admin ? 'Admin' : 'User' }}
            </StatusBadge>
            <span class="profile-id">#{{ user?.id ?? '—' }}</span>
          </div>
        </div>
      </div>

      <KVTable
        :rows="[
          { key: 'Username', value: user?.username, copy: true },
          { key: 'Email',    value: user?.email,    copy: true },
          { key: 'Account ID', value: user?.id,     mono: true, copy: true },
          { key: 'Member since', value: memberSince, mono: true },
        ]"
        class="profile-kv"
      />
    </SettingsSection>

    <SettingsSection
      title="Password"
      icon="key"
      description="Changing your password doesn't sign you out on other devices. Manage those on the Sessions tab."
    >
      <form class="pw-form" @submit.prevent="changePassword">
        <SettingsField label="Current password">
          <input
            v-model="currentPwd"
            type="password"
            autocomplete="current-password"
            class="sv2-input"
            placeholder="•••••••••••"
          />
        </SettingsField>
        <SettingsField
          label="New password"
          description="Minimum 8 characters."
          :hint="newPwdTooShort ? 'Too short — needs at least 8 characters.' : undefined"
        >
          <input
            v-model="newPwd"
            type="password"
            autocomplete="new-password"
            class="sv2-input"
            :class="{ invalid: newPwdTooShort }"
            placeholder="•••••••••••"
          />
        </SettingsField>
        <SettingsField
          label="Confirm new password"
          :hint="mismatch ? 'Passwords don\'t match.' : undefined"
        >
          <input
            v-model="confirmPwd"
            type="password"
            autocomplete="new-password"
            class="sv2-input"
            :class="{ invalid: mismatch }"
            placeholder="•••••••••••"
          />
        </SettingsField>

        <div class="pw-actions">
          <button type="submit" class="sv2-btn primary" :disabled="!canSubmit">
            <Icon v-if="saving" name="spinner" :size="13" />
            {{ saving ? 'Saving…' : 'Change password' }}
          </button>
        </div>

        <div v-if="flash" class="pw-flash" :class="flash.kind">
          <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
          {{ flash.text }}
        </div>
      </form>
    </SettingsSection>
  </div>
</template>

<style scoped>
.profile-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 12px;
}
.profile-avatar {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: var(--accent-ink);
  font-weight: 700;
  font-size: 18px;
  letter-spacing: 0.04em;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.profile-info { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 4px; }
.profile-name { font-size: 16px; font-weight: 600; color: var(--fg-0); }
.profile-email { font-size: 12px; color: var(--fg-3); font-family: var(--font-mono); }
.profile-meta { display: flex; align-items: center; gap: 10px; margin-top: 4px; }
.profile-id { font-family: var(--font-mono); font-size: 11px; color: var(--fg-4); }
.profile-kv { margin-top: 4px; }

.pw-form { display: block; }
.pw-actions {
  display: flex;
  justify-content: flex-end;
  padding: 16px 0 0;
}
.pw-flash {
  margin-top: 12px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.pw-flash.ok {
  background: color-mix(in srgb, var(--good) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--good) 25%, transparent);
  color: var(--good);
}
.pw-flash.err {
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
  color: var(--bad);
}

.sv2-input {
  width: 100%;
  max-width: 380px;
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-mono);
  transition: border-color 0.12s, background 0.12s;
}
.sv2-input:focus { outline: none; border-color: var(--gold); background: var(--bg-1); }
.sv2-input.invalid { border-color: var(--bad); }

.sv2-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 9px 18px;
  border-radius: var(--r-sm);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
</style>
