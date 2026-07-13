<script setup lang="ts">
import { TooltipProvider } from 'reka-ui'
import SettingsSidebar from '~/components/settings/SettingsSidebar.vue'

// Phone (<=720px): the 240px SettingsSidebar disappears (docs/responsive-plan
// W3d) and is replaced by a compact header row (current section name + a
// button) that opens the same nav in a full-height AppSheet — same shape as
// the music Browse sheet (pages/music.vue), but reusing <SettingsSidebar>
// itself rather than flat-relisting its links: unlike MusicSidebar, this
// component carries no local collapsible/cover state, so there's nothing
// fighting an unscoped override — see SettingsSidebar's `variant="sheet"`.
const route = useRoute()
const { itemByPath } = useSettingsNav()
const { isPhone } = useViewport()
const navOpen = ref(false)
const dashboardWide = computed(() => route.path === '/settings/dashboard' || route.path === '/settings/activity')

const currentTitle = computed(() => itemByPath.value.get(route.path)?.item.label ?? 'Settings')

// SettingsSidebar's links don't know they're inside a sheet, so close it
// here on navigation rather than teaching the shared component about its
// presentation context (same reasoning as music.vue's flat list closing
// itself per-link, just centralised since every link already changes route).
watch(() => route.path, () => { navOpen.value = false })
</script>

<template>
  <!--
    Topbar + two-column body. Sidebar and main scroll independently — the
    outer .app is locked to 100vh (set by heya.css) so we can't rely on
    document scroll.
  -->
  <TooltipProvider :delay-duration="400" :skip-delay-duration="200">
    <a href="#main-content" class="skip-link">Skip to content</a>
    <div class="app">
      <AppTopBar />
      <div class="app-main sv2-shell">
        <SettingsSidebar v-if="!isPhone" />
        <main id="main-content" class="sv2-main scroll" tabindex="-1">
          <div v-if="isPhone" class="sv2-phone-head">
            <div class="sv2-phone-title">{{ currentTitle }}</div>
            <button type="button" class="sv2-phone-nav-btn" @click="navOpen = true">
              <Icon name="list" :size="16" />
              <span>Sections</span>
            </button>
          </div>
          <div class="sv2-content" :class="{ 'sv2-content-dashboard': dashboardWide }">
            <SettingsPageTabs />
            <slot />
          </div>
        </main>
      </div>
      <BottomNav />
      <ConfirmDialog />
      <!-- Same phone player mount as layouts/default.vue — music keeps
           playing while the user pokes at Settings, so the bar follows. -->
      <MobilePlayerHost />
      <DesktopPlayerHost />

      <AppSheet v-if="isPhone" v-model:open="navOpen" title="Settings" size="full">
        <SettingsSidebar variant="sheet" />
      </AppSheet>
    </div>
  </TooltipProvider>
</template>

<style scoped>
.sv2-shell {
  display: flex;
  height: 100%;
  overflow: hidden;
}

.sv2-main {
  flex: 1;
  min-width: 0;
}

.sv2-content {
  max-width: 1080px;
  margin: 0 auto;
  padding: 32px 40px 80px;
}
.sv2-content-dashboard { max-width: 1320px; }

/* Phone-only compact header — replaces the persistent SettingsSidebar with
   a section title + a button that opens the nav sheet. Mirrors
   pages/music.vue's .music-phone-header treatment. */
.sv2-phone-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px 4px;
}
.sv2-phone-title {
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sv2-phone-nav-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 14px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  color: var(--fg-1);
  font-size: 13px;
  font-weight: 500;
  flex-shrink: 0;
}
.sv2-phone-nav-btn:active { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }

@media (max-width: 720px) {
  .sv2-content {
    max-width: 100%;
    padding: 8px 16px 24px;
  }
}
</style>
