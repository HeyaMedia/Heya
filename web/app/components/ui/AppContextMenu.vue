<!--
  AppContextMenu — right-click menu.

  Wraps reka-ui's ContextMenu primitives with the shared .surface chrome.
  Wrap any element to give it a contextmenu:

    <AppContextMenu :items="ctxItems">
      <div class="grid-card">…</div>
    </AppContextMenu>

  Items are the same shape used everywhere (shared/types ContextMenuItem):
  `{ label, icon?, action?, disabled?, separator?, submenu? }`. One level of
  submenu is supported — reka handles flip/positioning/keyboard nav on the
  nested popper for free.

  The previous singleton + manual x/y positioning + useContextMenu
  composable went away; the consumer just composes items inline for each
  trigger. Items are evaluated per-render which is cheap object literal
  work, and reka only mounts the menu content lazily on right-click.
-->
<template>
  <ContextMenuRoot v-model:open="open">
    <ContextMenuTrigger as-child>
      <slot />
    </ContextMenuTrigger>
    <ContextMenuPortal>
      <ContextMenuContent class="surface app-context-menu" :collision-padding="8">
        <template v-for="(item, i) in items" :key="i">
          <ContextMenuSeparator
            v-if="item.separator"
            class="surface-divider"
          />
          <ContextMenuSub v-else-if="item.submenu && item.submenu.length">
            <ContextMenuSubTrigger
              class="surface-item app-context-item"
              :disabled="item.disabled"
            >
              <Icon v-if="item.icon" :name="item.icon" :size="14" class="surface-item-icon" />
              <span>{{ item.label }}</span>
              <Icon name="chevright" :size="11" class="app-context-sub-arrow" />
            </ContextMenuSubTrigger>
            <ContextMenuPortal>
              <ContextMenuSubContent class="surface app-context-menu" :collision-padding="8" :side-offset="-2">
                <ContextMenuItem
                  v-for="(sub, j) in item.submenu"
                  :key="j"
                  class="surface-item app-context-item"
                  :disabled="sub.disabled"
                  @select="sub.action?.()"
                >
                  <Icon v-if="sub.icon" :name="sub.icon" :size="14" class="surface-item-icon" />
                  <span>{{ sub.label }}</span>
                </ContextMenuItem>
              </ContextMenuSubContent>
            </ContextMenuPortal>
          </ContextMenuSub>
          <ContextMenuItem
            v-else
            class="surface-item app-context-item"
            :disabled="item.disabled"
            @select="item.action?.()"
          >
            <Icon v-if="item.icon" :name="item.icon" :size="14" class="surface-item-icon" />
            <span>{{ item.label }}</span>
          </ContextMenuItem>
        </template>
      </ContextMenuContent>
    </ContextMenuPortal>
  </ContextMenuRoot>
</template>

<script setup lang="ts">
import {
  ContextMenuRoot, ContextMenuTrigger, ContextMenuPortal,
  ContextMenuContent, ContextMenuItem, ContextMenuSeparator,
  ContextMenuSub, ContextMenuSubTrigger, ContextMenuSubContent,
} from 'reka-ui'
import type { ContextMenuItem as ContextMenuItemType } from '~~/shared/types'

defineProps<{
  items: ContextMenuItemType[]
}>()

const open = defineModel<boolean>('open', { default: false })
</script>

<!--
  Content is portaled out of this component, so its styling has to be
  unscoped (the same constraint AppMenu lives with). Only the trigger
  side (which is `as-child` and just forwards the slotted element)
  doesn't need any styling from this component.
-->
<style>
.app-context-menu {
  min-width: 200px;
  max-width: 280px;
  padding: 4px;
}

.app-context-item {
  /* Override the wider surface-item padding for the denser context-menu
     rhythm. Surface-item is tuned for menu rows like the user dropdown;
     context menus pack more options into the same vertical space. */
  padding: 7px 10px;
  font-size: 13px;
  border-radius: var(--r-sm);
  /* Reka stamps data-state on submenu triggers and data-highlighted on the
     row under the keyboard cursor — surface-item already styles these via
     `[data-highlighted]`. */
  user-select: none;
}

.app-context-sub-arrow {
  margin-left: auto;
  opacity: 0.5;
}
.app-context-item[data-state="open"] .app-context-sub-arrow {
  opacity: 1;
}

/* Touch pass — reka's ContextMenuTrigger already opens on long-press for
   touch/pen pointers (700ms default `pressOpenDelay`, see
   node_modules/reka-ui/src/ContextMenu/ContextMenuTrigger.vue), so no extra
   JS is needed here. Coarse pointers just need comfortable tap targets. */
@media (pointer: coarse) {
  .app-context-menu {
    max-width: 320px;
  }
  .app-context-item {
    min-height: 44px;
    padding: 10px 14px;
    font-size: 14px;
  }
}
</style>
