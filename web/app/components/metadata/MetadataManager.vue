<template>
  <div class="mm" :class="{ 'mm-modal': !!fixedMediaId }">
    <MetadataColumnBrowser
      v-if="!fixedMediaId"
      @select-media="onSelectMedia"
      @select-season="onSelectSeason"
      @select-episode="onSelectEpisode"
    />
    <MetadataEditor
      :media-id="activeMediaId"
      :season-id="activeSeasonId"
      :episode-id="activeEpisodeId"
      @close="$emit('close')"
    />
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  fixedMediaId?: number
  fixedSeasonId?: number | null
  fixedEpisodeId?: number | null
}>()

defineEmits<{ close: [] }>()

const selectedMediaId = ref<number | null>(null)
const selectedSeasonId = ref<number | null>(null)
const selectedEpisodeId = ref<number | null>(null)

const activeMediaId = computed(() => props.fixedMediaId || selectedMediaId.value)
const activeSeasonId = computed(() => props.fixedMediaId ? (props.fixedSeasonId ?? null) : selectedSeasonId.value)
const activeEpisodeId = computed(() => props.fixedMediaId ? (props.fixedEpisodeId ?? null) : selectedEpisodeId.value)

function onSelectMedia(id: number) {
  selectedMediaId.value = id
  selectedSeasonId.value = null
  selectedEpisodeId.value = null
}

function onSelectSeason(_mediaId: number, seasonId: number) {
  selectedSeasonId.value = seasonId
  selectedEpisodeId.value = null
}

function onSelectEpisode(_mediaId: number, episodeId: number) {
  selectedEpisodeId.value = episodeId
}
</script>

<style scoped>
.mm {
  display: flex;
  height: 100%;
  background: var(--bg-2);
}
</style>
