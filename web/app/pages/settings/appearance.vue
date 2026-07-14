<script setup lang="ts">
import type { ThemeMode } from '~/composables/useAppearance'

definePageMeta({ layout: 'settings' })

// Everything on this page applies live (attributes on <html> flip
// immediately via useAppearance) and persists to /api/me/settings, with a
// localStorage mirror for flash-free boot. No Save button — appearance is
// the one settings page where instant feedback beats a commit step.

const { prefs, set } = useAppearance()
const { sections, toggle, move, reset } = useHomeSections()

type ThemeChoice = { value: ThemeMode; label: string; hint: string }
const themes: ThemeChoice[] = [
  { value: 'dark', label: 'Heya Dark', hint: 'The cinematic default' },
  { value: 'oled', label: 'OLED Black', hint: 'True black for OLED panels' },
  { value: 'light', label: 'Heya Light', hint: 'Warm paper for daylight' },
  { value: 'system', label: 'System', hint: 'Follow the OS setting' },
]

// Mini-preview surface colors per card (static — the preview shows what
// you'd get, independent of the active theme).
const previewVars: Record<Exclude<ThemeMode, 'system'>, Record<string, string>> = {
  dark: { '--pv-bg': '#0c0c10', '--pv-bar': 'rgba(255,255,255,0.07)', '--pv-line': 'rgba(255,255,255,0.10)', '--pv-text': '#f4f3ee' },
  oled: { '--pv-bg': '#000000', '--pv-bar': 'rgba(255,255,255,0.09)', '--pv-line': 'rgba(255,255,255,0.12)', '--pv-text': '#f4f3ee' },
  light: { '--pv-bg': '#f1eee7', '--pv-bar': 'rgba(35,30,20,0.08)', '--pv-line': 'rgba(35,30,20,0.14)', '--pv-text': '#1d1a14' },
}

const activeAccentHex = computed(
  () => ACCENTS.find((a) => a.name === prefs.value.accent)?.hex ?? ACCENTS[0]!.hex,
)
const activeThemeLabel = computed(() => themes.find(theme => theme.value === prefs.value.theme)?.label ?? 'System')
const activeAccentLabel = computed(() => ACCENTS.find(accent => accent.name === prefs.value.accent)?.label ?? 'Heya Gold')
const visibleSectionCount = computed(() => sections.value.filter(section => !section.hidden).length)

const ambientOn = computed({
  get: () => prefs.value.ambientMode !== 'off',
  set: (v: boolean) => set('ambientMode', v ? 'on' : 'off'),
})
const showUnavailableRecs = computed({
  get: () => prefs.value.showUnavailableRecs,
  set: (v: boolean) => set('showUnavailableRecs', v),
})
const ambientIntensity = computed({
  get: () => prefs.value.ambientIntensity || AMBIENT_INTENSITY_DEFAULT,
  set: (v: number) => set('ambientIntensity', v),
})

const isDefaultSections = computed(
  () => sections.value.every((s, i) => !s.hidden && HOME_SECTION_DEFS[i]?.id === s.id),
)
</script>

<template>
  <div>
    <SettingsContextHero
      title="Appearance"
      icon="brightness"
      eyebrow="Live & synced"
      description="Shape Heya around your screen and taste. Every change previews instantly and follows your account to other devices."
    >
      <div class="context-fact"><strong>{{ activeThemeLabel }}</strong><span>Theme</span></div>
      <div class="context-fact"><strong>{{ activeAccentLabel }}</strong><span>Accent</span></div>
      <div class="context-fact"><strong>{{ visibleSectionCount }}</strong><span>Home rows</span></div>
    </SettingsContextHero>

    <SettingsSection title="Theme" icon="brightness"
      description="Dark is the Heya look. OLED trades the deep greys for true black; Light is the same design on warm paper.">
      <div class="theme-grid">
        <button
          v-for="t in themes"
          :key="t.value"
          type="button"
          class="theme-card"
          :class="{ active: prefs.theme === t.value }"
          :aria-pressed="prefs.theme === t.value"
          @click="set('theme', t.value)"
        >
          <!-- System = split preview, half dark / half light -->
          <div v-if="t.value === 'system'" class="theme-preview split">
            <div class="split-half" :style="previewVars.dark">
              <div class="theme-bar" />
              <div class="theme-content">
                <div class="theme-chip" :style="{ background: activeAccentHex }" />
                <div class="theme-line long" />
              </div>
            </div>
            <div class="split-half" :style="previewVars.light">
              <div class="theme-bar" />
              <div class="theme-content">
                <div class="theme-chip" :style="{ background: activeAccentHex }" />
                <div class="theme-line long" />
              </div>
            </div>
          </div>
          <div v-else class="theme-preview" :style="previewVars[t.value]">
            <div class="theme-bar" />
            <div class="theme-content">
              <div class="theme-chip" :style="{ background: activeAccentHex }" />
              <div class="theme-line long" />
              <div class="theme-line short" />
            </div>
          </div>
          <div class="theme-label">
            <Icon v-if="prefs.theme === t.value" name="check" :size="12" class="theme-check" />
            <span>{{ t.label }}</span>
            <span class="theme-hint">{{ t.hint }}</span>
          </div>
        </button>
      </div>
    </SettingsSection>

    <div class="appearance-grid">
    <SettingsSection title="Accent" icon="sparkle"
      description="The color that carries selection, progress, and emphasis throughout the app.">
      <div class="accent-grid">
        <button
          v-for="a in ACCENTS"
          :key="a.name"
          type="button"
          class="accent-cell"
          :class="{ active: prefs.accent === a.name }"
          :style="{ '--swatch': a.hex }"
          :aria-label="a.label"
          :aria-pressed="prefs.accent === a.name"
          @click="set('accent', a.name)"
        >
          <span class="accent-dot">
            <Icon v-if="prefs.accent === a.name" name="check" :size="14" />
          </span>
          <span class="accent-cell-name">{{ a.label }}</span>
        </button>
      </div>
    </SettingsSection>

    <SettingsSection title="Density" icon="grid"
      description="Choose how much media fits on screen at once.">
      <div class="density-grid" role="radiogroup" aria-label="Density">
        <label class="density-card" :class="{ active: prefs.density === 'comfortable' }">
          <input
            type="radio" name="density" value="comfortable" :checked="prefs.density === 'comfortable'"
            @change="set('density', 'comfortable')"
          />
          <div class="density-body">
            <div class="density-title">Comfortable</div>
            <div class="density-desc">Default — generous spacing, larger posters.</div>
            <div class="density-preview">
              <div class="density-row tall" /><div class="density-row tall" /><div class="density-row tall" />
            </div>
          </div>
        </label>
        <label class="density-card" :class="{ active: prefs.density === 'compact' }">
          <input
            type="radio" name="density" value="compact" :checked="prefs.density === 'compact'"
            @change="set('density', 'compact')"
          />
          <div class="density-body">
            <div class="density-title">Compact</div>
            <div class="density-desc">Tighter grids and rows — more items per screen.</div>
            <div class="density-preview">
              <div class="density-row" /><div class="density-row" /><div class="density-row" />
              <div class="density-row" /><div class="density-row" />
            </div>
          </div>
        </label>
      </div>
    </SettingsSection>
    </div>

    <SettingsSection title="Ambient background" icon="image"
      description="Rotates artwork from your libraries behind the app — movies on Movies, artists on Music, everything on Home.">
      <div class="ambient-controls">
        <div class="ambient-row">
          <div class="ambient-row-text">
            <div class="ambient-row-title">Show ambient background</div>
            <div class="ambient-row-desc">Slow crossfade, pauses in hidden tabs, honors reduced motion.</div>
          </div>
          <AppSwitch v-model="ambientOn" size="md" aria-label="Show ambient background" />
        </div>
        <div class="ambient-row" :class="{ disabled: !ambientOn }">
          <div class="ambient-row-text">
            <div class="ambient-row-title">Intensity</div>
            <div class="ambient-row-desc">How much the artwork shows through the canvas.</div>
          </div>
          <div class="ambient-slider">
            <AppSlider v-model="ambientIntensity" :min="5" :max="60" :step="5" :disabled="!ambientOn" aria-label="Ambient intensity" />
            <span class="ambient-pct">{{ ambientIntensity }}%</span>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Detail pages" icon="film"
      description="Movie and TV detail page behavior.">
      <div class="ambient-controls">
        <div class="ambient-row">
          <div class="ambient-row-text">
            <div class="ambient-row-title">Show unavailable recommendations</div>
            <div class="ambient-row-desc">
              "More Like This" normally lists only titles in your library. On,
              it also shows the rest — they open on the public metadata provider in a new tab.
            </div>
          </div>
          <AppSwitch v-model="showUnavailableRecs" size="md" aria-label="Show unavailable recommendations" />
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Home sections" icon="rows"
      description="Choose which rows appear on Home and in what order.">
      <template #actions>
        <button v-if="!isDefaultSections" class="sv2-btn ghost" type="button" @click="reset()">
          Reset to default
        </button>
      </template>
      <div class="sections-list">
        <div
          v-for="(s, i) in sections"
          :key="s.id"
          class="section-row"
          :class="{ hidden: s.hidden }"
        >
          <div class="section-move">
            <button
              type="button" class="move-btn" :disabled="i === 0"
              :aria-label="`Move ${s.label} up`" @click="move(s.id, -1)"
            >
              <Icon name="chevup" :size="12" />
            </button>
            <button
              type="button" class="move-btn" :disabled="i === sections.length - 1"
              :aria-label="`Move ${s.label} down`" @click="move(s.id, 1)"
            >
              <Icon name="chevdown" :size="12" />
            </button>
          </div>
          <div class="section-text">
            <div class="section-name">{{ s.label }}</div>
            <div class="section-desc">{{ s.desc }}</div>
          </div>
          <AppSwitch
            :model-value="!s.hidden"
            :aria-label="`Show ${s.label}`"
            @update:model-value="toggle(s.id)"
          />
        </div>
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.appearance-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(300px, 0.85fr);
  gap: 16px;
  align-items: start;
}
.appearance-grid :deep(.sv2-section) { height: calc(100% - 16px); }

/* ── Theme cards ── */
.theme-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}
.theme-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  cursor: pointer;
  text-align: left;
  transition: border-color 0.12s, background 0.12s;
}
.theme-card:hover { border-color: var(--border-strong); }
.theme-card.active { border-color: var(--gold); background: var(--gold-soft); }
.theme-preview {
  aspect-ratio: 16 / 10;
  border-radius: var(--r-sm);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: var(--pv-bg);
  border: 1px solid var(--border);
}
.theme-preview .theme-bar { height: 14%; background: var(--pv-bar); }
.theme-preview .theme-content { flex: 1; padding: 12% 14%; display: flex; flex-direction: column; gap: 6px; }
.theme-preview .theme-chip { width: 36%; height: 12%; border-radius: 999px; }
.theme-preview .theme-line { height: 7%; background: var(--pv-line); border-radius: 999px; }
.theme-preview .theme-line.long { width: 80%; }
.theme-preview .theme-line.short { width: 50%; }
.theme-preview.split { flex-direction: row; }
.split-half { flex: 1; display: flex; flex-direction: column; background: var(--pv-bg); }
.split-half .theme-content { padding: 12% 10%; }
.theme-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  padding: 2px 6px 2px;
  min-width: 0;
}
.theme-label .theme-hint {
  margin-left: auto;
  color: var(--fg-2);
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.theme-check { color: var(--gold); flex-shrink: 0; }

/* ── Accent swatches ── */
.accent-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(76px, 1fr));
  gap: 10px;
}
.accent-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 10px 12px 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  cursor: pointer;
  min-width: 76px;
  transition: border-color 0.12s, background 0.12s;
}
.accent-cell:hover { border-color: var(--border-strong); }
.accent-cell.active { border-color: var(--swatch); background: color-mix(in srgb, var(--swatch) 10%, transparent); }
.accent-dot {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, color-mix(in srgb, var(--swatch) 78%, #000), var(--swatch));
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(0, 0, 0, 0.7);
  box-shadow: 0 0 0 1px var(--border), 0 4px 12px color-mix(in srgb, var(--swatch) 25%, transparent);
}
.accent-cell-name { font-size: 11px; color: var(--fg-2); }
.accent-cell.active .accent-cell-name { color: var(--fg-0); }

/* ── Density ── */
.density-grid { display: grid; grid-template-columns: 1fr; gap: 10px; }
.density-card {
  display: flex;
  gap: 10px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.density-card:hover { border-color: var(--border-strong); }
.density-card.active { border-color: var(--gold); background: var(--gold-soft); }
.density-card input { accent-color: var(--gold); margin-top: 3px; }
.density-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.density-title { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.density-desc  { font-size: 11.5px; color: var(--fg-2); line-height: 1.4; }
.density-preview {
  margin-top: 8px;
  background: var(--bg-0);
  border-radius: var(--r-xs);
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.density-row { height: 6px; background: rgb(var(--ink) / 0.06); border-radius: 2px; }
.density-row.tall { height: 10px; }

/* ── Ambient ── */
.ambient-controls { display: flex; flex-direction: column; gap: 4px; }
.ambient-row {
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.ambient-row + .ambient-row { margin-top: 8px; }
.ambient-row.disabled { opacity: 0.55; }
.ambient-row-text { flex: 1; min-width: 0; }
.ambient-row-title { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.ambient-row-desc { font-size: 11.5px; color: var(--fg-2); margin-top: 2px; }
.ambient-slider { display: flex; align-items: center; gap: 12px; width: 220px; }
.ambient-pct { font-family: var(--font-mono); font-size: 11px; color: var(--fg-2); width: 36px; text-align: right; }

/* ── Home sections ── */
.sections-list {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  overflow: hidden;
}
.section-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
}
.section-row + .section-row { border-top: 1px solid var(--border); }
.section-row.hidden .section-name,
.section-row.hidden .section-desc { color: var(--fg-3); }
.section-move { display: flex; flex-direction: column; gap: 2px; }
.move-btn {
  width: 22px;
  height: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  color: var(--fg-2);
  transition: background 0.1s, color 0.1s;
}
.move-btn:hover:not(:disabled) { background: rgb(var(--ink) / 0.07); color: var(--fg-0); }
.move-btn:disabled { opacity: 0.3; cursor: default; }
@media (pointer: coarse) {
  .move-btn { width: 44px; height: 44px; }
}
.section-text { flex: 1; min-width: 0; }
.section-name { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.section-desc { font-size: 11.5px; color: var(--fg-2); margin-top: 1px; }

@media (max-width: 920px) {
  .appearance-grid { grid-template-columns: 1fr; gap: 0; }
  .appearance-grid :deep(.sv2-section) { height: auto; }
}
@media (max-width: 520px) {
  .theme-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .accent-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 7px; }
  .accent-cell { min-width: 0; padding-inline: 6px; }
  .ambient-row { align-items: flex-start; gap: 12px; }
  .ambient-slider { width: 135px; }
  .section-row { padding-inline: 10px; }
}
</style>
