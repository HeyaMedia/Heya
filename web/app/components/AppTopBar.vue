<template>
  <header class="topbar">
    <NuxtLink to="/" class="topbar-brand">
      <div class="brand-mark">
        <svg width="22" height="22" viewBox="0 0 22 22">
          <circle cx="11" cy="11" r="10" fill="none" stroke="var(--gold)" stroke-width="1.5" />
          <circle cx="11" cy="11" r="4" fill="var(--gold)" />
          <circle cx="11" cy="11" r="1.5" fill="#0a0a0a" />
        </svg>
      </div>
      <span class="brand-name">heya<span class="brand-dot">.</span>media</span>
    </NuxtLink>

    <nav class="topbar-tabs">
      <NuxtLink
        v-for="t in tabs"
        :key="t.to"
        :to="t.to"
        class="tab"
        :class="{ active: isActive(t) }"
      >
        <Icon :name="t.icon" :size="16" />
        <span>{{ t.label }}</span>
      </NuxtLink>
    </nav>

    <div class="topbar-right">
      <div class="search-wrap open">
        <Icon name="search" :size="16" />
        <input
          v-model="searchVal"
          placeholder="Search titles, artists, books…"
          @keydown.enter="doSearch"
          @keydown.escape="searchVal = ''"
        />
        <button v-if="searchVal" class="search-close" @click="searchVal = ''">
          <Icon name="close" :size="14" />
        </button>
      </div>
      <button class="btn-icon" title="Cast"><Icon name="cast" :size="18" /></button>
      <button class="btn-icon" title="Notifications"><Icon name="bell" :size="18" /></button>
      <NuxtLink to="/settings" class="btn-icon" title="Settings"><Icon name="settings" :size="18" /></NuxtLink>
      <div v-if="user" class="avatar" :title="user.username">
        <span>{{ user.username.slice(0, 2).toUpperCase() }}</span>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
const route = useRoute()
const { user } = useAuth()
const searchOpen = ref(false)
const searchVal = ref('')
const searchInput = ref<HTMLInputElement>()

const tabs = [
  { to: '/', label: 'Home', icon: 'home', match: ['/'] },
  { to: '/movies', label: 'Movies', icon: 'film', match: ['/movies'] },
  { to: '/tv', label: 'TV', icon: 'tv', match: ['/tv'] },
  { to: '/music', label: 'Music', icon: 'music', match: ['/music'] },
  { to: '/books', label: 'Books', icon: 'book', match: ['/books'] },
]

function isActive(t: typeof tabs[0]) {
  if (t.to === '/' && route.path === '/') return true
  if (t.to !== '/' && route.path.startsWith(t.to)) return true
  if (t.to === '/movies' && route.path.startsWith('/media/')) return true
  return false
}

function openSearch() {
  searchOpen.value = true
  nextTick(() => searchInput.value?.focus())
}

function closeSearch() {
  searchOpen.value = false
  searchVal.value = ''
}

function doSearch() {
  if (searchVal.value.trim()) {
    navigateTo(`/search?q=${encodeURIComponent(searchVal.value)}`)
    closeSearch()
  }
}
</script>

<style scoped>
.topbar {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 24px;
  padding: 0 24px;
  background: rgba(7, 7, 10, 0.85);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-bottom: 1px solid var(--border);
  height: var(--topbar-h);
  z-index: 50;
  position: relative;
}
.topbar-brand { display: flex; align-items: center; gap: 10px; cursor: pointer; text-decoration: none; }
.brand-mark { display: flex; align-items: center; justify-content: center; }
.brand-name { font-size: 16px; font-weight: 600; letter-spacing: -0.01em; color: var(--fg-0); }
.brand-name .brand-dot { color: var(--gold); }
.topbar-tabs { display: flex; gap: 2px; justify-self: center; }
.topbar-tabs .tab {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 0 16px; height: 36px;
  border-radius: var(--r-md);
  color: var(--fg-2);
  font-size: 13px; font-weight: 500;
  transition: color 0.15s ease, background 0.15s ease;
  text-decoration: none;
}
.topbar-tabs .tab:hover { color: var(--fg-0); background: rgba(255,255,255,0.04); }
.topbar-tabs .tab.active { color: var(--gold); }
.topbar-right { display: flex; align-items: center; gap: 4px; }
.search-wrap { display: flex; align-items: center; gap: 8px; }
.search-wrap.open {
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 10px 0 12px;
  height: 36px;
  width: 280px;
}
.search-wrap input { background: transparent; border: 0; outline: 0; color: var(--fg-0); font-size: 13px; flex: 1; padding: 0; }
.search-wrap input::placeholder { color: var(--fg-3); }
.search-close { color: var(--fg-3); }
.search-close:hover { color: var(--fg-0); }
.avatar {
  width: 32px; height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
  font-size: 11px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  margin-left: 6px;
  cursor: pointer;
  letter-spacing: 0.04em;
}
</style>
