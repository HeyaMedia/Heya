<script setup lang="ts">
// /settings → first page in the user's allowed nav. Admins land on the
// dashboard; everyone else on their profile. Keeps the URL meaningful even
// when typed bare.
definePageMeta({ layout: 'settings' })

const { user, token, ready } = useAuth()

// ready.value flips true after the token hydrates synchronously at boot, but
// user.value lags until /api/auth/me resolves a tick later. Wait for the
// user payload (or for the user to be unambiguously absent) before deciding
// where to land.
watchEffect(() => {
  if (!ready.value) return
  if (token.value && !user.value) return
  const target = user.value?.is_admin ? '/settings/dashboard' : '/settings/profile'
  navigateTo(target, { replace: true })
})
</script>

<template>
  <div />
</template>
