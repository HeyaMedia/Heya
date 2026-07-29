<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import {
  managerAlbumDetailQuery,
  type ManagerTrackView,
} from '~/queries/manager'

const route = useRoute()
const router = useRouter()

const libraryId = computed(() => Number(route.params.id))
const itemId = computed(() => Number(route.params.item))
const albumRef = computed(() => String(route.params.album))

const detailQuery = useQuery(() => managerAlbumDetailQuery(albumRef.value))
const detail = computed(() => detailQuery.data.value)
const album = computed(() => detail.value?.album)
const loading = computed(() => detailQuery.isLoading.value && !detail.value)

useLiveRefresh([
  {
    events: ['media.updated', 'manager.changed'],
    keys: [['manager', 'album']],
  },
])

const artistPath = computed(() => `/manager/library/${libraryId.value}/${itemId.value}`)
// Real history back when we came from the artist page (scroll-memory
// restores); plain navigation for deep links.
function goBack(event: MouseEvent) {
  if (event.defaultPrevented || event.button !== 0
    || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  event.preventDefault()
  const previous = (history.state as { back?: string } | null)?.back
  if (typeof previous === 'string' && previous.startsWith(artistPath.value)) router.back()
  else router.push(artistPath.value)
}

const TYPE_LABELS: Record<string, string> = {
  album: 'Album', ep: 'EP', single: 'Single', remix: 'Remix',
  compilation: 'Compilation', soundtrack: 'Soundtrack', live: 'Live',
  mixtape: 'Mixtape', demo: 'Demo', broadcast: 'Broadcast', other: 'Other',
}

const coverSrc = computed(() => {
  const al = album.value
  if (!al) return null
  if (al.in_library) return useAlbumCoverUrl(detail.value?.artist_slug, al.slug)
  return metadataImageProxyUrl(al.cover_url ?? '') || null
})

const publicLink = computed(() => {
  const al = album.value
  if (!al?.in_library || !detail.value?.artist_slug || !al.slug) return null
  return `/music/artist/${detail.value.artist_slug}/${al.slug}`
})

function trackLength(seconds: number): string {
  if (!seconds) return '—'
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}

// Multi-disc albums get one table section per disc.
const discs = computed(() => {
  const tracks = detail.value?.tracks ?? []
  const byDisc = new Map<number, ManagerTrackView[]>()
  for (const track of tracks) {
    const disc = track.disc || 1
    if (!byDisc.has(disc)) byDisc.set(disc, [])
    byDisc.get(disc)!.push(track)
  }
  return [...byDisc.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([number, tracks]) => ({ number, tracks }))
})

function albumLink(ref: number | string): string {
  return `${artistPath.value}/${ref}`
}
</script>

<template>
  <div>
    <div v-if="loading" class="mgr-loading">Loading album…</div>

    <template v-else-if="detail && album">
      <a :href="artistPath" class="det-back" @click="goBack">
        <Icon name="back" :size="13" /> {{ detail.artist_name }}
      </a>

      <!-- ── Hero ────────────────────────────────────────────────────── -->
      <div class="alb-hero">
        <div class="alb-cover-wrap">
          <NuxtImg v-if="coverSrc" :src="coverSrc" class="alb-cover" :alt="''" width="360" />
          <div v-else class="alb-cover" aria-hidden="true" />
        </div>
        <div class="alb-facts">
          <div class="alb-eyebrow mono">{{ TYPE_LABELS[album.type] ?? album.type }}</div>
          <h1 class="alb-title">{{ album.title }}</h1>
          <div class="alb-meta">
            <NuxtLink :to="artistPath" class="alb-artist">{{ detail.artist_name }}</NuxtLink>
            <span v-if="album.release_date || album.year">{{ album.release_date || album.year }}</span>
            <span v-if="detail.duration_seconds">{{ Math.round(detail.duration_seconds / 60) }} min</span>
            <span v-if="album.rating">★ {{ album.rating.toFixed(1) }}</span>
          </div>
          <div class="alb-chips">
            <span class="det-badge" :class="album.tracks_have >= album.tracks_total && album.tracks_have > 0 ? 'tone-good' : (album.in_library ? 'tone-warn' : 'tone-bad')">
              {{ album.in_library ? `${album.tracks_have} / ${album.tracks_total} tracks` : 'Not in library' }}
            </span>
            <span v-if="album.in_library" class="det-badge tone-none">{{ fmtBytes(album.size_bytes) }}</span>
            <span v-if="!album.in_library && album.tracks_total" class="det-badge tone-none">{{ album.tracks_total }} tracks</span>
            <span v-if="detail.label" class="det-badge tone-none">{{ detail.label }}</span>
            <span v-if="detail.country" class="det-badge tone-none">{{ detail.country }}</span>
            <span v-if="detail.genres?.length" class="det-badge tone-none">{{ detail.genres.slice(0, 3).join(', ') }}</span>
          </div>
          <p v-if="detail.overview" class="alb-overview">{{ detail.overview }}</p>
          <div class="alb-actions">
            <NuxtLink v-if="publicLink" :to="publicLink" class="mgr-btn"><Icon name="play" :size="13" /> Open in Heya</NuxtLink>
            <template v-if="detail.editions?.length">
              <span class="alb-editions-label mono">Editions:</span>
              <NuxtLink
                v-for="edition in detail.editions"
                :key="edition.id || `d${edition.discography_id}`"
                :to="albumLink(edition.id || `d${edition.discography_id}`)"
                class="mgr-btn alb-edition-btn"
                :class="{ wanted: !edition.in_library }"
              >{{ edition.title }}</NuxtLink>
            </template>
          </div>
        </div>
      </div>

      <!-- ── Tracks ──────────────────────────────────────────────────── -->
      <div v-if="discs.length" class="det-sections">
        <section v-for="disc in discs" :key="disc.number" class="det-section">
          <div class="det-section-head static">
            <span class="det-section-title">{{ discs.length > 1 ? `Disc ${disc.number}` : 'Tracks' }}</span>
            <span class="det-section-sub mono">{{ disc.tracks.filter(t => t.has_file).length }}/{{ disc.tracks.length }} on disk</span>
          </div>
          <div class="det-tablewrap">
            <table class="det-table">
              <thead>
                <tr><th class="num alb-num-col">#</th><th>Title</th><th class="num">Length</th><th>Quality</th><th class="num">Size</th></tr>
              </thead>
              <tbody>
                <tr v-for="track in disc.tracks" :key="track.id" :class="{ 'det-row-wanted': !track.has_file }">
                  <td class="num mono">{{ track.number }}</td>
                  <td class="det-ep-title">{{ track.title }}</td>
                  <td class="num mono">{{ trackLength(track.duration_seconds) }}</td>
                  <td>
                    <span v-if="track.has_file" class="det-badge tone-good">{{ track.quality }}</span>
                    <span v-else class="det-badge tone-bad">Missing</span>
                  </td>
                  <td class="num mono">{{ track.size_bytes ? fmtBytes(track.size_bytes) : '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
      <div v-else class="alb-empty">
        <Icon name="info" :size="14" />
        This release isn't in the library — the provider catalog knows
        <template v-if="album.tracks_total"> it has {{ album.tracks_total }} tracks but</template>
        no tracklist for it yet.
      </div>
    </template>
  </div>
</template>

<style scoped>
.det-back {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-2);
  text-decoration: none;
}
.det-back:hover { color: var(--gold-bright); }

/* ── Hero ────────────────────────────────────────────────────────────── */
.alb-hero {
  display: flex;
  gap: 20px;
  padding: 20px;
  margin-bottom: 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
}
.alb-cover-wrap { flex: 0 0 180px; }
@media (max-width: 720px) {
  .alb-cover-wrap { flex-basis: 110px; }
}
.alb-cover {
  display: block;
  width: 100%;
  aspect-ratio: 1;
  border-radius: var(--r-md);
  object-fit: cover;
  background: var(--bg-3);
  box-shadow: 0 6px 24px rgb(var(--ink) / 0.15);
}
.alb-facts { min-width: 0; flex: 1; }
.alb-eyebrow {
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--gold-bright);
}
.alb-title {
  margin: 4px 0 0;
  font-size: clamp(19px, 3vw, 27px);
  font-weight: 700;
  color: var(--fg-0);
  line-height: 1.15;
}
.alb-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 6px;
  font-size: 13px;
  color: var(--fg-2);
}
.alb-artist { color: var(--fg-1); font-weight: 500; text-decoration: none; }
.alb-artist:hover { color: var(--gold-bright); }
.alb-chips {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 12px;
}
.alb-overview {
  margin: 12px 0 0;
  max-width: 860px;
  font-size: 13px;
  line-height: 1.55;
  color: var(--fg-1);
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.alb-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 14px;
}
.alb-editions-label { font-size: 10.5px; color: var(--fg-3); text-transform: uppercase; letter-spacing: 0.08em; }
.alb-edition-btn { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; }
.alb-edition-btn.wanted { color: var(--fg-3); }

/* ── Tracks (shares det- table vocabulary with the artist page) ──────── */
.det-sections { display: flex; flex-direction: column; gap: 12px; }
.det-section {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.det-section-head {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 12px 14px;
  color: var(--fg-1);
}
.det-section-title { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.det-section-sub { margin-left: auto; font-size: 11px; color: var(--fg-3); }
.det-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 9px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  border: 1px solid var(--border);
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.06);
}
.det-badge.tone-good { color: var(--good); border-color: color-mix(in srgb, var(--good) 40%, transparent); background: color-mix(in srgb, var(--good) 9%, transparent); }
.det-badge.tone-warn { color: var(--gold-bright); border-color: color-mix(in srgb, var(--gold) 45%, transparent); background: var(--gold-soft); }
.det-badge.tone-bad { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); background: color-mix(in srgb, var(--bad) 9%, transparent); }

.det-tablewrap { border-top: 1px solid var(--border); }
.det-table { width: 100%; border-collapse: collapse; }
.det-table th {
  padding: 8px 12px;
  text-align: left;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
  white-space: nowrap;
}
.det-table th.num,
.det-table td.num { text-align: right; }
.alb-num-col { width: 46px; }
.det-table tbody tr { border-top: 1px solid var(--border); }
.det-table tbody tr:hover { background: rgb(var(--ink) / 0.04); }
.det-table td {
  padding: 8px 12px;
  font-size: 12.5px;
  color: var(--fg-1);
  white-space: nowrap;
}
.det-table td.mono { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); }
.det-ep-title { max-width: 480px; overflow: hidden; text-overflow: ellipsis; color: var(--fg-0); }
.det-row-wanted .det-ep-title { color: var(--fg-2); }

.alb-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 24px 16px;
  color: var(--fg-2);
  font-size: 13px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}
</style>
