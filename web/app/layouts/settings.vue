<script setup lang="ts">
import { TooltipProvider } from 'reka-ui'
import SettingsSidebar from '~/components/settings/SettingsSidebar.vue'
</script>

<template>
  <!--
    Topbar + two-column body. Sidebar and main scroll independently — the
    outer .app is locked to 100vh (set by heya.css) so we can't rely on
    document scroll.
  -->
  <TooltipProvider :delay-duration="400" :skip-delay-duration="200">
    <div class="app">
      <AppTopBar />
      <div class="app-main sv2-shell">
        <SettingsSidebar />
        <main class="sv2-main scroll">
          <div class="sv2-content">
            <slot />
          </div>
        </main>
      </div>
      <BottomNav />
      <ConfirmDialog />
      <!-- Same phone player mount as layouts/default.vue — music keeps
           playing while the user pokes at Settings, so the bar follows. -->
      <MobilePlayerHost />
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
  max-width: 1000px;
  margin: 0 auto;
  padding: 32px 40px 80px;
}
</style>
