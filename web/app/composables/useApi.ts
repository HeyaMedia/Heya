import type { UseFetchOptions } from 'nuxt/app'

export function useApi<T>(url: string | (() => string), opts: UseFetchOptions<T> = {}) {
  const { token } = useAuth()

  return useFetch(url, {
    ...opts,
    headers: {
      ...opts.headers as Record<string, string>,
      ...(token.value ? { Authorization: `Bearer ${token.value}` } : {}),
    },
    onResponseError({ response }) {
      if (response.status === 401) {
        const { logout } = useAuth()
        logout()
      }
    },
  })
}

export function apiFetch<T>(url: string, opts: RequestInit = {}): Promise<T> {
  const { token } = useAuth()

  return $fetch<T>(url, {
    ...opts,
    headers: {
      ...opts.headers as Record<string, string>,
      ...(token.value ? { Authorization: `Bearer ${token.value}` } : {}),
    },
  })
}
