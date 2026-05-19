export interface User {
  id: number
  username: string
  email: string
  is_admin: boolean
}

export interface AuthResponse {
  token: string
  user: User
}

export interface Library {
  id: number
  name: string
  media_type: MediaType
  paths: string[]
  created_by: number
}

export type MediaType = 'movie' | 'tv' | 'music' | 'book'

export interface MediaItem {
  id: number
  library_id: number
  media_type: MediaType
  title: string
  sort_title: string
  year: string
  description: string
  poster_path: string
  backdrop_path: string
  external_ids: Record<string, string>
  created_at: string
  updated_at: string
}

export interface Movie {
  id: number
  media_item_id: number
  tmdb_id: number | null
  imdb_id: string
  runtime_minutes: number
  tagline: string
  genres: string[]
  rating: string | null
  release_date: string | null
  original_title: string
  original_language: string
  budget: number
  revenue: number
}

export interface TVSeries {
  id: number
  media_item_id: number
  tmdb_id: number | null
  imdb_id: string
  genres: string[]
  rating: string | null
  first_air_date: string | null
  last_air_date: string | null
  status: string
  seasons_count: number
  episodes_count: number
}

export interface TVSeason {
  id: number
  tv_series_id: number
  season_number: number
  name: string
  overview: string
  episode_count: number
  air_date: string | null
  poster_path: string
}

export interface Artist {
  id: number
  media_item_id: number
  musicbrainz_id: string
  genres: string[]
}

export interface Album {
  id: number
  artist_id: number
  title: string
  release_date: string | null
  musicbrainz_id: string
  genres: string[]
  track_count: number
}

export interface Book {
  id: number
  media_item_id: number
  author_id: number | null
  isbn: string
  isbn13: string
  pages: number
  publisher: string
  publish_date: string
  genres: string[]
  subjects: string[]
  rating: string | null
  open_library_key: string
}

export interface MediaDetail {
  media_item: MediaItem
  movie?: Movie
  tv_series?: TVSeries
  seasons?: TVSeason[]
  artist?: Artist
  albums?: Album[]
  book?: Book
  author?: { id: number; name: string }
}

export interface LibraryFile {
  id: number
  library_id: number
  file_path: string
  file_size: number
  status: string
  parse_result: string
  media_item_id: number | null
}

export interface FileStats {
  status: string
  count: number
}

export interface MatchCandidate {
  id: number
  library_file_id: number
  provider_name: string
  provider_id: string
  title: string
  year: string
  confidence: number
  metadata: string
}

export interface UnmatchedFile {
  file: LibraryFile
  candidates: MatchCandidate[]
}

export interface HealthResponse {
  status: string
  database: string
  version: string
}
