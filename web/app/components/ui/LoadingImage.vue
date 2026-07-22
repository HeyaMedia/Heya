<script lang="ts">
// Offscreen images must not animate. `loading="lazy"` images that scroll out
// before the browser fetches them never fire load/error, so `is-loading`
// (and its spinner keyframes) would otherwise run forever — dozens of
// perpetual conic-gradient repaints measured at ~12% of a core on /music.
// One module-shared observer toggles `is-offscreen`, whose only effect is
// `animation-play-state: paused` — a paused spinner costs nothing per frame
// and resumes exactly where it left off when scrolled back into view.
let sharedObserver: IntersectionObserver | null = null
const observedTargets = new WeakMap<Element, (visible: boolean) => void>()

// Vue route changes remount image elements even when Pinia Colada paints the
// surrounding page from a warm cache. The browser still has those immutable
// image responses, but a fresh LoadingImage instance used to hide every one
// until its new `load` event fired. On phones that made a cached Back action
// replay the whole rail/hero pop-in sequence.
//
// Remember successful *request variants* (source + NuxtImg sizing hints), then
// let the browser paint them natively on later mounts. This stores only small
// strings, not decoded pixels or blobs; the browser remains the actual cache.
const loadedRequestKeys = new Set<string>()
const MAX_LOADED_REQUEST_KEYS = 2500

function rememberLoadedRequest(key: string) {
  if (!key || loadedRequestKeys.has(key)) return
  loadedRequestKeys.add(key)
  if (loadedRequestKeys.size <= MAX_LOADED_REQUEST_KEYS) return
  for (const stale of [...loadedRequestKeys].slice(0, 250)) loadedRequestKeys.delete(stale)
}

function observeVisibility(el: Element, set: (visible: boolean) => void) {
  if (typeof IntersectionObserver === 'undefined') return
  sharedObserver ??= new IntersectionObserver((entries) => {
    for (const entry of entries) observedTargets.get(entry.target)?.(entry.isIntersecting)
  })
  observedTargets.set(el, set)
  sharedObserver.observe(el)
}

function unobserveVisibility(el: Element) {
  observedTargets.delete(el)
  sharedObserver?.unobserve(el)
}
</script>

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
const railMoving = inject<Readonly<Ref<boolean>> | null>('heya:rail-moving', null)
const resolvedSource = ref('')
const loading = ref(false)
const failed = ref(false)
const eased = ref(false)
const waitingForRailIdle = ref(false)
let generation = 0
let objectURL = ''
let controller: AbortController | null = null
let startedAt = 0

const offscreen = ref(false)
let observedEl: Element | null = null
let visibilityResolvers: Array<() => void> = []

function resolveVisibilityWaiters() {
  const resolvers = visibilityResolvers
  visibilityResolvers = []
  for (const resolve of resolvers) resolve()
}

// Resolves once the image is (back) in the viewport — event-driven, so parked
// materialize loops cost zero timers while offscreen.
function untilVisible(current: number) {
  if (!offscreen.value || current !== generation) return Promise.resolve()
  return new Promise<void>((resolve) => { visibilityResolvers.push(resolve) })
}

// The `:key` on NuxtImg recreates the <img> whenever the source changes, so a
// function ref (re-)observes each incarnation rather than only the first.
function trackImgEl(instance: unknown) {
  const el = (instance as { $el?: Element } | null)?.$el ?? null
  if (el === observedEl) return
  if (observedEl) unobserveVisibility(observedEl)
  observedEl = el instanceof Element ? el : null
  if (observedEl) {
    observeVisibility(observedEl, (visible) => {
      offscreen.value = !visible
      if (visible) resolveVisibilityWaiters()
    })
    // A memory/disk-cache hit can already be complete when Vue assigns the
    // function ref. Clear the JS loading treatment before the first paint;
    // the browser has decoded pixels ready and does not need our reveal gate.
    if (!waitingForRailIdle.value
      && !fetchPersistentSource.value
      && observedEl instanceof HTMLImageElement
      && observedEl.complete
      && observedEl.naturalWidth > 0) {
      rememberLoadedRequest(requestKey.value)
      loading.value = false
      failed.value = false
      eased.value = false
    }
  }
}

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
const requestKey = computed(() => JSON.stringify([
  canonicalSource.value,
  attrs.width ?? '',
  attrs.height ?? '',
  attrs.quality ?? '',
  attrs.densities ?? '',
  attrs.sizes ?? '',
  attrs.format ?? '',
  import.meta.client ? window.devicePixelRatio : 1,
]))

const renderedSource = computed(() => {
  if (waitingForRailIdle.value) return transparentPixel
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
    // Don't burn network polling 202s for images nobody can see; the loop
    // resumes the moment the element scrolls into the viewport.
    await untilVisible(current)
    if (current !== generation) return
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
  const remembered = !fetchPersistentSource.value && loadedRequestKeys.has(requestKey.value)
  waitingForRailIdle.value = !!props.src && !remembered && !!railMoving?.value
  controller?.abort()
  controller = import.meta.client ? new AbortController() : null
  releaseObjectURL()
  resolvedSource.value = fetchPersistentSource.value ? '' : canonicalSource.value
  failed.value = false
  eased.value = false
  loading.value = !!props.src && !remembered
  startedAt = performance.now()
  if (props.src && fetchPersistentSource.value && import.meta.client && !waitingForRailIdle.value) {
    void materialize(props.src, current)
  }
}

function onLoad(event: Event | string) {
  // The transparent pixel is only a stable layout surface while fetch polling
  // or while a rail fling postpones image decode/texture work.
  if (waitingForRailIdle.value || (fetchPersistentSource.value && loading.value)) return
  const remembered = loadedRequestKeys.has(requestKey.value)
  if (!fetchPersistentSource.value) rememberLoadedRequest(requestKey.value)
  eased.value = !remembered && performance.now() - startedAt > FAST_LOAD_MS
  loading.value = false
  failed.value = false
  emit('load', event)
}

function onError(event: Event | string) {
  if (waitingForRailIdle.value || fetchPersistentSource.value) return
  loading.value = false
  failed.value = true
  emit('error', event)
}

watch(() => [props.src, props.persistent], begin, { immediate: true })
watch(() => railMoving?.value ?? false, (moving) => {
  if (moving) {
    // Only pause unresolved work. A successful image stays mounted and keeps
    // scrolling as a compositor texture; there is no flash or re-request.
    if (!loading.value || !props.src || loadedRequestKeys.has(requestKey.value)) return
    waitingForRailIdle.value = true
    generation++
    controller?.abort()
    return
  }
  if (waitingForRailIdle.value) begin()
})
onBeforeUnmount(() => {
  generation++
  controller?.abort()
  releaseObjectURL()
  if (observedEl) unobserveVisibility(observedEl)
  observedEl = null
  resolveVisibilityWaiters()
})
</script>

<template>
  <NuxtImg
    v-if="renderedSource"
    :key="renderedSource"
    :ref="trackImgEl"
    decoding="async"
    v-bind="forwardedAttrs"
    :src="renderedSource"
    :class="[attrs.class, 'heya-loading-image', { 'is-loading': loading, 'is-failed': failed, 'is-eased': eased, 'is-offscreen': offscreen, 'is-rail-deferred': waitingForRailIdle }]"
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

/* Offscreen loading spinners freeze — a paused animation produces no frames,
   so a rail full of lazy images that never load stops costing paint. Only the
   is-loading state pauses: the one-shot is-eased fade must keep running even
   offscreen, because pausing a `from { opacity: 0 }` animation would freeze an
   already-loaded image at fully invisible until it crosses the viewport. */
.heya-loading-image.is-loading.is-offscreen {
  animation-play-state: paused;
}

/* During a fling the transparent stand-in is deliberately inert: no image
   decode, no texture upload and no spinner repaint competing with scrolling. */
.heya-loading-image.is-loading.is-rail-deferred {
  opacity: 0;
  background-image: none;
  animation: none;
}

@media (prefers-reduced-motion: reduce) {
  .heya-loading-image.is-loading { animation: heya-image-spinner-appear 0s 0.35s forwards; }
  .heya-loading-image:not(.is-loading) { animation: none; }
}
</style>
