<template>
  <div class="w-full max-w-sm">
    <div class="mb-8 text-center">
      <div class="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-xl bg-heya-primary text-2xl font-bold text-white">
        H
      </div>
      <h1 class="text-2xl font-semibold">Welcome to Heya</h1>
      <p class="mt-1 text-sm text-gray-500">Sign in to your media server</p>
    </div>

    <form class="card space-y-4 p-6" @submit.prevent="submit">
      <div v-if="isRegister">
        <label class="mb-1 block text-xs font-medium text-gray-400">Email</label>
        <input v-model="email" type="email" class="input" placeholder="you@example.com" required />
      </div>
      <div>
        <label class="mb-1 block text-xs font-medium text-gray-400">Username</label>
        <input v-model="username" type="text" class="input" placeholder="username" required />
      </div>
      <div>
        <label class="mb-1 block text-xs font-medium text-gray-400">Password</label>
        <input v-model="password" type="password" class="input" placeholder="&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;" required />
      </div>

      <div v-if="error" class="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-400">
        {{ error }}
      </div>

      <button type="submit" class="btn-primary w-full" :disabled="loading">
        {{ loading ? 'Please wait...' : (isRegister ? 'Create Account' : 'Sign In') }}
      </button>

      <button type="button" class="btn-ghost w-full text-xs" @click="isRegister = !isRegister">
        {{ isRegister ? 'Already have an account? Sign in' : 'Need an account? Register' }}
      </button>
    </form>
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

watchEffect(() => {
  if (isAuthenticated.value) navigateTo('/')
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
    error.value = e?.data?.error || e?.message || 'Something went wrong'
  } finally {
    loading.value = false
  }
}
</script>
