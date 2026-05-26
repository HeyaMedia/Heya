<script setup lang="ts">
definePageMeta({ layout: 'settings' })

// Appearance lives in localStorage for v1 — the design system isn't
// theme-ready (gold/dark is hard-coded into heya.css), so each option here
// is a forward-looking choice we'll honour when the theme tokens go live.
// When that lands, swap to /api/me/settings.appearance and drop the local
// shim.

type Density = 'comfortable' | 'compact'

const DENSITY_KEY = 'heya_density'

const density = useState<Density>('appearance_density', () => 'comfortable')

onMounted(() => {
  const stored = localStorage.getItem(DENSITY_KEY)
  if (stored === 'comfortable' || stored === 'compact') density.value = stored
})

watch(density, (v) => {
  if (import.meta.client) localStorage.setItem(DENSITY_KEY, v)
})
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Appearance</h2>
      <p class="sv2-page-desc">
        Theme, density, and accent. Stored on this device for now — once
        appearance becomes part of your account settings, your choice will
        sync across devices automatically.
      </p>
    </header>

    <SettingsSection title="Theme" icon="brightness"
      description="Heya ships with the warm-gold dark theme. Light and high-contrast variants are on the roadmap.">
      <div class="theme-grid">
        <div class="theme-card active">
          <div class="theme-preview dark">
            <div class="theme-bar" />
            <div class="theme-content">
              <div class="theme-chip gold" />
              <div class="theme-line long" />
              <div class="theme-line short" />
            </div>
          </div>
          <div class="theme-label">
            <Icon name="check" :size="12" class="theme-check" />
            Heya Dark
          </div>
        </div>
        <div class="theme-card disabled">
          <div class="theme-preview light">
            <div class="theme-bar" />
            <div class="theme-content">
              <div class="theme-chip" />
              <div class="theme-line long" />
              <div class="theme-line short" />
            </div>
          </div>
          <div class="theme-label muted">Heya Light · soon</div>
        </div>
        <div class="theme-card disabled">
          <div class="theme-preview hc">
            <div class="theme-bar" />
            <div class="theme-content">
              <div class="theme-chip" />
              <div class="theme-line long" />
              <div class="theme-line short" />
            </div>
          </div>
          <div class="theme-label muted">High Contrast · soon</div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Density" icon="grid"
      description="How much breathing room rows get. Compact stacks more on screen at the cost of readability.">
      <div class="density-grid">
        <label class="density-card" :class="{ active: density === 'comfortable' }">
          <input type="radio" value="comfortable" v-model="density" />
          <div class="density-body">
            <div class="density-title">Comfortable</div>
            <div class="density-desc">Default — generous spacing, more breathing room.</div>
            <div class="density-preview">
              <div class="density-row tall" /><div class="density-row tall" /><div class="density-row tall" />
            </div>
          </div>
        </label>
        <label class="density-card" :class="{ active: density === 'compact' }">
          <input type="radio" value="compact" v-model="density" />
          <div class="density-body">
            <div class="density-title">Compact</div>
            <div class="density-desc">Tighter rows for table-heavy power users.</div>
            <div class="density-preview">
              <div class="density-row" /><div class="density-row" /><div class="density-row" />
              <div class="density-row" /><div class="density-row" />
            </div>
          </div>
        </label>
      </div>
    </SettingsSection>

    <SettingsSection title="Accent" icon="sparkle"
      description="Brand accent — the gold against the dark background. Custom accents arrive with the theme system.">
      <div class="accent-row">
        <span class="accent-swatch" />
        <div class="accent-info">
          <div class="accent-name">Heya Gold</div>
          <div class="accent-hex">#e6b94a</div>
        </div>
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }

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
  transition: border-color 0.12s;
}
.theme-card.active { border-color: var(--gold); background: var(--gold-soft); }
.theme-card.disabled { opacity: 0.5; }
.theme-preview {
  aspect-ratio: 16 / 10;
  border-radius: var(--r-sm);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.theme-preview.dark  { background: #0a0a0a; }
.theme-preview.light { background: #f5f5f1; }
.theme-preview.hc    { background: #000; }
.theme-preview .theme-bar  { height: 14%; background: rgba(255,255,255,0.06); }
.theme-preview.light .theme-bar { background: rgba(0,0,0,0.08); }
.theme-preview.hc    .theme-bar { background: #fff; }
.theme-preview .theme-content { flex: 1; padding: 12% 14%; display: flex; flex-direction: column; gap: 6px; }
.theme-preview .theme-chip { width: 36%; height: 10%; background: rgba(255,255,255,0.10); border-radius: 999px; }
.theme-preview .theme-chip.gold { background: var(--gold); }
.theme-preview.light .theme-chip { background: rgba(0,0,0,0.10); }
.theme-preview.hc .theme-chip { background: #fff; }
.theme-preview .theme-line { height: 6%; background: rgba(255,255,255,0.06); border-radius: 999px; }
.theme-preview.light .theme-line { background: rgba(0,0,0,0.08); }
.theme-preview.hc .theme-line { background: #fff; opacity: 0.4; }
.theme-preview .theme-line.long  { width: 80%; }
.theme-preview .theme-line.short { width: 50%; }
.theme-label {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  padding: 4px 6px 2px;
}
.theme-label.muted { color: var(--fg-4); }
.theme-check { color: var(--gold); }

.density-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; }
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
.density-desc  { font-size: 11.5px; color: var(--fg-3); line-height: 1.4; }
.density-preview {
  margin-top: 8px;
  background: var(--bg-0);
  border-radius: var(--r-xs);
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.density-row { height: 6px; background: rgba(255,255,255,0.06); border-radius: 2px; }
.density-row.tall { height: 10px; }

.accent-row { display: flex; align-items: center; gap: 14px; padding: 4px 0; }
.accent-swatch {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  box-shadow: 0 0 0 1px var(--border), 0 4px 12px rgba(230, 185, 74, 0.18);
}
.accent-info { display: flex; flex-direction: column; gap: 2px; }
.accent-name { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.accent-hex { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-3); }
</style>
