<template>
  <aside class="lib-sidebar scroll" :class="{ 'lib-sidebar-sheet': variant === 'sheet' }">
    <!-- Section landing (movies / tv only). Bare `/movies` route; leads with the
         personalized "For You" row, then the discovery rails. The flat grid
         moves under "All {{ typeLabel }}" below, the steerable engine under
         "Recommendations" (its own route). -->
    <div v-if="showBrowse" class="lib-section" style="margin-bottom: 20px">
      <div
        class="lib-item"
        role="button"
        :class="{ active: activeView === 'browse' }"
        @click="$emit('view', 'browse')"
      >
        <Icon name="star" :size="16" style="color: var(--gold)" />
        <span>Browse</span>
      </div>
      <div
        class="lib-item"
        role="button"
        :class="{ active: activeView === 'recommendations' }"
        @click="$emit('view', 'recommendations')"
      >
        <Icon name="sparkle" :size="16" style="color: var(--gold)" />
        <span>Recommendations</span>
      </div>
    </div>

    <div class="lib-section">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 10px">Libraries</div>
      <div
        class="lib-item"
        role="button"
        :class="{ active: !activeLib && !activeView }"
        @click="selectLib(null)"
      >
        <Icon name="folder" :size="16" />
        <span>All {{ typeLabel }}</span>
        <span v-if="totalCount > 0" class="count">{{ totalCount }}</span>
      </div>
      <div
        v-for="lib in libraries"
        :key="lib.id"
        class="lib-item lib-item-nested"
        role="button"
        :class="{ active: activeLib === lib.id && !activeView }"
        @click="selectLib(lib.id)"
      >
        <Icon name="folder" :size="12" class="list-type-icon" />
        <span>{{ lib.name }}</span>
      </div>
    </div>

    <div class="lib-section" style="margin-top: 24px">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 10px">Collections</div>
      <div
        class="lib-item"
        role="button"
        :class="{ active: activeView === 'loved' }"
        @click="$emit('view', 'loved')"
      >
        <Icon name="heartfill" :size="16" style="color: var(--bad)" />
        <span>Loved</span>
        <span v-if="(lovedCount ?? 0) > 0" class="count">{{ lovedCount }}</span>
      </div>

      <!-- `role="button"` on every clickable row below isn't decorative: reka
           Drawer's swipe-to-dismiss (AppSheet side="left"/bottom) captures
           the mouse pointer on pointerdown for anything that ISN'T
           button/a/input/select/textarea/label/[role="button"] and retargets
           the resulting click to the drawer's root content element instead
           of this row — silently eating the click. Harmless outside a
           drawer (desktop aside, and phone where taps are real touch events
           that skip pointer capture entirely), but required once this
           component renders inside AppSheet's `variant="sheet"`. -->
      <div class="lib-item lists-toggle" role="button" @click="listsExpanded = !listsExpanded">
        <Icon name="list" :size="16" />
        <span>My Lists</span>
        <Icon :name="listsExpanded ? 'chevdown' : 'chevright'" :size="10" class="expand-icon" />
      </div>
      <template v-if="listsExpanded">
        <div
          v-for="l in displayLists"
          :key="l.id"
          class="lib-item lib-item-nested"
          role="button"
          :class="{ active: activeView === `list-${l.id}`, 'drop-target': dragOverListId === l.id }"
          @click="$emit('view', `list-${l.id}`)"
          @dragover.prevent="$emit('list-dragover', $event, l.id)"
          @dragleave="$emit('list-dragleave')"
          @drop="$emit('list-drop', $event, l.id)"
        >
          <Icon :name="l.list_type === 'smart' ? 'lightning' : 'bookmark'" :size="12" class="list-type-icon" />
          <span>{{ l.name }}</span>
          <span v-if="l.item_count > 0" class="count">{{ l.item_count }}</span>
        </div>
        <div class="lib-item lib-item-nested lib-item-action" role="button" @click="createList">
          <Icon name="plus" :size="12" />
          <span>New List</span>
        </div>
      </template>

      <!-- TMDB Collections → a page of their own (/movies/franchises). Movie-only,
           so this row renders only when the parent passes the (≥2-film) browse
           list in; tv/books leave it unset. A specific franchise
           (/movies/collection/N) keeps this row highlighted too. -->
      <div
        v-if="collections?.length"
        class="lib-item"
        role="button"
        :class="{ active: activeView === 'franchises' }"
        @click="$emit('view', 'franchises')"
      >
        <Icon name="film" :size="16" />
        <span>Franchises</span>
        <span class="count">{{ collections.length }}</span>
      </div>
    </div>

    <div v-if="totalCount > 0" class="lib-footer">
      <div class="lib-footer-text">{{ totalCount }} titles</div>
    </div>
  </aside>
</template>

<script setup lang="ts">
import type { Library, UserList, CollectionBrowse } from '~~/shared/types'

const props = defineProps<{
  libraries: Library[]
  activeLib: number | null
  activeView: string | null
  typeLabel: string
  totalCount: number
  lovedCount?: number
  userLists?: UserList[]
  dragOverListId?: number | null
  /** TMDB collections with local movies. Undefined hides the Franchises section. */
  collections?: CollectionBrowse[]
  /** Show the "Browse" + "Recommendations" landing rows at the top (movies / tv only). */
  showBrowse?: boolean
  /** 'sidebar' (default) = fixed 240px aside. 'sheet' = fills an AppSheet body
   *  on phone (movies/tv/books index.vue) — same markup/behavior, just sheds
   *  the standalone-aside chrome. See the `.lib-sidebar-sheet` rule below. */
  variant?: 'sidebar' | 'sheet'
}>()

const emit = defineEmits<{
  select: [id: number | null]
  view: [view: string]
  'list-drop': [event: DragEvent, listId: number]
  'list-dragover': [event: DragEvent, listId: number]
  'list-dragleave': []
}>()

const listsExpanded = ref(false)

// Reveal the active list's section. Now that the selection lives in the URL
// (a deep link / reload / back can land straight on a list), the accordion it
// lives in must open so the active row is actually visible. Expand-only —
// never auto-collapse, so it can't fight a manual toggle.
watch(() => props.activeView, (v) => {
  if (v?.startsWith('list-')) listsExpanded.value = true
}, { immediate: true })

const displayLists = computed(() => props.userLists || [])

function selectLib(id: number | null) {
  emit('select', id)
}

async function createList() {
  const name = prompt('List name:')
  if (!name?.trim()) return
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/me/lists', {
      method: 'POST',
      body: { name: name.trim() } as any,
    })
  } catch { /* empty */ }
}

</script>

<style scoped>
.lib-sidebar {
  width: 240px;
  flex-shrink: 0;
  background: var(--bg-2);
  border-right: 1px solid var(--border);
  padding: 20px 10px;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.lib-section { display: flex; flex-direction: column; }
.lib-footer {
  margin-top: auto;
  padding: 16px 14px 0;
  border-top: 1px solid var(--border);
}
.lib-footer-text {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
}
.lists-toggle { cursor: pointer; }
.expand-icon { margin-left: auto; opacity: 0.4; }
.lib-item-nested { padding-left: 38px; }
.lib-item-action { color: var(--fg-3); font-size: 12px; }
.lib-item-action:hover { color: var(--gold); }
.lib-item-empty { color: var(--fg-4); font-size: 11px; cursor: default; }
.lib-item-empty:hover { background: none; }

.list-type-icon { opacity: 0.4; flex-shrink: 0; }

.drop-target {
  background: color-mix(in srgb, var(--gold) 10%, transparent);
  border: 1px dashed var(--gold);
  border-radius: var(--r-sm);
}

/* ── Sheet variant (docs/responsive-plan.md W3b) ─────────────────────────
   Same component, same markup/behavior — rendered a second time inside the
   phone "Library" AppSheet in movies/tv/books index.vue instead of the
   persistent aside. AppSheet's body already supplies scroll + side padding,
   so this variant just sheds the standalone-aside chrome (fixed width,
   built-in scroll, border, own background/padding). Two classes on the same
   element (`.lib-sidebar.lib-sidebar-sheet`) beat the base rule's own
   specificity without `!important` or fighting from outside the component —
   the approach MusicSidebar (W1c) couldn't use because its collapsible
   groups + now-playing fold-out cover are coupled to being a persistent
   240px+ aside; this sidebar has neither, so owning the variant here was
   simpler than re-listing its links flatly the way music.vue's nav sheet
   does. */
.lib-sidebar-sheet {
  width: 100%;
  height: auto;
  flex-shrink: initial;
  background: transparent;
  border-right: 0;
  padding: 0;
}
.lib-sidebar-sheet .lib-footer { margin-top: 24px; }

/* Sheet instance only ever renders at phone width, but scope the bump to
   the breakpoint anyway so it can't leak into the desktop aside if variant
   handling ever changes. */
@media (max-width: 720px) {
  .lib-item { min-height: 44px; }
}
</style>
