<template>
  <div class="page-pad cs-page">
    <div class="cs-card surface">
      <div class="cs-icon">
        <Icon :name="icon" :size="40" />
      </div>
      <h1 class="cs-title">{{ title }}</h1>
      <p v-if="subtitle" class="cs-sub">{{ subtitle }}</p>
      <ul v-if="planned.length" class="cs-planned">
        <li v-for="p in planned" :key="p">{{ p }}</li>
      </ul>
      <div class="cs-tag">Coming soon</div>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  title: string
  subtitle?: string
  icon?: string
  planned?: string[]
}>(), {
  subtitle: '',
  icon: 'sparkle',
  planned: () => [],
})
</script>

<style scoped>
.cs-page { display: flex; align-items: flex-start; justify-content: center; padding-top: 64px; }
.cs-card {
  max-width: 560px;
  width: 100%;
  padding: 32px 36px;
  border-radius: var(--r-md);
  display: flex; flex-direction: column; align-items: flex-start; gap: 12px;
}
.cs-icon {
  width: 64px; height: 64px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-md);
  background: var(--gold-soft);
  color: var(--gold);
  margin-bottom: 8px;
}
.cs-title { font-size: 26px; font-weight: 700; letter-spacing: -0.01em; }
.cs-sub { color: var(--fg-2); line-height: 1.55; }
.cs-planned {
  display: flex; flex-direction: column; gap: 6px;
  margin: 8px 0 0;
  padding: 0 0 0 18px;
  color: var(--fg-2);
  font-size: 14px;
}
.cs-planned li::marker { color: var(--gold); }
.cs-tag {
  margin-top: 12px;
  padding: 4px 10px;
  border-radius: 999px;
  background: var(--gold-soft);
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  font-weight: 700;
}
</style>
