<script setup lang="ts">
// LibHead — the Heya 2.0 library page header (heya2.css `.lib-head`): a mono
// breadcrumb eyebrow above an Archivo condensed-display title, with an optional
// right-side tools slot for the restyled view/sort/filter controls. Used on the
// Movies/TV Browse landing + All grid, and the Books page. Library chrome stays
// on the brand --tone (the accent), so no per-page tone sampling here.
export interface Crumb {
  label: string
  to?: string
}

withDefaults(defineProps<{
  title: string
  /** Mono breadcrumb segments rendered above the title, joined by a dim `·`. */
  crumbs?: Crumb[]
}>(), { crumbs: () => [] })
</script>

<template>
  <div class="lib-head">
    <div class="grow">
      <div v-if="crumbs.length" class="lib-eyebrow">
        <template v-for="(c, i) in crumbs" :key="i">
          <span v-if="i > 0" class="sep">·</span>
          <NuxtLink v-if="c.to" :to="c.to" class="crumb-link">{{ c.label }}</NuxtLink>
          <span v-else>{{ c.label }}</span>
        </template>
      </div>
      <h1 class="lib-title">{{ title }}</h1>
    </div>
    <div v-if="$slots.tools" class="lib-tools">
      <slot name="tools" />
    </div>
  </div>
</template>

<style scoped>
.lib-head {
  display: flex;
  align-items: flex-end;
  gap: 28px;
  padding: 30px var(--pad-fluid) 22px;
}
.grow { flex: 1; min-width: 0; }

.lib-eyebrow {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
  font-family: var(--font-mono);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
  /* Reads over the ambient pool backdrop — same halo trick as SectionHeader. */
  text-shadow: 0 0 10px var(--bg-1), 0 1px 2px var(--bg-1);
}
.lib-eyebrow .sep { color: var(--fg-3); }
.lib-eyebrow .crumb-link { color: var(--fg-2); transition: color 0.12s ease; }
.lib-eyebrow .crumb-link:hover { color: var(--fg-0); }

.lib-title {
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(2rem, 3.4vw, 3rem);
  font-weight: 800;
  font-variation-settings: 'wdth' 115;
  letter-spacing: -0.02em;
  line-height: 1.0;
  color: var(--fg-0);
  text-wrap: balance;
  text-shadow: 0 1px 2px var(--bg-1), 0 0 18px var(--bg-1);
}

.lib-tools {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

@media (max-width: 720px) {
  .lib-head {
    flex-direction: column;
    align-items: stretch;
    gap: 14px;
    padding: 20px 16px 16px;
  }
  .lib-title { font-size: clamp(1.7rem, 8vw, 2.3rem); }
}
</style>
