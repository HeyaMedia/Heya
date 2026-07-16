/* Generated Workbox service worker extension for Heya track notifications. */
self.addEventListener('notificationclick', (event) => {
  if (event.notification?.tag !== 'heya-now-playing') return
  event.notification.close()
  const destination = event.notification.data?.url || '/music'
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    const client = windows[0]
    if (client) {
      await client.navigate(destination)
      await client.focus()
      return
    }
    await self.clients.openWindow(destination)
  })())
})
