<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import type { components } from '#open-fetch-schemas/heya'
type AuthSession = components['schemas']['AuthSessionView']

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const sessions = ref<AuthSession[]>([])
const loading = ref(true)
const { flash } = useFlash()

async function load() {
  loading.value = true
  try {
    sessions.value = await $heya('/api/me/auth-sessions')
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load sessions.' }
  } finally {
    loading.value = false
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
    sessions.value = sessions.value.filter(x => x.id !== s.id)
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
    sessions.value = sessions.value.filter(s => s.current)
    flash.value = { kind: 'ok', text: `${others} other ${others === 1 ? 'device was' : 'devices were'} signed out.` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to sign out other devices.' }
  }
}

// describeAgent / agentIcon / formatExpiry come from useUserAgent.ts,
// timeAgo from useFormat.ts — all auto-imported.

const otherCount = computed(() => sessions.value.filter(s => !s.current).length)

onMounted(load)
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
      <SettingsSection title="Active devices" icon="eye">
        <template #actions>
          <button
            v-if="otherCount > 0"
            class="sv2-btn ghost"
            @click="revokeOthers"
          >
            <Icon name="sign-out" :size="13" />
            Sign out other devices ({{ otherCount }})
          </button>
        </template>

        <div v-if="sessions.length === 0" class="empty-state">
          <Icon name="info" :size="14" />
          No active sessions — that's unusual, you'd be signed out.
        </div>

        <div v-else class="session-list">
          <div
            v-for="s in sessions"
            :key="s.id"
            class="session-card"
            :class="{ current: s.current }"
          >
            <div class="session-icon">
              <Icon :name="agentIcon(s.user_agent ?? '')" :size="18" />
            </div>
            <div class="session-info">
              <div class="session-name">
                {{ describeAgent(s.user_agent ?? '') }}
                <StatusBadge v-if="s.current" state="ok">This device</StatusBadge>
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
              :disabled="s.current"
              :title="s.current ? 'You can\'t sign yourself out from here — use the avatar menu' : 'Sign out this device'"
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
  border-color: rgba(111, 191, 124, 0.30);
  background: rgba(111, 191, 124, 0.04);
}

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
  color: var(--fg-4);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-meta {
  font-size: 11.5px;
  color: var(--fg-3);
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
  background: rgba(217, 107, 107, 0.12);
  color: var(--bad);
}
.session-revoke:disabled { opacity: 0.3; cursor: not-allowed; }

.sv2-btn.ghost:hover {
  border-color: rgba(217, 107, 107, 0.30);
  color: var(--bad);
}
</style>
