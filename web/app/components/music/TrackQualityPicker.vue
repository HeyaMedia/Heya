<template>
  <!-- Single-format tracks just show the chip as plain text — no menu. -->
  <span v-if="files.length <= 1" class="qp-chip qp-chip-static">
    {{ chipLabel(files[0]) }}
  </span>
  <AppMenu
    v-else
    align="start"
    :width="240"
    trigger-class="qp-chip"
  >
    <template #trigger>
      <span>{{ chipLabel(files[0]) }}</span>
      <Icon name="chevdown" :size="10" />
    </template>
    <div class="surface-section-label" style="padding: 6px 10px 4px">Quality</div>
    <DropdownMenuItem
      v-for="f in files"
      :key="f.id"
      class="surface-item qp-item"
      :class="{ active: f.id === selectedId }"
      @select="$emit('pick', f)"
    >
      <div class="qp-item-main">
        <div class="qp-item-format">{{ chipLabel(f) }}</div>
        <div class="qp-item-meta">
          <span v-if="f.bitrate_kbps > 0">{{ f.bitrate_kbps }} kbps</span>
          <span v-if="f.sample_rate_hz > 0">{{ Math.round(f.sample_rate_hz / 1000) }} kHz</span>
          <span v-if="f.channels > 0">{{ f.channels }}ch</span>
          <span v-if="f.size_bytes > 0">{{ fmtBytes(f.size_bytes) }}</span>
        </div>
      </div>
      <Icon v-if="f.id === selectedId" name="check" :size="12" class="qp-item-check" />
    </DropdownMenuItem>
  </AppMenu>
</template>

<script setup lang="ts">
import type { TrackFile } from '~~/shared/types'
import { DropdownMenuItem } from 'reka-ui'

defineProps<{
  files: TrackFile[]
  selectedId?: number
}>()

defineEmits<{ pick: [TrackFile] }>()

function chipLabel(f?: TrackFile): string {
  if (!f) return ''
  return formatTrackQuality(f) ?? ''
}

// fmtBytes (binary-adaptive) comes from useFormat.ts (auto-imported).
// formatTrackQuality comes from utils/trackQuality.ts (auto-imported) —
// shared with TrackList's phone-row quality label.
</script>

<style scoped>
/* Chip styling — applied to AppMenu's trigger button (via trigger-class)
   for the multi-format case, and to a plain <span> for single-format
   tracks where there's no menu to open. */
.qp-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
  padding: 2px 5px;
  border-radius: 3px;
  background: transparent;
  border: 1px solid transparent;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.qp-chip:hover,
.qp-chip[data-state="open"] {
  background: var(--bg-3);
  color: var(--fg-1);
  border-color: var(--border);
}
.qp-chip-static { cursor: default; }
.qp-chip-static:hover { background: transparent; color: var(--fg-3); border-color: transparent; }
</style>

<!--
  Menu rows live in AppMenu's portaled content — styling has to be
  unscoped to reach the rendered element.
-->
<style>
.qp-item { padding: 8px 10px; }
.qp-item.active { color: var(--gold); }
.qp-item-main { flex: 1; min-width: 0; }
.qp-item-format {
  font-size: 12px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.qp-item-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 10px;
  color: var(--fg-3);
  margin-top: 2px;
}
.qp-item-check { color: var(--gold); flex-shrink: 0; margin-left: 8px; }
</style>
