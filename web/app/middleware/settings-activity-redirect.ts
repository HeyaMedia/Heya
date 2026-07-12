// /settings/activity used to be a standalone Now Playing page. Its component
// now renders inside Dashboard; keep old bookmarks working without exposing a
// second navigation destination.
export default defineNuxtRouteMiddleware((to) => {
  if (to.path === '/settings/activity') {
    return navigateTo('/settings/dashboard', { replace: true })
  }
})
