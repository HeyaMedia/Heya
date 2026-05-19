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
        <div v-if="isRegister" class="field">
          <label>Email</label>
          <input v-model="email" type="email" placeholder="you@example.com" required />
        </div>
        <div class="field">
          <label>Username</label>
          <input v-model="username" type="text" placeholder="username" required />
        </div>
        <div class="field">
          <label>Password</label>
          <input v-model="password" type="password" placeholder="••••••••" required />
        </div>

        <div v-if="error" class="error-msg">{{ error }}</div>

        <button type="submit" class="btn btn-primary" style="width: 100%; margin-top: 8px" :disabled="loading">
          {{ loading ? 'Please wait…' : (isRegister ? 'Create Account' : 'Sign In') }}
        </button>

        <button type="button" class="toggle-btn" @click="isRegister = !isRegister">
          {{ isRegister ? 'Already have an account? Sign in' : 'Need an account? Register' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: 'auth' })

const { login, register, isAuthenticated } = useAuth()

const username = ref('')
const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)
const isRegister = ref(false)

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
    error.value = e?.data?.error || e?.message || 'Something went wrong'
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
.field { margin-bottom: 16px; }
.field label {
  display: block;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-2);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  margin-bottom: 6px;
}
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
  background: rgba(217, 107, 107, 0.1);
  border: 1px solid rgba(217, 107, 107, 0.3);
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
