<template>
  <div v-if="session" class="cast-video-mini" @click="emit('expand')">
    <div class="cvm-progress" :style="{ width: `${progress}%` }" />
    <div class="cvm-art" :style="artworkStyle"><Icon name="television-simple" :size="20" /></div>
    <div class="cvm-info">
      <strong>{{ session.title || 'Video' }}</strong>
      <span><Icon name="cast" :size="11" /> {{ transportLabel }} · {{ session.device_name }}</span>
    </div>
    <div class="cvm-controls">
      <button type="button" :aria-label="playing ? 'Pause' : 'Play'" @click.stop="togglePlayback">
        <Icon :name="busy ? 'loading' : (playing ? 'pause' : 'play')" :size="20" :class="{ 'cvm-spin': busy }" />
      </button>
      <button type="button" aria-label="Forward 10 seconds" @click.stop="skipForward"><Icon name="skipforward" :size="18" /></button>
    </div>
  </div>
</template>

<script setup lang="ts">
const emit = defineEmits<{ expand: [] }>()
const cast = useCastStore()
const { toast } = useToast()
const session = computed(() => cast.session?.media_kind === 'video' ? cast.session : null)
const playing = computed(() => session.value?.state === 'playing' || session.value?.state === 'starting')
const busy = computed(() => cast.connecting || session.value?.state === 'starting')
const transportLabel = computed(() => cast.isClientDevice ? 'HeyaConnect' : 'Chromecast')
const tick = ref(0)
const progress = computed(() => {
  tick.value
  const duration = session.value?.duration_sec ?? 0
  return duration > 0 ? Math.max(0, Math.min(100, (cast.livePositionSec() / duration) * 100)) : 0
})
const artworkStyle = computed(() => session.value?.media_item_id
  ? { backgroundImage: `url(${useBackdropUrl(session.value.media_item_id)})` }
  : {})
let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => { timer = setInterval(() => { tick.value++ }, 500) })
onScopeDispose(() => { if (timer) clearInterval(timer) })

async function togglePlayback() {
  if (busy.value) return
  try {
    if (playing.value) await cast.pause()
    else await cast.resume()
  } catch (error) {
    toast.err(error instanceof Error ? error.message : 'Could not control remote playback')
  }
}
function skipForward() {
  const duration = session.value?.duration_sec ?? Number.MAX_SAFE_INTEGER
  void cast.seekTo(Math.min(duration, cast.livePositionSec() + 10)).catch(() => toast.err('Could not seek remote playback'))
}
</script>

<style scoped>
.cast-video-mini {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  height: 64px;
  padding: 0 10px;
  border-top: 1px solid var(--border);
  background: var(--bg-2);
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
}
.cvm-progress {
  position: absolute;
  top: 0;
  left: 0;
  height: 2px;
  background: var(--gold);
  transition: width 0.2s linear;
}
.cvm-art {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  border-radius: var(--r-sm);
  background-color: var(--bg-3);
  background-position: center;
  background-size: cover;
  color: var(--fg-2);
  box-shadow: inset 0 0 0 1px var(--border);
}
.cvm-info { display: flex; flex: 1; min-width: 0; flex-direction: column; gap: 3px; }
.cvm-info strong,
.cvm-info span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cvm-info strong { color: var(--fg-0); font-size: 13px; font-weight: 600; }
.cvm-info span { display: flex; align-items: center; gap: 4px; color: var(--fg-3); font-size: 11px; }
.cvm-controls { display: flex; flex-shrink: 0; }
.cvm-controls button {
  display: inline-grid;
  place-items: center;
  width: 44px;
  height: 44px;
  border: 0;
  background: transparent;
  color: var(--fg-0);
  cursor: pointer;
}
.cvm-controls button:active { color: var(--gold); }
.cvm-spin { animation: cvm-spin 0.9s linear infinite; }
@keyframes cvm-spin { to { transform: rotate(360deg); } }
</style>
