<template>
  <NuxtLayout>
    <NuxtPage v-if="ready" />
  </NuxtLayout>
  <Lightbox />
  <!-- vue-query cache inspector — dev-only floating button (bottom-right).
       Click the TanStack logo to expand the panel; shows every query's
       state, fetch status, last-updated, and lets you manually invalidate. -->
  <VueQueryDevtools v-if="isDev" />
</template>

<script setup lang="ts">
import { VueQueryDevtools } from '@tanstack/vue-query-devtools'

const isDev = import.meta.dev
const route = useRoute()
const { ready, isAuthenticated } = useAuth()

// hydrate() + fetchUser() are now done once at SPA boot in plugins/auth.ts —
// removing the duplicate that lived here. Doubling the boot-time
// /api/auth/me call doubled the surface area for a transient error
// (backend bouncing, network blip) to fall into the old `catch → logout`
// path and boot the user mid-session.

watch([ready, isAuthenticated], ([r, auth]) => {
  if (r && !auth && route.path !== '/login') {
    navigateTo('/login')
  }
})
</script>
