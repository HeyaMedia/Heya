<script setup lang="ts">
// MixCollage — a mix's artwork built from the mix itself: a 2×2 grid of the
// distinct album covers its tracks draw from. A mix is a cross-section of a
// library, and the collage shows that breadth honestly — four records from
// four corners of the catalog — instead of repeating the seed artist's
// portrait on every card. Falls back to the seed art (then the icon tile)
// when the mix spans fewer than four albums, so sparse libraries keep
// today's look.
const props = defineProps<{
  /** (artist_slug, album_slug) per track — duplicates fine, we dedupe. */
  tracks: Array<{ artist_slug: string; album_slug: string }>
  /** Seed-artist art used when the mix spans <4 distinct albums. */
  seedSrc?: string | null
  alt?: string
}>()

// `art` reports the first image that ACTUALLY rendered (post error-cascade),
// or null when the collage fell through to the icon tile — so consumers that
// tone-sample (the playlist hero's Play button) follow the healed image
// instead of probing a candidate that may 404. Optional to listen to.
const emit = defineEmits<{ art: [src: string | null] }>()
let announced: string | null | undefined
function announce(src: string | null) {
  if (src === announced) return
  announced = src
  emit('art', src)
}
function onImgLoad(src: string) { announce(src) }

// Image URLs are unconditional (they can 404 when an album genuinely has no
// art anywhere) — so every candidate is disposable: a failed load drops the
// URL and the collage recomputes with what's left, cascading grid → single →
// icon tile. Without this, one dead cover rendered as a blank box showing
// its alt text (mirror of Poster.vue's imgError handling).
const failed = ref(new Set<string>())
watch(() => [props.tracks, props.seedSrc] as const, () => { failed.value = new Set() })
function onImgError(src: string) {
  if (failed.value.has(src)) return
  const next = new Set(failed.value)
  next.add(src)
  failed.value = next
}

const covers = computed(() => {
  const seen = new Set<string>()
  const urls: string[] = []
  for (const t of props.tracks) {
    if (!t.artist_slug || !t.album_slug) continue
    const key = `${t.artist_slug}/${t.album_slug}`
    if (seen.has(key)) continue
    seen.add(key)
    const url = useAlbumCoverUrl(t.artist_slug, t.album_slug)
    if (url && !failed.value.has(url)) urls.push(url)
    if (urls.length === 4) break
  }
  return urls
})

const seedOk = computed(() => !!props.seedSrc && !failed.value.has(props.seedSrc))

const mode = computed<'grid' | 'single' | 'fallback'>(() => {
  if (covers.value.length >= 4) return 'grid'
  if (seedOk.value || covers.value.length > 0) return 'single'
  return 'fallback'
})

const singleSrc = computed(() => (seedOk.value ? props.seedSrc! : covers.value[0] || ''))

// Declared AFTER `mode` — an immediate watch reads it at setup (TDZ).
watch(mode, (m) => { if (m === 'fallback') announce(null) }, { immediate: true })
</script>

<template>
  <!-- Mode class is `is-*`, deliberately distinct from the inner element
       classes: `mcg-single` names the absolutely-positioned <img>, and
       reusing it on the root once made the root itself absolute — it escaped
       its grid cell and filled the whole .app with the cover. -->
  <div class="mcg" :class="`is-${mode}`">
    <template v-if="mode === 'grid'">
      <NuxtImg
        v-for="(src, i) in covers"
        :key="src"
        :src="src"
        :alt="i === 0 ? (alt ?? '') : ''"
        :width="160"
        :quality="80"
        densities="1x 2x"
        loading="lazy"
        class="mcg-cell"
        @load="onImgLoad(src)"
        @error="onImgError(src)"
      />
    </template>
    <NuxtImg
      v-else-if="mode === 'single'"
      :key="singleSrc"
      :src="singleSrc"
      :alt="alt ?? ''"
      :width="320"
      :quality="80"
      densities="1x 2x"
      loading="lazy"
      class="mcg-single"
      @load="onImgLoad(singleSrc)"
      @error="onImgError(singleSrc)"
    />
    <div v-else class="mcg-fallback"><Icon name="sparkle" :size="36" /></div>
    <slot />
  </div>
</template>

<style scoped>
.mcg {
  position: relative;
  aspect-ratio: 1 / 1;
  background: var(--bg-3);
  border-radius: var(--r-md);
  overflow: hidden;
  box-shadow: 0 8px 18px rgb(var(--shade) / 0.45);
}
.mcg.is-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  grid-template-rows: 1fr 1fr;
  /* Hairline seams so the four covers read as a deliberate collage, not a
     broken image. gap paints .mcg's bg-3 through — theme-correct. */
  gap: 2px;
}
.mcg-cell { width: 100%; height: 100%; object-fit: cover; display: block; }
.mcg-single { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.mcg-fallback {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--gold);
  background: linear-gradient(135deg, color-mix(in srgb, var(--gold) 10%, transparent), color-mix(in srgb, var(--gold) 2%, transparent));
}
</style>
