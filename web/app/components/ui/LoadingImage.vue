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
let generation = 0
let objectURL = ''
let controller: AbortController | null = null

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
  loading.value = !!props.src
  if (props.src && fetchPersistentSource.value && import.meta.client) void materialize(props.src, current)
}

function onLoad(event: Event | string) {
  // The transparent pixel is only a stable layout surface while fetch polling.
  if (fetchPersistentSource.value && loading.value) return
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
    v-bind="forwardedAttrs"
    :src="renderedSource"
    :class="[attrs.class, 'heya-loading-image', { 'is-loading': loading, 'is-failed': failed }]"
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
  animation: heya-image-spinner 0.85s linear infinite;
}

@keyframes heya-image-spinner {
  to { --heya-image-spinner-angle: 360deg; }
}
</style>
