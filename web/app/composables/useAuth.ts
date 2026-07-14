// Bearer-token auth backed by the Nuxt-injected `$heya` client.
//
// Why login/register call $fetch directly instead of $heya: those endpoints run
// *before* a token exists, but plugins/heyaApi.client.ts still attaches the
// `Authorization` header on every $heya call. That's harmless for /login &
// /register (header is empty), but the shared client also runs the global 401
// handler — and a wrong-password login is a 401 we want to surface, not a
// forced logout. Easier to keep these two on plain $fetch.
import type { User, AuthResponse } from '~~/shared/types'
import { useQueryCache } from '@pinia/colada'
import { clearPersistedQueryCache } from '~/utils/queryPersistence.client'

const TOKEN_KEY = 'heya_token'
const USER_ID_KEY = 'heya_user_id'

const _ready = ref(false)

export function useAuth() {
  const user = useState<User | null>('auth_user', () => null)
  const token = useState<string | null>('auth_token', () => null)
  const ready = _ready

  const isAuthenticated = computed(() => !!token.value)

  function hydrate() {
    if (import.meta.client && !_ready.value) {
      const stored = localStorage.getItem(TOKEN_KEY)
      if (stored) token.value = stored
      _ready.value = true
    }
  }

  async function login(username: string, password: string) {
    const data = await $fetch<AuthResponse>('/api/auth/login', {
      method: 'POST',
      body: { username, password },
    })
    token.value = data.token
    user.value = data.user
    localStorage.setItem(TOKEN_KEY, data.token)
    localStorage.setItem(USER_ID_KEY, String(data.user.id))
  }

  async function register(username: string, email: string, password: string) {
    const data = await $fetch<AuthResponse>('/api/auth/register', {
      method: 'POST',
      body: { username, email, password },
    })
    token.value = data.token
    user.value = data.user
    localStorage.setItem(TOKEN_KEY, data.token)
    localStorage.setItem(USER_ID_KEY, String(data.user.id))
  }

  async function fetchUser() {
    if (!token.value) return
    try {
      // Use raw $fetch so this works during plugin boot — the shared client
      // is registered in plugins/heyaApi.client.ts which
      // runs *after* plugins/auth.ts alphabetically, so a $heya() call here
      // would race and ship without an Authorization header, get 401, and
      // be silently swallowed below. login() and register() take the same
      // shortcut for the same reason.
      const current = await $fetch<User>('/api/auth/me', {
        headers: { Authorization: `Bearer ${token.value}` },
      })
      user.value = current
      localStorage.setItem(USER_ID_KEY, String(current.id))
    } catch {
      // Intentionally silent. Logout-on-error here was too aggressive
      // and booted the user out for any transient blip (backend bouncing
      // during dev, network hiccups, gateway timeouts). The `$heya`
      // response interceptor (plugins/heyaApi.client.ts) already
      // calls logout() on a genuine 401 — that's the only signal that
      // means "your token is invalid, please log back in". For everything
      // else, the next successful call will fill `user` and the user
      // keeps using the app uninterrupted.
    }
  }

  function logout() {
    const nuxtApp = useNuxtApp()
    const persistedUserId = user.value?.id ?? localStorage.getItem(USER_ID_KEY)
    if (token.value) {
      nuxtApp.$heya('/api/auth/logout', { method: 'POST' }).catch(() => {})
    }
    token.value = null
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_ID_KEY)
    // Query data is user-scoped. Remove it rather than merely invalidating it
    // so another account signing in within the same SPA session can never see
    // the previous user's warm cache (also required before disk persistence).
    const queryCache = useQueryCache(nuxtApp.$pinia)
    queryCache.getEntries().forEach(entry => queryCache.remove(entry))
    if (persistedUserId) void clearPersistedQueryCache(persistedUserId)
    navigateTo('/login')
  }

  return { user, token, isAuthenticated, ready, hydrate, login, register, fetchUser, logout }
}
