<template>
  <aside class="lib-sidebar scroll" :class="{ 'lib-sidebar-sheet': variant === 'sheet' }">
    <div class="lib-section">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 10px">Libraries</div>
      <div
        class="lib-item"
        :class="{ active: !activeLib && !activeView }"
        @click="selectLib(null)"
      >
        <Icon name="folder" :size="16" />
        <span>All {{ typeLabel }}</span>
        <span class="count">{{ totalCount }}</span>
      </div>
      <div
        v-for="lib in libraries"
        :key="lib.id"
        class="lib-item lib-item-nested"
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
        :class="{ active: activeView === 'loved' }"
        @click="$emit('view', 'loved')"
      >
        <Icon name="heartfill" :size="16" style="color: var(--bad)" />
        <span>Loved</span>
        <span v-if="(lovedCount ?? 0) > 0" class="count">{{ lovedCount }}</span>
      </div>

      <div class="lib-item lists-toggle" @click="listsExpanded = !listsExpanded">
        <Icon name="list" :size="16" />
        <span>My Lists</span>
        <Icon :name="listsExpanded ? 'chevdown' : 'chevright'" :size="10" class="expand-icon" />
      </div>
      <template v-if="listsExpanded">
        <div
          v-for="l in displayLists"
          :key="l.id"
          class="lib-item lib-item-nested"
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
        <div class="lib-item lib-item-nested lib-item-action" @click="createList">
          <Icon name="plus" :size="12" />
          <span>New List</span>
        </div>
      </template>

      <!-- TMDB Collections — movie-only, so the section renders only when the
           parent page passes the browse result in (tv/books leave it unset). -->
      <template v-if="collections">
        <div class="lib-item lists-toggle" @click="collectionsExpanded = !collectionsExpanded" style="margin-top: 4px">
          <Icon name="film" :size="16" />
          <span>Franchises</span>
          <Icon :name="collectionsExpanded ? 'chevdown' : 'chevright'" :size="10" class="expand-icon" />
        </div>
        <template v-if="collectionsExpanded">
          <div
            v-for="c in collections"
            :key="c.id"
            class="lib-item lib-item-nested"
            :class="{ active: activeView === `collection-${c.id}` }"
            @click="$emit('view', `collection-${c.id}`)"
          >
            <span>{{ c.name }}</span>
            <span class="count">{{ c.movie_count }}</span>
          </div>
          <div v-if="!collections.length" class="lib-item lib-item-nested lib-item-empty">
            No franchises
          </div>
        </template>
      </template>
    </div>

    <div class="lib-footer">
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
const collectionsExpanded = ref(false)

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
  background: rgba(212,175,55,0.1);
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
