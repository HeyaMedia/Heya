export function useImageUrl(mediaId: number | undefined, type: 'poster' | 'backdrop') {
  if (!mediaId) return null
  return `/api/media/${mediaId}/image/${type}`
}

export function usePosterUrl(mediaId: number | undefined) {
  return useImageUrl(mediaId, 'poster')
}

export function useBackdropUrl(mediaId: number | undefined) {
  return useImageUrl(mediaId, 'backdrop')
}

// useAlbumCoverUrl returns the canonical album-cover URL. Use this instead
// of binding `album.cover_path` directly — the raw column may hold a
// `data/...` filesystem path (the Nuxt router treats those as routes and
// renders the SPA shell) or an upstream URL. The endpoint resolves both:
// serves the local file when present, 302-redirects to upstream otherwise.
export function useAlbumCoverUrl(albumId: number | undefined) {
  if (!albumId) return null
  return `/api/albums/${albumId}/cover`
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

export function slugify(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export function mediaUrl(item: { id: number; title: string; year?: string; media_type: string; slug?: string }): string {
  const typeMap: Record<string, string> = {
    movie: 'movies',
    tv: 'tv',
    music: 'music',
    book: 'books',
  }
  const prefix = typeMap[item.media_type] || 'media'
  const s = item.slug || slugify(item.title) + (item.year ? '-' + item.year : '')
  return `/${prefix}/${s}`
}

export function personUrl(person: { id: number; name: string; slug?: string }): string {
  const s = person.slug || slugify(person.name)
  return `/person/${s}`
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
