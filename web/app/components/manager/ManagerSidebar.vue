<script setup lang="ts">
withDefaults(defineProps<{ variant?: 'sidebar' | 'sheet' }>(), { variant: 'sidebar' })

const route = useRoute()
const { groups } = useManagerNav()

function isActive(item: ManagerNavItem) {
  return route.path === item.to
}
</script>

<template>
  <nav
    class="mgr-sidebar scroll"
    :class="{ 'mgr-sidebar-sheet': variant === 'sheet' }"
    aria-label="Manager navigation"
  >
    <template v-for="group in groups" :key="group.id">
      <div v-if="group.items.length" class="mgr-group">
        <div v-if="group.label" class="mgr-group-label">{{ group.label }}</div>
        <ul class="mgr-list">
          <li v-for="item in group.items" :key="item.to">
            <NuxtLink
              :to="item.to"
              class="mgr-item"
              :class="{ active: isActive(item) }"
              :aria-current="isActive(item) ? 'page' : undefined"
            >
              <Icon :name="item.icon" :size="15" class="mgr-item-icon" />
              <span class="mgr-item-label">{{ item.label }}</span>
            </NuxtLink>
          </li>
        </ul>
      </div>
    </template>
  </nav>
</template>

<style scoped>
/* Same rail grammar as SettingsSidebar (hairline-ruled groups, mono eyebrow
   labels, gold active key-rail) — a different room in the same house. */
.mgr-sidebar {
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

.mgr-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0 12px;
}
.mgr-group + .mgr-group {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--hair);
}

.mgr-group-label {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--fg-3);
  padding: 10px 12px 7px;
}

.mgr-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.mgr-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 12px;
  border-radius: var(--r-sm);
  font-size: 13px;
  color: var(--fg-2);
  transition: background 0.12s, color 0.12s;
}
.mgr-item:hover { background: rgb(var(--ink) / 0.04); color: var(--fg-0); }
.mgr-item.active { background: var(--gold-soft); color: var(--gold-bright); }
.mgr-item.active .mgr-item-icon { color: var(--gold); }
.mgr-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 2px;
  background: var(--gold);
}

.mgr-item-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mgr-item-icon {
  flex-shrink: 0;
  color: var(--fg-3);
  transition: color 0.12s;
}
.mgr-item:hover .mgr-item-icon { color: var(--fg-1); }

.mgr-sidebar.mgr-sidebar-sheet {
  width: 100%;
  height: auto;
  flex-shrink: 1;
  border-right: 0;
  padding: 4px 0 8px;
}
.mgr-sidebar-sheet .mgr-item {
  min-height: 44px;
  padding: 0 14px;
  font-size: 15px;
}
</style>
