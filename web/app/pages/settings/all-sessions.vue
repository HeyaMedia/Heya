<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminSessionsQuery } from '~/queries/settings'
import type { AdminSession } from '~/queries/settings'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const sessionsData = useQuery(adminSessionsQuery())
const sessions = computed(() => sessionsData.data.value ?? [])
const loading = computed(() => sessionsData.isLoading.value)
const kindFilter = ref<'' | 'session' | 'api_token'>('')
const userFilter = ref<string>('')
const { flash } = useFlash()

async function load() {
  try {
    await sessionsData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load sessions.' }
  }
}

async function revoke(s: AdminSession) {
  const ok = await confirm({
    title: 'Revoke this session?',
    message: s.kind === 'api_token'
      ? `Deletes ${s.username}'s API token "${s.name || 'unnamed'}". Any client using it will start getting 401s immediately.`
      : `Signs ${s.username} out from "${describeAgent(s.user_agent ?? '')}". They can sign back in by entering credentials again.`,
    destructive: true,
    confirmLabel: 'Revoke',
  })
  if (!ok) return
  try {
    await $heya('/api/admin/sessions/{id}', { method: 'DELETE', path: { id: s.id } })
    await load()
    flash.value = { kind: 'ok', text: 'Session revoked.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Revoke failed.' }
  }
}

// describeAgent / agentIcon / formatExpiry come from useUserAgent.ts,
// timeAgo from useFormat.ts — all auto-imported. agentIcon takes the raw
// UA string; API tokens pick "key" at the call site.

const filtered = computed(() => sessions.value.filter(s => {
  if (kindFilter.value && s.kind !== kindFilter.value) return false
  if (userFilter.value && s.username !== userFilter.value) return false
  return true
}))

const counts = computed(() => ({
  total: sessions.value.length,
  sessions: sessions.value.filter(s => s.kind === 'session').length,
  apiTokens: sessions.value.filter(s => s.kind === 'api_token').length,
  users: new Set(sessions.value.map(s => s.username)).size,
}))

const allUsernames = computed(() =>
  Array.from(new Set(sessions.value.map(s => s.username))).sort(),
)

</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">All sessions</h2>
      <p class="sv2-page-desc">
        Browser sessions and API tokens across every user. Admin-only —
        you can sign anyone out, but a revoked browser session just kicks
        the user back to login.
      </p>
    </header>

    <div class="tiles">
      <MetricTile label="Total" :value="counts.total" icon="eye" />
      <MetricTile label="Browser sessions" :value="counts.sessions" icon="cpu" />
      <MetricTile label="API tokens" :value="counts.apiTokens" icon="key" />
      <MetricTile label="Distinct users" :value="counts.users" icon="users" />
    </div>

    <SettingsSection title="Active sessions" icon="eye">
      <template #actions>
        <select v-model="kindFilter" class="sv2-select">
          <option value="">All kinds</option>
          <option value="session">Browser sessions</option>
          <option value="api_token">API tokens</option>
        </select>
        <select v-model="userFilter" class="sv2-select">
          <option value="">All users</option>
          <option v-for="u in allUsernames" :key="u" :value="u">{{ u }}</option>
        </select>
      </template>

      <div v-if="loading" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>

      <div v-else-if="filtered.length === 0" class="empty-state">
        <Icon name="info" :size="14" />
        {{ sessions.length === 0 ? 'No active sessions.' : 'No sessions match the filter.' }}
      </div>

      <div v-else class="sess-list">
        <div v-for="s in filtered" :key="s.id" class="sess-card" :class="s.kind">
          <div class="sess-icon" :class="s.kind"><Icon :name="s.kind === 'api_token' ? 'key' : agentIcon(s.user_agent ?? '')" :size="16" /></div>
          <div class="sess-body">
            <div class="sess-row">
              <span class="sess-user">{{ s.username }}</span>
              <StatusBadge v-if="s.is_admin" state="warn">admin</StatusBadge>
              <StatusBadge :state="s.kind === 'api_token' ? 'idle' : 'ok'">
                {{ s.kind === 'api_token' ? 'token' : 'session' }}
              </StatusBadge>
              <span v-if="s.name" class="sess-name">"{{ s.name }}"</span>
            </div>
            <div v-if="s.kind === 'session'" class="sess-ua">
              {{ describeAgent(s.user_agent ?? '') }}
              <span class="sess-ua-raw">· {{ s.user_agent || 'no user-agent' }}</span>
            </div>
            <div class="sess-meta">
              <span>last seen {{ timeAgo(s.last_seen_at) }}</span>
              <span v-if="s.ip">· {{ s.ip }}</span>
              <span>· signed in {{ timeAgo(s.created_at) }}</span>
              <span>· {{ formatExpiry(s.expires_at) }}</span>
            </div>
          </div>
          <button class="sess-revoke" :title="`Revoke session #${s.id}`" @click="revoke(s)">
            <Icon name="close" :size="14" />
          </button>
        </div>
      </div>
    </SettingsSection>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
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

.sv2-select {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-size: 12px;
  padding: 6px 10px;
  cursor: pointer;
  outline: none;
}
.sv2-select:focus { border-color: var(--gold); }

.sess-list { display: flex; flex-direction: column; gap: 8px; }
.sess-card {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color 0.15s ease;
}
.sess-card:hover { border-color: var(--border-strong); }
.sess-card.api_token { border-left: 3px solid var(--gold); padding-left: 14px; }

.sess-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--fg-3);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.sess-icon.api_token { color: var(--gold); }
.sess-icon.session { color: var(--good); }

.sess-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.sess-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.sess-user { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.sess-name { font-family: var(--font-mono); font-size: 11px; color: var(--gold); }

.sess-ua {
  font-size: 12px; color: var(--fg-2);
  display: flex; gap: 6px; flex-wrap: wrap;
}
.sess-ua-raw {
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-4);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  min-width: 0; flex: 1;
}
.sess-meta {
  font-family: var(--font-mono); font-size: 11px; color: var(--fg-3);
  display: flex; flex-wrap: wrap; gap: 4px;
}

.sess-revoke {
  width: 30px; height: 30px;
  border-radius: var(--r-sm);
  color: var(--fg-3);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  transition: background 0.12s, color 0.12s;
}
.sess-revoke:hover {
  background: color-mix(in srgb, var(--bad) 12%, transparent);
  color: var(--bad);
}

/* Phone: minmax(180px) only fits 1 column at 390px — force 2. */
@media (max-width: 720px) {
  .tiles { grid-template-columns: repeat(2, 1fr); }
}
</style>
