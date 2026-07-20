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
  const queryCache = useQueryCache()

  const invalidate = () => {
    // CW_QUERY_KEY from useWatchResume.ts — kept as a literal here since that
    // module doesn't export the constant.
    queryCache.invalidateQueries({ key: ['me', 'watch', 'continue'] })
    queryCache.invalidateQueries({ key: ['me', 'watch', 'recent'] })
    // Episode-level feed behind the TV Recommended "Recently Watched" rail —
    // must invalidate here too (not only in BrowseView) so watching an
    // episode from the player / detail page keeps the rail fresh cross-page.
    queryCache.invalidateQueries({ key: ['me', 'watch', 'recent-episodes'] })
    queryCache.invalidateQueries({ key: ['me', 'state'] })
  }

  // Hidden tabs defer to a single catch-up invalidation on return — a
  // backgrounded tab refetching four query keys because some other client
  // marked an episode watched is wasted radio/CPU nobody can see.
  let hiddenPending = false
  on('media.watched', () => {
    if (document.visibilityState === 'hidden') {
      hiddenPending = true
      return
    }
    invalidate()
  })
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible' || !hiddenPending) return
    hiddenPending = false
    invalidate()
  })
})
