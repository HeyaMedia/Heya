<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'

const { user, logout } = useAuth()
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

    <DropdownMenuItem class="surface-item" as-child>
      <NuxtLink to="/settings">
        <Icon name="settings" :size="15" class="surface-item-icon" />
        <span>Settings</span>
      </NuxtLink>
    </DropdownMenuItem>

    <div class="surface-divider" />

    <DropdownMenuItem class="surface-item surface-item-destructive" @select="logout()">
      <Icon name="close" :size="15" class="surface-item-icon" />
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

</style>
