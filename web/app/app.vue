<template>
  <NuxtLayout>
    <NuxtPage v-if="ready" />
  </NuxtLayout>
  <Lightbox />
</template>

<script setup lang="ts">
const route = useRoute()
const { ready, hydrate, token, isAuthenticated, fetchUser } = useAuth()

onMounted(async () => {
  hydrate()
  if (token.value) {
    await fetchUser()
  }
})

watch([ready, isAuthenticated], ([r, auth]) => {
  if (r && !auth && route.path !== '/login') {
    navigateTo('/login')
  }
})
</script>
