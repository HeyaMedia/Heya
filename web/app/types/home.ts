export interface ContinueWatchingItem {
  id: number
  entity_type: string
  entity_id: number
  progress_seconds: number
  total_seconds: number
  media_item_id: number
  media_item_public_id?: string
  title: string
  poster_path: string
  slug: string
  media_type: string
  episode_number?: number
  episode_title?: string
  season_number?: number
  // Enriched by the backend so the frontend can navigate to /watch without a
  // second lookup. Zero means the file no longer resolves.
  file_id: number
  file_public_id?: string
}

export interface UpNextItem {
  id: number
  title: string
  slug: string
  season_number: number
  episode_number: number
  episode_label: string
  play_file_id: number
  play_file_public_id?: string
  // Episode identity lets the watch route render and persist episode-specific
  // activity instead of attributing it only to the parent series.
  episode_id?: number
  runtime_minutes?: number
  public_id?: string
  media_item_public_id?: string
}
