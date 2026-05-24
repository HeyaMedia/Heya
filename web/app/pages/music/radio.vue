<template>
  <div class="page-pad">
    <header class="radio-head">
      <div class="radio-head-art">
        <Icon name="radio" :size="36" />
      </div>
      <div class="radio-head-meta">
        <div class="m-kind">Internet Radio</div>
        <h1 class="m-title">Tune anything from anywhere</h1>
        <p class="m-sub">Browse 60,000+ stations powered by Radio Browser. Save your favorites; sort by country, genre, language, or codec quality.</p>
        <div class="m-actions">
          <button class="btn btn-primary" disabled title="Coming later">
            <Icon name="plus" :size="14" /> Add Custom Stream
          </button>
        </div>
      </div>
    </header>

    <div class="m-tabs">
      <button v-for="t in tabs" :key="t" class="m-tab" :class="{ active: tab === t }" @click="tab = t">{{ t }}</button>
    </div>

    <div v-if="tab === 'Featured'">
      <h2 class="row-h">Top Voted</h2>
      <div class="station-grid">
        <div v-for="i in 8" :key="i" class="station-tile placeholder">
          <div class="station-art-placeholder">
            <Icon name="radio" :size="22" />
          </div>
          <div class="station-name">Loading…</div>
          <div class="station-genre">—</div>
        </div>
      </div>
      <h2 class="row-h" style="margin-top: 36px">Most Popular</h2>
      <div class="station-grid">
        <div v-for="i in 8" :key="i" class="station-tile placeholder">
          <div class="station-art-placeholder">
            <Icon name="radio" :size="22" />
          </div>
          <div class="station-name">Loading…</div>
          <div class="station-genre">—</div>
        </div>
      </div>
    </div>

    <div v-else-if="tab === 'Favorites'" class="m-empty-state">
      <Icon name="heart" :size="40" class="m-empty-icon" />
      <h3>No favorite stations yet</h3>
      <p>Tap the heart on any station to keep it pinned here.</p>
    </div>

    <div v-else-if="tab === 'By Country'" class="m-empty-state">
      <Icon name="globe" :size="40" class="m-empty-icon" />
      <h3>Country index coming later</h3>
      <p>Grouped grid of stations per country, sorted by listener count and codec quality.</p>
    </div>

    <div v-else class="m-empty-state">
      <Icon name="music" :size="40" class="m-empty-icon" />
      <h3>Genre browse coming later</h3>
      <p>Jazz, electronica, classical, talk, sports — drill down by tag.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: 'default' })

const tabs = ['Featured', 'Favorites', 'By Country', 'By Genre'] as const
type Tab = (typeof tabs)[number]
const tab = ref<Tab>('Featured')
</script>

<style scoped>
.radio-head {
  display: flex;
  gap: 24px;
  align-items: flex-end;
  padding: 32px 0 24px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 24px;
}
.radio-head-art {
  width: 160px;
  height: 160px;
  border-radius: var(--r-md);
  background: linear-gradient(135deg, #1e2a3a, #0d1a2e);
  display: flex; align-items: center; justify-content: center;
  color: var(--gold);
  box-shadow: 0 16px 36px rgba(0,0,0,0.5);
  flex-shrink: 0;
}
.radio-head-meta { flex: 1; min-width: 0; }
.m-kind {
  font-size: 11px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.12em;
  color: var(--fg-2); margin-bottom: 6px;
}
.m-title {
  font-size: clamp(28px, 3.5vw, 44px);
  font-weight: 800;
  line-height: 1.05;
  margin-bottom: 8px;
  letter-spacing: -0.02em;
  color: var(--fg-0);
}
.m-sub { color: var(--fg-2); margin-bottom: 18px; max-width: 60ch; }
.m-actions { display: flex; gap: 10px; }
.m-actions :deep(.btn-primary) {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 0 18px; height: 40px;
  border-radius: 999px;
  font-weight: 600;
}

.m-tabs {
  display: flex;
  gap: 6px;
  margin-bottom: 24px;
  border-bottom: 1px solid var(--border);
}
.m-tab {
  padding: 10px 16px;
  background: transparent;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--fg-2);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s;
}
.m-tab:hover { color: var(--fg-0); }
.m-tab.active { color: var(--gold); border-bottom-color: var(--gold); }

.row-h { font-size: 18px; font-weight: 700; margin-bottom: 14px; }
.station-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 16px;
}
.station-tile {
  background: rgba(255,255,255,0.04);
  border-radius: var(--r-md);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transition: background 0.15s;
  cursor: pointer;
}
.station-tile:hover { background: rgba(255,255,255,0.08); }
.station-tile.placeholder { pointer-events: none; opacity: 0.55; }
.station-art-placeholder {
  width: 60px; height: 60px;
  border-radius: var(--r-sm);
  background: rgba(255,255,255,0.06);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
}
.station-name { font-size: 13px; font-weight: 600; color: var(--fg-0); }
.station-genre { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }

.m-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 80px 24px;
  color: var(--fg-2);
}
.m-empty-icon { color: var(--fg-3); margin-bottom: 16px; }
.m-empty-state h3 { font-size: 18px; font-weight: 600; color: var(--fg-1); margin-bottom: 8px; }
.m-empty-state p { font-size: 13px; max-width: 50ch; color: var(--fg-2); }
</style>
