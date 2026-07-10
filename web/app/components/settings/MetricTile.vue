<script setup lang="ts">
defineProps<{
  label: string
  value: string | number
  sub?: string
  delta?: number              // positive/negative — renders with arrow
  tone?: 'neutral' | 'good' | 'warn' | 'bad'
  icon?: string
  sparkline?: number[]
}>()
</script>

<template>
  <div class="sv2-tile" :class="tone ? `tone-${tone}` : ''">
    <div class="sv2-tile-head">
      <Icon v-if="icon" :name="icon" :size="13" class="sv2-tile-icon" />
      <span class="sv2-tile-label">{{ label }}</span>
    </div>
    <div class="sv2-tile-value">{{ value }}</div>
    <div v-if="sub || delta != null" class="sv2-tile-sub">
      <span v-if="delta != null" class="sv2-tile-delta" :class="delta >= 0 ? 'up' : 'down'">
        {{ delta >= 0 ? '+' : '' }}{{ delta }}
      </span>
      <span v-if="sub">{{ sub }}</span>
    </div>
    <Sparkline v-if="sparkline?.length" :points="sparkline" class="sv2-tile-spark" />
  </div>
</template>

<style scoped>
.sv2-tile {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  position: relative;
  overflow: hidden;
}
.sv2-tile.tone-good { border-color: color-mix(in srgb, var(--good) 30%, transparent); }
.sv2-tile.tone-warn { border-color: color-mix(in srgb, var(--gold) 30%, transparent); }
.sv2-tile.tone-bad  { border-color: color-mix(in srgb, var(--bad) 30%, transparent); }

.sv2-tile-head {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--fg-3);
}
.sv2-tile-icon { color: var(--fg-3); }
.sv2-tile-label {
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.02em;
}

.sv2-tile-value {
  font-size: 22px;
  font-weight: 600;
  letter-spacing: -0.02em;
  color: var(--fg-0);
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
  /* Long string values (encoder names, hostnames) get a clean ellipsis
     instead of a hard clip against the tile's overflow:hidden — matters
     more now that phone tiles are half the desktop width. */
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sv2-tile-sub {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 11px;
  color: var(--fg-3);
}
.sv2-tile-delta {
  font-family: var(--font-mono);
  font-weight: 600;
}
.sv2-tile-delta.up   { color: var(--good); }
.sv2-tile-delta.down { color: var(--bad);  }

.sv2-tile-spark {
  margin-top: 8px;
  height: 28px;
  width: 100%;
}
</style>
