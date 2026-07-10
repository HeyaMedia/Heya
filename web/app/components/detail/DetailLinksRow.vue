<template>
  <div v-if="links.length" class="dlr">
    <NuxtLink
      v-for="l in links"
      :key="l.key"
      :to="l.to"
      :target="l.external ? '_blank' : undefined"
      class="dlr-link"
      :class="{ prominent: l.prominent }"
    >
      <Icon v-if="l.icon" :name="l.icon" :size="12" />
      {{ l.label }}
      <span v-if="l.external" class="dlr-ext">↗</span>
    </NuxtLink>
  </div>
</template>

<script setup lang="ts">
// Provenance rail across the top of detail heroes: the collection first
// (prominent — it's internal navigation), then the title's external
// identities (IMDb / TMDB / TVDB / AniDB / heya.media) as quiet glass chips
// opening in new tabs. One place to gather everything that says "this title
// elsewhere".
import type { MediaItem } from '~~/shared/types'

const props = defineProps<{
  mediaItem: MediaItem
  collection?: { id: number; name: string } | null
}>()

interface RowLink {
  key: string
  label: string
  to: string
  external: boolean
  icon?: string
  prominent?: boolean
}

const links = computed<RowLink[]>(() => {
  const out: RowLink[] = []
  const ids = props.mediaItem.external_ids ?? {}
  const kind = props.mediaItem.media_type === 'movie' ? 'movie' : 'tv'
  if (props.collection) {
    out.push({
      key: 'collection',
      label: props.collection.name,
      to: `/collection/${props.collection.id}`,
      external: false,
      icon: 'folder',
      prominent: true,
    })
  }
  if (ids.imdb) out.push({ key: 'imdb', label: 'IMDb', to: `https://www.imdb.com/title/${ids.imdb}/`, external: true })
  if (ids.tmdb) out.push({ key: 'tmdb', label: 'TMDB', to: `https://www.themoviedb.org/${kind}/${ids.tmdb}`, external: true })
  if (ids.tvdb) out.push({ key: 'tvdb', label: 'TVDB', to: `https://thetvdb.com/dereferrer/series/${ids.tvdb}`, external: true })
  if (ids.anidb) out.push({ key: 'anidb', label: 'AniDB', to: `https://anidb.net/anime/${ids.anidb}`, external: true })
  // heya.media: the stored slug when the item has one, else the
  // fetch-on-demand provider-id construction.
  const heya = props.mediaItem.heya_slug
    ? `https://heya.media/${props.mediaItem.heya_slug}`
    : heyaMediaExternalUrl(props.mediaItem.media_type, ids)
  if (heya) out.push({ key: 'heya', label: 'heya.media', to: heya, external: true })
  return out
})
</script>

<style scoped>
.dlr {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-bottom: 14px;
}
.dlr-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 11px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.05em;
  color: var(--fg-1);
  background: color-mix(in oklab, var(--bg-2) 80%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  transition: color 0.15s, background 0.15s, border-color 0.15s;
}
.dlr-link:hover { color: var(--fg-0); background: var(--bg-3); border-color: var(--border-strong); }
.dlr-ext { opacity: 0.55; font-size: 10px; }
/* The collection: internal navigation, worth a louder coat. */
.dlr-link.prominent {
  color: var(--gold);
  font-weight: 600;
  background: color-mix(in srgb, var(--gold) 12%, var(--bg-2));
  border-color: color-mix(in srgb, var(--gold) 40%, transparent);
}
.dlr-link.prominent:hover {
  color: var(--gold-bright);
  background: color-mix(in srgb, var(--gold) 20%, var(--bg-2));
}
</style>
