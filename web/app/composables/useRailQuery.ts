// Shared plumbing for the infinite horizontal rails (queries/rails.ts).
//
// railLoadMore guards Pinia Colada's loadNextPage: colada's default
// cancelRefetch=true means a second call mid-flight CANCELS and restarts the
// fetch, so an unguarded scroll handler could keep a page perpetually
// restarting. One in-flight load at a time, and never while the entry is
// already fetching.
import type { UseInfiniteQueryReturn } from '@pinia/colada'

type AnyInfiniteQuery = Pick<UseInfiniteQueryReturn<never, never, never>, 'asyncStatus' | 'hasNextPage' | 'loadNextPage'>

export function railLoadMore(q: AnyInfiniteQuery): () => void {
  let inflight = false
  return () => {
    if (inflight || q.asyncStatus.value === 'loading' || !q.hasNextPage.value) return
    inflight = true
    void q.loadNextPage().finally(() => {
      inflight = false
    })
  }
}
