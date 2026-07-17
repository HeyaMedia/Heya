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

withDefaults(
  defineProps<{
    cells?: LedgerCell[]
    /**
     * The strip sits on plain themed canvas (browse landings, music home)
     * rather than glassing over a hero-art seam: use theme ink so the light
     * theme keeps contrast, and drop the dark scrim + blur.
     */
    canvas?: boolean
    /**
     * Cold-cache shell: with no cells yet, render ghost cells at the strip's
     * final height so the page doesn't reflow when the queries land. Never
     * shown once any real cell exists.
     */
    pending?: boolean
  }>(),
  { cells: () => [], canvas: false, pending: false },
)
</script>

<template>
  <div class="ledger-strip" :class="{ 'ls-canvas': canvas }">
    <slot>
      <div v-for="(c, i) in cells" :key="i" class="ls-cell">
        <span class="ls-k">{{ c.k }}</span>
        <span class="ls-v" :class="{ tone: c.tone }">{{ c.v
          }}<span v-if="c.unit" class="ls-u"> {{ c.unit }}</span
          ><small v-if="c.sub">{{ c.sub }}</small></span>
      </div>
      <template v-if="!cells.length && pending">
        <div v-for="i in 3" :key="`ghost-${i}`" class="ls-cell ls-skel" aria-hidden="true">
          <span class="ls-k"><span class="ls-ghost" :style="{ width: `${38 + i * 14}px`, height: '12px' }" /></span>
          <span class="ls-v"><span class="ls-ghost" :style="{ width: `${56 + i * 24}px`, height: '20px' }" /></span>
        </div>
      </template>
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
  backdrop-filter: blur(var(--glass-blur-md));
  -webkit-backdrop-filter: blur(var(--glass-blur-md));
  /* Edge-to-edge rules; cells inset from the page gutter. */
  padding: 0 var(--pad-fluid);
  /* Ledger ink: this strip sits at the hero's hard-clip seam, over the dark
     art grade, so its labels/values stay light in every theme — themed --ink
     would flip near-black in the light theme and disappear against the seam. */
  --lk: 233 236 242;
}

/* On-canvas variant: themed ink (flips with the light theme), faint themed
   wash instead of the over-art scrim, no blur needed. */
.ledger-strip.ls-canvas {
  --lk: var(--ink);
  background: rgb(var(--ink) / 0.03);
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
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

/* Cold-cache ghosts — same cell box, shimmer bars sized to the k/v line
   heights so the strip claims its settled height before any query lands. */
.ls-skel .ls-v { display: block; }
.ls-ghost {
  display: block;
  border-radius: 3px;
  background: rgb(var(--lk) / 0.12);
  animation: ls-ghost-pulse 1.4s ease-in-out infinite;
}
@keyframes ls-ghost-pulse {
  50% { opacity: 0.45; }
}
@media (prefers-reduced-motion: reduce) {
  .ls-ghost { animation: none; }
}

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
