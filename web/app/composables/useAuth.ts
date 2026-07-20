// Browser/Tauri auth is backed by a same-origin HttpOnly cookie. The sentinel
// below keeps existing reactive consumers working without exposing the real
// credential to JavaScript; raw API clients still receive bearer tokens.
import type { User, AuthResponse } from '~~/shared/types'
import { useQueryCache } from '@pinia/colada'
import { clearPersistedQueryCache } from '~/utils/queryPersistence.client'

const TOKEN_KEY = 'heya_token'
const USER_ID_KEY = 'heya_user_id'
const COOKIE_SESSION = '__heya_http_only_cookie__'

const _ready = ref(false)

export function isBearerAuthToken(value: string | null | undefined): value is string {
  return !!value && value !== COOKIE_SESSION
}

export function withAuthHeaders(target: RequestInfo | URL, headers?: HeadersInit): Headers {
  const merged = withClientSurfaceHeaders(target, headers)
  const token = useAuth().token.value
  if (isBearerAuthToken(token)) merged.set('Authorization', `Bearer ${token}`)
  else merged.delete('Authorization')
  return merged
}

export function useAuth() {
  const user = useState<User | null>('auth_user', () => null)
  const token = useState<string | null>('auth_token', () => null)
  const ready = _ready
  const isAuthenticated = computed(() => token.value !== null)

  // A pre-cookie browser may still have the old localStorage bearer. Keep it
  // only long enough for /api/auth/me to exchange it for an HttpOnly cookie.
  function hydrate() {
    if (!import.meta.client || token.value) return
    const legacy = localStorage.getItem(TOKEN_KEY)
    token.value = legacy || COOKIE_SESSION
  }

  async function login(username: string, password: string) {
    const data = await $fetch<AuthResponse>('/api/auth/login', {
      method: 'POST',
      body: { username, password },
      headers: withClientSurfaceHeaders('/api/auth/login'),
    })
    token.value = COOKIE_SESSION
    user.value = data.user
    localStorage.removeItem(TOKEN_KEY)
    localStorage.setItem(USER_ID_KEY, String(data.user.id))
  }

  async function register(username: string, email: string, password: string) {
    const data = await $fetch<AuthResponse>('/api/auth/register', {
      method: 'POST',
      body: { username, email, password },
      headers: withClientSurfaceHeaders('/api/auth/register'),
    })
    token.value = COOKIE_SESSION
    user.value = data.user
    localStorage.removeItem(TOKEN_KEY)
    localStorage.setItem(USER_ID_KEY, String(data.user.id))
  }

  async function fetchUser() {
    hydrate()
    try {
      const current = await $fetch<User>('/api/auth/me', {
        headers: withAuthHeaders('/api/auth/me'),
      })
      user.value = current
      token.value = COOKIE_SESSION
      localStorage.removeItem(TOKEN_KEY)
      localStorage.setItem(USER_ID_KEY, String(current.id))
    } catch (error: any) {
      if (error?.response?.status === 401 || error?.statusCode === 401) {
        token.value = null
        user.value = null
        localStorage.removeItem(TOKEN_KEY)
      }
    } finally {
      _ready.value = true
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
    const queryCache = useQueryCache(nuxtApp.$pinia)
    queryCache.getEntries().forEach(entry => queryCache.remove(entry))
    if (persistedUserId) void clearPersistedQueryCache(persistedUserId)
    navigateTo('/login')
  }

  return { user, token, isAuthenticated, ready, hydrate, login, register, fetchUser, logout }
}
