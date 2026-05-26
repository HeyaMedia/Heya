<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type Health = components['schemas']['HealthBody']
type Ready  = components['schemas']['ReadyBody']

const { $heya } = useNuxtApp()

const health = ref<Health | null>(null)
const ready = ref<Ready | null>(null)
const loading = ref(true)

async function load() {
  try {
    const [h, r] = await Promise.all([
      $heya('/api/health'),
      $heya('/api/health/ready'),
    ])
    health.value = h
    ready.value = r
  } catch {} finally {
    loading.value = false
  }
}

const overallTone = computed<'good' | 'warn' | 'bad'>(() => {
  if (!ready.value) return 'warn'
  return ready.value.status === 'ok' ? 'good' : 'bad'
})

const buildKv = computed(() => [
  { key: 'Version',  value: health.value?.version ?? '—', mono: true, copy: true },
  { key: 'Database', value: health.value?.database ?? '—' },
  { key: 'Status',   value: health.value?.status ?? '—' },
])

const MEDIA_CAPS = [
  { kind: 'movie', icon: 'film',  label: 'Movies' },
  { kind: 'tv',    icon: 'tv',    label: 'TV Shows' },
  { kind: 'music', icon: 'music', label: 'Music' },
  { kind: 'book',  icon: 'book',  label: 'Books' },
]

const SOURCES = [
  { name: 'TMDB',         desc: 'Movies & TV metadata, posters, backdrops' },
  { name: 'TVDB',         desc: 'TV series data & episode information' },
  { name: 'MusicBrainz',  desc: 'Music catalog, artist & album metadata' },
  { name: 'OpenLibrary',  desc: 'Book metadata, covers, and author info' },
  { name: 'Fanart.tv',    desc: 'High-quality fan artwork, logos, thumbnails' },
  { name: 'OMDb',         desc: 'Aggregated ratings from RT, Metacritic, IMDb' },
]

const STACK = ['Go', 'Nuxt 4', 'PostgreSQL', 'River', 'sqlc', 'ffmpeg', 'Huma', 'Phosphor']

function componentIcon(name: string): string {
  switch (name) {
    case 'database':   return 'database'
    case 'watcher':    return 'eye'
    case 'scheduler':  return 'timer'
    case 'transcoder': return 'film'
    case 'tailscale':  return 'network'
    default:           return 'pulse'
  }
}

onMounted(load)
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">About Heya</h2>
      <p class="sv2-page-desc">
        Build info, supported media, upstream data sources, and what the
        server is made of.
      </p>
    </header>

    <section class="about-hero">
      <div class="hero-logo">
        <svg width="40" height="40" viewBox="0 0 22 22">
          <circle cx="11" cy="11" r="10" fill="none" stroke="var(--gold)" stroke-width="1.5" />
          <circle cx="11" cy="11" r="4" fill="var(--gold)" />
          <circle cx="11" cy="11" r="1.5" fill="var(--bg-2)" />
        </svg>
      </div>
      <div class="hero-text">
        <div class="hero-name">heya<span class="gold-dot">.</span>media</div>
        <div class="hero-tagline">A traditional storehouse for your digital media</div>
        <div class="hero-row">
          <StatusBadge :state="overallTone === 'good' ? 'ok' : overallTone === 'warn' ? 'warn' : 'error'">
            {{ overallTone === 'good' ? 'All systems' : overallTone === 'warn' ? 'Loading' : 'Degraded' }}
          </StatusBadge>
          <span class="hero-version">{{ health?.version || '—' }}</span>
        </div>
      </div>
    </section>

    <SettingsSection title="Build" icon="info"
      description="What this binary thinks it is. Click any value to copy.">
      <KVTable :rows="buildKv" />
    </SettingsSection>

    <SettingsSection title="Subsystems" icon="pulse"
      description="Per-component readiness from /api/health/ready. The dashboard polls this every few seconds.">
      <div v-if="loading" class="loading-state">
        <Icon name="spinner" :size="14" /> Probing components…
      </div>
      <div v-else-if="ready" class="comp-list">
        <div v-for="c in ready.components" :key="c.name" class="comp-row">
          <div class="comp-name">
            <Icon :name="componentIcon(c.name)" :size="14" />
            <span>{{ c.name }}</span>
          </div>
          <div class="comp-msg">{{ c.message || (c.ok ? 'healthy' : 'check failed') }}</div>
          <StatusBadge :state="c.ok ? 'ok' : 'error'">
            {{ c.ok ? 'ok' : 'down' }}
          </StatusBadge>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Supported media" icon="film">
      <div class="cap-grid">
        <div v-for="c in MEDIA_CAPS" :key="c.kind" class="cap-card" :class="`tone-${c.kind}`">
          <Icon :name="c.icon" :size="18" />
          <span>{{ c.label }}</span>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Data sources" icon="database"
      description="Upstream metadata providers. Reached through heya.media — the only outbound client in the binary.">
      <div class="src-list">
        <div v-for="s in SOURCES" :key="s.name" class="src-row">
          <span class="src-name">{{ s.name }}</span>
          <span class="src-desc">{{ s.desc }}</span>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Built with" icon="wrench">
      <div class="stack-row">
        <span v-for="t in STACK" :key="t" class="stack-tag">{{ t }}</span>
      </div>
    </SettingsSection>

    <SettingsSection title="Inspired by" icon="heart">
      <div class="src-list">
        <div class="src-row">
          <span class="src-name">Kyoo</span>
          <span class="src-desc">
            Transcoding architecture: hardware acceleration, keyframe-aligned
            segmentation, and adaptive bitrate ladders.
            <a href="https://github.com/zoriya/kyoo" target="_blank" rel="noopener" class="src-link">github.com/zoriya/kyoo</a>
          </span>
        </div>
        <div class="src-row">
          <span class="src-name">Plex</span>
          <span class="src-desc">
            Settings IA — unified "you" + "server" shell with a single sidebar.
          </span>
        </div>
      </div>
    </SettingsSection>

    <footer class="about-foot">
      <Icon name="copyright" :size="11" />
      <span>Heya Media — built with care.</span>
    </footer>
  </div>
</template>

<style scoped>
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }

.about-hero {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 22px 24px;
  margin-bottom: 28px;
  background: linear-gradient(135deg, var(--bg-2), var(--bg-1));
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  position: relative;
  overflow: hidden;
}
.about-hero::after {
  content: '';
  position: absolute;
  inset: 0;
  background: radial-gradient(circle at 100% 0%, rgba(230, 185, 74, 0.07), transparent 60%);
  pointer-events: none;
}
.hero-logo {
  width: 56px;
  height: 56px;
  border-radius: var(--r-md);
  background: rgba(230, 185, 74, 0.10);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  box-shadow: inset 0 0 0 1px rgba(230, 185, 74, 0.2);
}
.hero-text { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.hero-name { font-size: 22px; font-weight: 600; letter-spacing: -0.02em; color: var(--fg-0); }
.gold-dot { color: var(--gold); }
.hero-tagline { font-size: 12.5px; color: var(--fg-2); }
.hero-row { display: flex; align-items: center; gap: 10px; margin-top: 6px; }
.hero-version { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); letter-spacing: 0.04em; }

.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.comp-list {
  display: flex; flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  overflow: hidden;
}
.comp-row {
  display: grid;
  grid-template-columns: 200px 1fr auto;
  align-items: center;
  gap: 16px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--border);
  font-size: 12.5px;
}
.comp-row:last-child { border-bottom: 0; }
.comp-name { display: flex; align-items: center; gap: 8px; color: var(--fg-1); font-weight: 500; }
.comp-msg  { color: var(--fg-3); font-family: var(--font-mono); font-size: 11.5px; }

.cap-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 8px;
}
.cap-card {
  display: flex; align-items: center; gap: 10px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
}
.cap-card.tone-movie { box-shadow: inset 3px 0 0 var(--gold); }
.cap-card.tone-movie > :first-child { color: var(--gold); }
.cap-card.tone-tv    { box-shadow: inset 3px 0 0 rgb(140, 160, 255); }
.cap-card.tone-tv    > :first-child { color: rgb(140, 160, 255); }
.cap-card.tone-music { box-shadow: inset 3px 0 0 rgb(200, 140, 255); }
.cap-card.tone-music > :first-child { color: rgb(200, 140, 255); }
.cap-card.tone-book  { box-shadow: inset 3px 0 0 rgb(140, 220, 180); }
.cap-card.tone-book  > :first-child { color: rgb(140, 220, 180); }

.src-list {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.src-row {
  display: grid;
  grid-template-columns: 130px 1fr;
  gap: 14px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--border);
  font-size: 12.5px;
  align-items: baseline;
}
.src-row:last-child { border-bottom: 0; }
.src-name { font-weight: 600; color: var(--fg-1); }
.src-desc { color: var(--fg-3); line-height: 1.5; }
.src-link {
  color: var(--gold);
  text-decoration: none;
  font-family: var(--font-mono);
  font-size: 11px;
  margin-left: 6px;
}
.src-link:hover { text-decoration: underline; }

.stack-row {
  display: flex; flex-wrap: wrap; gap: 6px;
}
.stack-tag {
  font-size: 12px;
  font-weight: 500;
  padding: 5px 12px;
  border-radius: 999px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  color: var(--fg-1);
}

.about-foot {
  display: flex; align-items: center; gap: 6px;
  font-size: 11px;
  color: var(--fg-4);
  font-family: var(--font-mono);
  padding: 16px 0 0;
  margin-top: 12px;
  border-top: 1px solid var(--border);
}
</style>
