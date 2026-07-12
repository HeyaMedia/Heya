<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import { mySessionsQuery } from '~/queries/settings'
import type { AuthSession } from '~/queries/settings'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const sessionsData = useQuery(mySessionsQuery())
const sessions = computed(() => sessionsData.data.value ?? [])
const loading = computed(() => sessionsData.isLoading.value)
const { flash } = useFlash()

async function load() {
  try {
    await sessionsData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load sessions.' }
  }
}

async function revoke(s: AuthSession) {
  if (s.current) return
  const ok = await confirm({
    title: 'Sign out this device?',
    message: `The device using "${describeAgent(s.user_agent ?? '')}" will be signed out. They can sign back in by entering credentials again.`,
    destructive: true,
    confirmLabel: 'Sign out',
  })
  if (!ok) return
  try {
    await $heya('/api/me/auth-sessions/{id}', { method: 'DELETE', path: { id: s.id } })
    await load()
    flash.value = { kind: 'ok', text: 'Device signed out.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to sign out device.' }
  }
}

async function revokeOthers() {
  const others = sessions.value.filter(s => !s.current).length
  if (others === 0) return
  const ok = await confirm({
    title: 'Sign out other devices?',
    message: `${others} other ${others === 1 ? 'device' : 'devices'} will be signed out. You'll stay signed in on this one.`,
    destructive: true,
    confirmLabel: 'Sign out others',
  })
  if (!ok) return
  try {
    await $heya('/api/me/auth-sessions/revoke-others', { method: 'POST' })
    await load()
    flash.value = { kind: 'ok', text: `${others} other ${others === 1 ? 'device was' : 'devices were'} signed out.` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to sign out other devices.' }
  }
}

// describeAgent / agentIcon / formatExpiry come from useUserAgent.ts,
// timeAgo from useFormat.ts — all auto-imported.

const otherCount = computed(() => sessions.value.filter(s => !s.current).length)
const currentSession = computed(() => sessions.value.find(session => session.current) ?? null)
const otherSessions = computed(() => sessions.value.filter(session => !session.current))

</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">My sessions</h2>
      <p class="sv2-page-desc">
        Browsers and CLIs signed in to your account. Sign out a single device
        or boot every other device at once.
      </p>
    </header>

    <div v-if="loading" class="loading-state">
      <Icon name="spinner" :size="16" /> Loading…
    </div>

    <template v-else>
      <div class="session-summary">
        <div class="session-summary-copy">
          <span class="session-summary-icon"><Icon name="shield" :size="18" /></span>
          <div>
            <strong>{{ sessions.length }} active {{ sessions.length === 1 ? 'session' : 'sessions' }}</strong>
            <p>{{ otherCount ? `${otherCount} other ${otherCount === 1 ? 'device can' : 'devices can'} access your account.` : 'Only this device is signed in.' }}</p>
          </div>
        </div>
        <StatusBadge :state="otherCount > 4 ? 'warn' : 'ok'">{{ otherCount > 4 ? 'Review access' : 'Looks good' }}</StatusBadge>
      </div>

      <SettingsSection v-if="currentSession" title="This device" icon="cpu" description="The browser session you are using right now.">
        <div class="session-card current featured">
          <div class="session-icon"><Icon :name="agentIcon(currentSession.user_agent ?? '')" :size="18" /></div>
          <div class="session-info">
            <div class="session-name">
              {{ describeAgent(currentSession.user_agent ?? '') }}
              <StatusBadge state="ok">Current</StatusBadge>
            </div>
            <div class="session-ua">{{ currentSession.user_agent || 'No User-Agent recorded' }}</div>
            <div class="session-meta">
              <span>Last seen {{ timeAgo(currentSession.last_seen_at) }}</span>
              <span v-if="currentSession.ip">· {{ currentSession.ip }}</span>
              <span>· signed in {{ timeAgo(currentSession.created_at) }}</span>
              <span>· {{ formatExpiry(currentSession.expires_at) }}</span>
            </div>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title="Other devices" icon="eye" :description="otherCount ? 'Revoke anything you no longer recognise or use.' : 'No other browsers or devices are signed in.'">
        <template #actions>
          <button
            v-if="otherCount > 0"
            class="sv2-btn danger"
            @click="revokeOthers"
          >
            <Icon name="sign-out" :size="13" />
            Sign out all ({{ otherCount }})
          </button>
        </template>

        <div v-if="otherSessions.length === 0" class="empty-state good-empty">
          <Icon name="check" :size="15" />
          No other devices have access to this account.
        </div>

        <div v-else class="session-list">
          <div
            v-for="s in otherSessions"
            :key="s.id"
            class="session-card"
          >
            <div class="session-icon">
              <Icon :name="agentIcon(s.user_agent ?? '')" :size="18" />
            </div>
            <div class="session-info">
              <div class="session-name">
                {{ describeAgent(s.user_agent ?? '') }}
              </div>
              <div class="session-ua">{{ s.user_agent || 'No User-Agent recorded' }}</div>
              <div class="session-meta">
                <span>Last seen {{ timeAgo(s.last_seen_at) }}</span>
                <span v-if="s.ip">· {{ s.ip }}</span>
                <span>· signed in {{ timeAgo(s.created_at) }}</span>
                <span>· {{ formatExpiry(s.expires_at) }}</span>
              </div>
            </div>
            <button
              class="session-revoke"
              title="Sign out this device"
              aria-label="Sign out this device"
              @click="revoke(s)"
            >
              <Icon name="close" :size="14" />
            </button>
          </div>
        </div>
      </SettingsSection>

      <SettingsFlash :flash="flash" />
    </template>
  </div>
</template>

<style scoped>
.loading-state, .empty-state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-3);
  font-size: 13px;
  padding: 20px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.empty-state.good-empty { color: var(--good); border-color: color-mix(in srgb, var(--good) 22%, var(--border)); background: color-mix(in srgb, var(--good) 5%, var(--bg-2)); }

.session-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
  padding: 15px 18px;
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: linear-gradient(145deg, var(--bg-1), var(--bg-2));
}
.session-summary-copy { min-width: 0; display: flex; align-items: center; gap: 12px; }
.session-summary-icon { width: 38px; height: 38px; display: grid; place-items: center; flex-shrink: 0; border-radius: var(--r-sm); background: color-mix(in srgb, var(--good) 10%, transparent); color: var(--good); }
.session-summary-copy strong { color: var(--fg-0); font-size: 13px; font-weight: 620; }
.session-summary-copy p { margin: 3px 0 0; color: var(--fg-2); font-size: 11.5px; }

.session-list { display: flex; flex-direction: column; gap: 8px; }
.session-card {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color 0.12s, background 0.12s;
}
.session-card.current {
  border-color: color-mix(in srgb, var(--good) 30%, transparent);
  background: color-mix(in srgb, var(--good) 4%, transparent);
}
.session-card.featured { padding: 16px 17px; }

.session-icon {
  width: 36px;
  height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  flex-shrink: 0;
}
.session-card.current .session-icon { color: var(--good); }

.session-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.session-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
}
.session-ua {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-meta {
  font-size: 11.5px;
  color: var(--fg-2);
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.session-revoke {
  width: 28px;
  height: 28px;
  border-radius: var(--r-sm);
  color: var(--fg-3);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: background 0.12s, color 0.12s;
}
.session-revoke:hover:not(:disabled) {
  background: color-mix(in srgb, var(--bad) 12%, transparent);
  color: var(--bad);
}
.session-revoke:disabled { opacity: 0.3; cursor: not-allowed; }
@media (pointer: coarse) {
  .session-revoke { width: 44px; height: 44px; }
}

@media (max-width: 620px) {
  .session-summary { align-items: flex-start; }
  .session-summary > :last-child { display: none; }
  .session-card { padding: 13px 12px; }
  .session-meta { gap: 3px 6px; }
}
</style>
