<script setup lang="ts">
defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  src?: string | null
  /** Poll a HeyaMetadata image URL through its 202 materialization phase. */
  persistent?: boolean
}>(), { src: '', persistent: false })

const emit = defineEmits<{
  load: [event: Event | string]
  error: [event: Event | string]
}>()

const attrs = useAttrs()
const transparentPixel = 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs='
const resolvedSource = ref('')
const loading = ref(false)
const failed = ref(false)
const eased = ref(false)
let generation = 0
let objectURL = ''
let controller: AbortController | null = null
let startedAt = 0

// Loads that resolve within this window (HTTP cache hits, same-tick decodes)
// appear together with the surrounding page paint — easing them in would make
// every remounted rail "flicker into existence" on each navigation. Only
// genuinely late arrivals fade over the spinner background.
const FAST_LOAD_MS = 100

const forwardedAttrs = computed(() => {
  const { class: _class, ...rest } = attrs
  return rest
})

const canonicalSource = computed(() => metadataImageProxyUrl(props.src))
const fetchPersistentSource = computed(() => props.persistent && canonicalSource.value === props.src)

const renderedSource = computed(() => {
  if (fetchPersistentSource.value && loading.value) return transparentPixel
  return resolvedSource.value || canonicalSource.value
})

function releaseObjectURL() {
  if (objectURL) URL.revokeObjectURL(objectURL)
  objectURL = ''
}

function sleep(ms: number, current: number) {
  return new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, ms)
    if (current !== generation) {
      window.clearTimeout(timer)
      resolve()
    }
  })
}

function retryDelay(response: Response, attempt: number) {
  const seconds = Number.parseInt(response.headers.get('Retry-After') || '', 10)
  if (Number.isFinite(seconds) && seconds >= 0) return Math.max(250, seconds * 1000)
  return Math.min(750 + attempt * 250, 5000)
}

async function materialize(source: string, current: number) {
  let attempt = 0
  while (current === generation) {
    try {
      const response = await fetch(source, {
        cache: 'no-store',
        signal: controller?.signal,
        headers: withClientSurfaceHeaders(source),
      })
      if (response.ok && response.status === 200 && response.headers.get('content-type')?.toLowerCase().startsWith('image/')) {
        const blob = await response.blob()
        if (current !== generation) return
        releaseObjectURL()
        objectURL = URL.createObjectURL(blob)
        resolvedSource.value = objectURL
        eased.value = true
        loading.value = false
        failed.value = false
        return
      }
      // HeyaMetadata normally answers 202. Transient gateway/rate-limit
      // responses are retried too so a phone can stay on the page while the
      // durable image job catches up.
      if (response.status !== 202 && response.status !== 408 && response.status !== 429 && response.status < 500) {
        loading.value = false
        failed.value = true
        emit('error', `${response.status} ${response.statusText}`)
        return
      }
      await sleep(retryDelay(response, attempt++), current)
    } catch (error) {
      // A temporary network handoff (mobile Wi-Fi ↔ cellular) should not turn
      // a valid canonical image into a permanent broken-image placeholder.
      await sleep(Math.min(1000 + attempt++ * 500, 5000), current)
      if (current !== generation) return
      if (error instanceof DOMException && error.name === 'AbortError') return
    }
  }
}

function begin() {
  const current = ++generation
  controller?.abort()
  controller = import.meta.client ? new AbortController() : null
  releaseObjectURL()
  resolvedSource.value = fetchPersistentSource.value ? '' : canonicalSource.value
  failed.value = false
  eased.value = false
  loading.value = !!props.src
  startedAt = performance.now()
  if (props.src && fetchPersistentSource.value && import.meta.client) void materialize(props.src, current)
}

function onLoad(event: Event | string) {
  // The transparent pixel is only a stable layout surface while fetch polling.
  if (fetchPersistentSource.value && loading.value) return
  eased.value = performance.now() - startedAt > FAST_LOAD_MS
  loading.value = false
  failed.value = false
  emit('load', event)
}

function onError(event: Event | string) {
  if (fetchPersistentSource.value) return
  loading.value = false
  failed.value = true
  emit('error', event)
}

watch(() => [props.src, props.persistent], begin, { immediate: true })
onBeforeUnmount(() => { generation++; controller?.abort(); releaseObjectURL() })
</script>

<template>
  <NuxtImg
    v-if="renderedSource"
    :key="renderedSource"
    decoding="async"
    v-bind="forwardedAttrs"
    :src="renderedSource"
    :class="[attrs.class, 'heya-loading-image', { 'is-loading': loading, 'is-failed': failed, 'is-eased': eased }]"
    @load="onLoad"
    @error="onError"
  />
</template>

<style scoped>
@property --heya-image-spinner-angle {
  syntax: '<angle>';
  initial-value: 0deg;
  inherits: false;
}

.heya-loading-image.is-loading {
  --heya-image-spinner-angle: 0deg;
  background-color: var(--bg-3, #151515);
  background-image:
    radial-gradient(circle at center, var(--bg-3, #151515) 0 8px, transparent 9px),
    conic-gradient(from var(--heya-image-spinner-angle) at center, transparent 0 22%, var(--gold, #c8a84e) 23% 48%, transparent 49% 100%);
  background-position: center;
  background-repeat: no-repeat;
  background-size: 30px 30px;
  /* Grace period before the spinner shows: cache hits and fast responses
     resolve inside it, so virtualized rails don't strobe spinners while
     scrolling — only genuinely slow images ever surface one. */
  opacity: 0;
  animation:
    heya-image-spinner-appear 0.2s ease 0.35s forwards,
    heya-image-spinner 0.85s linear infinite;
}

/* Slow-loaded pixels ease in instead of snapping over the spinner. Fast loads
   (cache hits) skip the ease entirely — see FAST_LOAD_MS. `from`-only
   keyframes interpolate to the element's own computed opacity, so parents
   that keep the image hidden (crossfade layers at opacity 0) stay hidden. */
.heya-loading-image.is-eased:not(.is-loading) {
  animation: heya-image-fade-in 0.22s ease;
}

@keyframes heya-image-spinner {
  to { --heya-image-spinner-angle: 360deg; }
}
@keyframes heya-image-spinner-appear {
  to { opacity: 1; }
}
@keyframes heya-image-fade-in {
  from { opacity: 0; }
}

@media (prefers-reduced-motion: reduce) {
  .heya-loading-image.is-loading { animation: heya-image-spinner-appear 0s 0.35s forwards; }
  .heya-loading-image:not(.is-loading) { animation: none; }
}
</style>
