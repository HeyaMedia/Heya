const _open = ref(false)
const _images = ref<string[]>([])
const _index = ref(0)

export function useLightbox() {
  function open(src: string | string[], startIndex = 0) {
    _images.value = Array.isArray(src) ? src : [src]
    _index.value = startIndex
    _open.value = true
  }

  function close() {
    _open.value = false
  }

  function next() {
    if (_index.value < _images.value.length - 1) _index.value++
  }

  function prev() {
    if (_index.value > 0) _index.value--
  }

  return {
    isOpen: _open,
    images: _images,
    index: _index,
    currentSrc: computed(() => _images.value[_index.value] || ''),
    hasNext: computed(() => _index.value < _images.value.length - 1),
    hasPrev: computed(() => _index.value > 0),
    total: computed(() => _images.value.length),
    open,
    close,
    next,
    prev,
  }
}
