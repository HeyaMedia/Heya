<template>
  <div class="space-y-6">
    <h1 class="text-2xl font-semibold">Settings</h1>

    <div class="card p-6">
      <h2 class="mb-4 text-lg font-medium">Server Status</h2>
      <div class="space-y-2 text-sm">
        <div class="flex justify-between">
          <span class="text-gray-400">Status</span>
          <span :class="health?.status === 'ok' ? 'text-green-400' : 'text-red-400'">
            {{ health?.status || 'checking...' }}
          </span>
        </div>
        <div class="flex justify-between">
          <span class="text-gray-400">Database</span>
          <span :class="health?.database === 'connected' ? 'text-green-400' : 'text-red-400'">
            {{ health?.database || 'checking...' }}
          </span>
        </div>
        <div class="flex justify-between">
          <span class="text-gray-400">Version</span>
          <span class="text-gray-300">{{ health?.version || '-' }}</span>
        </div>
      </div>
    </div>

    <div class="card p-6">
      <h2 class="mb-4 text-lg font-medium">Account</h2>
      <div class="space-y-2 text-sm">
        <div class="flex justify-between">
          <span class="text-gray-400">Username</span>
          <span class="text-gray-300">{{ user?.username || '-' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-gray-400">Email</span>
          <span class="text-gray-300">{{ user?.email || '-' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-gray-400">Role</span>
          <span :class="user?.is_admin ? 'text-heya-primary' : 'text-gray-300'">
            {{ user?.is_admin ? 'Admin' : 'User' }}
          </span>
        </div>
      </div>
    </div>

    <div class="card p-6">
      <h2 class="mb-4 text-lg font-medium">API</h2>
      <div class="space-y-2 text-sm">
        <div class="flex items-center justify-between">
          <span class="text-gray-400">OpenAPI Spec</span>
          <a href="/api/openapi.json" target="_blank" class="text-heya-primary hover:underline">/api/openapi.json</a>
        </div>
        <div class="flex items-center justify-between">
          <span class="text-gray-400">API Documentation</span>
          <a href="/api/docs" target="_blank" class="text-heya-primary hover:underline">Scalar Docs</a>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { HealthResponse } from '~~/shared/types'

const { user, isAuthenticated } = useAuth()
watchEffect(() => {
  if (!isAuthenticated.value) navigateTo('/login')
})

const health = ref<HealthResponse | null>(null)

onMounted(async () => {
  try {
    health.value = await $fetch<HealthResponse>('/api/health')
  } catch { /* empty */ }
})
</script>
