export interface ShowState {
  media_item_id: number
  total_episodes: number
  watched_episodes: number
}

export interface SeasonState {
  season_id: number
  total_episodes: number
  watched_episodes: number
}

export interface EpisodeProgress {
  episode_id: number
  progress_seconds: number
  total_seconds: number
  completed: boolean
}

export interface UserStateMovies {
  favorited: number[]
  watched: number[]
}

export interface UserStateSeries {
  shows: ShowState[]
  favorited: number[]
}

export interface UserStateSeasons {
  seasons: SeasonState[]
  favorited_media: number[]
  favorited_seasons: number[]
}

export interface UserStateEpisodes {
  seasons: SeasonState[]
  watched_episode_ids: number[]
  episode_progress: EpisodeProgress[]
  favorited_media: number[]
  favorited_seasons: number[]
}

export async function fetchUserState(scope: 'movies'): Promise<UserStateMovies>
export async function fetchUserState(scope: 'series'): Promise<UserStateSeries>
export async function fetchUserState(scope: 'seasons', seriesId: number): Promise<UserStateSeasons>
export async function fetchUserState(scope: 'episodes', seriesId: number): Promise<UserStateEpisodes>
export async function fetchUserState(scope: string, seriesId?: number): Promise<any> {
  return apiFetch('/api/user/state', {
    method: 'POST',
    body: JSON.stringify({ scope, series_id: seriesId || 0 }),
  })
}
