// Boot-time appearance sync.
//
// The inline head script (nuxt.config.ts) already stamped the theme
// attributes from localStorage before first paint — this plugin only
// (re)applies through the composable so reactive state matches the DOM,
// then reconciles with the server copy once auth is up. Runs after
// plugins/auth.ts (alphabetical), so the token is already hydrated.
export default defineNuxtPlugin(() => {
  const { apply, hydrateFromServer } = useAppearance()
  const { token } = useAuth()

  apply()

  if (!token.value) return
  // Fire-and-forget: a failure just means we ride the localStorage mirror.
  $fetch<{ appearance?: Record<string, never> }>('/api/me/settings', {
    headers: { Authorization: `Bearer ${token.value}` },
  })
    .then((s) => hydrateFromServer(s.appearance))
    .catch(() => {})
})
