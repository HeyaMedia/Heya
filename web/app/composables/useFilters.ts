import type { EnrichedMediaItem, FilterState } from '~~/shared/types'

export function defaultFilters(): FilterState {
  return {
    genres: [],
    yearMin: null,
    yearMax: null,
    ratingMin: null,
    ratingMax: null,
    resolutions: [],
    videoFormats: [],
    audioFormats: [],
    watched: 'all',
    studioIds: [],
    studioNames: [],
    personIds: [],
    personNames: [],
    language: null,
  }
}

export function hasActiveFilters(f: FilterState): boolean {
  return (
    f.genres.length > 0 ||
    f.yearMin !== null ||
    f.yearMax !== null ||
    f.ratingMin !== null ||
    f.ratingMax !== null ||
    f.resolutions.length > 0 ||
    f.videoFormats.length > 0 ||
    f.audioFormats.length > 0 ||
    f.watched !== 'all' ||
    f.studioIds.length > 0 ||
    f.personIds.length > 0 ||
    f.language !== null
  )
}

export function applyFilters(
  items: EnrichedMediaItem[],
  filters: FilterState,
  watchedSet: Set<number>,
  personMediaIds?: Set<number>,
  studioMediaIds?: Set<number>,
): EnrichedMediaItem[] {
  if (!hasActiveFilters(filters)) return items

  return items.filter((item) => {
    if (filters.genres.length > 0) {
      const itemGenres = item.genres || []
      if (!filters.genres.every(g => itemGenres.includes(g))) return false
    }

    if (filters.yearMin !== null || filters.yearMax !== null) {
      const y = parseInt(item.year)
      if (isNaN(y)) return false
      if (filters.yearMin !== null && y < filters.yearMin) return false
      if (filters.yearMax !== null && y > filters.yearMax) return false
    }

    if (filters.ratingMin !== null || filters.ratingMax !== null) {
      const r = item.rating ?? 0
      if (filters.ratingMin !== null && r < filters.ratingMin) return false
      if (filters.ratingMax !== null && r > filters.ratingMax) return false
    }

    if (filters.resolutions.length > 0) {
      if (!item.resolution || !filters.resolutions.includes(item.resolution)) return false
    }

    if (filters.videoFormats.length > 0) {
      const formats = item.video_formats || []
      if (!filters.videoFormats.every(format => formats.includes(format))) return false
    }

    if (filters.audioFormats.length > 0) {
      const formats = item.audio_formats || []
      if (!filters.audioFormats.every(format => formats.includes(format))) return false
    }

    if (filters.watched === 'watched' && !watchedSet.has(item.id)) return false
    if (filters.watched === 'unwatched' && watchedSet.has(item.id)) return false

    if (filters.personIds.length > 0 && personMediaIds) {
      if (!personMediaIds.has(item.id)) return false
    }

    if (filters.studioIds.length > 0 && studioMediaIds) {
      if (!studioMediaIds.has(item.id)) return false
    }

    if (filters.language !== null) {
      if (item.original_language !== filters.language) return false
    }

    return true
  })
}

export function extractAvailableGenres(items: EnrichedMediaItem[]): string[] {
  const genreSet = new Set<string>()
  for (const item of items) {
    for (const g of item.genres || []) genreSet.add(g)
  }
  return [...genreSet].sort()
}

export function extractYearRange(items: EnrichedMediaItem[]): [number, number] {
  let min = 9999
  let max = 0
  for (const item of items) {
    const y = parseInt(item.year)
    if (!isNaN(y)) {
      if (y < min) min = y
      if (y > max) max = y
    }
  }
  return [min === 9999 ? 1900 : min, max === 0 ? new Date().getFullYear() : max]
}

export function extractLanguages(items: EnrichedMediaItem[]): string[] {
  const langs = new Set<string>()
  for (const item of items) {
    if (item.original_language) langs.add(item.original_language)
  }
  return [...langs].sort()
}

export function filterToJSON(f: FilterState): string {
  return JSON.stringify(f)
}

export function filterFromJSON(json: string | null): FilterState {
  if (!json) return defaultFilters()
  try {
    return { ...defaultFilters(), ...JSON.parse(json) }
  } catch {
    return defaultFilters()
  }
}
