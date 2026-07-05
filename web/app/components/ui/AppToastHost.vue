<!--
  AppToastHost — global toast notifications. Mounted once in app.vue (like
  Lightbox / ConfirmDialog); reads the module-level queue from useToast().

  Teleported to body, fixed + bottom-center, above AppSheet (400) and the
  phone search overlay (450) so a toast fired from anywhere is never buried
  under a sheet or the fullscreen search. `.toast-host` itself carries
  pointer-events:none so it never eats clicks over the playbar/mini-player
  when empty or between cards — only `.toast-card` re-enables them for
  tap-to-dismiss.

  Bottom offset clears the app's own bottom chrome:
  - Desktop (>720px): fixed ~110px clears the music Playbar (--playbar-h:
    88px, in-flow at the bottom of /music) with a bit of margin. Playbar
    only exists on /music, but a constant offset elsewhere is harmless —
    the host is empty (zero visible height) until a toast fires, so it
    never shifts other desktop layout per the desktop-unchanged rule.
  - Phone (<=720px): clears BottomNav (--bottomnav-h + --safe-bottom), and
    when MobilePlayerHost's mini-player is docked, adds --miniplayer-h too
    — same `body:has(.global-miniplayer-dock)` pattern heya.css uses for
    `.app-main` padding, scoped to body since this host teleports there
    instead of living inside `.app`.

  Scoped styles reach the teleported content fine — Vue's own Teleport
  keeps the component's data-v-* scoping id on the moved nodes (see
  AppSearchOverlay.vue, which does the same).
-->
<template>
  <Teleport to="body">
    <TransitionGroup tag="div" name="toast" class="toast-host" role="status" aria-live="polite">
      <div
        v-for="t in toasts"
        :key="t.id"
        class="surface toast-card"
        :class="`toast-${t.tone}`"
        @click="dismiss(t.id)"
      >
        <Icon :name="t.icon" :size="15" class="toast-icon" />
        <span class="toast-msg">{{ t.message }}</span>
      </div>
    </TransitionGroup>
  </Teleport>
</template>

<script setup lang="ts">
const { toasts, dismiss } = useToast()
</script>

<style scoped>
.toast-host {
  position: fixed;
  left: 50%;
  transform: translateX(-50%);
  bottom: 110px;
  z-index: 600;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  pointer-events: none;
  width: min(420px, calc(100vw - 32px));
}

@media (max-width: 720px) {
  .toast-host {
    bottom: calc(var(--bottomnav-h) + var(--safe-bottom) + 16px);
  }
}
/* Mini-player only ever docks on phone (JS-gated in MobilePlayerHost), so
   this is effectively a phone-only override even though it isn't nested in
   the media query above — matches heya.css's own `.app:has(...)` rule. */
body:has(.global-miniplayer-dock) .toast-host {
  bottom: calc(var(--bottomnav-h) + var(--miniplayer-h) + var(--safe-bottom) + 16px);
}

.toast-card {
  /* `.surface` (surface.css, applied in the template) gives the glass
     background/border/shadow. Override its default z-index (200) — the
     host already sets 600 on `.toast-host` — and skip its [data-state]
     scale-in animation (reka-specific); this uses its own TransitionGroup
     enter/leave below instead. */
  pointer-events: auto;
  z-index: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 100%;
  padding: 10px 16px;
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  cursor: pointer;
}

.toast-icon { flex-shrink: 0; }
.toast-ok .toast-icon { color: var(--good); }
.toast-err .toast-icon { color: var(--bad); }
.toast-info .toast-icon { color: var(--gold); }

.toast-msg {
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.toast-move,
.toast-enter-active,
.toast-leave-active {
  transition: transform 0.18s cubic-bezier(0.16, 1, 0.3, 1), opacity 0.18s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(10px);
}
/* Leaving items are removed from flow immediately so siblings can slide
   into place without the leaving card holding a gap open. */
.toast-leave-active { position: absolute; }
</style>
