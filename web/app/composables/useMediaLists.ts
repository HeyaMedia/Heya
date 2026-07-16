import type { Ref } from 'vue'

export interface UserListEntry {
  id: number
  name: string
  description?: string
  item_count: number
  /** Whether the media item this hook is keyed on is already in the list. */
  contains: boolean
}

/**
 * User-list membership ops for a single media item ("Add to List" flow).
 * Pure data layer — the dialog UI lives in `AddToListDialog.vue`.
 */
export function useMediaLists(mediaItemId: Ref<number>) {
  const { $heya } = useNuxtApp()
  const userLists = ref<UserListEntry[]>([])

  async function loadLists() {
    try {
      userLists.value = await $heya('/api/me/lists', {
        query: { media_item_id: mediaItemId.value },
      }) as UserListEntry[]
    } catch { /* empty */ }
  }

  async function createList(name: string, description: string, mediaType: string) {
    if (!name.trim()) return
    await $heya('/api/me/lists', {
      method: 'POST',
      // The API validates the full manual-list shape — name-only bodies 422
      // (list_type and media_type are enums). Anime items file under the tv
      // section: the list enum has no 'anime' member.
      body: {
        name: name.trim(),
        description: description.trim(),
        list_type: 'manual',
        filter_json: null,
        media_type: mediaType === 'anime' ? 'tv' : mediaType,
      } as any,
    })
    await loadLists()
  }

  async function toggleListItem(l: UserListEntry) {
    if (l.contains) {
      await $heya('/api/me/lists/{id}/items/{media_id}', {
        method: 'DELETE',
        path: { id: l.id, media_id: mediaItemId.value },
      })
    } else {
      await $heya('/api/me/lists/{id}/items', {
        method: 'POST',
        path: { id: l.id },
        body: { media_item_id: mediaItemId.value } as any,
      })
    }
    await loadLists()
  }

  return { userLists, loadLists, createList, toggleListItem }
}
