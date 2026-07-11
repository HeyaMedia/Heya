// Boot-time auth hydration.
//
// Plugins run client-side before any component mounts. We:
//   1. Read the stashed token out of localStorage into the useState ref
//      so the rest of the app sees `isAuthenticated` correctly on first
//      render (otherwise the topbar / route guards would flicker as
//      "logged out" for a frame).
//   2. If a token was found, fetch the current user payload — this gives
//      us username/email/is_admin for UI without needing to wait for the
//      first lazy API call.
//
// The `fetchUser()` call is intentionally tolerant: a transient error
// (backend restarting, network hiccup) leaves the token in place rather
// than booting the user. Only the openFetch `onResponseError:heya` hook
// (plugins/heyaApi.client.ts) escalates a real 401 to a logout.
export default defineNuxtPlugin({
  name: 'heya:auth',
  async setup() {
    const { hydrate, token, fetchUser } = useAuth()
    hydrate()
    if (token.value) await fetchUser()
  },
})
