<template>
  <div v-if="links.length" class="ext-links">
    <a
      v-for="link in links"
      :key="link.key"
      :href="link.url"
      target="_blank"
      rel="noopener noreferrer"
      class="ext-chip"
      :title="link.label"
    >
      <span class="ext-chip-label">{{ link.label }}</span>
    </a>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  externalIds: Record<string, string> | null | undefined
  kind: 'artist' | 'album'
}>()

interface Link {
  key: string
  label: string
  url: string
}

// Provider URL templates per kind. Two flavors of `mbid` because MB has
// separate /artist/{mbid} and /release/{mbid} hierarchies — we route based
// on the chip's owning entity, not on what's in the ID itself.
const TEMPLATES: Record<'artist' | 'album', Record<string, { label: string; url: (id: string) => string }>> = {
  artist: {
    mbid:                { label: 'MusicBrainz', url: (id) => `https://musicbrainz.org/artist/${id}` },
    musicbrainz:         { label: 'MusicBrainz', url: (id) => `https://musicbrainz.org/artist/${id}` },
    musicbrainz_artist:  { label: 'MusicBrainz', url: (id) => `https://musicbrainz.org/artist/${id}` },
    discogs:             { label: 'Discogs',     url: (id) => `https://www.discogs.com/artist/${id}` },
    apple:               { label: 'Apple Music', url: (id) => `https://music.apple.com/artist/${id}` },
    deezer:              { label: 'Deezer',      url: (id) => `https://www.deezer.com/artist/${id}` },
    spotify:             { label: 'Spotify',     url: (id) => `https://open.spotify.com/artist/${id}` },
    wikidata:            { label: 'Wikidata',    url: (id) => `https://www.wikidata.org/wiki/${id}` },
    wikipedia:           { label: 'Wikipedia',   url: (id) => `https://en.wikipedia.org/wiki/${id}` },
  },
  album: {
    mbid:                { label: 'MusicBrainz', url: (id) => `https://musicbrainz.org/release/${id}` },
    musicbrainz:         { label: 'MusicBrainz', url: (id) => `https://musicbrainz.org/release/${id}` },
    mb_release:          { label: 'MusicBrainz', url: (id) => `https://musicbrainz.org/release/${id}` },
    mb_release_group:    { label: 'MusicBrainz', url: (id) => `https://musicbrainz.org/release-group/${id}` },
    discogs:             { label: 'Discogs',     url: (id) => `https://www.discogs.com/release/${id}` },
    apple:               { label: 'Apple Music', url: (id) => `https://music.apple.com/album/${id}` },
    deezer:              { label: 'Deezer',      url: (id) => `https://www.deezer.com/album/${id}` },
    spotify:             { label: 'Spotify',     url: (id) => `https://open.spotify.com/album/${id}` },
  },
}

const links = computed<Link[]>(() => {
  const ids = props.externalIds ?? {}
  const templates = TEMPLATES[props.kind] ?? {}
  const seen = new Set<string>()
  const out: Link[] = []
  for (const [key, val] of Object.entries(ids)) {
    if (!val) continue
    const tmpl = templates[key]
    if (!tmpl) continue
    // De-dupe by label so we don't render three MusicBrainz chips when the
    // same MBID is stored under {mbid, musicbrainz, musicbrainz_artist}.
    if (seen.has(tmpl.label)) continue
    seen.add(tmpl.label)
    out.push({ key, label: tmpl.label, url: tmpl.url(val) })
  }
  // Stable provider order so the chip row doesn't reshuffle between renders.
  const order = ['MusicBrainz', 'Discogs', 'Apple Music', 'Spotify', 'Deezer', 'Wikipedia', 'Wikidata']
  out.sort((a, b) => order.indexOf(a.label) - order.indexOf(b.label))
  return out
})
</script>

<style scoped>
.ext-links {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 12px;
}
.ext-chip {
  display: inline-flex;
  align-items: center;
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  padding: 4px 10px;
  border-radius: 999px;
  background: var(--bg-3);
  color: var(--fg-2);
  border: 1px solid var(--border);
  text-decoration: none;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.ext-chip:hover {
  background: var(--bg-4);
  color: var(--fg-0);
  border-color: var(--fg-3);
}
</style>
