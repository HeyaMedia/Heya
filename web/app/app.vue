<template>
  <NuxtLoadingIndicator color="var(--gold)" :height="3" :throttle="120" />
  <NuxtLayout>
    <NuxtPage v-if="ready" />
  </NuxtLayout>
  <Lightbox />
  <AppToastHost />
  <OfflineIndicator />
  <!-- Global touch affordances (left-edge swipe → sidebar, pull-to-refresh).
       Client-only; renders only a transient pull indicator on touch devices. -->
  <TouchGestures />
  <!-- Pinia Colada's query inspector is dev-only and tree-shaken from the
       production bundle. It exposes cache entries, status and fetch timing. -->
  <component :is="ColadaDevtools" v-if="ColadaDevtools" />
  <component :is="DataMetrics" v-if="DataMetrics" />
</template>

<script setup lang="ts">
import { defineAsyncComponent } from 'vue'

const ColadaDevtools = import.meta.dev
  ? defineAsyncComponent(() => import('@pinia/colada-devtools').then(mod => mod.PiniaColadaDevtools))
  : null
const DataMetrics = import.meta.dev
  ? defineAsyncComponent(() => import('~/components/dev/DataMetrics.vue'))
  : null
const route = useRoute()
const { ready, isAuthenticated } = useAuth()

// Bridge OS media keys / lock-screen transport to the player. Mounted here
// (not Playbar) so the bridge is always active regardless of route —
// Playbar only exists under /music and is hidden entirely on phone. No-op
// on SSR and on browsers without the Media Session API (guards itself).
useMediaSession()

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
