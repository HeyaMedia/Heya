<script setup lang="ts">
import { TooltipProvider } from 'reka-ui'
import ManagerSidebar from '~/components/manager/ManagerSidebar.vue'

// The manager is Heya's back-of-house: same topbar, same tokens, but an ops
// shell (dense tables, statuses, queues) instead of the browse chrome. Mirrors
// layouts/settings.vue — persistent 240px rail on desktop, compact header +
// nav sheet on phone.
const route = useRoute()
const { titleByPath } = useManagerNav()
const { isPhone } = useViewport()
const navOpen = ref(false)

const currentTitle = computed(() => {
  const exact = titleByPath.value.get(route.path)
  if (exact) return exact
  if (route.path.startsWith('/manager/library/')) return 'Library'
  return 'Manager'
})

// The playbar follows the user into the manager, so its keyboard has to too
// (same reasoning as layouts/settings.vue).
useGlobalHotkeys()

watch(() => route.path, () => { navOpen.value = false })
</script>

<template>
  <TooltipProvider :delay-duration="400" :skip-delay-duration="200">
    <a href="#main-content" class="skip-link">Skip to content</a>
    <div class="app">
      <AppTopBar />
      <div class="app-main mgr-shell">
        <ManagerSidebar v-if="!isPhone" />
        <main id="main-content" class="mgr-main scroll" tabindex="-1">
          <div v-if="isPhone" class="mgr-phone-head">
            <div class="mgr-phone-title">{{ currentTitle }}</div>
            <button type="button" class="mgr-phone-nav-btn" @click="navOpen = true">
              <Icon name="list" :size="16" />
              <span>Sections</span>
            </button>
          </div>
          <div class="mgr-content">
            <slot />
          </div>
        </main>
      </div>
      <BottomNav />
      <ConfirmDialog />
      <MobilePlayerHost />
      <DesktopPlayerHost />

      <AppSheet v-if="isPhone" v-model:open="navOpen" title="Manager" size="full">
        <ManagerSidebar variant="sheet" />
      </AppSheet>
    </div>
  </TooltipProvider>
</template>

<style scoped>
.mgr-shell {
  display: flex;
  height: 100%;
  overflow: hidden;
}

.mgr-main {
  flex: 1;
  min-width: 0;
}

/* Wider than settings' 1080px — ops tables (queue, history, catalogs) want
   the room. */
.mgr-content {
  max-width: 1320px;
  margin: 0 auto;
  padding: 32px 40px 80px;
}

.mgr-phone-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px 12px;
  border-bottom: 1px solid var(--hair);
  margin-bottom: 4px;
}
.mgr-phone-title {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 22px;
  font-weight: 720;
  letter-spacing: -0.02em;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mgr-phone-nav-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 15px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border-strong);
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  flex-shrink: 0;
}
.mgr-phone-nav-btn:active { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }

@media (max-width: 720px) {
  .mgr-content {
    max-width: 100%;
    padding: 8px 16px 24px;
  }
}
</style>

<!-- Shared manager form vocabulary. Unscoped and mounted on the layout on
     purpose: dialog content is portaled (scoped rules can't reach it), and
     page-level unscoped blocks only ship with that page's chunk — a direct
     landing on another /manager page would render an unstyled dialog. -->
<style>
.mgr-form { display: flex; flex-direction: column; gap: 12px; }
.mgr-field { display: flex; flex-direction: column; gap: 5px; }
.mgr-field > span {
  font-size: 12px;
  font-weight: 580;
  color: var(--fg-1);
}
.mgr-input {
  height: 34px;
  padding: 0 11px;
  border-radius: var(--r-sm);
  background: rgb(var(--shade) / 0.3);
  border: 1px solid var(--border);
  color: var(--fg-0);
  font-size: 13px;
  outline: none;
  transition: border-color 0.12s;
}
.mgr-input:focus { border-color: var(--gold); }
.mgr-input.mono { font-family: var(--font-mono); font-size: 12px; }
.mgr-form-error {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: 12px;
  color: var(--bad);
}

/* Shared button vocabulary for every /manager page and its portaled
   dialogs — pages must not carry their own copies. */
.mgr-btn,
.mgr-btn-gold {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 32px;
  padding: 0 14px;
  border-radius: var(--r-sm);
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mgr-btn {
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-1);
}
.mgr-btn:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.mgr-btn-gold {
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  color: var(--gold-bright);
}
.mgr-btn-gold:hover { background: color-mix(in srgb, var(--gold) 18%, transparent); }
.mgr-btn:disabled,
.mgr-btn-gold:disabled { opacity: 0.6; pointer-events: none; }

.mgr-btn-icon {
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mgr-btn-icon:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.mgr-btn-icon.danger:hover { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); }
.mgr-btn-icon:disabled { opacity: 0.4; pointer-events: none; }

.mgr-flash {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12.5px;
}
.mgr-flash.ok { background: color-mix(in srgb, var(--good) 10%, transparent); border: 1px solid color-mix(in srgb, var(--good) 30%, transparent); color: var(--good); }
.mgr-flash.err { background: color-mix(in srgb, var(--bad) 10%, transparent); border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent); color: var(--bad); }

.mgr-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-3);
  font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}
</style>
