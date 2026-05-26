import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'

// Single app-wide QueryClient — every useQuery / useMutation call in the
// app reads/writes this cache. Living in a plugin (not a per-component ref)
// is what gives us survive-the-remount behavior: navigating from /music →
// /music/artist/x and back doesn't refetch any queries whose cache hasn't
// expired.
//
// `.client.ts` because the app is CSR-only (ssr: false in nuxt.config.ts);
// there's no SSR hydration phase to worry about, so no payload sharing
// between server and client is needed.
//
// Defaults explained:
//   staleTime: 60s — short enough that obviously-fresh data refetches
//     in the background when you come back, long enough that rapid
//     navigation reuses cache.
//   gcTime: 30min — how long unused query data stays in memory after
//     its last subscriber unmounted. Generous because RAM is cheap and
//     coming back to a screen we visited 10 minutes ago should be instant.
//   refetchOnWindowFocus: true — Spotify-style "always live when you
//     come back to the tab". Cheap because the staleTime gate still
//     applies; only stale queries actually fire.
//   retry: 1 — we have our own 401 logout flow, retrying the same
//     failing call once is enough to absorb transient network blips
//     without spamming.
export default defineNuxtPlugin((nuxtApp) => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 1000 * 60,
        gcTime: 1000 * 60 * 30,
        refetchOnWindowFocus: true,
        retry: 1,
      },
    },
  })

  nuxtApp.vueApp.use(VueQueryPlugin, { queryClient })

  // Expose the client via Nuxt's injection system so plays/mutations
  // outside the component tree (composables, services) can invalidate
  // queries by key.
  return {
    provide: {
      queryClient,
    },
  }
})

// Type augmentation so `useNuxtApp().$queryClient` is typed correctly.
declare module '#app' {
  interface NuxtApp {
    $queryClient: QueryClient
  }
}
