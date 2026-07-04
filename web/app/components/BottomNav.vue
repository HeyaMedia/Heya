<!--
  BottomNav — fixed phone tab bar (<=720px only).

  Renders the same five sections as AppTopBar's `.topbar-tabs`, sourced from
  useNavTabs() so the two never drift. Mounted in layouts/default.vue and
  layouts/settings.vue; `/watch/*` pages use `definePageMeta({ layout: false
  })` so they never mount a layout (and never get a BottomNav) — nothing
  extra to gate here.
-->
<template>
  <nav class="bottom-nav">
    <NuxtLink
      v-for="t in tabs"
      :key="t.to"
      :to="t.to"
      class="bn-tab"
      :class="{ active: isActive(t) }"
    >
      <Icon :name="t.icon" :size="22" />
      <span>{{ t.label }}</span>
    </NuxtLink>
  </nav>
</template>

<script setup lang="ts">
const { tabs, isActive } = useNavTabs()
</script>

<style scoped>
.bottom-nav {
  display: flex;
  align-items: stretch;
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  /* Base tap-target height plus the safe-area inset, so the 56px of usable
     bar always sits above a home-indicator bar rather than being squeezed
     by it — the extra inset is pure padding underneath the content. */
  height: calc(var(--bottomnav-h) + var(--safe-bottom));
  padding-bottom: var(--safe-bottom);
  background: color-mix(in oklab, var(--bg-1) 88%, transparent);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-top: 1px solid var(--border);
  /* Above the playbar (40) so mini-player chrome from W1b can dock directly
     on top of this bar; below the topbar (50) since the two never overlap
     vertically. Sheets/dialogs live well above both. */
  z-index: 45;
}

/* Phone-only: exists solely as the mobile replacement for .topbar-tabs. */
@media (min-width: 720.02px) {
  .bottom-nav { display: none; }
}

.bn-tab {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  color: var(--fg-3);
  text-decoration: none;
}
.bn-tab span {
  font-size: 10px;
  font-weight: 500;
  line-height: 1;
}
.bn-tab.active { color: var(--gold); }
</style>
