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

const covers = computed(() => {
  const seen = new Set<string>()
  const urls: string[] = []
  for (const t of props.tracks) {
    if (!t.artist_slug || !t.album_slug) continue
    const key = `${t.artist_slug}/${t.album_slug}`
    if (seen.has(key)) continue
    seen.add(key)
    const url = useAlbumCoverUrl(t.artist_slug, t.album_slug)
    if (url) urls.push(url)
    if (urls.length === 4) break
  }
  return urls
})

const mode = computed<'grid' | 'single' | 'fallback'>(() => {
  if (covers.value.length >= 4) return 'grid'
  if (props.seedSrc || covers.value.length > 0) return 'single'
  return 'fallback'
})

const singleSrc = computed(() => props.seedSrc || covers.value[0] || '')
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
      />
    </template>
    <NuxtImg
      v-else-if="mode === 'single'"
      :src="singleSrc"
      :alt="alt ?? ''"
      :width="320"
      :quality="80"
      densities="1x 2x"
      loading="lazy"
      class="mcg-single"
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
