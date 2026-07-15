<script setup lang="ts">
withDefaults(defineProps<{ variant?: 'sidebar' | 'sheet' }>(), { variant: 'sidebar' })

const route = useRoute()
const { groups } = useSettingsNav()

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
    <template v-for="group in groups" :key="group.id">
      <div class="sv2-group">
        <div class="sv2-group-label">{{ group.label }}</div>
        <ul class="sv2-list">
          <li v-for="item in group.items" :key="item.to">
            <NuxtLink
              :to="item.to"
              class="sv2-item"
              :class="{ active: isActive(item) }"
              :aria-current="isActive(item) ? 'page' : undefined"
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
/* Hairline-ruled groups (Heya 2.0 sidebar grammar) — one hairline between
   each group, matching the reskinned LibrarySidebar's .lib-section rule. */
.sv2-group + .sv2-group {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--hair);
}

/* Mono uppercase group label — the eyebrow grammar shared with
   LibrarySidebar's .section-title. */
.sv2-group-label {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--fg-3);
  padding: 10px 12px 7px;
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
.sv2-item:hover { background: rgb(var(--ink) / 0.04); color: var(--fg-0); }
/* Gold active state + left key-rail — the exact chrome as LibrarySidebar's
   .lib-item.active (gold-soft wash, gold-bright label, 3px gold rail). */
.sv2-item.active { background: var(--gold-soft); color: var(--gold-bright); }
.sv2-item.active .sv2-item-icon { color: var(--gold); }
.sv2-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 2px;
  background: var(--gold);
}

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
