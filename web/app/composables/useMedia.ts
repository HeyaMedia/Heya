const TMDB_IMAGE_BASE = 'https://image.tmdb.org/t/p'

export function usePosterUrl(path: string | null | undefined, size: 'w185' | 'w342' | 'w500' = 'w342') {
  if (!path) return null
  if (path.startsWith('http')) return path
  return `${TMDB_IMAGE_BASE}/${size}${path}`
}

export function useBackdropUrl(path: string | null | undefined, size: 'w780' | 'w1280' | 'original' = 'w1280') {
  if (!path) return null
  if (path.startsWith('http')) return path
  return `${TMDB_IMAGE_BASE}/${size}${path}`
}

export function mediaTypeColor(type: string) {
  const colors: Record<string, string> = {
    movie: 'text-heya-movie',
    tv: 'text-heya-tv',
    music: 'text-heya-music',
    book: 'text-heya-book',
  }
  return colors[type] || 'text-gray-400'
}

export function mediaTypeBg(type: string) {
  const colors: Record<string, string> = {
    movie: 'bg-heya-movie/20 text-heya-movie',
    tv: 'bg-heya-tv/20 text-heya-tv',
    music: 'bg-heya-music/20 text-heya-music',
    book: 'bg-heya-book/20 text-heya-book',
  }
  return colors[type] || 'bg-gray-500/20 text-gray-400'
}

export function mediaTypeLabel(type: string) {
  const labels: Record<string, string> = {
    movie: 'Movie',
    tv: 'TV Show',
    music: 'Music',
    book: 'Book',
  }
  return labels[type] || type
}
