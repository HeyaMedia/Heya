<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>

<script setup lang="ts">
const { token, fetchUser } = useAuth()

onMounted(async () => {
  if (token.value) {
    await fetchUser()
  }
})
</script>
