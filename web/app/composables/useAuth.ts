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
      const data = await $fetch<User>('/api/auth/me', {
        headers: { Authorization: `Bearer ${token.value}` },
      })
      user.value = data
    } catch {
      logout()
    }
  }

  function logout() {
    if (token.value) {
      $fetch('/api/auth/logout', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token.value}` },
      }).catch(() => {})
    }
    token.value = null
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    navigateTo('/login')
  }

  return { user, token, isAuthenticated, ready, hydrate, login, register, fetchUser, logout }
}
