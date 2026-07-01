<template>
  <NuxtLayout>
    <NuxtPage v-if="ready" />
  </NuxtLayout>
  <Lightbox />
  <!-- Dev-only in-app query-cache overview (bottom-left toggle, ⌘⇧Q). Reads
       the live $queryClient: every query's key, status, staleness, observers,
       age + per-query invalidate/refetch/remove. Async-imported behind
       import.meta.dev so it (and its type-only deps) drop out of the prod
       single binary. Replaces the old TanStack floating widget. -->
  <component :is="QueryCachePanel" v-if="QueryCachePanel" />
</template>

<script setup lang="ts">
import { defineAsyncComponent } from 'vue'

const QueryCachePanel = import.meta.dev
  ? defineAsyncComponent(() => import('~/components/dev/QueryCachePanel.vue'))
  : null
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
