<script setup lang="ts">
const route = useRoute()
const { sectionByPath } = useSettingsNav()

const section = computed(() => sectionByPath.value.get(route.path))
const tabs = computed(() => section.value?.tabs ?? [])
</script>

<template>
  <nav v-if="tabs.length > 1" class="settings-tabs" :aria-label="`${section?.label} sections`">
    <NuxtLink
      v-for="tab in tabs"
      :key="tab.to"
      :to="tab.to"
      class="settings-tab"
      :class="{ active: route.path === tab.to }"
      :aria-current="route.path === tab.to ? 'page' : undefined"
    >
      {{ tab.label }}
    </NuxtLink>
  </nav>
</template>

<style scoped>
/* Mono pill row (Heya 2.0 grammar) — same shape as the committed season
   switcher (.stab / .seasontabs) and the mode pills: hairline-bordered
   pills, gold-tinted when active. */
.settings-tabs {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 100%;
  margin-bottom: 24px;
  padding-bottom: 2px;
  overflow-x: auto;
  scrollbar-width: none;
  width: fit-content;
}
.settings-tabs::-webkit-scrollbar { display: none; }

.settings-tab {
  flex: 0 0 auto;
  min-height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 15px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  white-space: nowrap;
  transition: color 0.15s, background 0.15s, border-color 0.15s, box-shadow 0.15s;
}
.settings-tab:hover:not(.active) { color: var(--fg-0); border-color: var(--fg-3); }
.settings-tab.active {
  color: var(--gold-bright);
  border-color: color-mix(in srgb, var(--gold) 55%, transparent);
  background: var(--gold-soft);
  box-shadow: 0 0 16px var(--gold-glow);
}

@media (max-width: 720px) {
  .settings-tabs {
    width: 100%;
    margin-bottom: 16px;
  }
  .settings-tab { min-height: 36px; }
}
</style>
