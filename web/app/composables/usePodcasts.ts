// usePodcasts: shared helpers for the podcast surface. Wraps episode
// playback (proxy URL + auth token), subscription state, and progress
// reporting so individual pages don't re-implement the plumbing.

import type { Track } from '~/composables/usePlayer'

export interface Podcast {
  id: number
  title: string
  author: string
  description: string
  artwork_url: string
  feed_url: string
  categories: Record<string, string>
  language: string
  episode_count: number
}

export interface PodcastEpisode {
  guid: string
  title: string
  description: string
  pub_date: string
  duration_secs: number
  audio_url: string
  audio_type: string
  audio_size: number
  episode_number?: number | null
  season_number?: number | null
  artwork_url?: string | null
}

export interface PodcastDetail {
  feed_url: string
  title: string
  author: string
  description: string
  artwork_url: string
  link: string
  language: string
  categories: string[]
  episodes: PodcastEpisode[]
}

// episodeToTrack — wrap an episode in the Track shape, routing the audio
// through Heya's proxy so CORS / auth are taken care of.
export function episodeToTrack(podcast: PodcastDetail, episode: PodcastEpisode): Track {
  const { token } = useAuth()
  const params = new URLSearchParams({ url: episode.audio_url })
  if (token.value) params.set('token', token.value)
  return {
    // Negative id space mirrors the radio-station hack so episodes don't
    // collide with music track ids. A consistent hash keeps Now Playing
    // stable across pages.
    id: -episodeHash(podcast.feed_url, episode.guid),
    title: episode.title,
    artist: podcast.author || podcast.title,
    album: podcast.title,
    duration: episode.duration_secs || 0,
    stream_url: `/api/podcasts/episode/stream?${params.toString()}`,
    poster: episode.artwork_url || podcast.artwork_url || undefined,
    source: 'podcast',
  }
}

function episodeHash(feedURL: string, guid: string): number {
  let hash = 5381
  const s = feedURL + '::' + guid
  for (let i = 0; i < s.length; i++) {
    hash = ((hash << 5) + hash) ^ s.charCodeAt(i)
  }
  return Math.abs(hash) % 1_000_000_000
}

// usePodcastActions — subscription state + episode playback. The
// subscribed-set is cached app-wide so the toggle is instant across pages.
export function usePodcastActions() {
  const { play, queue } = usePlayer()
  const subscribedSet = useState<Set<string>>('podcast_subs', () => new Set())
  const subscribedLoaded = useState('podcast_subs_loaded', () => false)

  async function ensureSubscriptionsLoaded() {
    if (subscribedLoaded.value) return
    subscribedLoaded.value = true
    try {
      const { $heya } = useNuxtApp()
      const res = await $heya('/api/me/podcasts/subscriptions') as { items: Array<{ feed_url: string }> }
      subscribedSet.value = new Set((res.items ?? []).map((s) => s.feed_url))
    } catch { /* stay empty */ }
  }

  function isSubscribed(feedURL: string) { return subscribedSet.value.has(feedURL) }

  async function subscribe(p: { feed_url: string; title: string; author: string; artwork_url: string }) {
    const { $heya } = useNuxtApp()
    try {
      await $heya('/api/me/podcasts/subscriptions', { method: 'POST', body: {
        feed_url: p.feed_url, title: p.title, author: p.author, artwork_url: p.artwork_url,
      } })
      subscribedSet.value.add(p.feed_url)
      subscribedSet.value = new Set(subscribedSet.value)
    } catch (e) { console.warn('subscribe failed:', e) }
  }

  async function unsubscribe(feedURL: string) {
    const { $heya } = useNuxtApp()
    try {
      await $heya('/api/me/podcasts/subscriptions', { method: 'DELETE', query: { url: feedURL } })
      subscribedSet.value.delete(feedURL)
      subscribedSet.value = new Set(subscribedSet.value)
    } catch (e) { console.warn('unsubscribe failed:', e) }
  }

  // playEpisode loads one episode as the queue. We could append future
  // episodes to extend the queue, but most listeners go episode-by-episode
  // so a single-item queue is cleaner and avoids surprise autoplay.
  async function playEpisode(podcast: PodcastDetail, episode: PodcastEpisode) {
    const track = episodeToTrack(podcast, episode)
    queue.value = [track]
    await play(track)
  }

  return { ensureSubscriptionsLoaded, isSubscribed, subscribe, unsubscribe, playEpisode, subscribedSet }
}
