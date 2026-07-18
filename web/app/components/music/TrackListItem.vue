<template>
  <AppContextMenu :items="contextItems(track, index)">
    <div
      class="tl-row tl-track"
      :class="{ 'tl-active': active, 'tl-missing': track.available === false, 'tl-phone-row': isPhone }"
      :style="!isPhone ? { gridTemplateColumns } : undefined"
      :draggable="!isCoarse"
      :role="track.available === false ? undefined : 'button'"
      :tabindex="track.available === false ? -1 : 0"
      :aria-label="track.available === false ? undefined : `Play ${track.title}`"
      @click="onRowClick"
      @dblclick="onRowClick"
      @keydown="onRowKeydown"
      @dragstart="onDragStart($event, { kind: 'track', track: { id: track.id, title: track.title } })"
      @dragend="onDragEnd"
    >
      <template v-if="!isPhone">
        <div
          v-for="col in columns"
          :key="col.key"
          class="tl-cell"
          :class="[`tl-c-${col.kind}`, { 'tl-title-inline-art': col.kind === 'title' && col.inlineArt }]"
          @click="col.kind === 'rating' ? $event.stopPropagation() : undefined"
        >
          <template v-if="col.kind === 'index'">
            {{ displayIndex(index) }}
          </template>

          <template v-else-if="col.kind === 'art'">
            <VuMeter v-if="vuMeterIn === 'art' && active" :playing="playing" />
            <template v-else>
              <LoadingImage :src="track.poster ?? ''" :alt="track.album" :width="112" :quality="80" densities="1x 2x" loading="lazy" />
              <div v-if="track.available !== false" class="tl-art-play"><Icon name="play" :size="artPlayIconSize" /></div>
              <div v-else class="tl-art-missing" title="Missing on disk"><Icon name="trash" :size="artPlayIconSize" /></div>
            </template>
          </template>

          <template v-else-if="col.kind === 'title'">
            <template v-if="col.inlineArt">
              <VuMeter v-if="vuMeterIn === 'title' && active" :playing="playing" />
              <Poster
                v-else
                :idx="track.id"
                :src="track.poster ?? null"
                aspect="1/1"
                class="tl-title-thumb"
                :style="{ width: `${col.inlineArtSize ?? 40}px`, height: `${col.inlineArtSize ?? 40}px` }"
              />
            </template>
            <div class="tl-title-text">
              <div class="tl-title">{{ track.title }}</div>
              <!-- Full credit string when it says more than the artist alone
                   ("A feat. B") — the artist link keeps its target, the
                   join/feat remainder rides as plain text after it. A
                   visible dedicated Artist column suppresses the subtitle
                   (no point saying it twice — plexify's rule). -->
              <template v-if="!hasArtistColumn">
                <NuxtLink
                  v-if="col.subtitle === 'artist-link' && track.artist_slug"
                  :to="`/music/artist/${track.artist_slug}`"
                  class="tl-artist tl-artist-link"
                  @click.stop
                >{{ track.artist }}<template v-if="creditRemainder"><span class="tl-feat">{{ creditRemainder }}</span></template></NuxtLink>
                <div v-else-if="col.subtitle === 'artist-plain'" class="tl-artist tl-artist-plain">{{ subtitleArtist }}</div>
                <div v-else-if="col.subtitle === 'artist-album-year'" class="tl-artist tl-artist-combo">{{ subtitleFull }}</div>
              </template>
            </div>
          </template>

          <template v-else-if="col.kind === 'artist'">
            <NuxtLink
              v-if="track.artist_slug"
              :to="`/music/artist/${track.artist_slug}`"
              class="tl-album-link"
              @click.stop
            >{{ track.artist }}</NuxtLink>
            <span v-else class="tl-album-link tl-album-plain">{{ track.artist }}</span>
          </template>

          <template v-else-if="col.kind === 'album'">
            <NuxtLink
              v-if="col.linkAlbum !== false && track.artist_slug && track.album_slug"
              :to="`/music/artist/${track.artist_slug}/${track.album_slug}`"
              class="tl-album-link"
              @click.stop
            >{{ track.album }}</NuxtLink>
            <span v-else class="tl-album-link tl-album-plain">{{ track.album }}</span>
          </template>

          <template v-else-if="col.kind === 'year'">{{ track.album_year || '—' }}</template>

          <template v-else-if="col.kind === 'rating'">
            <ReactionControl :model-value="track.rating ?? 0" size="sm" @update:model-value="(v) => onRatingChange?.(track.id, v)" />
          </template>

          <template v-else-if="col.kind === 'duration'">{{ durationFormatter(track.duration) }}</template>

          <template v-else-if="col.kind === 'meta'">
            <span :title="col.tooltip?.(track) || undefined">{{ col.format?.(track) ?? '' }}</span>
          </template>

          <template v-else-if="col.kind === 'custom'">
            <slot :name="`cell-${col.key}`" :track="track" :index="index" :active="active" />
          </template>
        </div>
      </template>

      <template v-else>
        <div class="tl-phone-thumb">
          <LoadingImage v-if="hasArt" :src="track.poster ?? ''" :alt="track.album" :width="112" :quality="80" densities="1x 2x" loading="lazy" />
          <span v-else class="tl-phone-idx">{{ displayIndex(index) }}</span>
        </div>
        <div class="tl-phone-main">
          <div class="tl-title tl-phone-title">{{ track.title }}</div>
          <div class="tl-phone-sub">{{ subtitlePhone }}</div>
        </div>
        <div class="tl-phone-right">
          <div class="tl-phone-dur">{{ durationFormatter(track.duration) }}</div>
          <div v-if="track.quality" class="tl-phone-quality">{{ track.quality }}</div>
        </div>
        <button type="button" class="tl-phone-more" aria-label="More actions" @click.stop="emit('open-sheet', track, index)">
          <Icon name="more" :size="18" />
        </button>
      </template>
    </div>
  </AppContextMenu>
</template>

<script setup lang="ts">
import type { ContextMenuItem } from '~~/shared/types'
import type { TrackListColumn, TrackListRow } from './TrackList.vue'

const props = defineProps<{
  track: TrackListRow
  index: number
  columns: TrackListColumn[]
  gridTemplateColumns: string
  contextItems: (track: TrackListRow, index: number) => ContextMenuItem[]
  active: boolean
  playing: boolean
  isPhone: boolean
  isCoarse: boolean
  hasArt: boolean
  vuMeterIn: 'art' | 'title' | 'none'
  displayIndex: (index: number) => number | string
  onRatingChange?: (id: number, value: number) => void
  artPlayIconSize: number
  durationFormatter: (seconds: number) => string
  onDragStart: (event: DragEvent, payload: { kind: 'track', track: { id: number, title: string } }) => void
  onDragEnd: () => void
}>()

const emit = defineEmits<{
  'row-click': [index: number]
  'open-sheet': [track: TrackListRow, index: number]
}>()

// A dedicated Artist column supersedes the title-cell artist subtitle.
const hasArtistColumn = computed(() => props.columns.some((c) => c.kind === 'artist'))

// Full credit string ("A feat. B") when it says more than the bare artist;
// falls back to the artist name.
const subtitleArtist = computed(() => {
  const d = props.track.artists_display
  return d && d !== props.track.artist ? d : props.track.artist
})

// The credit tail past the primary artist's name (" feat. B") — rendered
// as plain text after the artist link so the link target stays the artist.
const creditRemainder = computed(() => {
  const d = props.track.artists_display
  if (!d || d === props.track.artist || !d.startsWith(props.track.artist)) return ''
  return d.slice(props.track.artist.length)
})

const subtitleFull = computed(() => {
  let value = subtitleArtist.value
  if (props.track.album) value += ` · ${props.track.album}`
  if (props.track.album_year) value += ` · ${props.track.album_year}`
  return value
})

const subtitlePhone = computed(() => props.track.album
  ? `${subtitleArtist.value} · ${props.track.album}`
  : subtitleArtist.value)

function onRowClick() {
  if (props.track.available === false) return
  emit('row-click', props.index)
}

// Keyboard mirror of the row's @click — the row itself is the play control
// (playbook item 1). Guard on target===currentTarget so Enter/Space pressed
// on a nested focusable (artist/album link, star rating, phone "more" button)
// doesn't ALSO fire this row's action via bubbling.
function onRowKeydown(e: KeyboardEvent) {
  if (e.target !== e.currentTarget) return
  if (e.key !== 'Enter' && e.key !== ' ') return
  e.preventDefault()
  onRowClick()
}
</script>
