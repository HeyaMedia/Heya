<!--
  ResumeDialog — opens when the user clicks a Continue Watching tile, asks
  whether to resume from the saved position or start over. Each choice
  routes into /watch/{file_id} with the appropriate `t=` query param so the
  player can seek on canplay.

  The tile that opens this dialog already has the resume position + file
  id, so this stays a pure presentational component — no fetching of its
  own, just two buttons that call back.
-->
<template>
  <AppDialog v-model="open" :title="title" size="sm">
    <p class="rd-sub">{{ subline }}</p>
    <div class="rd-progress">
      <div class="rd-progress-bar"><div class="rd-progress-fill" :style="{ width: progressPct + '%' }" /></div>
      <div class="rd-progress-label mono">{{ progressLabel }} / {{ totalLabel }}</div>
    </div>
    <template #footer>
      <button class="btn btn-secondary" @click="open = false">Cancel</button>
      <button class="btn btn-secondary" @click="onStartOver">
        <Icon name="rewind" :size="14" /> Start over
      </button>
      <button class="btn btn-primary" @click="onResume">
        <Icon name="play" :size="14" /> Resume at {{ progressLabel }}
      </button>
    </template>
  </AppDialog>
</template>

<script setup lang="ts">
const open = defineModel<boolean>({ required: true })

const props = defineProps<{
  fileId: number
  mediaItemId: number
  title: string
  progressSeconds: number
  totalSeconds: number
  /** Subtitle shown above the progress bar, e.g. "S02E03 · The Long Night". */
  subline?: string
  /** "movie" | "episode" — drives backend session display formatting. */
  entityType?: string
  /** For episode: the episode_id. For movie: optional, defaults to mediaItemId. */
  entityId?: number
}>()

const progressPct = computed(() => {
  if (props.totalSeconds <= 0) return 0
  return Math.min(100, Math.round((props.progressSeconds / props.totalSeconds) * 100))
})

function formatTime(s: number): string {
  const total = Math.max(0, Math.floor(s))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const sec = total % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
  return `${m}:${String(sec).padStart(2, '0')}`
}

const progressLabel = computed(() => formatTime(props.progressSeconds))
const totalLabel = computed(() => formatTime(props.totalSeconds))

function navigate(t: number) {
  if (!props.fileId) return
  const params = new URLSearchParams({
    media_item_id: String(props.mediaItemId),
    title: props.title,
  })
  if (t > 0) params.set('t', String(t))
  // entity_type/entity_id flow through so the now-playing session can
  // pick the right title shape (series + S/E for episodes, etc.).
  if (props.entityType) params.set('entity_type', props.entityType)
  if (props.entityId) params.set('entity_id', String(props.entityId))
  open.value = false
  navigateTo(`/watch/${props.fileId}?${params}`)
}

function onResume() {
  navigate(props.progressSeconds)
}

function onStartOver() {
  navigate(0)
}
</script>

<style scoped>
.rd-sub { color: var(--fg-2); font-size: 13px; margin-bottom: 14px; }
.rd-progress { display: flex; flex-direction: column; gap: 6px; padding: 4px 0 8px; }
.rd-progress-bar {
  height: 6px;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 999px;
  overflow: hidden;
}
.rd-progress-fill {
  height: 100%;
  background: var(--gold);
  border-radius: 999px;
}
.rd-progress-label { font-size: 11px; color: var(--fg-3); align-self: flex-end; }
.mono { font-family: var(--font-mono); }

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-radius: var(--r-md);
  font-size: 13px;
  cursor: pointer;
  font-family: inherit;
  border: 1px solid transparent;
  transition: background 0.15s, border-color 0.15s;
}
.btn-secondary {
  background: var(--bg-2);
  border-color: var(--border);
  color: var(--fg-1);
}
.btn-secondary:hover { background: var(--bg-3); }
.btn-primary {
  background: var(--gold);
  color: var(--bg-0);
  font-weight: 600;
  border-color: var(--gold);
}
.btn-primary:hover { filter: brightness(1.1); }
</style>
