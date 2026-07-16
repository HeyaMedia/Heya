<template>
  <NuxtLoadingIndicator color="var(--gold)" :height="3" :throttle="120" />
  <NativeWindowChrome />
  <NuxtLayout>
    <NuxtPage v-if="ready" />
  </NuxtLayout>
  <Lightbox />
  <AppToastHost />
  <!-- Global "Track information" dialog — driven by the useTrackInfo()
       singleton so the central track context menu opens it from anywhere. -->
  <TrackInfoDialog />
  <!-- Global text-prompt dialog — driven by the usePrompt() singleton so any
       surface (incl. render-less composables) can replace window.prompt(). -->
  <TextPromptDialog />
  <OfflineIndicator />
  <!-- Global touch affordances (left-edge swipe → sidebar, pull-to-refresh).
       Client-only; renders only a transient pull indicator on touch devices. -->
  <TouchGestures />
  <!-- Pinia Colada's query inspector is dev-only and tree-shaken from the
       production bundle. It exposes cache entries, status and fetch timing. -->
  <component :is="ColadaDevtools" v-if="ColadaDevtools" />
  <component :is="DataMetrics" v-if="DataMetrics && !route.path.startsWith('/watch/')" />
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

// One app-wide coordinator selects the browser Media Session adapter or, only
// after an origin-validated handshake, HeyaClient's native OS-media adapter.
// The player remains controllable after navigating away from /music.
useSystemMediaIntegration()

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
