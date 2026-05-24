<template>
  <div class="settings-layout">
    <aside class="settings-sidebar scroll">
      <div class="sidebar-header">
        <Icon name="settings" :size="18" />
        <span>Settings</span>
      </div>
      <nav class="sidebar-nav">
        <NuxtLink
          v-for="tab in tabs"
          :key="tab.to"
          :to="tab.to"
          class="nav-item"
          :class="{ active: isActive(tab.to) }"
        >
          <div class="nav-icon">
            <Icon :name="tab.icon" :size="16" />
          </div>
          <div class="nav-text">
            <span class="nav-label">{{ tab.label }}</span>
            <span class="nav-hint">{{ tab.hint }}</span>
          </div>
        </NuxtLink>
      </nav>
      <div class="sidebar-footer">
        <div class="version-tag">Heya v1.0.0</div>
      </div>
    </aside>
    <div class="settings-content scroll">
      <div class="settings-page">
        <NuxtPage />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const route = useRoute()

function isActive(to: string) {
  if (to === '/settings') return route.path === '/settings' || route.path === '/settings/'
  return route.path.startsWith(to)
}

const tabs = [
  { to: '/settings', label: 'Dashboard', icon: 'chart-bar', hint: 'Overview & stats' },
  { to: '/settings/libraries', label: 'Libraries', icon: 'folder', hint: 'Media sources' },
  { to: '/settings/users', label: 'Users', icon: 'users', hint: 'Accounts & access' },
  { to: '/settings/server', label: 'Server', icon: 'hard-drives', hint: 'Health & diagnostics' },
  { to: '/settings/tailscale', label: 'Tailscale', icon: 'cloud', hint: 'tsnet & Funnel' },
  { to: '/settings/transcoding', label: 'Transcoding', icon: 'film', hint: 'Hardware & cache' },
  { to: '/settings/metadata', label: 'Metadata Editor', icon: 'database', hint: 'Edit & manage' },
  { to: '/settings/providers', label: 'Providers', icon: 'key', hint: 'API credentials' },
  { to: '/settings/sonicanalysis', label: 'Sonic Analysis', icon: 'music', hint: 'ML pipeline + models' },
  { to: '/settings/tasks', label: 'Tasks', icon: 'timer', hint: 'Scheduled & recurring' },
  { to: '/settings/jobs', label: 'Jobs', icon: 'list', hint: 'Queue monitor' },
  { to: '/settings/logs', label: 'Logs', icon: 'clipboard', hint: 'Server log stream' },
  { to: '/settings/about', label: 'About', icon: 'info', hint: 'Version & credits' },
]
</script>

<style scoped>
.settings-layout {
  display: flex;
  height: 100%;
}

.settings-sidebar {
  width: 260px;
  flex-shrink: 0;
  background: var(--bg-2);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
}

.sidebar-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 24px 20px 20px;
  font-size: 15px;
  font-weight: 600;
  color: var(--fg-0);
  letter-spacing: -0.01em;
}

.sidebar-nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0 10px;
  flex: 1;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: var(--r-md);
  cursor: pointer;
  text-decoration: none;
  transition: all 0.15s ease;
  position: relative;
}

.nav-item:hover {
  background: rgba(255, 255, 255, 0.04);
}

.nav-item.active {
  background: var(--gold-soft);
}

.nav-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 10px;
  bottom: 10px;
  width: 3px;
  border-radius: 2px;
  background: var(--gold);
}

.nav-icon {
  width: 32px;
  height: 32px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  background: rgba(255, 255, 255, 0.03);
  flex-shrink: 0;
  transition: all 0.15s ease;
}

.nav-item:hover .nav-icon {
  color: var(--fg-1);
  background: rgba(255, 255, 255, 0.06);
}

.nav-item.active .nav-icon {
  color: var(--gold);
  background: rgba(230, 185, 74, 0.12);
}

.nav-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.nav-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
  transition: color 0.15s ease;
}

.nav-item.active .nav-label {
  color: var(--gold-bright);
  font-weight: 600;
}

.nav-hint {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 1px;
}

.nav-item.active .nav-hint {
  color: rgba(230, 185, 74, 0.5);
}

.sidebar-footer {
  padding: 16px 20px;
  border-top: 1px solid var(--border);
}

.version-tag {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-4);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.settings-content {
  flex: 1;
  min-width: 0;
  background: var(--bg-1);
}

.settings-page {
  padding: 32px 48px 80px;
  max-width: 960px;
}

@media (max-width: 1100px) {
  .settings-page {
    padding: 24px 24px 80px;
  }
}
</style>
