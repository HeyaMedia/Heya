import { storeToRefs } from 'pinia'
import { useLightboxStore } from '~/stores/lightbox'

export function useLightbox() {
  const store = useLightboxStore()
  return { ...storeToRefs(store), open: store.open, close: store.close, next: store.next, prev: store.prev }
}
