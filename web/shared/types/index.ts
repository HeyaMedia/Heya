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
  slug: string
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

export interface MediaAsset {
  id: number
  media_item_id: number
  asset_type: string
  source: string
  local_path: string
  remote_url: string
  language: string
  label: string
  sort_order: number
  width: number
  height: number
  file_size: number
}

export interface MediaExtra {
  id: number
  media_item_id: number
  extra_type: string
  title: string
  file_path: string
  duration_ms: number
  file_size: number
}

export interface CastMember {
  id: number
  name: string
  character: string
  display_order: number
  gender: number
  profile_path: string
}

export interface CrewMember {
  id: number
  name: string
  job: string
  department: string
  profile_path: string
}

export interface Keyword {
  id: number
  tmdb_id: number
  name: string
}

export interface MediaVideo {
  id: number
  media_item_id: number
  name: string
  site: string
  video_key: string
  video_type: string
  language: string
  official: boolean
}

export interface MediaCertification {
  id: number
  media_item_id: number
  country: string
  certification: string
  release_date: string | null
  release_type: number
}

export interface MediaRecommendation {
  id: number
  media_item_id: number
  recommended_tmdb_id: number
  title: string
  poster_path: string
  media_type: string
  vote_average: string
  release_date: string
  local_media_item_id: number | null
  local_poster_path: string | null
}

export interface ProductionCompany {
  id: number
  tmdb_id: number
  name: string
  logo_path: string
  origin_country: string
}

export interface PersonDetail {
  id: number
  tmdb_id: number
  name: string
  also_known_as: string[]
  biography: string
  birthday: string
  deathday: string
  place_of_birth: string
  gender: number
  profile_path: string
  homepage: string
  imdb_id: string
}

export interface PersonCastCredit {
  character: string
  display_order: number
  media_item_id: number
  title: string
  year: string
  media_type: string
  poster_path: string
}

export interface PersonCrewCredit {
  job: string
  department: string
  media_item_id: number
  title: string
  year: string
  media_type: string
  poster_path: string
}

export interface PersonResponse {
  person: PersonDetail
  cast_credits?: PersonCastCredit[]
  crew_credits?: PersonCrewCredit[]
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
  cast?: CastMember[]
  crew?: CrewMember[]
  keywords?: Keyword[]
  videos?: MediaVideo[]
  certifications?: MediaCertification[]
  recommendations?: MediaRecommendation[]
  production_companies?: ProductionCompany[]
  assets?: MediaAsset[]
  extras?: MediaExtra[]
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
