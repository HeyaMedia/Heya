// Episode presence — "do we actually have this episode?"
//
// tv_episodes rows are the full provider catalog (every episode TMDB/TVDB
// knows about), so a currently-airing season lists episodes we don't hold yet.
// Presence is derived server-side from parsed library files into the detail
// doc's `episode_files` map, keyed `s{season}e{episode}` (see BuildEpisodeFileMap
// in internal/service/media.go). Every episode listing/count on the TV detail
// and season pages routes through these helpers so they agree — the season
// listing, the season-card "N eps", the hero "X episodes", and the watched
// badges all mean the same thing.

type EpisodeFileMap = Record<string, { file_id: number, size?: number }> | undefined | null

export function isEpisodePresent(files: EpisodeFileMap, seasonNumber: number, episodeNumber: number): boolean {
  return !!files?.[`s${seasonNumber}e${episodeNumber}`]
}

// The episodes to actually surface for a season: only the ones we have. Falls
// back to the full list when nothing resolves as present (e.g. a season pack
// parsed without per-episode numbers) so a season we *do* have never renders
// as empty / "0 eps".
export function presentEpisodes<T extends { episode_number: number }>(
  files: EpisodeFileMap,
  seasonNumber: number,
  episodes: T[] | undefined | null,
): T[] {
  const all = episodes || []
  const present = all.filter(ep => isEpisodePresent(files, seasonNumber, ep.episode_number))
  return present.length ? present : all
}

export function presentEpisodeCount(
  files: EpisodeFileMap,
  seasonNumber: number,
  episodes: { episode_number: number }[] | undefined | null,
): number {
  return presentEpisodes(files, seasonNumber, episodes).length
}
