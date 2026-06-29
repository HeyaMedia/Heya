// Global, WS-driven cache invalidation for structural server-side changes
// that a single page's local invalidation can't cover.
//
// Today that's `library.deleted`: deleting a library cascades across an
// entire media type and everything derived from it (home rails, mixes,
// recommendations, watch state). The stale data only surfaces when you later
// navigate to one of those pages, so the listener must be global and
// always-registered — not tied to whichever page happens to be mounted.
//
// We blow away the whole vue-query cache: library deletes are rare and
// destructive, so a full invalidate is the can't-miss-a-key choice (active
// queries refetch now, inactive ones become stale and refetch on next visit).
// The handler fires for the acting tab, other open tabs, AND CLI deletes —
// the backend emits the event from the service layer, not the HTTP handler.
export default defineNuxtPlugin(() => {
  const { on } = useEventBus()
  const nuxtApp = useNuxtApp()

  // $queryClient is provided by plugins/vue-query.client.ts. Read it lazily
  // inside the handler (off the captured nuxtApp, not at setup time) so plugin
  // load order doesn't matter — by the time an event arrives at runtime, every
  // plugin has long since initialised and provided the client.
  on('library.deleted', () => {
    nuxtApp.$queryClient.invalidateQueries()
  })
})
