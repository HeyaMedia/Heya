<script setup lang="ts">
withDefaults(defineProps<{
  title: string
  description: string
  icon: string
  eyebrow?: string
  tone?: 'accent' | 'local' | 'connected'
}>(), {
  eyebrow: 'Personal settings',
  tone: 'accent',
})
</script>

<template>
  <section class="context-hero" :class="`tone-${tone}`">
    <div class="context-icon" aria-hidden="true">
      <Icon :name="icon" :size="23" weight="duotone" />
    </div>
    <div class="context-copy">
      <span class="context-eyebrow">{{ eyebrow }}</span>
      <h2>{{ title }}</h2>
      <p>{{ description }}</p>
    </div>
    <div v-if="$slots.default" class="context-facts">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.context-hero {
  --context-color: var(--gold);
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 16px;
  margin-bottom: 18px;
  padding: 19px 20px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--context-color) 30%, var(--border-strong));
  border-radius: var(--r-lg);
  background:
    radial-gradient(circle at 100% 0%, color-mix(in srgb, var(--context-color) 9%, transparent), transparent 48%),
    linear-gradient(145deg, color-mix(in srgb, var(--bg-1) 90%, var(--bg-2)), var(--bg-2));
}
.context-hero.tone-local { --context-color: color-mix(in srgb, #7e9fe7 72%, var(--fg-0)); }
.context-hero.tone-connected { --context-color: var(--good); }
.context-icon {
  width: 48px;
  height: 48px;
  display: grid;
  place-items: center;
  flex: none;
  border: 1px solid color-mix(in srgb, var(--context-color) 38%, var(--border));
  border-radius: 14px;
  background: color-mix(in srgb, var(--context-color) 11%, transparent);
  color: var(--context-color);
  box-shadow: inset 0 1px rgb(255 255 255 / 0.04);
}
.context-copy { min-width: 0; }
.context-eyebrow {
  display: block;
  margin-bottom: 3px;
  color: var(--context-color);
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.context-copy h2 {
  margin: 0;
  color: var(--fg-0);
  font-size: 19px;
  font-weight: 660;
  letter-spacing: -0.025em;
}
.context-copy p {
  max-width: 680px;
  margin: 4px 0 0;
  color: var(--fg-2);
  font-size: 13px;
  line-height: 1.5;
}
.context-facts {
  display: flex;
  align-items: stretch;
  gap: 7px;
}
.context-facts :deep(.context-fact) {
  min-width: 92px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  padding: 9px 11px;
  border: 1px solid var(--border-strong);
  border-radius: var(--r-sm);
  background: color-mix(in srgb, var(--bg-2) 92%, var(--context-color));
}
.context-facts :deep(.context-fact strong) {
  color: var(--fg-0);
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 650;
  white-space: nowrap;
}
.context-facts :deep(.context-fact span) {
  color: var(--fg-2);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  white-space: nowrap;
}

@media (max-width: 840px) {
  .context-hero { grid-template-columns: auto minmax(0, 1fr); }
  .context-facts { grid-column: 1 / -1; }
  .context-facts :deep(.context-fact) { flex: 1; }
}
@media (max-width: 520px) {
  .context-hero { gap: 12px; padding: 16px; }
  .context-icon { width: 42px; height: 42px; border-radius: 12px; }
  .context-copy h2 { font-size: 17px; }
  .context-facts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; }
  .context-facts :deep(.context-fact) { min-width: 0; }
  .context-facts :deep(.context-fact:last-child:nth-child(odd)) { grid-column: 1 / -1; }
}
</style>
