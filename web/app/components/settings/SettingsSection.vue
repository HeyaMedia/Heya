<script setup lang="ts">
defineProps<{
  title: string
  description?: string
  icon?: string
  info?: string
  lockedBy?: string
}>()
</script>

<template>
  <section class="sv2-section">
    <header class="sv2-section-head">
      <div class="sv2-section-titles">
        <h3 class="sv2-section-title">
          <Icon v-if="icon" :name="icon" :size="14" />
          <span>{{ title }}</span>
          <AppTooltip v-if="info" :label="info">
            <Icon name="info" :size="12" class="sv2-section-info" />
          </AppTooltip>
          <span v-if="lockedBy" class="sv2-section-lock" :title="`Managed by ${lockedBy}`">
            <Icon name="key" :size="10" />
            {{ lockedBy }}
          </span>
        </h3>
        <p v-if="description" class="sv2-section-desc">{{ description }}</p>
      </div>
      <div class="sv2-section-actions">
        <slot name="actions" />
      </div>
    </header>
    <div class="sv2-section-body">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.sv2-section {
  margin-bottom: 16px;
  padding: 18px 20px 20px;
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: linear-gradient(145deg, var(--bg-1), color-mix(in srgb, var(--bg-2) 72%, var(--bg-1)));
  box-shadow: 0 1px 0 rgb(var(--ink) / 0.025);
}
.sv2-section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.sv2-section-titles {
  min-width: 0;
  flex: 1;
}
.sv2-section-title {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 0;
  font-size: 14px;
  font-weight: 620;
  letter-spacing: -0.01em;
  color: var(--fg-0);
}
.sv2-section-title > :deep(svg) { color: var(--gold); }
.sv2-section-info {
  color: var(--fg-3);
  cursor: help;
}
.sv2-section-lock {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  margin-left: 6px;
  padding: 2px 6px;
  border-radius: var(--r-xs);
  background: var(--gold-soft);
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 9px;
  text-transform: none;
  letter-spacing: 0.02em;
}
.sv2-section-desc {
  margin: 4px 0 0;
  font-size: 12.5px;
  color: var(--fg-3);
  text-transform: none;
  letter-spacing: 0;
  font-weight: 400;
  line-height: 1.5;
}
.sv2-section-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.sv2-section-actions :deep(.link-arrow) {
  min-height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0 11px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--fg-2);
  font-size: 11.5px;
  font-weight: 550;
  line-height: 1;
  white-space: nowrap;
  transition: border-color 0.12s, background 0.12s, color 0.12s;
}
.sv2-section-actions :deep(.link-arrow:hover) {
  border-color: var(--border-strong);
  background: rgb(var(--ink) / 0.045);
  color: var(--fg-0);
}
.sv2-section-body { display: block; }

/* Phone: every page's #actions slot (buttons, selects, search inputs) sits
   in this same flex row with flex-shrink:0 — on a 390px viewport that
   overflows horizontally for nearly every admin page. Stack title above
   actions and let actions wrap instead, one shared fix instead of a
   per-page media query. */
@media (max-width: 720px) {
  .sv2-section {
    margin-bottom: 12px;
    padding: 15px 14px 16px;
    border-radius: var(--r-md);
  }
  .sv2-section-head {
    flex-direction: column;
    align-items: stretch;
  }
  .sv2-section-actions {
    flex-wrap: wrap;
    width: 100%;
  }
  .sv2-section-actions :deep(.link-arrow) { min-height: 36px; }
}
</style>
