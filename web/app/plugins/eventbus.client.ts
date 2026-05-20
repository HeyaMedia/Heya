export default defineNuxtPlugin(() => {
  const { user } = useAuth()
  const { connect, disconnect } = useEventBus()

  watch(user, (u) => {
    if (u) connect()
    else disconnect()
  }, { immediate: true })
})
