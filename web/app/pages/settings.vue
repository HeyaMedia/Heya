<template>
  <div class="mt-layout">
    <aside class="settings-nav scroll">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 12px">Settings</div>
      <div
        v-for="tab in tabs"
        :key="tab.id"
        class="lib-item"
        :class="{ active: activeTab === tab.id }"
        @click="activeTab = tab.id"
      >
        <Icon :name="tab.icon" :size="16" />
        <span>{{ tab.label }}</span>
      </div>
    </aside>
    <div class="library-main scroll page-pad">

      <!-- Libraries -->
      <template v-if="activeTab === 'libraries'">
        <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 24px">Libraries</h2>
        <div v-if="libraries.length" style="display: flex; flex-direction: column; gap: 16px">
          <div v-for="lib in libraries" :key="lib.id" class="settings-card">
            <div style="display: flex; align-items: center; gap: 14px">
              <div class="lib-icon" :class="lib.media_type">
                <Icon :name="lib.media_type === 'movie' ? 'film' : lib.media_type === 'tv' ? 'tv' : lib.media_type === 'music' ? 'music' : 'book'" :size="20" />
              </div>
              <div>
                <div style="font-size: 16px; font-weight: 500">{{ lib.name }}</div>
                <div style="font-size: 12px; color: var(--fg-2); font-family: var(--font-mono)">{{ lib.paths.join(', ') }}</div>
              </div>
            </div>
            <div style="display: flex; gap: 8px; margin-top: 12px">
              <button class="btn-ghost-sm" @click="scanLib(lib.id)">Scan Now</button>
              <button class="btn-ghost-sm" style="color: var(--bad)">Remove</button>
            </div>
          </div>
        </div>
        <button class="btn btn-primary" style="margin-top: 20px" @click="navigateTo('/libraries')">
          <Icon name="plus" :size="16" />
          Add Library
        </button>
      </template>

      <!-- Server -->
      <template v-if="activeTab === 'server'">
        <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 24px">Server</h2>
        <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 16px; margin-bottom: 32px">
          <div class="settings-card">
            <div style="font-size: 11px; color: var(--fg-2); font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.1em">Status</div>
            <div style="font-size: 20px; font-weight: 600; margin-top: 4px; color: var(--good)">{{ health?.status === 'ok' ? 'Online' : 'Offline' }}</div>
          </div>
          <div class="settings-card">
            <div style="font-size: 11px; color: var(--fg-2); font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.1em">Database</div>
            <div style="font-size: 20px; font-weight: 600; margin-top: 4px" :style="{ color: health?.database === 'connected' ? 'var(--good)' : 'var(--bad)' }">{{ health?.database || '…' }}</div>
          </div>
        </div>
        <div class="settings-card">
          <div style="font-size: 14px; font-weight: 500; margin-bottom: 12px">API Reference</div>
          <div style="display: flex; gap: 12px">
            <a href="/api/openapi.json" target="_blank" class="btn-ghost-sm">OpenAPI Spec</a>
            <a href="/api/docs" target="_blank" class="btn-ghost-sm">Scalar Docs</a>
          </div>
        </div>
      </template>

      <!-- Users -->
      <template v-if="activeTab === 'users'">
        <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 24px">Users</h2>
        <div style="display: flex; align-items: center; gap: 14px; margin-bottom: 20px">
          <div class="avatar" style="width: 48px; height: 48px; font-size: 14px">
            {{ user?.username?.slice(0, 2).toUpperCase() }}
          </div>
          <div>
            <div style="font-size: 16px; font-weight: 500">{{ user?.username }}</div>
            <div style="font-size: 12px; color: var(--fg-2)">{{ user?.email }}</div>
          </div>
          <Chip v-if="user?.is_admin" gold>Admin</Chip>
        </div>
      </template>

      <!-- About -->
      <template v-if="activeTab === 'about'">
        <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 24px">About Heya</h2>
        <div class="settings-card">
          <div style="font-family: var(--font-mono); font-size: 13px; color: var(--fg-1); line-height: 2">
            <div>Server: <span style="color: var(--fg-0)">Heya v1.0.0</span></div>
            <div>Backend: <span style="color: var(--fg-0)">Go 1.26</span></div>
            <div>Frontend: <span style="color: var(--fg-0)">Nuxt 4</span></div>
            <div>Database: <span style="color: var(--fg-0)">PostgreSQL 17</span></div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Library, HealthResponse } from '~~/shared/types'

const { user, isAuthenticated } = useAuth()

const activeTab = ref('libraries')
const libraries = ref<Library[]>([])
const health = ref<HealthResponse | null>(null)

const tabs = [
  { id: 'libraries', label: 'Libraries', icon: 'folder' },
  { id: 'server', label: 'Server', icon: 'network' },
  { id: 'users', label: 'Users', icon: 'user' },
  { id: 'about', label: 'About', icon: 'globe' },
]

async function scanLib(id: number) {
  try { await apiFetch(`/api/libraries/${id}/scan`, { method: 'POST' }) } catch {}
}

onMounted(async () => {
  const [libRes, healthRes] = await Promise.allSettled([
    apiFetch<Library[]>('/api/libraries'),
    $fetch<HealthResponse>('/api/health'),
  ])
  if (libRes.status === 'fulfilled') libraries.value = libRes.value
  if (healthRes.status === 'fulfilled') health.value = healthRes.value
})
</script>

<style scoped>
.settings-nav {
  width: 240px;
  flex-shrink: 0;
  background: var(--bg-2);
  border-right: 1px solid var(--border);
  padding: 20px 10px;
}
.settings-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 20px;
}
.lib-icon {
  width: 40px; height: 40px;
  border-radius: var(--r-md);
  display: flex; align-items: center; justify-content: center;
  background: var(--gold-soft);
  color: var(--gold);
}
.avatar {
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
  font-weight: 700;
  display: flex; align-items: center; justify-content: center;
}
</style>
