// Display names for heya.media upstream provider slugs (artist
// metadata_sources, top-track / similar-artist provider attribution).
const PROVIDER_LABELS: Record<string, string> = {
  musicbrainz: 'MusicBrainz',
  lastfm: 'Last.fm',
  listenbrainz: 'ListenBrainz',
  audiodb: 'TheAudioDB',
  tidal: 'TIDAL',
  deezer: 'Deezer',
  discogs: 'Discogs',
  apple: 'Apple Music',
  fanart: 'Fanart.tv',
  wikidata: 'Wikidata',
  bandcamp: 'Bandcamp',
  spotify: 'Spotify',
  tmdb: 'TMDB',
  tvdb: 'TheTVDB',
}

export function providerLabel(slug: string): string {
  const key = slug.trim().toLowerCase()
  if (PROVIDER_LABELS[key]) return PROVIDER_LABELS[key]
  return key.charAt(0).toUpperCase() + key.slice(1)
}
