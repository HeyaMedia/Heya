<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-brand">
        <svg width="36" height="36" viewBox="0 0 22 22">
          <circle cx="11" cy="11" r="10" fill="none" stroke="var(--gold)" stroke-width="1.5" />
          <circle cx="11" cy="11" r="4" fill="var(--gold)" />
          <circle cx="11" cy="11" r="1.5" fill="var(--bg-1)" />
        </svg>
        <span class="brand-name">heya<span style="color: var(--gold)">.</span>media</span>
      </div>
      <p class="login-sub">Sign in to your media server</p>

      <form @submit.prevent="submit">
        <div v-if="isTauriClient" class="server-field">
          <div class="server-label">Server</div>
          <div class="server-row">
            <span class="server-origin" :title="serverOrigin">{{ serverOrigin }}</span>
            <a class="change-server" :href="TAURI_SWITCH_SERVER_URI">Change</a>
          </div>
        </div>
        <div v-if="isRegister" class="field">
          <label for="login-email">Email</label>
          <input id="login-email" v-model="email" type="email" placeholder="you@example.com" autocomplete="email" required />
        </div>
        <div class="field">
          <label for="login-username">Username</label>
          <input id="login-username" v-model="username" type="text" placeholder="username" autocomplete="username" required />
        </div>
        <div class="field">
          <label for="login-password">Password</label>
          <input id="login-password" v-model="password" type="password" placeholder="••••••••" :autocomplete="isRegister ? 'new-password' : 'current-password'" :minlength="isRegister ? 15 : 1" maxlength="256" required />
        </div>

        <div v-if="error" class="error-msg" role="alert">{{ error }}</div>

        <button type="submit" class="btn btn-primary" style="width: 100%; margin-top: 8px" :disabled="loading">
          {{ loading ? 'Please wait…' : (isRegister ? 'Create Account' : 'Sign In') }}
        </button>

        <button v-if="registrationEnabled" type="button" class="toggle-btn" @click="isRegister = !isRegister">
          {{ isRegister ? 'Already have an account? Sign in' : 'Need an account? Register' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: 'auth' })

const { login, register, isAuthenticated } = useAuth()
const { isTauriClient } = useClientSurface()

const username = ref('')
const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)
const isRegister = ref(false)
const registrationEnabled = ref(false)
const serverOrigin = import.meta.client ? window.location.origin : ''

onMounted(async () => {
  try {
    const status = await $fetch<{ enabled: boolean }>('/api/auth/registration', {
      headers: withClientSurfaceHeaders('/api/auth/registration'),
    })
    registrationEnabled.value = status.enabled
  } catch {
    // Closed is the safe fallback when setup state cannot be determined.
    registrationEnabled.value = false
    isRegister.value = false
  }
})

async function submit() {
  error.value = ''
  loading.value = true
  try {
    if (isRegister.value) {
      await register(username.value, email.value, password.value)
    } else {
      await login(username.value, password.value)
    }
    navigateTo('/')
  } catch (e: any) {
    // Backend errors are huma ErrorModel: { title, status, detail }.
    error.value = e?.statusCode === 401
      ? 'Invalid username or password'
      : e?.data?.detail || e?.message || 'Something went wrong'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--bg-0);
}
.login-card {
  width: 100%;
  max-width: 380px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: 40px;
}
.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 4px;
}
.brand-name { font-size: 20px; font-weight: 600; }
.login-sub { font-size: 13px; color: var(--fg-2); margin: 0 0 28px; }
.field, .server-field { margin-bottom: 16px; }
.field label, .server-label {
  display: block;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-2);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  margin-bottom: 6px;
}
.server-row {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  height: 40px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 12px 0 14px;
  font-size: 13px;
}
.server-origin {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  color: var(--fg-1);
  font-family: var(--font-mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.change-server {
  flex: none;
  color: var(--fg-2);
  font-size: 12px;
  text-decoration: none;
}
.change-server:hover, .change-server:focus-visible { color: var(--gold); }
.field input {
  width: 100%;
  height: 40px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 14px;
  color: var(--fg-0);
  font-size: 14px;
  outline: none;
  transition: border-color 0.15s ease;
}
.field input:focus { border-color: var(--gold); }
.field input::placeholder { color: var(--fg-3); }
.error-msg {
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
  border-radius: var(--r-md);
  padding: 10px 14px;
  font-size: 13px;
  color: var(--bad);
  margin-bottom: 12px;
}
.toggle-btn {
  display: block;
  width: 100%;
  text-align: center;
  margin-top: 12px;
  font-size: 12px;
  color: var(--fg-2);
  cursor: pointer;
}
.toggle-btn:hover { color: var(--gold); }
</style>
