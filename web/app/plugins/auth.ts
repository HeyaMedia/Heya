// Boot-time auth hydration.
//
// Plugins run client-side before any component mounts. We:
//   1. Offer any legacy localStorage bearer to /api/auth/me once so the server
//      can migrate it into an HttpOnly cookie.
//   2. Resolve the cookie-backed current user before route middleware renders.
//
// The `fetchUser()` call is intentionally tolerant: a transient error
// (backend restarting, network hiccup) leaves the token in place rather
// than booting the user. Only the `$heya` response interceptor in
// plugins/heyaApi.client.ts escalates a real 401 to a logout.
export default defineNuxtPlugin({
  name: 'heya:auth',
  async setup() {
    const { hydrate, fetchUser } = useAuth()
    hydrate()
    await fetchUser()
  },
})
