<template>
  <div class="mea">
    <div v-if="!albums.length" class="mea-empty">
      <Icon name="music" :size="28" />
      <span>No albums for this artist.</span>
    </div>
    <div v-else class="mea-list">
      <div v-for="al in albums" :key="al.id" class="mea-row">
        <NuxtImg v-if="coverUrl(al)" :src="coverUrl(al)!" class="mea-cover" :width="240" :quality="80" densities="1x 2x" loading="lazy" />
        <div v-else class="mea-cover mea-cover-empty">
          <Icon name="music" :size="16" />
        </div>
        <div class="mea-info">
          <div class="mea-title-line">
            <span class="mea-title">{{ al.title }}</span>
            <span v-if="al.year" class="mea-year">{{ al.year }}</span>
            <span v-if="al.album_type && al.album_type !== 'album'" class="mea-type">{{ al.album_type }}</span>
          </div>
          <div class="mea-sub">
            <span v-if="al.label" class="mea-label-text">{{ al.label }}</span>
            <span v-if="al.heya_id" class="mea-heya" :title="al.heya_id">
              <Icon name="check" :size="11" /> Heya
            </span>
            <span v-else class="mea-heya mea-heya-missing">
              <Icon name="warning" :size="11" /> Not linked to Heya
            </span>
          </div>
        </div>
        <div class="mea-actions">
          <button class="btn btn-ghost-sm" title="Edit album fields" @click="editAlbum = al">
            <Icon name="pencil" :size="13" /> Edit
          </button>
          <button class="btn btn-ghost-sm" title="Choose a different Heya release" @click="identifyAlbum = al">
            <Icon name="search" :size="13" /> Identify
          </button>
        </div>
      </div>
    </div>

    <MetadataAlbumEditDialog
      :album="editAlbum"
      :show="!!editAlbum"
      @saved="onSaved"
      @identify="identifyAlbum = editAlbum; editAlbum = null"
      @close="editAlbum = null"
    />
    <MetadataAlbumIdentifyDialog
      :album="identifyAlbum"
      :show="!!identifyAlbum"
      @applied="onIdentified"
      @close="identifyAlbum = null"
    />
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  albums: any[]
  artistSlug: string
}>()

const emit = defineEmits<{ refresh: [] }>()

const editAlbum = ref<any | null>(null)
const identifyAlbum = ref<any | null>(null)

function coverUrl(al: any): string | null {
  return useAlbumCoverUrl(props.artistSlug, al.slug)
}

function onSaved() {
  editAlbum.value = null
  emit('refresh')
}

function onIdentified() {
  identifyAlbum.value = null
  emit('refresh')
}
</script>

<style scoped>
.mea {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.mea-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 64px 0;
  color: var(--fg-3);
  font-size: 14px;
}

.mea-list {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-1);
  overflow: hidden;
}

.mea-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 10px 14px;
  border-bottom: 1px solid rgb(var(--ink) / 0.04);
}
.mea-row:last-child {
  border-bottom: none;
}
.mea-row:hover {
  background: rgb(var(--ink) / 0.02);
}

.mea-cover {
  width: 44px;
  height: 44px;
  border-radius: var(--r-sm);
  object-fit: cover;
  flex-shrink: 0;
  background: var(--bg-3);
}
.mea-cover-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
}

.mea-info {
  flex: 1;
  min-width: 0;
}

.mea-title-line {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
}

.mea-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mea-year {
  font-size: 12px;
  color: var(--fg-2);
  flex-shrink: 0;
}

.mea-type {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  padding: 1px 6px;
  border-radius: 4px;
  background: rgb(var(--ink) / 0.07);
  color: var(--fg-2);
  flex-shrink: 0;
}

.mea-sub {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 2px;
  font-size: 11px;
  color: var(--fg-3);
}

.mea-label-text {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mea-heya {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: rgb(110, 190, 130);
  flex-shrink: 0;
}
.mea-heya-missing {
  color: rgb(220, 170, 90);
}

.mea-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
</style>
