<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })
</script>

<template>
  <div class="me-page">
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Metadata editor</h2>
      <p class="sv2-page-desc">
        Split-pane workspace for rewriting metadata by hand. Browse on the
        left, edit on the right. Refresh policies and per-library defaults
        live on <NuxtLink to="/settings/metadata" class="inline-link">Metadata</NuxtLink>.
      </p>
    </header>

    <div class="me-host">
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
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }
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
</style>
