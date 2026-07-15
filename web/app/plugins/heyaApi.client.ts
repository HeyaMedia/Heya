import type { FetchOptions } from 'ofetch'

type HeyaPathValue = string | number | boolean

export type HeyaRequestOptions = FetchOptions & {
  /** Values substituted into `{name}` placeholders before the request. */
  path?: Record<string, HeyaPathValue>
}

export type HeyaClient = <T = any>(
  url: `/api/${string}`,
  options?: HeyaRequestOptions,
) => Promise<T>

export function resolveHeyaPath(url: string, values: Record<string, HeyaPathValue> = {}): string {
  return url.replace(/\{([^}]+)\}/g, (_placeholder, name: string) => {
    const value = values[name]
    if (value === undefined) {
      throw new Error(`Missing path parameter: ${name}`)
    }
    return encodeURIComponent(String(value))
  })
}

// Nuxt-native API transport. Pinia Colada remains responsible for query keys,
// caching and persistence; this plugin only owns HTTP concerns shared by every
// query/mutation: path expansion, client version, untrusted surface metadata,
// bearer auth, and 401 logout.
// `.client.ts` is intentional because auth state lives in localStorage and the
// application is an SPA (`ssr: false`).
export default defineNuxtPlugin({
  name: 'heya:api',
  setup(nuxtApp) {
    const apiFetch = $fetch.create({
      onRequest({ request, options }) {
        const { token } = useAuth()
        const headers = withClientSurfaceHeaders(request, options.headers)
        headers.set('X-Heya-Client-Version', nuxtApp.$config.public.heyaVersion)
        if (token.value) {
          headers.set('Authorization', `Bearer ${token.value}`)
        }
        options.headers = headers
      },
      onResponseError({ response }) {
        if (response.status === 401) {
          useAuth().logout()
        }
      },
    })

    const heya: HeyaClient = (url, options = {}) => {
      const { path, ...fetchOptions } = options
      // ofetch's public options allow arbitrary HTTP verbs, while Nitro's
      // generated $fetch overload narrows them per route. `$heya` deliberately
      // accepts the public transport type because it serves every API route.
      return apiFetch(resolveHeyaPath(url, path), fetchOptions as any)
    }

    return {
      provide: { heya },
    }
  },
})
