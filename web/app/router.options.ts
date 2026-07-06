import type { RouterConfig } from '@nuxt/schema'
import type { RouteRecordRaw } from 'vue-router'

// Browse-view routes for the movies / tv library pages. The sidebar selection
// (library / loved / list / franchise) lives in the PATH so each pick earns a
// real history entry — back/forward walks the selection chain instead of
// leaving the page. See useBrowseState.ts for the store⇄path sync.
//
// Each sub-route renders the SAME index component as `/movies` (resp. `/tv`)
// and carries a shared, stable `meta.key`. That key is load-bearing: Nuxt
// keys pages by the matched route's interpolated path by default, so without a
// common key, moving between these sibling routes would remount the (heavy,
// up-to-5000-item) browse page and refetch the whole library on every click.
//
// The numeric `(\d+)` constraints keep these from shadowing the `/movies/:slug`
// detail route for real slugs; only the static `/movies/loved` reserves a word
// (a movie whose slug is exactly "loved" would be unreachable — acceptable).
export default <RouterConfig>{
  routes: (routes) => {
    const extra: RouteRecordRaw[] = []
    const defs = [
      { base: 'movies', key: 'browse-movies', franchises: true },
      { base: 'tv', key: 'browse-tv', franchises: false },
    ]
    for (const { base, key, franchises } of defs) {
      const index = routes.find(r => r.path === `/${base}`)
      // Nuxt's scanned page records are single-view (always carry `component`);
      // the RouteRecordRaw union just doesn't narrow that for TS here.
      const component = (index as { component?: RouteRecordRaw['component'] } | undefined)?.component
      if (!component) continue
      extra.push(
        // Bare `/movies` is the Recommended landing; the flat grid lives at
        // `/movies/all` (see useBrowseState's recommendedDefault).
        { path: `/${base}/all`, component, meta: { key } },
        { path: `/${base}/loved`, component, meta: { key } },
        { path: `/${base}/library/:libId(\\d+)`, component, meta: { key } },
        { path: `/${base}/list/:listId(\\d+)`, component, meta: { key } },
      )
      if (franchises) {
        extra.push({ path: `/${base}/franchises`, component, meta: { key } })
        // The per-franchise view is the rich standalone /collection/:id page
        // (linked from the Franchises grid + movie "part of collection"
        // badges). Keep the old browse-filter URL working as a redirect.
        extra.push({ path: `/${base}/collection/:colId(\\d+)`, redirect: (to) => `/collection/${to.params.colId}` })
      }
    }
    return [...routes, ...extra]
  },
}
