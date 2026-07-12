<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import { myApiTokensQuery, mySessionsQuery } from '~/queries/settings'

const { user } = useAuth()
const { $heya } = useNuxtApp()
const sessionsData = useQuery(mySessionsQuery())
const tokensData = useQuery(myApiTokensQuery())

const sessionCount = computed(() => sessionsData.data.value?.length ?? 0)
const otherSessionCount = computed(() => sessionsData.data.value?.filter(session => !session.current).length ?? 0)
const tokenCount = computed(() => tokensData.data.value?.length ?? 0)
const initials = computed(() => user.value?.username?.slice(0, 2).toUpperCase() || 'HE')

const currentPwd = ref('')
const newPwd = ref('')
const confirmPwd = ref('')
const saving = ref(false)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

const newPwdTooShort = computed(() => newPwd.value.length > 0 && newPwd.value.length < 8)
const mismatch = computed(() => confirmPwd.value.length > 0 && newPwd.value !== confirmPwd.value)
const canSubmit = computed(() =>
  currentPwd.value.length > 0
  && newPwd.value.length >= 8
  && newPwd.value === confirmPwd.value
  && !saving.value,
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
    flash.value = { kind: 'ok', text: 'Password updated. Existing devices remain signed in until you revoke them.' }
    currentPwd.value = ''
    newPwd.value = ''
    confirmPwd.value = ''
  } catch (error: any) {
    flash.value = {
      kind: 'err',
      text: error?.response?.status === 401
        ? 'Current password is incorrect.'
        : error?.data?.detail ?? error?.message ?? 'Failed to change password.',
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
      <p class="sv2-page-desc">Your Heya identity, account access, and password.</p>
    </header>

    <section class="account-hero">
      <div class="account-avatar">{{ initials }}</div>
      <div class="account-copy">
        <div class="account-name-row">
          <h3>{{ user?.username }}</h3>
          <StatusBadge :state="user?.is_admin ? 'warn' : 'idle'">{{ user?.is_admin ? 'Admin' : 'User' }}</StatusBadge>
        </div>
        <p>{{ user?.email }}</p>
        <div class="account-meta">
          <span>Account <strong>#{{ user?.id ?? '—' }}</strong></span>
          <span>{{ sessionCount }} signed-in {{ sessionCount === 1 ? 'device' : 'devices' }}</span>
          <span>{{ tokenCount }} API {{ tokenCount === 1 ? 'token' : 'tokens' }}</span>
        </div>
      </div>
    </section>

    <div class="profile-grid">
      <SettingsSection title="Access & security" icon="shield" description="Review every credential currently able to access this account.">
        <div class="security-links">
          <SettingsLinkCard
            to="/settings/sessions"
            title="My sessions"
            description="Browsers and devices currently signed in"
            icon="eye"
            :value="sessionCount"
            value-label="devices"
            :tone="otherSessionCount > 4 ? 'warn' : 'neutral'"
          />
          <SettingsLinkCard
            to="/settings/tokens"
            title="API tokens"
            description="Long-lived access for scripts and integrations"
            icon="key"
            :value="tokenCount"
            value-label="tokens"
          />
        </div>
        <p class="security-note">
          Changing your password does not revoke existing sessions or tokens. Review both lists if you suspect account access you do not recognise.
        </p>
      </SettingsSection>

      <SettingsSection title="Change password" icon="key" description="Use at least 8 characters. Other sessions remain active after the change.">
        <form class="password-form" @submit.prevent="changePassword">
          <SettingsField label="Current password" v-slot="{ fieldId }">
            <input :id="fieldId" v-model="currentPwd" type="password" autocomplete="current-password" class="profile-input" placeholder="•••••••••••" />
          </SettingsField>
          <SettingsField label="New password" :hint="newPwdTooShort ? 'Too short — use at least 8 characters.' : undefined" v-slot="{ fieldId, hintId }">
            <input
              :id="fieldId"
              v-model="newPwd"
              type="password"
              autocomplete="new-password"
              class="profile-input"
              :class="{ invalid: newPwdTooShort }"
              :aria-invalid="newPwdTooShort"
              :aria-describedby="hintId"
              placeholder="•••••••••••"
            />
          </SettingsField>
          <SettingsField label="Confirm new password" :hint="mismatch ? 'Passwords do not match.' : undefined" v-slot="{ fieldId, hintId }">
            <input
              :id="fieldId"
              v-model="confirmPwd"
              type="password"
              autocomplete="new-password"
              class="profile-input"
              :class="{ invalid: mismatch }"
              :aria-invalid="mismatch"
              :aria-describedby="hintId"
              placeholder="•••••••••••"
            />
          </SettingsField>

          <div class="password-actions">
            <button type="submit" class="sv2-btn primary" :disabled="!canSubmit">
              <Icon v-if="saving" name="spinner" :size="13" />
              {{ saving ? 'Updating…' : 'Update password' }}
            </button>
          </div>

          <div v-if="flash" class="password-flash" :class="flash.kind" role="status" aria-live="polite">
            <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
            {{ flash.text }}
          </div>
        </form>
      </SettingsSection>
    </div>
  </div>
</template>

<style scoped>
.account-hero {
  display: flex;
  align-items: center;
  gap: 18px;
  margin-bottom: 16px;
  padding: 20px 22px;
  border: 1px solid color-mix(in srgb, var(--gold) 20%, var(--border));
  border-radius: var(--r-lg);
  background:
    radial-gradient(circle at 100% 0%, color-mix(in srgb, var(--gold) 8%, transparent), transparent 48%),
    linear-gradient(145deg, var(--bg-1), var(--bg-2));
}
.account-avatar {
  width: 68px;
  height: 68px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: var(--accent-ink);
  font-size: 20px;
  font-weight: 750;
  letter-spacing: 0.04em;
  box-shadow: 0 8px 24px color-mix(in srgb, var(--gold) 16%, transparent);
}
.account-copy { min-width: 0; }
.account-name-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.account-name-row h3 { margin: 0; color: var(--fg-0); font-size: 20px; font-weight: 660; letter-spacing: -0.025em; }
.account-copy > p { margin: 4px 0 0; color: var(--fg-2); font-family: var(--font-mono); font-size: 12px; }
.account-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 16px; margin-top: 10px; color: var(--fg-2); font-size: 11px; }
.account-meta strong { color: var(--fg-2); font-family: var(--font-mono); }

.profile-grid { display: grid; grid-template-columns: minmax(0, 0.9fr) minmax(360px, 1.1fr); gap: 16px; align-items: start; }
.security-links { display: flex; flex-direction: column; gap: 8px; }
.security-note { margin: 14px 2px 0; color: var(--fg-2); font-size: 11.5px; line-height: 1.55; }
.password-form { display: block; }
.profile-input {
  width: 100%;
  padding: 9px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--fg-0);
  font-family: var(--font-mono);
  font-size: 13px;
  outline: none;
  transition: border-color 0.12s, background 0.12s;
}
.profile-input:focus { border-color: var(--gold); background: var(--bg-1); }
.profile-input.invalid { border-color: var(--bad); }
.password-actions { display: flex; justify-content: flex-end; padding-top: 16px; }
.password-flash { display: flex; align-items: center; gap: 8px; margin-top: 12px; padding: 10px 12px; border: 1px solid; border-radius: var(--r-sm); font-size: 11.5px; }
.password-flash.ok { border-color: color-mix(in srgb, var(--good) 25%, transparent); background: color-mix(in srgb, var(--good) 8%, transparent); color: var(--good); }
.password-flash.err { border-color: color-mix(in srgb, var(--bad) 28%, transparent); background: color-mix(in srgb, var(--bad) 8%, transparent); color: var(--bad); }

@media (max-width: 900px) {
  .profile-grid { grid-template-columns: 1fr; }
}
@media (max-width: 520px) {
  .account-hero { align-items: flex-start; padding: 17px 16px; }
  .account-avatar { width: 52px; height: 52px; font-size: 16px; }
  .account-meta { flex-direction: column; align-items: flex-start; gap: 4px; }
}
</style>
