// Runs before auth hydration and route middleware so `?heya_client=1` cannot
// be lost when an unauthenticated launch is redirected to /login.
export default defineNuxtPlugin({
  name: 'heya:client-surface',
  enforce: 'pre',
  setup() {
    captureClientSurfaceMarker()
  },
})
