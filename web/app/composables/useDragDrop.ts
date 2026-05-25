import type { EnrichedMediaItem } from '~~/shared/types'

const dragState = reactive({
  dragging: false,
  item: null as EnrichedMediaItem | null,
  overListId: null as number | null,
})

export function useDragDrop() {
  function onDragStart(event: DragEvent, item: EnrichedMediaItem) {
    dragState.dragging = true
    dragState.item = item
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'copy'
      event.dataTransfer.setData('text/plain', String(item.id))
    }
  }

  function onDragEnd() {
    dragState.dragging = false
    dragState.item = null
    dragState.overListId = null
  }

  function onListDragOver(event: DragEvent, listId: number) {
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy'
    dragState.overListId = listId
  }

  function onListDragLeave() {
    dragState.overListId = null
  }

  async function onListDrop(event: DragEvent, listId: number) {
    event.preventDefault()
    dragState.overListId = null

    const mediaId = dragState.item?.id
    if (!mediaId) return

    try {
      const { $heya } = useNuxtApp()
      await $heya('/api/me/lists/{id}/items', {
        method: 'POST',
        path: { id: listId },
        body: { media_item_id: mediaId },
      })
    } catch { /* list may not accept duplicates */ }

    dragState.dragging = false
    dragState.item = null
  }

  return { dragState, onDragStart, onDragEnd, onListDragOver, onListDragLeave, onListDrop }
}
