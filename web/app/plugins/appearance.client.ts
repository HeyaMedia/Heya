// Boot-time appearance sync.
//
// The inline head script (nuxt.config.ts) already stamped the theme
// attributes from localStorage before first paint — this plugin only
// (re)applies through the composable so reactive state matches the DOM,
// then reconciles with the server copy once auth is up.
//
// NOTE: this plugin runs BEFORE plugins/auth.ts (alphabetical order), so it
// initializes the auth state first. hydrate() offers a pre-cookie legacy
// bearer once when present; otherwise it represents the same-origin cookie
// without exposing the credential to JavaScript.
import { withAuthHeaders } from '~/composables/useAuth'

export default defineNuxtPlugin(() => {
  const { apply, hydrateFromServer } = useAppearance()
  const { token, hydrate } = useAuth()

  apply()
  hydrate()

  if (!token.value) return
  // Fire-and-forget: a failure just means we ride the localStorage mirror.
  $fetch<{ appearance?: Record<string, unknown> }>('/api/me/settings', {
    headers: withAuthHeaders('/api/me/settings'),
  })
    .then((s) => hydrateFromServer(s.appearance))
    .catch(() => {})
})
