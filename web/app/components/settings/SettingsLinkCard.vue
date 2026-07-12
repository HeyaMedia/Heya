<script setup lang="ts">
withDefaults(defineProps<{
  to: string
  title: string
  description: string
  icon: string
  value?: string | number
  valueLabel?: string
  tone?: 'neutral' | 'good' | 'warn' | 'bad'
}>(), {
  value: '',
  valueLabel: '',
  tone: 'neutral',
})
</script>

<template>
  <NuxtLink :to="to" class="settings-link-card" :class="`tone-${tone}`">
    <span class="settings-link-icon"><Icon :name="icon" :size="17" /></span>
    <span class="settings-link-copy">
      <span class="settings-link-title">{{ title }}</span>
      <span class="settings-link-description">{{ description }}</span>
    </span>
    <span v-if="value !== ''" class="settings-link-value">
      <strong>{{ value }}</strong>
      <small v-if="valueLabel">{{ valueLabel }}</small>
    </span>
    <Icon name="chevright" :size="13" class="settings-link-arrow" />
  </NuxtLink>
</template>

<style scoped>
.settings-link-card {
  min-width: 0;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 12px;
  padding: 13px 14px;
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  background: var(--bg-2);
  transition: border-color 0.14s, background 0.14s, transform 0.14s;
}
.settings-link-card:hover {
  border-color: var(--border-strong);
  background: color-mix(in srgb, var(--bg-2) 82%, var(--gold-soft));
  transform: translateY(-1px);
}
.settings-link-card.tone-warn { border-color: color-mix(in srgb, var(--gold) 24%, var(--border)); }
.settings-link-card.tone-bad { border-color: color-mix(in srgb, var(--bad) 28%, var(--border)); }

.settings-link-icon {
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  background: var(--gold-soft);
  color: var(--gold);
}
.settings-link-copy { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.settings-link-title { color: var(--fg-0); font-size: 13px; font-weight: 610; }
.settings-link-description {
  overflow: hidden;
  color: var(--fg-2);
  font-size: 11.5px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.settings-link-value { display: flex; flex-direction: column; align-items: flex-end; gap: 1px; }
.settings-link-value strong { color: var(--fg-1); font-family: var(--font-mono); font-size: 16px; }
.settings-link-value small { color: var(--fg-2); font-size: 9.5px; font-weight: 600; letter-spacing: 0.06em; text-transform: uppercase; }
.settings-link-arrow { color: var(--fg-3); transition: color 0.14s, transform 0.14s; }
.settings-link-card:hover .settings-link-arrow { color: var(--gold); transform: translateX(2px); }

@media (max-width: 520px) {
  .settings-link-card { grid-template-columns: auto minmax(0, 1fr) auto; }
  .settings-link-value { display: none; }
}
</style>
