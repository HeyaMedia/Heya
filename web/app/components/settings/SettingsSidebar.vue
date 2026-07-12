<script setup lang="ts">
withDefaults(defineProps<{ variant?: 'sidebar' | 'sheet' }>(), { variant: 'sidebar' })

const route = useRoute()
const { groups, isAdmin } = useSettingsNav()

function isActive(item: SettingsNavItem) {
  return route.path === item.to
    || item.aliases?.includes(route.path) === true
    || item.tabs?.some(tab => tab.to === route.path) === true
}
</script>

<template>
  <nav
    class="sv2-sidebar scroll"
    :class="{ 'sv2-sidebar-sheet': variant === 'sheet' }"
    aria-label="Settings navigation"
  >
    <template v-for="(group, idx) in groups" :key="group.id">
      <div v-if="idx === 1 && isAdmin" class="sv2-divider" />

      <div class="sv2-group">
        <div class="sv2-group-label">{{ group.label }}</div>
        <ul class="sv2-list">
          <li v-for="item in group.items" :key="item.to">
            <NuxtLink
              :to="item.to"
              class="sv2-item"
              :class="{ active: isActive(item) }"
            >
              <Icon :name="item.icon" :size="15" class="sv2-item-icon" />
              <span class="sv2-item-label">{{ item.label }}</span>
            </NuxtLink>
          </li>
        </ul>
      </div>
    </template>
  </nav>
</template>

<style scoped>
.sv2-sidebar {
  width: 240px;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  background: var(--bg-1);
  padding: 20px 0 32px;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.sv2-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0 12px;
}

.sv2-group-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--fg-4);
  padding: 12px 12px 6px;
}

.sv2-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.sv2-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 12px;
  border-radius: var(--r-sm);
  font-size: 13px;
  color: var(--fg-2);
  transition: background 0.12s, color 0.12s;
}
.sv2-item:hover { background: rgb(var(--ink) / 0.03); color: var(--fg-0); }
.sv2-item.active { background: var(--gold-soft); color: var(--gold); }
.sv2-item.active .sv2-item-icon { color: var(--gold); }

.sv2-item-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sv2-item-icon {
  flex-shrink: 0;
  color: var(--fg-3);
  transition: color 0.12s;
}
.sv2-item:hover .sv2-item-icon { color: var(--fg-1); }

.sv2-divider {
  height: 1px;
  background: var(--border);
  margin: 12px;
}

.sv2-sidebar.sv2-sidebar-sheet {
  width: 100%;
  height: auto;
  flex-shrink: 1;
  border-right: 0;
  padding: 4px 0 8px;
}
.sv2-sidebar-sheet .sv2-item {
  min-height: 44px;
  padding: 0 14px;
  font-size: 15px;
}
</style>
