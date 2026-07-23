<template>
  <AppMenu
    v-if="djAvailable"
    v-model="menuOpen"
    align="end"
    :width="320"
    :side-offset="10"
    :trigger-class="triggerClasses"
    :trigger-title="activeMode ? `DJ ${activeMode.name}` : 'Choose a DJ'"
    :trigger-aria-label="activeMode ? `DJ ${activeMode.name} active. Choose DJ.` : 'Choose a DJ'"
    content-class="dj-menu"
  >
    <template #trigger>
      <Icon :name="djChanging ? 'spinner' : 'sparkle'" :size="props.variant === 'mini' ? 14 : props.iconSize" :weight="djMode !== 'off' ? 'fill' : undefined" />
      <span v-if="props.variant === 'mini'" class="dj-trigger-label">DJ</span>
      <span v-if="djMode !== 'off'" class="dj-active-dot" />
    </template>

    <div class="dj-menu-head">
      <div>
        <div class="dj-menu-kicker">HEYA DJs</div>
        <div class="dj-menu-title">Who should take the next turn?</div>
      </div>
      <span v-if="activeMode" class="dj-current">{{ activeMode.name }}</span>
    </div>

    <DropdownMenuItem
      v-for="mode in modes"
      :key="mode.id"
      class="dj-mode surface-item"
      :class="{ selected: djMode === mode.id }"
      :disabled="djChanging"
      @select="choose(mode.id)"
    >
      <span class="dj-mode-icon"><Icon :name="mode.icon" :size="18" /></span>
      <span class="dj-mode-copy">
        <span class="dj-mode-name">{{ mode.name }}</span>
        <span class="dj-mode-description">{{ mode.description }}</span>
      </span>
      <Icon v-if="djMode === mode.id" name="check" :size="15" class="dj-mode-check" />
    </DropdownMenuItem>

    <template v-if="djMode !== 'off'">
      <DropdownMenuSeparator class="surface-divider" />
      <DropdownMenuItem class="dj-off surface-item" :disabled="djChanging" @select="choose('off')">
        <Icon name="stop" :size="16" />
        <span>
          <span class="dj-mode-name">Turn DJ off</span>
          <span class="dj-mode-description">Remove its upcoming tracks and return to your queue.</span>
        </span>
      </DropdownMenuItem>
    </template>
  </AppMenu>
</template>

<script setup lang="ts">
import { DropdownMenuItem, DropdownMenuSeparator } from 'reka-ui'
import { DJ_MODE_LABELS, type DJMode } from '~/composables/useQueue'

const props = withDefaults(defineProps<{
  variant?: 'icon' | 'mini'
  iconSize?: number
}>(), {
  variant: 'icon',
  iconSize: 18,
})

interface DJChoice {
  id: Exclude<DJMode, 'off'>
  name: string
  description: string
  icon: string
}

const modes: DJChoice[] = [
  { id: 'echo', name: DJ_MODE_LABELS.echo, icon: 'pulse', description: 'Keeps following the closest musical match without repeats.' },
  { id: 'flow', name: DJ_MODE_LABELS.flow, icon: 'queue', description: 'Adds two recommendations, then hands back to your queue.' },
  { id: 'voyage', name: DJ_MODE_LABELS.voyage, icon: 'compass', description: 'Builds three steps to your next song, or toward chill.' },
  { id: 'encore', name: DJ_MODE_LABELS.encore, icon: 'repeat', description: 'Adds one same-artist track between queued songs.' },
  { id: 'spotlight', name: DJ_MODE_LABELS.spotlight, icon: 'target', description: 'Takes over with the current artist’s closest sonic matches.' },
  { id: 'timewarp', name: DJ_MODE_LABELS.timewarp, icon: 'clock', description: 'Keeps the era going, favoring matching genres and styles.' },
]

const { djMode, djChanging, djAvailable, setDJMode } = usePlayerBindings()
const { toast } = useToast()
const menuOpen = ref(false)
const activeMode = computed(() => modes.find(mode => mode.id === djMode.value) ?? null)
const triggerClasses = computed<Record<string, boolean>>(() => ({
  'dj-trigger': true,
  [`dj-trigger-${props.variant}`]: true,
  active: djMode.value !== 'off',
}))

async function choose(mode: DJMode) {
  if (djChanging.value || mode === djMode.value) return
  try {
    await setDJMode(mode)
    if (mode === 'off') toast.info('DJ stepped away')
    else toast.ok(`${modes.find(item => item.id === mode)?.name ?? 'DJ'} is on deck`)
  } catch (error) {
    toast.err(apiErrorMessage(error, 'Could not change DJ'))
  }
}
</script>

<style>
/* AppMenu portals both its trigger internals and menu surface across component
   boundaries, so this deliberately unscoped block owns the complete DJ unit. */
.dj-trigger {
  position: relative;
  color: var(--fg-2);
  transition: color 0.16s ease, background 0.16s ease, border-color 0.16s ease;
}
.dj-trigger-icon {
  width: 36px;
  height: 36px;
  border-radius: 50%;
}
.dj-trigger-icon:hover { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }
.dj-trigger-mini {
  min-width: 38px;
  height: 32px;
  gap: 4px;
  padding: 0 8px;
  border: 1px solid rgb(var(--ink) / 0.1);
  border-radius: 999px;
  background: rgb(var(--shade) / 0.18);
  font: 700 10px var(--font-mono);
  letter-spacing: 0.08em;
}
.dj-trigger.active {
  color: var(--gold);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 35%, transparent);
}
.dj-trigger-label { line-height: 1; }
.dj-active-dot {
  position: absolute;
  top: 3px;
  right: 3px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--gold);
  box-shadow: 0 0 8px var(--gold-glow);
}
.dj-trigger-mini .dj-active-dot { top: 2px; right: 4px; }

.dj-menu { padding: 8px; }
.dj-menu-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 9px 10px;
}
.dj-menu-kicker {
  font: 700 9px var(--font-mono);
  letter-spacing: 0.2em;
  color: var(--gold);
}
.dj-menu-title {
  margin-top: 3px;
  font-size: 13px;
  font-weight: 700;
  color: var(--fg-0);
}
.dj-current {
  flex-shrink: 0;
  padding: 4px 8px;
  border-radius: 999px;
  background: var(--gold-soft);
  color: var(--gold);
  font: 700 9px var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.dj-mode,
.dj-off {
  display: flex;
  align-items: center;
  width: 100%;
  gap: 10px;
  padding: 9px;
  border-radius: var(--r-sm);
  color: var(--fg-1);
  outline: none;
  cursor: pointer;
}
.dj-mode[data-highlighted],
.dj-off[data-highlighted] { background: rgb(var(--ink) / 0.06); }
.dj-mode.selected { background: var(--gold-soft); }
.dj-mode-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 34px;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: rgb(var(--ink) / 0.05);
  color: var(--fg-2);
}
.dj-mode.selected .dj-mode-icon { color: var(--gold); background: color-mix(in srgb, var(--gold) 13%, transparent); }
.dj-mode-copy,
.dj-off > span {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}
.dj-mode-name { font-size: 12px; font-weight: 700; color: var(--fg-0); }
.dj-mode-description { font-size: 10px; line-height: 1.35; color: var(--fg-3); }
.dj-mode-check { flex-shrink: 0; color: var(--gold); }
.dj-off { color: var(--fg-3); }
.dj-off[data-highlighted] { color: var(--bad); }

@media (pointer: coarse) {
  .dj-mode, .dj-off { min-height: 54px; }
}
</style>
