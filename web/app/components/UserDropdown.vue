<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'

const { user, logout } = useAuth()
const { prefs, set } = useAppearance()
const settingsTarget = computed(() => user.value?.is_admin ? '/settings/dashboard' : '/settings/profile')

// The user-scoped settings links, straight from the settings nav's "You"
// group — one source of truth, so this menu stays in sync with the
// settings sidebar automatically.
const { groups } = useSettingsNav()
const youItems = computed(() => groups.value.find(g => g.id === 'you')?.items ?? [])

const THEMES = [
  { value: 'dark' as const, label: 'Dark' },
  { value: 'light' as const, label: 'Light' },
  { value: 'oled' as const, label: 'OLED' },
]
</script>

<template>
  <AppMenu :width="280" trigger-class="ud-trigger" :trigger-title="user?.username ?? ''">
    <template #trigger>
      <div class="ud-avatar">
        <span>{{ user?.username?.slice(0, 2).toUpperCase() }}</span>
      </div>
    </template>

    <div class="surface-header ud-header">
      <div class="ud-header-avatar">
        <span>{{ user?.username?.slice(0, 2).toUpperCase() }}</span>
      </div>
      <div class="ud-header-info">
        <div class="ud-username">{{ user?.username }}</div>
        <div class="ud-email">{{ user?.email }}</div>
      </div>
    </div>

    <div class="surface-divider" />

    <!-- Theme quick-switch. Real DropdownMenuItems so arrow-key navigation
         reaches them (plain buttons are invisible to the menu's roving
         focus); `select` is prevented so picking a theme applies instantly
         WITHOUT closing the menu. Full appearance controls live in
         Settings → Appearance. -->
    <div class="ud-theme-row" role="group" aria-label="Theme">
      <DropdownMenuItem
        v-for="t in THEMES"
        :key="t.value"
        class="ud-theme-btn"
        :class="{ active: prefs.theme === t.value }"
        :aria-checked="prefs.theme === t.value"
        @select="(e: Event) => { e.preventDefault(); set('theme', t.value) }"
      >{{ t.label }}</DropdownMenuItem>
    </div>

    <div class="surface-divider" />

    <DropdownMenuItem v-for="item in youItems" :key="item.to" class="surface-item" as-child>
      <NuxtLink :to="item.to">
        <Icon :name="item.icon" :size="15" class="surface-item-icon" />
        <span>{{ item.label }}</span>
      </NuxtLink>
    </DropdownMenuItem>

    <div class="surface-divider" />

    <DropdownMenuItem class="surface-item" as-child>
      <NuxtLink :to="settingsTarget">
        <Icon name="settings" :size="15" class="surface-item-icon" />
        <span>Settings</span>
      </NuxtLink>
    </DropdownMenuItem>

    <DropdownMenuItem class="surface-item surface-item-destructive" @select="logout()">
      <Icon name="sign-out" :size="15" class="surface-item-icon" />
      <span>Sign Out</span>
    </DropdownMenuItem>
  </AppMenu>
</template>

<style scoped>
.ud-trigger:hover .ud-avatar,
.ud-trigger[data-state="open"] .ud-avatar { box-shadow: 0 0 0 2px color-mix(in srgb, var(--gold) 30%, transparent); }

.ud-avatar {
  width: 32px; height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: var(--accent-ink);
  font-size: 11px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  letter-spacing: 0.04em;
  transition: box-shadow 0.15s;
}
</style>

<!-- Portaled content — must be unscoped so the rules reach <body>. -->
<style>
.ud-header-avatar {
  width: 40px; height: 40px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: var(--accent-ink);
  font-size: 14px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.ud-header-info { min-width: 0; flex: 1; }
.ud-username { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.ud-email {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 1px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Theme quick-switch row */
.ud-theme-row {
  display: flex;
  gap: 4px;
  padding: 8px 10px;
}
.ud-theme-btn {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px 0;
  border-radius: var(--r-sm);
  font-size: 10.5px;
  font-weight: 700;
  font-family: var(--font-mono);
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.ud-theme-btn:hover,
.ud-theme-btn[data-highlighted] {
  color: var(--fg-0);
  background: rgb(var(--ink) / 0.09);
  outline: none;
}
.ud-theme-btn.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}
</style>
