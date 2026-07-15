<script setup lang="ts">
// The signature Heya 2.0 element: a hairline-ruled mono spec strip that sits
// at the hero's hard-clip seam. Full-width edge-to-edge — NO side margins (the
// hairlines run the whole viewport width); the first/last cells inset via the
// container's --pad-fluid padding. USER-FACING facts only — never ops
// telemetry (see PLAN cardinal rule 2).
export interface LedgerCell {
  /** Mono uppercase label. */
  k: string
  /** Mono value. */
  v: string
  /** Small dim suffix rendered after the value (e.g. "x265 · 10-bit"). */
  sub?: string
  /** Inline unit clause rendered inside the value (e.g. "of 7"). */
  unit?: string
  /** Paint the value in the page --tone. */
  tone?: boolean
}

withDefaults(defineProps<{ cells?: LedgerCell[] }>(), { cells: () => [] })
</script>

<template>
  <div class="ledger-strip">
    <slot>
      <div v-for="(c, i) in cells" :key="i" class="ls-cell">
        <span class="ls-k">{{ c.k }}</span>
        <span class="ls-v" :class="{ tone: c.tone }">{{ c.v
          }}<span v-if="c.unit" class="ls-u"> {{ c.unit }}</span
          ><small v-if="c.sub">{{ c.sub }}</small></span>
      </div>
    </slot>
  </div>
</template>

<style scoped>
.ledger-strip {
  border-top: 1px solid rgb(var(--lk) / 0.18);
  border-bottom: 1px solid rgb(var(--lk) / 0.1);
  display: flex;
  flex-wrap: wrap;
  position: relative;
  z-index: 3;
  /* Literal-equivalent dark scrim, token-clean: this strip glasses over the
     hero-art seam (CLAUDE.md allows dark over artwork; --shade is 0 0 0). */
  background: rgb(var(--shade) / 0.3);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
  /* Edge-to-edge rules; cells inset from the page gutter. */
  padding: 0 var(--pad-fluid);
  /* Ledger ink: this strip sits at the hero's hard-clip seam, over the dark
     art grade, so its labels/values stay light in every theme — themed --ink
     would flip near-black in the light theme and disappear against the seam. */
  --lk: 233 236 242;
}

.ls-cell {
  flex: 1 1 auto;
  min-width: 90px;
  padding: 14px 22px 15px 0;
  margin-right: 22px;
  border-right: 1px solid rgb(var(--lk) / 0.1);
}
.ls-cell:last-child { border-right: 0; margin-right: 0; }

.ls-k {
  display: block;
  margin-bottom: 4px;
  font: 600 10px var(--font-mono);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: rgb(var(--lk) / 0.45);
}
.ls-v {
  font: 600 16.5px var(--font-mono);
  letter-spacing: -0.01em;
  color: rgb(var(--lk) / 0.95);
}
.ls-v.tone { color: var(--tone); }
.ls-v small {
  margin-left: 6px;
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.04em;
  color: rgb(var(--lk) / 0.45);
}
.ls-v .ls-u { font-size: 11.5px; color: rgb(var(--lk) / 0.6); }

/* Phone: a horizontal scroll strip, cells never wrap (heya2.css ≤760 block,
   folded onto the app's 720px breakpoint). */
@media (max-width: 720px) {
  .ledger-strip {
    flex-wrap: nowrap;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .ledger-strip::-webkit-scrollbar { display: none; }
  .ls-cell {
    flex: 0 0 auto;
    min-width: max-content;
    padding-right: 18px;
    margin-right: 18px;
  }
}
</style>
