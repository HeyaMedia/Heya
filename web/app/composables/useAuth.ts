// Bearer-token auth backed by the typed `$heya` openFetch client.
//
// Why login/register call $fetch directly instead of $heya: those endpoints run
// *before* a token exists, but plugins/heyaApi.client.ts still attaches the
// `Authorization` header on every $heya call. That's harmless for /login &
// /register (header is empty), but the openFetch path also runs the global 401
// handler — and a wrong-password login is a 401 we want to surface, not a
// forced logout. Easier to keep these two on plain $fetch.
import type { User, AuthResponse } from '~~/shared/types'

const TOKEN_KEY = 'heya_token'

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
  }

  async function register(username: string, email: string, password: string) {
    const data = await $fetch<AuthResponse>('/api/auth/register', {
      method: 'POST',
      body: { username, email, password },
    })
    token.value = data.token
    user.value = data.user
    localStorage.setItem(TOKEN_KEY, data.token)
  }

  async function fetchUser() {
    if (!token.value) return
    try {
      const { $heya } = useNuxtApp()
      user.value = await $heya('/api/auth/me')
    } catch {
      logout()
    }
  }

  function logout() {
    if (token.value) {
      const { $heya } = useNuxtApp()
      $heya('/api/auth/logout', { method: 'POST' }).catch(() => {})
    }
    token.value = null
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    navigateTo('/login')
  }

  return { user, token, isAuthenticated, ready, hydrate, login, register, fetchUser, logout }
}
