// Global, WS-driven cache invalidation so Continue Watching (and the rails
// derived from watch state) update live across every open tab/page — not
// just the tab that performed the mark-watched/unwatch mutation.
//
// `media.watched` fires from the service layer whenever watch state changes
// (mark watched, unwatch, or progress crossing the completion threshold), so
// this listener is global and always-registered rather than tied to
// whichever page happens to be mounted.
export default defineNuxtPlugin(() => {
  const { on } = useEventBus()
  const nuxtApp = useNuxtApp()

  // $queryClient is provided by plugins/vue-query.client.ts. Read it lazily
  // inside the handler (off the captured nuxtApp, not at setup time) so plugin
  // load order doesn't matter — by the time an event arrives at runtime, every
  // plugin has long since initialised and provided the client.
  on('media.watched', () => {
    // CW_QUERY_KEY from useWatchResume.ts — kept as a literal here since that
    // module doesn't export the constant.
    nuxtApp.$queryClient.invalidateQueries({ queryKey: ['me', 'watch', 'continue'] })
    nuxtApp.$queryClient.invalidateQueries({ queryKey: ['me', 'watch', 'recent'] })
    nuxtApp.$queryClient.invalidateQueries({ queryKey: ['me', 'state'] })
  })
})
