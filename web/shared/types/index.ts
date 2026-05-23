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

export interface LibrarySettings {
  watch: boolean
  preferred_language: string
  preferred_country: string
  auto_collections: boolean
  metadata_refresh_days: number
  fetch_ratings: boolean
  save_nfo: boolean
  save_images: boolean
  enable_trickplay: boolean
  generate_thumbnails: boolean
}

export interface Library {
  id: number
  name: string
  media_type: MediaType
  paths: string[]
  created_by: number
  settings: LibrarySettings
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
  tagline: string
  original_title: string
  original_language?: string
  status: string
  provider_kind: string
  heya_slug: string
  heya_enriched_at: string | null
  created_at: string
  updated_at: string
  available?: boolean
}

// Mirrors sqlc.Movie (pgtype.Numeric serializes to either number or string).
export interface Movie {
  id: number
  media_item_id: number
  runtime_minutes: number
  tagline: string
  genres: string[]
  rating: number | string | null
  release_date: string | null
  original_title: string
  original_language: string
  budget: number
  revenue: number
  popularity?: number | string | null
  collection_id?: number | null
  status?: string
  homepage?: string
  spoken_languages?: string[]
  origin_country?: string[]
}

// Mirrors sqlc.TvSeries.
export interface TVSeries {
  id: number
  media_item_id: number
  status: string
  genres: string[]
  rating: number | string | null
  first_air_date: string | null
  last_air_date: string | null
  original_name: string
  original_language: string
  number_of_seasons: number
  number_of_episodes: number
  popularity?: number | string | null
  spoken_languages?: string[]
  origin_country?: string[]
}

// Mirrors sqlc.TvSeason plus the optional `episodes` array the media-detail
// service layer wraps it in.
export interface TVSeason {
  id: number
  series_id: number
  season_number: number
  title: string
  overview: string
  poster_path: string
  air_date: string | null
  end_date?: string | null
  status?: string
  aired_episodes: number
  external_ids?: Record<string, string>
  episodes?: TVEpisode[]
}

export interface TVEpisode {
  id: number
  season_id: number
  episode_number: number
  title: string
  overview: string
  still_path: string
  runtime_minutes: number
  air_date: string | null
  rating: number | string | null
  absolute_number?: number
  is_special?: boolean
  episode_type?: number
  external_ids?: Record<string, string>
  source?: string
  preferred_title?: string
  preferred_overview?: string
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
  score: string
  likes: number
  aspect: string
}

export interface MediaExtra {
  id: number
  media_item_id: number
  extra_type: string
  title: string
  file_path: string
  duration_ms: number
  file_size: number
  thumbnail_path: string
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
  external_ids: Record<string, string>
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
  source: string
}

export interface MediaRecommendation {
  id: number
  media_item_id: number
  external_ids: Record<string, string>
  title: string
  poster_path: string
  media_type: string
  vote_average: number | string
  release_date: string
  local_media_item_id: number | null
  local_poster_path: string | null
}

export interface ProductionCompany {
  id: number
  external_ids: Record<string, string>
  name: string
  logo_path: string
  origin_country: string
}

export interface PersonDetail {
  id: number
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
  external_ids: Record<string, string>
  popularity: string
  slug: string
  sort_name: string
  known_for_department: string
  birth_year: number
  heya_slug: string
  heya_enriched_at: string | null
}

export interface PersonBiography {
  id: number
  person_id: number
  language: string
  biography: string
}

export interface PersonProfile {
  id: number
  person_id: number
  url: string
  source: string
  aspect: string
  width: number
  height: number
  score: string
  sort_order: number
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
  biographies?: PersonBiography[]
  profiles?: PersonProfile[]
}

export interface MediaFile {
  id: number
  size: number
}

// TranscodeReasonTag mirrors internal/server/stream_info_handlers.go's
// reasonStrings() output. Each tag explains one specific incompatibility
// between the source and the client.
export type TranscodeReasonTag =
  | 'container'
  | 'video_codec'
  | 'audio_codec'
  | 'bit_depth'
  | 'hdr'
  | 'audio_channels'
  | 'quality_override'
  | 'codec_tag'
  | 'rotation'
  | 'interlaced'
  | 'anamorphic'
  | 'lossless_audio'
  | 'dolby_vision'

export interface PlaybackDecision {
  action: 'direct_play' | 'remux' | 'transcode'
  profile: string
  reason: string
  reasons: TranscodeReasonTag[]
  reason_bits: number
  copy_video: boolean
  copy_audio: boolean
  needs_tonemap?: boolean
  needs_fmp4?: boolean

  // Surgical fixes applied on top of the action.
  strip_dovi_el?: boolean
  retag_hevc?: boolean
  deinterlace?: boolean
  rotate?: number
  fix_anamorphic?: boolean
  downmix_stereo?: boolean
}

export interface QualityOption {
  label: string
  height: number
}

export interface StreamInfoResponse {
  container: string
  duration: number
  size: number
  bit_rate: number
  library_id: number
  playback: PlaybackDecision
  video: StreamVideo[]
  audio: StreamAudio[]
  subtitle: StreamSubtitle[]
  qualities: QualityOption[] | null
}

export interface StreamVideo {
  index: number
  codec: string
  codec_long: string
  profile?: string
  width: number
  height: number
  pix_fmt?: string
  hdr: boolean
  color_transfer?: string
  color_primaries?: string
  color_space?: string
  bit_rate?: string
  is_default: boolean
}

export interface StreamAudio {
  index: number
  codec: string
  codec_long: string
  channels: number
  channel_layout?: string
  sample_rate?: string
  bit_rate?: string
  language: string
  title?: string
  is_default: boolean
}

export interface StreamSubtitle {
  index: number
  codec: string
  language: string
  title?: string
  is_default: boolean
  is_forced: boolean
  is_hearing_impaired: boolean
}

export interface EpisodeFileEntry {
  file_id: number
  size: number
}

export interface Collection {
  id: number
  name: string
  overview: string
  poster_path: string
  backdrop_path: string
}

export interface MediaDetail {
  media_item: MediaItem
  available: boolean
  files?: MediaFile[]
  episode_files?: Record<string, EpisodeFileEntry>
  movie?: Movie
  tv_series?: TVSeries
  seasons?: TVSeason[]
  artist?: Artist
  albums?: Album[]
  book?: Book
  author?: { id: number; name: string }
  collection?: Collection
  cast?: CastMember[]
  crew?: CrewMember[]
  keywords?: Keyword[]
  videos?: MediaVideo[]
  certifications?: MediaCertification[]
  recommendations?: MediaRecommendation[]
  production_companies?: ProductionCompany[]
  external_ratings?: ExternalRating[]
  assets?: MediaAsset[]
  extras?: MediaExtra[]
  titles?: MediaTitle[]
  overviews?: MediaOverview[]
  preferred_title?: string
  preferred_overview?: string
  preferred_certification?: string
}

export interface ExternalRating {
  id: number
  source: string
  value: string
  score: string
  votes: number
  raw_value: string
}

export interface MediaTitle {
  id: number
  media_item_id: number
  title: string
  language: string
  country: string
  title_type: string
  source: string
}

export interface MediaOverview {
  id: number
  media_item_id: number
  language: string
  overview: string
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

export interface EnrichedMediaItem extends MediaItem {
  genres: string[]
  rating: number | null
  runtime_minutes?: number
  resolution?: string
  release_date?: string
  collection_id?: number
  first_air_date?: string
  last_air_date?: string
  number_of_seasons?: number
  number_of_episodes?: number
}

export interface FilterState {
  genres: string[]
  yearMin: number | null
  yearMax: number | null
  ratingMin: number | null
  ratingMax: number | null
  resolutions: string[]
  watched: 'all' | 'watched' | 'unwatched'
  studioIds: number[]
  studioNames: string[]
  personIds: number[]
  personNames: string[]
  language: string | null
}

export interface UserList {
  id: number
  user_id: number
  name: string
  description: string
  list_type: 'manual' | 'smart'
  filter_json: FilterState | null
  media_type: string
  icon: string
  item_count: number
  created_at: string
  updated_at: string
}

export interface CollectionBrowse {
  id: number
  name: string
  poster_path: string
  movie_count: number
}

export interface PlaybackPreference {
  media_item_id: number
  audio_language: string
  subtitle_language: string
  subtitle_mode: string
}

export interface LanguageInfo {
  code: string
  count: number
}

export interface MediaLanguagesResponse {
  audio_languages: LanguageInfo[]
  subtitle_languages: LanguageInfo[]
}

export interface ContextMenuItem {
  label: string
  icon?: string
  action?: () => void
  separator?: boolean
  submenu?: ContextMenuItem[]
  disabled?: boolean
}

export interface UpdateMediaMetadataRequest {
  title?: string
  sort_title?: string
  year?: string
  description?: string
  external_ids?: Record<string, string>
  tagline?: string
  genres?: string[]
  release_date?: string
  original_title?: string
  original_language?: string
  runtime_minutes?: number
  status?: string
  first_air_date?: string
  last_air_date?: string
  networks?: string[]
  original_name?: string
}

export interface ProviderSearchResult {
  provider_id: string
  provider_name: string
  title: string
  year: string
  description: string
  poster_url: string
}

export interface ArtworkSearchResult {
  url: string
  source: string
  asset_type: string
  language: string
  width?: number
  height?: number
}
