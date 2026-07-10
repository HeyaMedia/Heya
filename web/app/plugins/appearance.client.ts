// Boot-time appearance sync.
//
// The inline head script (nuxt.config.ts) already stamped the theme
// attributes from localStorage before first paint — this plugin only
// (re)applies through the composable so reactive state matches the DOM,
// then reconciles with the server copy once auth is up.
//
// NOTE: this plugin runs BEFORE plugins/auth.ts (alphabetical order), so
// the token isn't hydrated yet — hydrate() it ourselves (idempotent; it
// just lifts localStorage into the shared useState) or the server fetch
// below would silently never run and appearance would stop syncing
// across devices.
export default defineNuxtPlugin(() => {
  const { apply, hydrateFromServer } = useAppearance()
  const { token, hydrate } = useAuth()

  apply()
  hydrate()

  if (!token.value) return
  // Fire-and-forget: a failure just means we ride the localStorage mirror.
  $fetch<{ appearance?: Record<string, never> }>('/api/me/settings', {
    headers: { Authorization: `Bearer ${token.value}` },
  })
    .then((s) => hydrateFromServer(s.appearance))
    .catch(() => {})
})
