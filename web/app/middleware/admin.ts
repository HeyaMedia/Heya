// Route-level gate for admin-only pages. Pages opt in with:
//   definePageMeta({ middleware: 'admin' })
// Non-admins land on the personal profile page instead of seeing a blank
// admin-gated panel. Server endpoints are still admin-gated on their own.
//
// plugins/auth.ts initializes cookie auth synchronously and then fetches the user
// payload asynchronously. Skip the gate while user.value hasn't resolved yet
// (token present, user still null) so an admin doesn't get bounced to
// /profile on first visit before /api/auth/me has come back.
export default defineNuxtRouteMiddleware(() => {
  const { user, token, ready } = useAuth()
  if (!ready.value) return
  if (token.value && !user.value) return
  if (!user.value?.is_admin) return navigateTo('/settings/profile')
})
