<script setup lang="ts">
// MusicEmptyState — the shared empty/zero state for /music pages. Every one
// of these pages floats on the ambient art pool, so a bare fg-3 paragraph
// (the old pattern) washes out exactly when the page has nothing else to
// anchor the eye. This gives the message a quiet glass landing zone and
// turns it into an invitation: say what fills the page, link the action.
//
//   <MusicEmptyState icon="star" title="No ratings yet">
//     Rate a track from the <NuxtLink to="/music/songs">Songs page</NuxtLink>
//     — anything 1★+ lands here.
//   </MusicEmptyState>
//
// `compact` drops the tall padding for inline section use.
defineProps<{
  icon: string
  title: string
  compact?: boolean
}>()
</script>

<template>
  <div class="mes" :class="{ compact }">
    <div class="mes-icon"><Icon :name="icon" :size="26" /></div>
    <h3 class="mes-title">{{ title }}</h3>
    <p class="mes-body"><slot /></p>
  </div>
</template>

<style scoped>
.mes {
  max-width: 460px;
  margin: 48px auto 0;
  padding: 36px 32px;
  text-align: center;
  background: color-mix(in oklab, var(--bg-2) 85%, transparent);
  -webkit-backdrop-filter: blur(14px);
  backdrop-filter: blur(14px);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-el);
}
.mes.compact { margin-top: 8px; padding: 24px 24px; }

.mes-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: var(--gold-soft);
  color: var(--gold);
  margin-bottom: 14px;
}
.mes-title {
  font-size: 16px;
  font-weight: 650;
  color: var(--fg-0);
  margin-bottom: 6px;
  letter-spacing: -0.005em;
}
.mes-body {
  font-size: 13px;
  line-height: 1.6;
  color: var(--fg-2);
  margin: 0;
}
.mes-body :deep(a) { color: var(--gold); text-decoration: none; }
.mes-body :deep(a:hover) { text-decoration: underline; }
</style>
