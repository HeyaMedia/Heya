<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()
import { adminUsersQuery, castConfigQuery, castStatusQuery } from '~/queries/settings'

const enabled = ref(false)
const baseUrl = ref('')
const devices = ref('')
const allowedUserIds = ref<number[]>([])
const configData = useQuery(castConfigQuery())
const statusData = useQuery(castStatusQuery())
const usersData = useQuery(adminUsersQuery())
const users = computed(() => usersData.data.value ?? [])
const status = computed(() => statusData.data.value ?? null)
const loading = computed(() => configData.isLoading.value || usersData.isLoading.value)
const saving = ref(false)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

watch(() => configData.data.value, (value) => {
  if (!value) return
  enabled.value = value.enabled
  baseUrl.value = value.base_url
  devices.value = value.devices
  const live = usersData.data.value ? new Set(usersData.data.value.map(user => user.id)) : null
  allowedUserIds.value = (value.allowed_user_ids ?? []).filter(id => !live || live.has(id))
}, { immediate: true })

// User deletion may leave an old ID in a persisted allowlist from an older
// server version. Keep the editor self-healing; the next save drops it.
watch(() => usersData.data.value, (value) => {
  if (!value) return
  const live = new Set(value.map(user => user.id))
  allowedUserIds.value = allowedUserIds.value.filter(id => live.has(id))
}, { immediate: true })

async function save(next: { enabled?: boolean, baseUrl?: string, devices?: string, allowedUserIds?: number[] }) {
  saving.value = true
  flash.value = null
  try {
    const res = await $heya('/api/cast/config', {
      method: 'PUT',
      body: {
        enabled: next.enabled ?? enabled.value,
        base_url: next.baseUrl ?? baseUrl.value,
        devices: next.devices ?? devices.value,
        allowed_user_ids: next.allowedUserIds ?? allowedUserIds.value,
      },
    })
    enabled.value = res.enabled
    baseUrl.value = res.base_url
    devices.value = res.devices
    allowedUserIds.value = [...(res.allowed_user_ids ?? [])]
    flash.value = { kind: 'ok', text: res.enabled ? 'Casting enabled — discovery is running.' : 'Casting disabled.' }
    await statusData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Save failed.' }
    await configData.refetch()
  } finally {
    saving.value = false
  }
}

function toggleUserAccess(userId: number, allowed: boolean) {
  const next = new Set(allowedUserIds.value)
  if (allowed) next.add(userId)
  else next.delete(userId)
  allowedUserIds.value = [...next].sort((a, b) => a - b)
}

// Diagnostics stay live while the page is open — discovery results and
// static-target retries land on a ~minute cadence.
let refreshTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  ensureSources()
  refreshTimer = setInterval(() => { void statusData.refetch() }, 10_000)
})
onScopeDispose(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})

const deviceRows = computed(() =>
  (status.value?.devices ?? []).map((d) => ({
    name: d.name,
    provider: d.provider,
    model: [d.manufacturer, d.model].filter(Boolean).join(' '),
    addr: `${d.addr}:${d.port}`,
    mediaOrigin: d.media_origin,
    seen: timeAgoShort(d.last_seen),
  })))

// The #1 "no devices" cause is a subnet mismatch: highlight when every
// discovered receiver (or none at all) sits outside the server's legs.
const interfaceList = computed(() => status.value?.interfaces ?? [])
</script>

<template>
  <div class="settings-page">
    <SettingsContextHero
      title="Casting"
      icon="cast"
      eyebrow="Server · Playback"
      description="Heya controls AirPlay and Chromecast receivers itself — clients only send controls. Discovery and playback both require the server and receiver to share a reachable network path."
    />

    <SettingsSection
      title="Server-side casting"
      icon="cast"
      :description="enabled
        ? 'On — the server browses for receivers and streams to them on request.'
        : 'Off — no discovery, no cast sessions.'"
      :lockedBy="isLocked('cast.enabled') ? lockTooltip('cast.enabled') : undefined"
    >
      <template #actions>
        <label class="cs-switch" :title="lockTooltip('cast.enabled')">
          <input
            type="checkbox"
            aria-label="Enable server-side casting"
            :checked="enabled"
            :disabled="loading || saving || isLocked('cast.enabled')"
            @change="save({ enabled: ($event.target as HTMLInputElement).checked })"
          />
          <span class="cs-slider" />
        </label>
      </template>

      <div v-if="flash" class="cs-flash" :class="flash.kind" :role="flash.kind === 'err' ? 'alert' : 'status'" aria-live="polite">{{ flash.text }}</div>

      <p class="cs-hint">
        Receivers are found via mDNS, which only works on networks the server is
        directly attached to — multicast does not cross containers or VLANs. If the
        list below stays empty, give the container an interface on the receivers'
        network (<code>hostNetwork</code> / macvlan) or enable mDNS reflection on
        your router. Details in <code>docs/deployment.md</code>.
      </p>
    </SettingsSection>

    <SettingsSection
      title="User access"
      icon="users"
      description="Choose which regular users may discover and control server-side cast receivers. Admins always retain access."
    >
      <div v-if="loading" class="cs-empty">Loading users…</div>
      <div v-else class="cs-user-list">
        <label v-for="u in users" :key="u.id" class="cs-user-row" :class="{ implicit: u.is_admin }">
          <input
            type="checkbox"
            :checked="u.is_admin || allowedUserIds.includes(u.id)"
            :disabled="saving || u.is_admin"
            :aria-label="`Allow ${u.username} to cast`"
            @change="toggleUserAccess(u.id, ($event.target as HTMLInputElement).checked)"
          />
          <span class="cs-user-copy">
            <span class="cs-user-name">{{ u.username }}</span>
            <span class="cs-user-email">{{ u.email }}</span>
          </span>
          <StatusBadge :state="u.is_admin ? 'warn' : (allowedUserIds.includes(u.id) ? 'ok' : 'idle')">
            {{ u.is_admin ? 'admin · always allowed' : (allowedUserIds.includes(u.id) ? 'allowed' : 'blocked') }}
          </StatusBadge>
        </label>
      </div>
      <div class="cs-access-actions">
        <p class="cs-hint">
          Blocked users do not receive receiver/session data and all cast API calls return forbidden. Revoking access also stops that user's active cast sessions.
        </p>
        <button class="btn btn-primary" :disabled="loading || saving" @click="save({ allowedUserIds })">Save access</button>
      </div>
    </SettingsSection>

    <SettingsSection
      title="Receiver media URL"
      icon="link"
      description="Chromecast and later URL-pull receivers fetch media back from this Heya origin."
      :lockedBy="isLocked('cast.base_url') ? lockTooltip('cast.base_url') : undefined"
    >
      <div class="cs-devices-row">
        <input
          v-model="baseUrl"
          type="url"
          class="cs-devices-input"
          placeholder="Automatic — server LAN address"
          aria-label="Receiver-facing Heya URL"
          :disabled="saving || isLocked('cast.base_url')"
          @keydown.enter="save({ baseUrl })"
        />
        <button
          class="btn btn-primary"
          :disabled="saving || isLocked('cast.base_url')"
          @click="save({ baseUrl })"
        >Save</button>
      </div>
      <p class="cs-hint">
		Leave empty to derive <code>https://&lt;server-LAN-IP&gt;:HEYA_PORT</code>
		for each receiver. URL-pull receivers that cannot trust Heya’s local CA
		need an explicit browser-trusted <code>https://</code> origin. Also
		settable with <code>HEYA_CAST_BASE_URL=…</code>.
      </p>
    </SettingsSection>

    <SettingsSection
      title="Network diagnostics"
      icon="network"
      :description="status?.running ? 'Discovery is running.' : 'Discovery is not running.'"
    >
      <div class="cs-diag-block">
        <div class="cs-diag-label">Server network legs</div>
        <p class="cs-diag-note">mDNS can only hear receivers sharing one of these subnets.</p>
        <KVTable v-if="interfaceList.length" :rows="interfaceList.map(i => ({ key: i.name, value: i.addr, mono: true }))" />
        <p v-else class="cs-empty">No usable interfaces reported.</p>
      </div>

      <div class="cs-diag-block">
        <div class="cs-diag-label">Discovered receivers</div>
        <table v-if="deviceRows.length" class="cs-table">
          <thead><tr><th>Name</th><th>Protocol</th><th>Model</th><th>Address</th><th>Heya media origin</th><th>Last seen</th></tr></thead>
          <tbody>
            <tr v-for="d in deviceRows" :key="d.addr">
              <td>{{ d.name }}</td>
              <td>{{ d.provider }}</td>
              <td>{{ d.model }}</td>
              <td class="cs-mono">{{ d.addr }}</td>
              <td class="cs-mono">{{ d.mediaOrigin || 'unresolved' }}</td>
              <td>{{ d.seen }}</td>
            </tr>
          </tbody>
        </table>
        <p v-else class="cs-empty">
          Nothing discovered yet. If receivers exist, the server can't hear their
          mDNS — check the network legs above against the receivers' subnet.
        </p>
      </div>

      <div v-if="status?.static?.length" class="cs-diag-block">
        <div class="cs-diag-label">Pinned receivers (unicast)</div>
        <table class="cs-table">
          <thead><tr><th>Address</th><th>Status</th><th>Checked</th></tr></thead>
          <tbody>
            <tr v-for="s in status.static" :key="s.addr">
              <td class="cs-mono">{{ s.addr }}</td>
              <td>
                <span v-if="s.ok" class="cs-ok">✓ {{ s.name }}</span>
                <span v-else-if="s.error" class="cs-err" :title="s.error">✗ {{ s.error }}</span>
                <span v-else class="cs-empty">pending…</span>
              </td>
              <td>{{ s.checked_at && !s.checked_at.startsWith('0001') ? timeAgoShort(s.checked_at) : '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </SettingsSection>

    <SettingsSection
      title="Pinned receivers"
      icon="speakerhigh"
      description="Addresses resolved by direct unicast mDNS query — for networks that filter multicast. Receivers only answer unicast from their OWN subnet, so this cannot cross VLANs."
      :lockedBy="isLocked('cast.devices') ? lockTooltip('cast.devices') : undefined"
    >
      <div class="cs-devices-row">
        <input
          v-model="devices"
          type="text"
          class="cs-devices-input"
          placeholder="192.168.1.216, 192.168.1.242"
          aria-label="Pinned receiver addresses"
          :disabled="saving || isLocked('cast.devices')"
          @keydown.enter="save({ devices })"
        />
        <button
          class="btn btn-primary"
          :disabled="saving || isLocked('cast.devices')"
          @click="save({ devices })"
        >Save</button>
      </div>
      <p class="cs-hint">
        Comma-separated IPs (or <code>ip:port</code>). Saving restarts discovery;
        any active cast session stops. Also settable with
        <code>HEYA_CAST_DEVICES=…</code>, which locks this field.
      </p>
    </SettingsSection>
  </div>
</template>

<style scoped>
.cs-switch {
  position: relative;
  display: inline-block;
  width: 42px;
  height: 24px;
  flex: none;
}
.cs-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.cs-slider {
  position: absolute;
  inset: 0;
  border-radius: 999px;
  background: color-mix(in oklab, var(--text) 18%, transparent);
  transition: background 0.15s ease;
  cursor: pointer;
}
.cs-slider::before {
  content: '';
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--surface-0);
  transition: transform 0.15s ease;
}
.cs-switch input:checked + .cs-slider {
  background: var(--accent);
}
.cs-switch input:checked + .cs-slider::before {
  transform: translateX(18px);
}
.cs-switch input:disabled + .cs-slider {
  opacity: 0.5;
  cursor: not-allowed;
}
.cs-flash {
  margin: 0 0 12px;
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 13px;
}
.cs-flash.ok {
  background: color-mix(in srgb, var(--good) 14%, transparent);
}
.cs-flash.err {
  background: color-mix(in srgb, var(--bad) 16%, transparent);
}
.cs-hint {
  margin-top: 12px;
  font-size: 13px;
  color: var(--fg-2);
  line-height: 1.55;
}
.cs-hint code {
  font-size: 12px;
}
.cs-diag-block {
  margin-bottom: 18px;
}
.cs-diag-label {
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--fg-2);
  margin-bottom: 4px;
}
.cs-diag-note {
  font-size: 12px;
  color: var(--fg-3);
  margin: 0 0 8px;
}
.cs-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.cs-table th {
  text-align: left;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--fg-3);
  font-weight: 600;
  padding: 4px 10px 4px 0;
  border-bottom: 1px solid var(--border);
}
.cs-table td {
  padding: 6px 10px 6px 0;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  color: var(--fg-1);
}
.cs-mono {
  font-family: var(--font-mono);
  font-size: 12px;
}
.cs-ok { color: var(--good); }
.cs-err { color: var(--bad); }
.cs-empty {
  font-size: 13px;
  color: var(--fg-3);
}
.cs-devices-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.cs-user-list {
  display: grid;
  gap: 8px;
}
.cs-user-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 48px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 9px;
  cursor: pointer;
}
.cs-user-row.implicit {
  cursor: default;
}
.cs-user-copy {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 1px;
}
.cs-user-name {
  color: var(--fg-0);
  font-size: 13px;
  font-weight: 600;
}
.cs-user-email {
  overflow: hidden;
  color: var(--fg-3);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cs-access-actions {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-top: 12px;
}
.cs-access-actions .cs-hint {
  margin: 0;
  max-width: 680px;
}
.cs-devices-input {
  flex: 1;
  min-width: 0;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: rgb(var(--ink) / 0.03);
  color: var(--fg-0);
  font-family: var(--font-mono);
  font-size: 12px;
}
@media (max-width: 640px) {
  .cs-access-actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
