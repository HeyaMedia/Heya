// Wires the nuxt-open-fetch `heya` client into our auth flow:
// - injects `Authorization: Bearer <token>` from useAuth() on every request
// - logs the user out on 401 (mirrors the old apiFetch / useApiClient behaviour)
//
// `.client.ts` because the bearer token lives in localStorage and the whole app
// is SPA mode (`ssr: false`), so there's no server-side context that needs this.
export default defineNuxtPlugin((nuxtApp) => {
  nuxtApp.hook('openFetch:onRequest:heya', (ctx) => {
    const { token } = useAuth()
    ctx.options.headers.set('X-Heya-Client-Version', nuxtApp.$config.public.heyaVersion)
    if (token.value) {
      ctx.options.headers.set('Authorization', `Bearer ${token.value}`)
    }
  })

  nuxtApp.hook('openFetch:onResponseError:heya', (ctx) => {
    if (ctx.response.status === 401) {
      useAuth().logout()
    }
  })
})
