<!--
  HotkeyHelp — keyboard-shortcut reference, toggled with `?`. Mounted once in the
  music shell; open state is shared with useGlobalHotkeys via useState.
-->
<template>
  <AppDialog
    :model-value="open"
    title="Keyboard shortcuts"
    size="sm"
    @update:model-value="(v: boolean) => { if (!v) open = false }"
  >
    <div class="hk-groups">
      <div v-for="g in GROUPS" :key="g.title" class="hk-group">
        <div class="hk-group-title">{{ g.title }}</div>
        <div v-for="row in g.rows" :key="row.label" class="hk-row">
          <span class="hk-label">{{ row.label }}</span>
          <span class="hk-keys">
            <kbd v-for="(k, i) in row.keys" :key="i" class="hk-kbd">{{ k }}</kbd>
          </span>
        </div>
      </div>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
const open = useState('music_hotkey_help_open', () => false)

const GROUPS = [
  {
    title: 'Playback',
    rows: [
      { label: 'Play / pause', keys: ['Space'] },
      { label: 'Seek backward / forward 5s', keys: ['←', '→'] },
      { label: 'Previous / next track', keys: ['⇧ ←', '⇧ →'] },
      { label: 'Volume up / down', keys: ['↑', '↓'] },
    ],
  },
  {
    title: 'Toggles',
    rows: [
      { label: 'Mute', keys: ['M'] },
      { label: 'Shuffle', keys: ['S'] },
      { label: 'Repeat', keys: ['R'] },
      { label: 'Queue', keys: ['Q'] },
      { label: 'Lyrics', keys: ['L'] },
      { label: 'Visualizer', keys: ['V'] },
    ],
  },
  {
    title: 'Visualizer (when open)',
    rows: [
      { label: 'Previous / next preset', keys: ['←', '→'] },
      { label: 'Random preset', keys: ['R'] },
      { label: 'Browse presets', keys: ['O'] },
      { label: 'Switch mode', keys: ['1', '·', '4'] },
      { label: 'Native fullscreen', keys: ['F'] },
      { label: 'Close', keys: ['Esc'] },
    ],
  },
  {
    title: 'Help',
    rows: [{ label: 'This panel', keys: ['?'] }],
  },
] as const
</script>

<style scoped>
.hk-groups { display: flex; flex-direction: column; gap: 18px; }
.hk-group-title {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: var(--fg-3);
  margin-bottom: 10px;
}
.hk-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 5px 0;
}
.hk-label { font-size: 13px; color: var(--fg-1); }
.hk-keys { display: flex; gap: 6px; flex-shrink: 0; }
.hk-kbd {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 26px;
  height: 24px;
  padding: 0 7px;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-0);
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border-strong);
  border-bottom-width: 2px;
  border-radius: 6px;
}
</style>
