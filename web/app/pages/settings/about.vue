<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">About</h2>
      <p class="page-desc">Credits and acknowledgments</p>
    </div>

    <section class="section">
      <div class="about-hero">
        <div class="about-logo">
          <svg width="36" height="36" viewBox="0 0 22 22">
            <circle cx="11" cy="11" r="10" fill="none" stroke="var(--gold)" stroke-width="1.5" />
            <circle cx="11" cy="11" r="4" fill="var(--gold)" />
            <circle cx="11" cy="11" r="1.5" fill="var(--bg-2)" />
          </svg>
        </div>
        <div class="about-hero-text">
          <div class="about-name">heya<span class="gold-dot">.</span>media</div>
          <div class="about-tagline">A traditional storehouse for your digital media</div>
          <div class="about-version">{{ health?.version || 'v1.0.0' }}</div>
        </div>
      </div>
    </section>

    <section class="section">
      <h3 class="section-heading">
        <Icon name="film" :size="14" />
        Supported Media
      </h3>
      <div class="caps-grid">
        <div class="cap-card">
          <div class="cap-icon movie"><Icon name="film" :size="16" /></div>
          <div class="cap-label">Movies</div>
        </div>
        <div class="cap-card">
          <div class="cap-icon tv"><Icon name="tv" :size="16" /></div>
          <div class="cap-label">TV Shows</div>
        </div>
        <div class="cap-card">
          <div class="cap-icon music"><Icon name="music" :size="16" /></div>
          <div class="cap-label">Music</div>
        </div>
        <div class="cap-card">
          <div class="cap-icon book"><Icon name="book" :size="16" /></div>
          <div class="cap-label">Books</div>
        </div>
      </div>
    </section>

    <section class="section">
      <h3 class="section-heading">
        <Icon name="database" :size="14" />
        Data Sources
      </h3>
      <div class="source-list">
        <div class="source-row">
          <span class="source-name">TMDB</span>
          <span class="source-desc">Movies & TV metadata, posters, backdrops</span>
        </div>
        <div class="source-row">
          <span class="source-name">TVDB</span>
          <span class="source-desc">TV series data & episode information</span>
        </div>
        <div class="source-row">
          <span class="source-name">MusicBrainz</span>
          <span class="source-desc">Music catalog, artist & album metadata</span>
        </div>
        <div class="source-row">
          <span class="source-name">OpenLibrary</span>
          <span class="source-desc">Book metadata, covers, and author info</span>
        </div>
        <div class="source-row">
          <span class="source-name">Fanart.tv</span>
          <span class="source-desc">High-quality fan artwork, logos, thumbnails</span>
        </div>
        <div class="source-row">
          <span class="source-name">OMDb</span>
          <span class="source-desc">Aggregated ratings from RT, Metacritic, IMDb</span>
        </div>
      </div>
    </section>

    <section class="section">
      <h3 class="section-heading">
        <Icon name="wrench" :size="14" />
        Built With
      </h3>
      <div class="built-with">
        <span class="bw-tag">Go</span>
        <span class="bw-tag">Nuxt</span>
        <span class="bw-tag">PostgreSQL</span>
        <span class="bw-tag">River</span>
        <span class="bw-tag">ffmpeg</span>
        <span class="bw-tag">Phosphor Icons</span>
      </div>
    </section>

    <div class="about-footer">
      <Icon name="copyright" :size="12" />
      <span>Heya Media — Built with care.</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { HealthResponse } from '~~/shared/types'

const health = ref<HealthResponse | null>(null)

onMounted(async () => {
  try { health.value = await $fetch<HealthResponse>('/api/health') } catch {}
})
</script>

<style scoped>
.page-header { margin-bottom: 32px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin: 0 0 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

/* About hero */
.about-hero {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 28px 24px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
}

.about-logo {
  width: 56px;
  height: 56px;
  border-radius: var(--r-md);
  background: rgba(230, 185, 74, 0.08);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.about-name {
  font-size: 22px;
  font-weight: 600;
  letter-spacing: -0.02em;
}
.gold-dot { color: var(--gold); }
.about-tagline { font-size: 13px; color: var(--fg-2); margin-top: 2px; }
.about-version {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-top: 4px;
  letter-spacing: 0.04em;
}

/* Capabilities */
.caps-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}

.cap-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 20px 12px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.cap-icon {
  width: 36px;
  height: 36px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
}

.cap-icon.movie { background: var(--gold-soft); color: var(--gold); }
.cap-icon.tv { background: rgba(140, 160, 255, 0.12); color: rgb(140, 160, 255); }
.cap-icon.music { background: rgba(200, 140, 255, 0.12); color: rgb(200, 140, 255); }
.cap-icon.book { background: rgba(140, 220, 180, 0.12); color: rgb(140, 220, 180); }

.cap-label { font-size: 12px; font-weight: 500; color: var(--fg-1); }

/* Sources */
.source-list {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}

.source-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}
.source-row:last-child { border-bottom: none; }
.source-name { font-weight: 600; width: 110px; flex-shrink: 0; }
.source-desc { color: var(--fg-2); font-size: 12px; }

/* Built with */
.built-with {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.bw-tag {
  font-size: 12px;
  font-weight: 500;
  padding: 6px 14px;
  border-radius: 100px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  color: var(--fg-1);
}

/* Footer */
.about-footer {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--fg-4);
  font-family: var(--font-mono);
  padding-top: 20px;
  border-top: 1px solid var(--border);
}
</style>
