// Which section sidebar (if any) the current route offers, plus the open
// state of the compact-band drawer that holds it.
//
// In the compact band (720.02–1200px, see useViewport().isCompact) the
// persistent sidebars are hidden and AppTopBar shows a burger button instead;
// tapping it opens the section's sidebar as a left-side drawer. The section
// pages own the drawer mount; the topbar owns the trigger — this composable
// is the shared state between them (module singleton, same pattern as
// useToast/useConfirm).
//
// `kind` is derived from the route rather than registered by pages on mount:
// registration would race page transitions (new page's setup can run before
// the old page's unmount, so the old unregister clobbers the new register).
// Route paths are deterministic, so derive instead:
//   /movies, /tv, /books (index pages only)  -> 'library'
//   /music and everything under it           -> 'music'
// Detail pages (/movies/{slug}, ...) have no persistent sidebar on desktop,
// so they get no burger either.
import { computed, ref } from 'vue'

export type SectionSidebarKind = 'library' | 'music'

const LIBRARY_INDEX_PATHS = new Set(['/movies', '/tv', '/books'])

const open = ref(false)

export function useSectionSidebar() {
  const route = useRoute()

  const kind = computed<SectionSidebarKind | null>(() => {
    const path = route.path.replace(/\/+$/, '') || '/'
    if (path === '/music' || path.startsWith('/music/')) return 'music'
    if (LIBRARY_INDEX_PATHS.has(path)) return 'library'
    return null
  })

  // Drawer never survives a navigation — whatever you tapped, you went
  // somewhere; a lingering drawer over the new page is never right.
  watch(() => route.fullPath, () => {
    open.value = false
  })

  return {
    kind,
    open,
    toggle: () => {
      open.value = !open.value
    },
    close: () => {
      open.value = false
    },
  }
}
