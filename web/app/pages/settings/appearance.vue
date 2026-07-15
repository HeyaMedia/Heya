<script setup lang="ts">
import type { ThemeMode, TypeSet, AppearancePrefs } from '~/composables/useAppearance'

definePageMeta({ layout: 'settings' })

// Everything on this page applies live (attributes / inline vars on <html>
// flip immediately via useAppearance) and persists to /api/me/settings, with a
// localStorage mirror for flash-free boot. No Save button — appearance is the
// one settings page where instant feedback beats a commit step.

const { prefs, set, setAccentPreset, setAccentCustom } = useAppearance()
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
  () => prefs.value.accentCustomDerived?.accent
    ?? ACCENTS.find((a) => a.name === prefs.value.accent)?.hex ?? ACCENTS[0]!.hex,
)
const isCustomAccent = computed(() => !!prefs.value.accentCustom)
const activeThemeLabel = computed(() => themes.find(theme => theme.value === prefs.value.theme)?.label ?? 'System')
const activeAccentLabel = computed(() =>
  isCustomAccent.value ? 'Custom' : (ACCENTS.find(accent => accent.name === prefs.value.accent)?.label ?? 'Heya Gold'))
const visibleSectionCount = computed(() => sections.value.filter(section => !section.hidden).length)

// ── Custom accent picker ─────────────────────────────────────────────────
// The native color input drives a live derive; the hex field mirrors it and
// accepts typed values. Both feed setAccentCustom (which clamps + caches the
// family). Selecting a preset clears the override.
const colorValue = computed(() => prefs.value.accentCustom || activeAccentHex.value)
const hexField = ref(prefs.value.accentCustom ?? '')
watch(() => prefs.value.accentCustom, (v) => { hexField.value = v ?? '' })
function onColorInput(e: Event) {
  setAccentCustom((e.target as HTMLInputElement).value)
}
function commitHex() {
  const raw = hexField.value.trim()
  if (!raw) return
  const ok = setAccentCustom(raw.startsWith('#') ? raw : `#${raw}`)
  if (!ok) hexField.value = prefs.value.accentCustom ?? '' // revert a bad hex
}

// ── Type sets ─────────────────────────────────────────────────────────────
// Each preview card renders in its OWN faces (inline family) regardless of the
// active set, so you can compare before committing. Mono line stays JetBrains.
const TYPESET_FACES: Record<TypeSet, { display: string; sans: string }> = {
  heya: { display: "'Archivo Variable', sans-serif", sans: "'Inter', sans-serif" },
  editorial: { display: "'Fraunces Variable', Georgia, serif", sans: "'Source Serif 4 Variable', Georgia, serif" },
  grotesk: { display: "'Space Grotesk Variable', sans-serif", sans: "'Inter', sans-serif" },
  rounded: { display: "'Nunito Variable', sans-serif", sans: "'Inter', sans-serif" },
  system: { display: "system-ui, sans-serif", sans: "system-ui, sans-serif" },
}
function typesetStyle(t: TypeSet) {
  return { '--tset-display': TYPESET_FACES[t].display, '--tset-sans': TYPESET_FACES[t].sans }
}

// ── Segmented controls ────────────────────────────────────────────────────
const densityOptions = [
  { v: 'compact', l: 'Compact' },
  { v: 'comfortable', l: 'Comfortable' },
  { v: 'spacious', l: 'Spacious' },
] as const
const fontScaleOptions = [
  { v: 'sm', l: 'Small' },
  { v: 'md', l: 'Default' },
  { v: 'lg', l: 'Large' },
] as const

type FlairSeg = {
  key: keyof AppearancePrefs
  title: string
  desc: string
  options: readonly { v: string; l: string }[]
}
const flairSegs: FlairSeg[] = [
  { key: 'lighting', title: 'Lighting', desc: 'Directional card shadows and glowing primary buttons — or a flat, shadow-light surface.',
    options: [{ v: 'dramatic', l: 'Dramatic' }, { v: 'flat', l: 'Flat' }] },
  { key: 'glass', title: 'Glass', desc: 'Frosted blur behind the top bar, ledgers, panels and sheets — or crisp, blur-free chrome.',
    options: [{ v: 'rich', l: 'Rich' }, { v: 'minimal', l: 'Minimal' }] },
  { key: 'radius', title: 'Corners', desc: 'Rounded and soft, or tight and sharp across cards, buttons and panels.',
    options: [{ v: 'soft', l: 'Soft' }, { v: 'sharp', l: 'Sharp' }] },
  { key: 'hero', title: 'Hero height', desc: 'How much vertical space the hero artwork claims on movie, show and album pages.',
    options: [{ v: 'standard', l: 'Standard' }, { v: 'short', l: 'Short' }] },
  { key: 'motion', title: 'Motion', desc: 'Reduced turns animations off everywhere. Full still respects your system reduced-motion setting.',
    options: [{ v: 'system', l: 'System' }, { v: 'reduced', l: 'Reduced' }, { v: 'full', l: 'Full' }] },
  { key: 'scrollbar', title: 'Scrollbar', desc: 'Overlay floats a thin auto-hiding thumb over the content so pages run edge-to-edge. Classic brings back your browser’s native scrollbar.',
    options: [{ v: 'overlay', l: 'Overlay' }, { v: 'classic', l: 'Classic' }] },
]
function pickFlair(key: keyof AppearancePrefs, value: string) {
  set(key, value as never)
}

const toneFollow = computed({
  get: () => prefs.value.toneFollow !== false,
  set: (v: boolean) => set('toneFollow', v),
})
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
      description="Dark is the Heya look. OLED trades the deep greys for true black; Light is the same design on warm paper; System follows your OS.">
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

    <SettingsSection title="Accent" icon="sparkle"
      description="The color that carries selection, progress, and emphasis throughout the app. Pick a preset or dial in your own.">
      <div class="accent-grid">
        <button
          v-for="a in ACCENTS"
          :key="a.name"
          type="button"
          class="accent-cell"
          :class="{ active: prefs.accent === a.name && !isCustomAccent }"
          :style="{ '--swatch': a.hex }"
          :aria-label="a.label"
          :aria-pressed="prefs.accent === a.name && !isCustomAccent"
          @click="setAccentPreset(a.name)"
        >
          <span class="accent-dot">
            <Icon v-if="prefs.accent === a.name && !isCustomAccent" name="check" :size="14" />
          </span>
          <span class="accent-cell-name">{{ a.label }}</span>
        </button>

        <!-- Custom accent: native color input styled as the swatch. -->
        <div class="accent-cell custom" :class="{ active: isCustomAccent }" :style="{ '--swatch': colorValue }">
          <label class="accent-dot custom-dot">
            <input
              type="color"
              class="accent-color-input"
              :value="colorValue"
              aria-label="Custom accent color"
              @input="onColorInput"
            />
            <Icon v-if="isCustomAccent" name="check" :size="14" class="custom-check" />
            <Icon v-else name="sparkle" :size="13" class="custom-plus" />
          </label>
          <span class="accent-cell-name">Custom</span>
        </div>
      </div>

      <div class="accent-hex-row">
        <label class="accent-hex-label" for="accent-hex">Hex</label>
        <input
          id="accent-hex"
          v-model="hexField"
          type="text"
          class="accent-hex-input"
          placeholder="#e84a8f"
          spellcheck="false"
          autocomplete="off"
          @change="commitHex"
          @keydown.enter="commitHex"
        />
        <span class="accent-hex-hint">Type a hex, or use the Custom swatch. Presets clear it.</span>
      </div>
    </SettingsSection>

    <SettingsSection title="Type set" icon="type"
      description="Swaps the display and body faces. Mono ledgers always stay JetBrains Mono — the design signature.">
      <div class="typeset-grid">
        <button
          v-for="t in TYPESETS"
          :key="t.value"
          type="button"
          class="typeset-card"
          :class="{ active: prefs.typeset === t.value }"
          :aria-pressed="prefs.typeset === t.value"
          @click="set('typeset', t.value)"
        >
          <div class="typeset-preview" :style="typesetStyle(t.value)">
            <div class="tset-title">Aa</div>
            <div class="tset-sample">The quiet library hums.</div>
            <div class="tset-mono">01:23 · FLAC · 24/96</div>
          </div>
          <div class="typeset-label">
            <Icon v-if="prefs.typeset === t.value" name="check" :size="12" class="theme-check" />
            <span>{{ t.label }}</span>
            <span class="typeset-hint">{{ t.hint }}</span>
          </div>
        </button>
      </div>
    </SettingsSection>

    <div class="appearance-grid">
      <SettingsSection title="Density" icon="grid"
        description="How much media fits on screen at once — tighter grids and rows, or roomier ones.">
        <div class="seg" role="radiogroup" aria-label="Density">
          <button
            v-for="o in densityOptions"
            :key="o.v"
            type="button"
            class="seg-btn"
            role="radio"
            :aria-checked="prefs.density === o.v"
            :class="{ active: prefs.density === o.v }"
            @click="set('density', o.v)"
          >{{ o.l }}</button>
        </div>
      </SettingsSection>

      <SettingsSection title="Font size" icon="type"
        description="Nudge the base type scale up or down.">
        <div class="seg" role="radiogroup" aria-label="Font size">
          <button
            v-for="o in fontScaleOptions"
            :key="o.v"
            type="button"
            class="seg-btn"
            role="radio"
            :aria-checked="prefs.fontScale === o.v"
            :class="{ active: prefs.fontScale === o.v }"
            @click="set('fontScale', o.v)"
          >{{ o.l }}</button>
        </div>
      </SettingsSection>
    </div>

    <SettingsSection title="Flair" icon="sparkle"
      description="Fine-tune the depth, texture and motion of the interface.">
      <div class="flair-list">
        <div class="flair-row">
          <div class="flair-text">
            <div class="flair-title">Tone follow</div>
            <div class="flair-desc">
              Pages tint their accents from the artwork on screen. The music playbar always
              follows the playing track regardless — that's playback identity, not page tint.
            </div>
          </div>
          <AppSwitch v-model="toneFollow" size="md" aria-label="Tone follow" />
        </div>

        <div v-for="seg in flairSegs" :key="seg.key" class="flair-row">
          <div class="flair-text">
            <div class="flair-title">{{ seg.title }}</div>
            <div class="flair-desc">{{ seg.desc }}</div>
          </div>
          <div class="seg" role="radiogroup" :aria-label="seg.title">
            <button
              v-for="o in seg.options"
              :key="o.v"
              type="button"
              class="seg-btn"
              role="radio"
              :aria-checked="prefs[seg.key] === o.v"
              :class="{ active: prefs[seg.key] === o.v }"
              @click="pickFlair(seg.key, o.v)"
            >{{ o.l }}</button>
          </div>
        </div>
      </div>
    </SettingsSection>

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
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
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
  grid-template-columns: repeat(5, minmax(72px, 1fr));
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
  min-width: 72px;
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

/* Custom cell: the native color input sits invisibly over the swatch dot. */
.accent-cell.custom { position: relative; }
.custom-dot { position: relative; overflow: hidden; cursor: pointer; }
.accent-color-input {
  position: absolute;
  inset: -4px;
  width: calc(100% + 8px);
  height: calc(100% + 8px);
  padding: 0;
  border: 0;
  background: transparent;
  cursor: pointer;
  opacity: 0;
}
.custom-check { color: rgba(0, 0, 0, 0.72); pointer-events: none; }
.custom-plus { color: rgba(0, 0, 0, 0.6); pointer-events: none; }

.accent-hex-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
  flex-wrap: wrap;
}
.accent-hex-label {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.accent-hex-input {
  width: 118px;
  padding: 7px 10px;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-0);
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  outline: none;
  transition: border-color 0.12s, background 0.12s;
}
.accent-hex-input:focus { border-color: var(--gold); background: rgb(var(--ink) / 0.08); }
.accent-hex-hint { font-size: 11px; color: var(--fg-3); }

/* ── Type set cards ── */
.typeset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(168px, 1fr));
  gap: 12px;
}
.typeset-card {
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
.typeset-card:hover { border-color: var(--border-strong); }
.typeset-card.active { border-color: var(--gold); background: var(--gold-soft); }
.typeset-preview {
  padding: 14px 14px 12px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  border: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-height: 104px;
}
.tset-title {
  font-family: var(--tset-display);
  font-size: 34px;
  font-weight: 700;
  line-height: 1;
  color: var(--fg-0);
  letter-spacing: -0.01em;
}
.tset-sample {
  font-family: var(--tset-sans);
  font-size: 13px;
  color: var(--fg-1);
  line-height: 1.35;
}
.tset-mono {
  margin-top: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}
.typeset-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  padding: 2px 6px;
  min-width: 0;
}
.typeset-hint {
  margin-left: auto;
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 9.5px;
  letter-spacing: 0.04em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Segmented control (density / font size / flair) ── */
.seg {
  display: inline-flex;
  padding: 3px;
  gap: 2px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.seg-btn {
  flex: 1;
  padding: 7px 16px;
  border-radius: var(--r-sm);
  font-size: 12.5px;
  font-weight: 500;
  color: var(--fg-2);
  white-space: nowrap;
  transition: background 0.12s, color 0.12s;
}
.seg-btn:hover { color: var(--fg-0); }
.seg-btn.active {
  background: var(--gold-soft);
  color: var(--gold-bright);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--gold) 35%, transparent);
}

/* ── Flair rows ── */
.flair-list { display: flex; flex-direction: column; }
.flair-row {
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 14px 0;
  border-bottom: 1px solid var(--border);
}
.flair-row:last-child { border-bottom: 0; }
.flair-text { flex: 1; min-width: 0; }
.flair-title { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.flair-desc { font-size: 11.5px; color: var(--fg-2); margin-top: 2px; line-height: 1.45; max-width: 60ch; }

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
  .seg { display: flex; }
  .flair-row { align-items: flex-start; flex-direction: column; gap: 10px; }
  .ambient-row { align-items: flex-start; gap: 12px; }
  .ambient-slider { width: 135px; }
  .section-row { padding-inline: 10px; }
}
</style>
