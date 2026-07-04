// Shared primary-nav tab source — AppTopBar's `.topbar-tabs` row and
// BottomNav's phone tab strip both render off this so the five sections
// (and the active-tab matching rule) never drift apart.
export interface NavTab {
  to: string
  label: string
  icon: string
  match: string[]
}

// `match` is carried for future use (e.g. a data-driven isActive) but the
// current logic below still special-cases `/media/*`, mirroring the
// pre-extraction behavior verbatim.
export const NAV_TABS: NavTab[] = [
  { to: '/', label: 'Home', icon: 'home', match: ['/'] },
  { to: '/movies', label: 'Movies', icon: 'film', match: ['/movies'] },
  { to: '/tv', label: 'TV', icon: 'tv', match: ['/tv'] },
  { to: '/music', label: 'Music', icon: 'music', match: ['/music'] },
  { to: '/books', label: 'Books', icon: 'book', match: ['/books'] },
]

export function useNavTabs() {
  const route = useRoute()

  function isActive(t: NavTab) {
    if (t.to === '/' && route.path === '/') return true
    if (t.to !== '/' && route.path.startsWith(t.to)) return true
    // Movie detail pages live under /media/{id} (a shared numeric-ID route
    // used before slugs existed for this entity type) rather than /movies/*.
    if (t.to === '/movies' && route.path.startsWith('/media/')) return true
    return false
  }

  return { tabs: NAV_TABS, isActive }
}
