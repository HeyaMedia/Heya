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
.settings-tabs {
  display: flex;
  align-items: center;
  gap: 3px;
  max-width: 100%;
  margin-bottom: 24px;
  padding: 4px;
  overflow-x: auto;
  scrollbar-width: none;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: rgb(var(--ink) / 0.025);
  width: fit-content;
}
.settings-tabs::-webkit-scrollbar { display: none; }

.settings-tab {
  flex: 0 0 auto;
  min-height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 13px;
  border-radius: calc(var(--r-md) - 4px);
  color: var(--fg-3);
  font-size: 12px;
  font-weight: 550;
  white-space: nowrap;
  transition: color 0.12s, background 0.12s, box-shadow 0.12s;
}
.settings-tab:hover { color: var(--fg-1); background: rgb(var(--ink) / 0.035); }
.settings-tab.active {
  color: var(--fg-0);
  background: var(--bg-1);
  box-shadow: 0 1px 4px rgb(0 0 0 / 0.12), inset 0 0 0 1px rgb(var(--ink) / 0.035);
}

@media (max-width: 720px) {
  .settings-tabs {
    width: 100%;
    margin-bottom: 16px;
  }
  .settings-tab { min-height: 36px; }
}
</style>
