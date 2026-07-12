<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

// The Miller-column browser needs real width to show browse + edit panes
// side by side — there's no useful phone layout for it (docs/responsive-plan
// W3d), so phone gets a notice instead of a broken squeeze. Desktop/tablet
// are untouched.
const { isPhone } = useViewport()
</script>

<template>
  <div class="me-page">
    <SettingsContextHero
      title="Metadata editor"
      icon="pencil"
      eyebrow="Media · Manual corrections"
      description="Browse the library and correct titles, descriptions, dates, artwork references, and provider matches by hand."
    />

    <div v-if="isPhone" class="me-phone-notice">
      <Icon name="info" :size="28" />
      <p>The metadata editor needs a desktop-sized screen.</p>
      <span>Come back on a bigger display — or use <NuxtLink to="/settings/metadata" class="inline-link">Metadata</NuxtLink> for per-library policy.</span>
    </div>
    <div v-else class="me-host">
      <MetadataManager />
    </div>
  </div>
</template>

<style scoped>
.me-page {
  display: flex;
  flex-direction: column;
  /* Same vertical fill as the Logs page — let MetadataManager own the inside. */
  min-height: calc(100vh - 64px);
}

.sv2-page-head { margin-bottom: 14px; }
.inline-link { color: var(--gold); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

.me-host {
  flex: 1;
  min-height: 0;
  /* MetadataManager already has its own border/background; let it fill. */
  display: flex;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-2);
}
.me-host :deep(.mm) {
  flex: 1;
  height: auto;
  background: transparent;
}

.me-phone-notice {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  text-align: center;
  padding: 40px 24px;
  color: var(--fg-3);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.me-phone-notice p {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--fg-1);
}
.me-phone-notice span {
  font-size: 12.5px;
  line-height: 1.5;
  max-width: 320px;
}
</style>
