export default defineNuxtRouteMiddleware((to) => {
  const { isAuthenticated, ready } = useAuth()

  if (!ready.value) return

  const publicRoutes = ['/login']

  if (!isAuthenticated.value && !publicRoutes.includes(to.path)) {
    return navigateTo('/login')
  }

  if (isAuthenticated.value && to.path === '/login') {
    return navigateTo('/')
  }
})
