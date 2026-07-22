<template>
  <div v-if="loading" class="m-state">Loading…</div>
  <div v-else-if="!artist" class="m-state">Artist not found.</div>

  <!-- Tone vars are published on the page root (not a scroll root — the music
       shell owns that), mirroring the movie/TV ports + the playbar's
       --pb-accent. Every descendant inherits --tone/--tone-rgb/--tone-ink. The
       Playbar keeps its own track-following --pb-accent untouched. -->
  <div v-else class="artist2 hero-flush" :style="toneStyle">

    <!-- ── HERO: full-bleed backdrop as sharp art, hard-clipped at the ledger
         seam. HeroCanvas also publishes the shared hero art claim to the global
         AmbientBackdrop, so the blurred underlay mirrors this artist's backdrop
         and pops back to the music pool on unmount. Artists WITHOUT a backdrop
         asset hand the claim to the script below instead (the backdrop URL
         would 404 and leave the whole page with no ambient wash at all). ── -->
    <section class="hero-section artist-hero">
      <HeroCanvas :src="backdropA || ''" :src-b="backdropB" :show-a="showA" object-position="center 22%" :claim="backdropAssets.length > 0" />

      <!-- Backdrop tools — expand-to-lightbox + the shared prev/pause/next
           ring together, top-right (same cluster as the movie/TV heroes). -->
      <div v-if="backdropAssets.length > 0" class="hero-tools">
        <button class="hero-expand" aria-label="Expand backdrop" @click="openBackdropLightbox">
          <Icon name="expand" :size="13" />
        </button>
        <CycleControls
          v-if="backdropAssets.length > 1"
          v-model:paused="carouselPaused"
          :cycle-key="cycleKey"
          :duration="BACKDROP_INTERVAL"
          item-label="backdrop"
          @prev="retreatBackdrop"
          @next="advanceBackdrop"
        />
      </div>

      <div class="hero-inner">
        <div class="grow hero-ink">
          <div class="eyebrow">
            <span>Artist</span>
            <template v-if="artistTypeLabel">
              <span class="sep">&middot;</span><span>{{ artistTypeLabel }}</span>
            </template>
            <template v-if="originLabel">
              <span class="sep">&middot;</span><span>{{ originLabel }}</span>
            </template>
          </div>

          <h1 v-if="logoUrl && !logoFailed" class="title artist title-logo-wrap">
            <LoadingImage :src="logoUrl" :alt="artist.name" class="title-logo" :width="640" @error="logoFailed = true" />
          </h1>
          <h1 v-else class="title artist">{{ artist.name }}</h1>

          <div
            v-if="heroAliases"
            class="hero-aka"
            :title="`Also known as: ${artist.aliases!.join(', ')}`"
          >
            <span class="hero-aka-label">a.k.a.</span> {{ heroAliases }}
          </div>

          <p class="metaline">
            <span v-if="lifecycleLabel">{{ lifecycleLabel }}</span>
            <template v-if="statusLabel">
              <span class="dot">&middot;</span><span class="status">{{ statusLabel }}</span>
            </template>
            <template v-if="heroPills.length > 0">
              <span class="dot">&middot;</span>
              <NuxtLink
                v-for="tag in heroPills"
                :key="tag"
                :to="`/music/browse/genre/${encodeURIComponent(tag)}`"
                class="genre"
              >{{ tag }}</NuxtLink>
              <button
                v-if="heroPillOverflow > 0"
                class="genre genre-more"
                :title="pillsExpanded ? 'Show fewer' : heroPillsAll.join(' · ')"
                :aria-expanded="pillsExpanded"
                @click="pillsExpanded = !pillsExpanded"
              >{{ pillsExpanded ? '− less' : `+${heroPillOverflow}` }}</button>
            </template>
          </p>

          <!-- Overflow genres — slide out in their own row: the 0fr→1fr grid
               row animates the height (nudging the identity block above),
               each pill staggers in from the left. -->
          <div v-if="heroPillOverflow > 0" class="genre-flyout" :class="{ open: pillsExpanded }" :aria-hidden="!pillsExpanded">
            <div class="genre-flyout-inner">
              <NuxtLink
                v-for="(tag, i) in heroPillsOverflowList"
                :key="tag"
                :to="`/music/browse/genre/${encodeURIComponent(tag)}`"
                class="genre"
                :style="{ '--i': i }"
                :tabindex="pillsExpanded ? 0 : -1"
              >{{ tag }}</NuxtLink>
            </div>
          </div>

          <div class="actions">
            <span v-if="!artistPlayable" class="missing"><Icon name="trash" :size="13" /> Missing on disk</span>

            <div class="primary-actions">
              <button class="btn-play" :disabled="!artistPlayable" @click="playAll(false)">
                <span class="tri" /> Play
                <small v-if="playableTrackCount">{{ playableTrackCount }} TRACKS</small>
              </button>

              <div class="hero-rating" @click.stop>
                <ReactionControl
                  :model-value="artistRatings.get(artist.id) ?? 0"
                  size="sm"
                  @update:model-value="(v) => onRateArtist(artist!.id, v)"
                />
              </div>
            </div>

            <button class="pill" :disabled="!artistPlayable" @click="playAll(true)">
              <Icon name="shuffle" :size="15" /> Shuffle
            </button>
            <button class="pill" :disabled="!artistPlayable" @click="addAllToQueue">
              <Icon name="plus" :size="15" /> Add to queue
            </button>
            <button class="pill" :disabled="radio.starting.value || !artistPlayable" @click="startArtistRadio">
              <Icon name="radio" :size="15" /> Station
            </button>

            <button v-if="isAdmin" class="pill icon hero-edit" title="Edit Metadata" aria-label="Edit metadata" @click="showMetadataEditor = true">
              <Icon name="pencil" :size="15" />
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- ── LEDGER at the hard-clip seam — user-facing facts only. ── -->
    <LedgerStrip :cells="ledgerCells" />

    <!-- ── BODY ── -->
    <main class="page">

      <!-- Popular Tracks: .trk ledger rows, every current row feature kept. -->
      <section v-if="topTracks.length" class="section">
        <SectionHeader class="popular-header" title="Popular Tracks" subtitle="by plays">
          <template #actions>
            <button class="mini-pill" :disabled="!hasPlayableTopTracks" @click="playTopAll(false)">
              <Icon name="play" :size="12" /><span>Play</span>
            </button>
            <button class="mini-pill mini-pill-ghost" :disabled="!hasPlayableTopTracks" @click="playTopAll(true)">
              <Icon name="shuffle" :size="12" /><span>Shuffle</span>
            </button>
            <NuxtLink class="mini-pill mini-pill-ghost" :to="`/music/artist/${route.params.slug}/top-tracks`">
              <Icon name="chart" :size="12" /><span>Full chart</span>
            </NuxtLink>
          </template>
        </SectionHeader>

        <div class="trklist">
          <!-- AppContextMenu is as-child (no wrapper element). Right-click on
               desktop, long-press on touch; phone also gets a visible ⋯
               ActionSheet since the rating widget is hidden there. -->
          <AppContextMenu
            v-for="(t, idx) in topTracks.slice(0, ttExpanded ? topTracks.length : 8)"
            :key="`tt-${t.local_track_id}-${idx}`"
            :items="ttMenuItems(t)"
          >
          <!-- role="button": reka wrappers pointer-capture taps on plain
               elements and retarget the click away from the row — button/a/
               [role=button] targets are exempt. Also honest a11y: the row IS a
               play button. -->
          <div
            class="trk"
            role="button"
            :class="{ 'trk-missing': !isTopTrackPlayable(t), 'trk-active': isTopTrackActive(t) }"
            :draggable="!isCoarse && !!t.local_track_id"
            @click="onTtRowTap(t)"
            @dragstart="t.local_track_id && onDragStart($event, { kind: 'track', track: { id: t.local_track_id, title: t.title } })"
            @dragend="onDragEnd"
          >
            <div class="trk-n">
              <VuMeter v-if="isTopTrackActive(t)" :playing="playing" class="trk-vu" />
              <span v-else-if="isTopTrackPlayable(t)" class="trk-rank">{{ idx + 1 }}</span>
              <Icon v-else name="trash" :size="12" class="trk-missing-icon" :title="`${t.title} — missing on disk`" />
              <!-- .stop: on touch this button is opacity-0 but still hit-
                   testable; without it a tap fires both handlers (double play). -->
              <button
                v-if="isTopTrackPlayable(t) && !isTopTrackActive(t)"
                class="trk-hover-play"
                type="button"
                :title="`Play ${t.title}`"
                @click.stop="playTopTrack(t)"
              >
                <Icon name="play" :size="11" />
              </button>
            </div>

            <!-- Title is NOT a link: anywhere on the row plays the track —
                 only the album name (below) navigates. -->
            <div class="trk-meta">
              <span class="trk-t">{{ t.title }}</span>
            </div>

            <NuxtLink
              v-if="t.local_album_title && t.local_album_slug"
              :to="`/music/artist/${route.params.slug}/${t.local_album_slug}`"
              class="trk-al"
              @click.stop
            >{{ t.local_album_title }}</NuxtLink>
            <span v-else class="trk-al" />

            <div class="trk-stars" @click.stop>
              <ReactionControl
                :model-value="trackRatings.get(t.local_track_id!) ?? 0"
                size="sm"
                @update:model-value="(v) => onRateTrack(t.local_track_id!, v)"
              />
            </div>

            <div class="trk-d">{{ t.local_duration ? formatTime(t.local_duration) : '' }}</div>

            <button
              type="button"
              class="trk-more"
              aria-label="More actions"
              @click.stop="openTtSheet(t)"
            >
              <Icon name="more" :size="18" />
            </button>
          </div>
          </AppContextMenu>
        </div>

        <button v-if="topTracks.length > 8" class="see-all" @click="ttExpanded = !ttExpanded">
          {{ ttExpanded ? 'Show fewer' : `See all ${topTracks.length}` }}
        </button>
      </section>

      <!-- Discography, grouped by release kind (newest-first within group). -->
      <section
        v-for="group in groupedDiscography"
        :key="group.kind"
        class="section"
      >
        <SectionHeader :title="group.label" :subtitle="String(group.albums.length)" />
        <div class="album-grid">
          <AppContextMenu
            v-for="album in group.albums"
            :key="album.id"
            :items="discogMenuItems(album)"
          >
            <!-- MusicCard paints title/meta ON the cover (front-page /music
                 grammar); tint-caption mixes each cover's complement into
                 the title (shared MusicCard recipe). -->
            <NuxtLink
              :to="`/music/artist/${route.params.slug}/${album.slug}`"
              class="album-card"
              :class="{ 'album-active': isAlbumActive(album) }"
              :draggable="!isCoarse"
              @dragstart="onDragStart($event, discogDragPayload(album))"
              @dragend="onDragEnd"
            >
              <MusicCard
                :src="useAlbumCoverUrl(route.params.slug as string, album.slug) ?? undefined"
                :alt="album.title"
                :title="album.title"
                :subtitle="`${album.year || '—'}${album.tracks.length ? ` · ${album.tracks.length} tracks` : ''}`"
                :badge-tl="albumTypeFlag(group.kind)"
                :missing="!albumPlayable(album)"
                :no-play="!albumPlayable(album)"
                :width="416"
                @play="playAlbum(album, false)"
              />
              <div v-if="isAlbumActive(album)" class="album-nowplaying"><VuMeter :playing="playing" /></div>
            </NuxtLink>
          </AppContextMenu>
        </div>
      </section>

      <!-- Music videos — artist-scoped YouTube links (TheAudioDB via
           heya.media). External content, played in the same nocookie-embed
           modal the movie/TV Videos rows use. -->
      <section v-if="musicVideos.length" class="section">
        <SectionHeader title="Music Videos" :subtitle="String(musicVideos.length)">
          <template #actions>
            <div v-if="mvRail?.overflows" class="scroll-controls">
              <AppHoldButton class="scroll-ctrl-btn" aria-label="Scroll left" title="Hold to jump to start" @click="mvRail?.scrollByDir(-1)" @hold="mvRail?.scrollToStart()"><Icon name="chevleft" :size="14" /></AppHoldButton>
              <button class="scroll-ctrl-btn" aria-label="Scroll right" @click="mvRail?.scrollByDir(1)"><Icon name="chevright" :size="14" /></button>
            </div>
          </template>
        </SectionHeader>
        <AppRail ref="mvRail" :items="musicVideos" :tile-width="300" :phone-tile-width="260" aspect="16/9" :gap="16" :phone-gap="12" snap memory-key="artist-music-videos" :item-key="(v: MediaVideo) => v.video_key">
          <template #default="{ item: v, index: i }">
            <button class="video-card" @click="openVideo(v.video_key, v.name, v.description)">
              <MediaCard
                :idx="i"
                :src="`https://img.youtube.com/vi/${v.video_key}/mqdefault.jpg`"
                aspect="16/9"
                :title="v.name"
              >
                <template #badges>
                  <div class="video-play"><Icon name="play" :size="20" /></div>
                </template>
              </MediaCard>
            </button>
          </template>
        </AppRail>
      </section>

      <!-- Sonic similar — local pgvector centroids, circular avatars. -->
      <section v-if="sonicSimilar.length" class="section">
        <SectionHeader title="Sounds Like" :subtitle="String(sonicSimilar.length)">
          <template #actions>
            <div v-if="sonicRail?.overflows" class="scroll-controls">
              <AppHoldButton class="scroll-ctrl-btn" aria-label="Scroll left" title="Hold to jump to start" @click="sonicRail?.scrollByDir(-1)" @hold="sonicRail?.scrollToStart()"><Icon name="chevleft" :size="14" /></AppHoldButton>
              <button class="scroll-ctrl-btn" aria-label="Scroll right" @click="sonicRail?.scrollByDir(1)"><Icon name="chevright" :size="14" /></button>
            </div>
          </template>
        </SectionHeader>
        <AppRail ref="sonicRail" :items="sonicSimilar" :tile-width="150" :phone-tile-width="140" aspect="1/1" :gap="18" :phone-gap="14" snap memory-key="artist-sonic-similar">
          <template #default="{ item: row }">
            <NuxtLink
              :to="`/music/artist/${row.media_slug}`"
              class="sim-tile"
              :title="`${row.name} — cosine distance ${row.distance.toFixed(3)}`"
            >
              <MusicCard
                :src="usePosterUrl({ id: row.media_item_id, public_id: row.media_item_public_id })"
                :alt="row.name"
                :title="row.name"
                subtitle="sonic match"
                no-play
              />
            </NuxtLink>
          </template>
        </AppRail>
      </section>

      <!-- Similar artists — Last.fm + ListenBrainz via heya.media. Gated by the
           same Appearance switch as movie/TV recs; local rows use the library
           portrait (upstream Last.fm images are mostly dead). -->
      <section v-if="visibleSimilar.length" class="section">
        <SectionHeader title="Similar Artists" :subtitle="String(visibleSimilar.length)">
          <template #actions>
            <div v-if="simRail?.overflows" class="scroll-controls">
              <AppHoldButton class="scroll-ctrl-btn" aria-label="Scroll left" title="Hold to jump to start" @click="simRail?.scrollByDir(-1)" @hold="simRail?.scrollToStart()"><Icon name="chevleft" :size="14" /></AppHoldButton>
              <button class="scroll-ctrl-btn" aria-label="Scroll right" @click="simRail?.scrollByDir(1)"><Icon name="chevright" :size="14" /></button>
            </div>
          </template>
        </SectionHeader>
        <AppRail ref="simRail" :items="visibleSimilar" :tile-width="150" :phone-tile-width="140" aspect="1/1" :gap="18" :phone-gap="14" snap memory-key="artist-similar">
          <template #default="{ item: row }">
            <component
              :is="row.local_slug ? NuxtLink : 'div'"
              :to="row.local_slug ? `/music/artist/${row.local_slug}` : undefined"
              class="sim-tile"
              :class="{ 'sim-external': !row.local_slug }"
              :title="row.local_slug ? `Open ${row.name}` : `${row.name} (not in library)`"
            >
              <MusicCard
                :src="row.local_slug ? `/api/media/${row.local_slug}/image/poster` : row.image"
                :alt="row.name"
                :title="row.name"
                :subtitle="providerLabel(row.source)"
                no-play
              />
            </component>
          </template>
        </AppRail>
      </section>

      <!-- About + band lifecycle — two-column (mockup .cols). The RIGHT
           column drives the section height: on wide screens the About
           column absolutely fills the row and the bio clamps to whatever
           height is left over ("cuts off only what it has to"); More
           releases the clamp and the row grows. -->
      <section class="section cols" :class="{ 'cols-linked': colSideVisible && !bioOpen }">
        <div class="col-about">
          <div class="col-about-fill">
            <SectionHeader title="About" />
            <div v-if="cleanBio" class="prose">
              <p>{{ cleanBio }}</p>
            </div>
            <p v-else class="prose-empty">No biography available.</p>
            <button v-if="cleanBio" class="see-all bio-toggle" @click="bioOpen = !bioOpen">
              {{ bioOpen ? 'Less' : 'More' }}
            </button>

            <dl class="detail-grid">
              <div v-if="(artist.metadata_sources?.length ?? 0) > 0">
                <dt>Metadata sources</dt>
                <dd class="src-chips">
                  <span v-for="s in artist.metadata_sources" :key="s" class="src-chip">{{ providerLabel(s) }}</span>
                  <span class="src-via">via heya.media</span>
                </dd>
              </div>
              <div v-if="artist.musicbrainz_id">
                <dt>Library</dt>
                <dd>Music &middot; matched by MusicBrainz ID<br><span class="mbid">{{ artist.musicbrainz_id }}</span></dd>
              </div>
            </dl>
          </div>
        </div>

        <!-- Band lifecycle — compact wrapping chips (was full-width 48px
             avatar rows; big bands like orchestras pushed the About column
             off-screen). Tenure lives in the chip title + a tiny year range. -->
        <div v-if="colSideVisible" class="col-side">
          <template v-if="displayMembers.length > 0">
            <SectionHeader title="Members" :subtitle="String(displayMembers.length)" />
            <div class="member-chips">
              <component
                :is="m.local_slug ? NuxtLink : 'div'"
                v-for="m in visibleMembers"
                :key="`mem-${m.name}`"
                :to="m.local_slug ? `/music/artist/${m.local_slug}` : undefined"
                class="mchip"
                :class="{ 'mchip-linked': !!m.local_slug }"
                :title="memberTitle(m)"
              >
                <Poster v-if="m.local_slug" :idx="0" :src="`/api/media/${m.local_slug}/image/poster`" aspect="1/1" :width="56" class="mchip-av" />
                <span v-else class="mchip-av mchip-av-initials">{{ initials(m.name) }}</span>
                <span class="mchip-nm">{{ m.name }}</span>
                <span v-if="m.begin_year || m.end_year" class="mchip-yrs">{{ m.begin_year || '?' }}–{{ m.end_year || 'now' }}</span>
              </component>
              <button
                v-if="displayMembers.length > MEMBER_CHIP_MAX"
                class="mchip mchip-more"
                @click="membersExpanded = !membersExpanded"
              >{{ membersExpanded ? 'Show fewer' : `+${displayMembers.length - MEMBER_CHIP_MAX} more` }}</button>
            </div>
          </template>

          <template v-if="displayGroups.length > 0">
            <SectionHeader title="Member of" :subtitle="String(displayGroups.length)" :class="{ 'mt-gap': displayMembers.length > 0 }" />
            <div class="member-chips">
              <component
                :is="g.local_slug ? NuxtLink : 'div'"
                v-for="g in visibleGroups"
                :key="`grp-${g.name}`"
                :to="g.local_slug ? `/music/artist/${g.local_slug}` : undefined"
                class="mchip"
                :class="{ 'mchip-linked': !!g.local_slug }"
                :title="memberTitle(g)"
              >
                <Poster v-if="g.local_slug" :idx="0" :src="`/api/media/${g.local_slug}/image/poster`" aspect="1/1" :width="56" class="mchip-av" />
                <span v-else class="mchip-av mchip-av-initials">{{ initials(g.name) }}</span>
                <span class="mchip-nm">{{ g.name }}</span>
                <span v-if="g.begin_year || g.end_year" class="mchip-yrs">{{ g.begin_year || '?' }}–{{ g.end_year || 'now' }}</span>
              </component>
              <button
                v-if="displayGroups.length > MEMBER_CHIP_MAX"
                class="mchip mchip-more"
                @click="groupsExpanded = !groupsExpanded"
              >{{ groupsExpanded ? 'Show fewer' : `+${displayGroups.length - MEMBER_CHIP_MAX} more` }}</button>
            </div>
          </template>

          <!-- Around the web — the old "All links" dropdown dissolved into
               the same chip vocabulary as the member rows above. -->
          <template v-if="allLinks.length > 0 || hasExternalIds">
            <SectionHeader
              title="Around the web"
              :subtitle="allLinks.length ? String(allLinks.length) : ''"
              :class="{ 'mt-gap': displayMembers.length > 0 || displayGroups.length > 0 }"
            />
            <ExternalLinks kind="artist" :external-ids="detail?.media_item?.external_ids ?? {}" class="atw-ext-side" />
            <div v-if="allLinks.length" class="member-chips">
              <a
                v-for="l in visibleLinks"
                :key="l.url"
                :href="l.url"
                target="_blank"
                rel="noopener"
                class="mchip mchip-linked lchip"
                :title="l.sub ? `${l.label} — ${l.sub}` : l.label"
              >
                <span class="mchip-nm">{{ l.label }}</span>
                <span v-if="l.sub" class="mchip-yrs">{{ l.sub }}</span>
              </a>
              <button
                v-if="allLinks.length > LINK_CHIP_MAX"
                class="mchip mchip-more"
                @click="linksExpanded = !linksExpanded"
              >{{ linksExpanded ? 'Show fewer' : `+${allLinks.length - LINK_CHIP_MAX} more` }}</button>
            </div>
          </template>
        </div>
      </section>

    </main>

    <MetadataEditorModal
      v-if="detail"
      :media-id="detail.media_item.id"
      :show="showMetadataEditor"
      @close="onEditorClose"
    />

    <!-- Music-video modal — same nocookie YouTube embed as the movie page. -->
    <AppDialog
      :model-value="!!videoModal"
      :title="videoModal?.title"
      size="lg"
      prevent-auto-focus
      content-class="video-dialog"
      @update:model-value="(v) => v ? null : videoModal = null"
    >
      <iframe
        v-if="videoModal"
        class="video-dialog-iframe"
        :src="videoEmbedSrc(videoModal.key)"
        frameborder="0"
        allow="autoplay; encrypted-media; picture-in-picture"
        allowfullscreen
      />
      <div v-if="videoModal?.description" class="video-dialog-desc">{{ videoModal.description }}</div>
    </AppDialog>

    <!-- Phone ⋯ target for Popular Tracks rows (play/queue/rate/navigate). -->
    <ActionSheet
      v-model:open="ttSheetOpen"
      :items="ttSheetTrack ? ttMenuItems(ttSheetTrack) : []"
      :title="ttSheetTrack?.title"
    />
  </div>
</template>

<script setup lang="ts">
import { NuxtLink } from '#components'
import type { AlbumView, Artist, ArtistMember, ArtistTopTrackRow, MediaDetail, MediaVideo, TrackView } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'
import type { DragAlbumPayload } from '~/composables/useMusicDragDrop'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { useQuery, useQueryCache } from '@pinia/colada'
import { musicArtistDetailQuery } from '~/queries/music'

// Keep the slug-addressed artist query shared with the parent page. Besides
// avoiding a second detail fetch, this preserves the slug-only resolution
// needed by artists whose names are entirely numeric.
const props = defineProps<{ mediaId: number; slug: string }>()

const route = useRoute()
const { playContext, playTracks, addToQueue, currentTrack, playing, formatTime } = usePlayerBindings()
const radio = useRadio()

// Now-playing markers. A Popular Tracks row lights up when the playing track
// is it; a discography tile lights up when the playing track belongs to that
// album (album ids are globally unique, so an id match is unambiguous). Both
// read the shared usePlayerBindings() state, so they react live as playback advances.
function isTopTrackActive(t: ArtistTopTrackRow) {
  const id = currentTrack.value?.id
  return id != null && id === t.local_track_id
}
function isAlbumActive(al: AlbumView) {
  const albumId = currentTrack.value?.album_id
  return albumId != null && albumId > 0 && albumId === al.id
}
const { isCoarse, isPhone } = useViewport()

const { onDragStart, onDragEnd } = useMusicDragDrop()
// Popular Tracks context/⋯ items — the phone rows hide the rating widget, so
// this menu (Rate lives in it) is the rating path there.
const trackMenuActions = useMusicActions()

const artistRatings = useRatings('artist')
const trackRatings = useRatings('track')
async function onRateArtist(id: number, v: number) {
  try { await artistRatings.set(id, v) } catch { /* rollback handled */ }
}
async function onRateTrack(id: number, v: number) {
  try { await trackRatings.set(id, v) } catch { /* rollback handled */ }
}

async function startArtistRadio() {
  await radio.startRadio({ kind: 'artist', artist_slug: route.params.slug as string })
}

const bioOpen = ref(false)
const ttExpanded = ref(false)

const { user } = useAuth()
const isAdmin = computed(() => user.value?.is_admin === true)
const showMetadataEditor = ref(false)
const queryClient = useQueryCache()

function onEditorClose() {
  showMetadataEditor.value = false
  // Edits and refreshes land server-side; drop the cached detail so the
  // page (and this component) re-reads the updated artist.
  queryClient.invalidateQueries({ key: ['music', 'artist', 'detail', props.slug] })
}

interface SimilarArtistRow {
  name: string
  mbid?: string
  image?: string
  score: number
  source: string
  url?: string
  local_slug?: string
  local_artist_id?: number
}

interface SonicSimilarArtistRow {
  id: number
  name: string
  media_item_id: number
  media_item_public_id?: string
  media_slug: string
  distance: number
}

const { $heya } = useNuxtApp()
const detailQuery = useQuery(() => musicArtistDetailQuery(props.slug))
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)

const artistSlugForQueries = computed(() => detail.value?.media_item?.slug ?? (route.params.slug as string | undefined) ?? '')

const similarQuery = useQuery({
  key: () => ['music', 'artist', 'similar', artistSlugForQueries.value],
  query: async () => (await $heya('/api/music/artists/{slug}/similar', { path: { slug: artistSlugForQueries.value } })) as SimilarArtistRow[],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})
const similar = computed<SimilarArtistRow[]>(() => similarQuery.data.value ?? [])

const sonicSimilarQuery = useQuery({
  key: () => ['music', 'artist', 'sonic-similar', artistSlugForQueries.value, { limit: 12 }],
  query: async () => ((await $heya('/api/music/artists/{slug}/sonic-similar', { path: { slug: artistSlugForQueries.value }, query: { limit: 12 } })) as { items: SonicSimilarArtistRow[] }).items ?? [],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})
const sonicSimilar = computed<SonicSimilarArtistRow[]>(() => sonicSimilarQuery.data.value ?? [])

const topTracksQuery = useQuery({
  key: () => ['music', 'artist', 'top-tracks', artistSlugForQueries.value, { limit: 25 }],
  query: async () => ((await $heya('/api/music/artists/{slug}/top-tracks', { path: { slug: artistSlugForQueries.value }, query: { limit: 25 } })) as { items: ArtistTopTrackRow[] }).items ?? [],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})
// Owned-only filter — Last.fm rows we can't play are noise on a library page.
// External links to Last.fm still live in the "Around the web" section.
// Deduped by local_track_id so "Usseewa" + "うっせぇわ" (which both resolve
// to the same recording) collapse to one rail entry.
const topTracks = computed<ArtistTopTrackRow[]>(() => {
  const seen = new Set<number>()
  const out: ArtistTopTrackRow[] = []
  for (const t of topTracksQuery.data.value ?? []) {
    if (!t.local_track_id || seen.has(t.local_track_id)) continue
    seen.add(t.local_track_id)
    out.push(t)
  }
  return out
})

const hasPlayableTopTracks = computed(() => topTracks.value.some(isTopTrackPlayable))

const artist = computed<Artist | null>(() => detail.value?.artist ?? null)
watch(artist, (a) => {
  if (a?.id && a.id > 0) artistRatings.load(a.id).catch(() => 0)
}, { immediate: true })

// Prime the per-track rating cache once the top-tracks list lands so the
// rating widgets paint at correct values rather than starting at 0.
watch(topTracks, (rows) => {
  const ids = rows.filter((r) => r.local_track_id).map((r) => r.local_track_id!) as number[]
  if (ids.length) trackRatings.primeBulk(ids).catch(() => 0)
})

const albums = computed<AlbumView[]>(() => detail.value?.albums ?? [])

// Playability — a track needs a live file (TrackView.files is server-filtered
// to live files), an album needs a playable track, the artist needs a playable
// album. Missing items still render but can't be played.
function isTrackPlayable(t: TrackView) { return t.files.length > 0 }
function albumPlayable(al: AlbumView) { return al.tracks.some(isTrackPlayable) }
const artistPlayable = computed(() => albums.value.some(albumPlayable))
const playableTrackIds = computed(() => {
  const s = new Set<number>()
  for (const al of albums.value) for (const t of al.tracks) if (isTrackPlayable(t)) s.add(t.id)
  return s
})
const playableTrackCount = computed(() => playableTrackIds.value.size)
function isTopTrackPlayable(t: ArtistTopTrackRow) {
  return !!t.local_track_id && playableTrackIds.value.has(t.local_track_id)
}

// Discography tile drag payload — album.tracks is already loaded (detail
// query), so this carries trackIds straight through and the sidebar drop
// handler skips the album-detail re-fetch.
function discogDragPayload(album: AlbumView): DragAlbumPayload {
  return {
    kind: 'album',
    title: album.title,
    artist_slug: route.params.slug as string,
    album_slug: album.slug,
    trackIds: album.tracks.filter(isTrackPlayable).map((t) => t.id),
  }
}

// Logotype instead of the name when the artist has a logo asset — the assets
// list in the detail payload says so up front (no probing).
const logoFailed = ref(false)
const logoUrl = computed(() => {
  if (!detail.value?.media_item) return null
  if (!detail.value.assets?.some((as) => as.asset_type === 'logo')) return null
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/logo`
})
watch(logoUrl, () => { logoFailed.value = false })

const artistPosterUrl = computed(() => {
  if (!detail.value?.media_item) return null
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/poster`
})

// Crossfade backdrops — the same shared carousel engine as the movie/TV
// heroes. HeroCanvas renders the sharp A/B pair and claims the blurred
// ambient underlay; CycleControls owns the sleeping rotation clock.
const {
  showA, backdropA, backdropB, carouselPaused, cycleKey, backdropAssets,
  advanceBackdrop, retreatBackdrop, seedCarousel, openBackdropLightbox,
} = useBackdropCarousel(detail, { maxSortOrder: 1000 })

watch(detail, async (d) => {
  if (!d) return
  await nextTick()
  seedCarousel()
}, { immediate: true })

const currentHeroBackdrop = computed(() => (showA.value ? backdropA.value : backdropB.value) || null)

// No backdrop asset → the carousel's fallback URL 404s and HeroCanvas's claim
// would leave the page with no ambient wash at all. Claim the square artist
// photo for the blurred underlay instead — full-viewport cover is right here,
// since there's no sharp hero art to align with (the hero stays name-on-dark).
const background = useBackground()
watchEffect(() => {
  if (!detail.value) return
  if (backdropAssets.value.length > 0 || !artistPosterUrl.value) {
    background.clear()
    return
  }
  background.set(artistPosterUrl.value, { presentation: 'hero' })
})

const { prefs } = useAppearance()

// ── Tone follow: publish --tone/--tone-rgb/--tone-ink on the page root.
// Primary source is the AmbientBackdrop's own sampled tone (useBackgroundTone),
// which re-samples on every crossfade; a direct sample of the current backdrop
// (poster fallback) is the ambient-off fallback, sequence-guarded against a
// slow sample landing after the route already changed artists — same pattern
// as the movie/TV heroes + the playbar's --pb-accent.
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(() => currentHeroBackdrop.value || artistPosterUrl.value, (src) => {
  const seq = ++toneSeq
  if (!src) { localTone.value = null; return }
  sampleImageTone(src).then((t) => { if (seq === toneSeq) localTone.value = t })
}, { immediate: true })

const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value || localTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return toneStyleVars(t)
})

// Similar Artists honors the same Appearance switch as the movie/TV
// recommendation rails: off = only artists we can link into the library.
// Deduped by identity — Last.fm and ListenBrainz often both suggest the
// same artist and the raw feed rendered them twice.
const visibleSimilar = computed(() => {
  const rows = prefs.value.showUnavailableRecs
    ? similar.value
    : similar.value.filter((r) => r.local_slug)
  const seen = new Set<string>()
  return rows.filter((r) => {
    const key = r.local_slug || r.mbid || r.name.toLowerCase()
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
})

const totalAlbums = computed(() => albums.value.length)
const totalTracks = computed(() => albums.value.reduce((sum, al) => sum + al.tracks.length, 0))

// Hero aside: the first few aliases, deduped against the display name
// (MusicBrainz often lists the name itself as an alias). Full list lives
// in the tooltip.
const heroAliases = computed(() => {
  const name = artist.value?.name?.toLowerCase()
  const list = (artist.value?.aliases ?? []).filter((a) => a.toLowerCase() !== name)
  if (!list.length) return ''
  const shown = list.slice(0, 3).join(' · ')
  return list.length > 3 ? `${shown} +${list.length - 3}` : shown
})

// Hero pills: curated genres first (upstream separates them from folksonomy
// tags since the 2026-07 provider expansion), then tags that add something
// new, case-insensitively deduped. Capped with a +N chip whose tooltip
// carries the full list.
const HERO_PILL_MAX = 5
const heroPillsAll = computed(() => {
  const out: string[] = []
  const seen = new Set<string>()
  for (const t of [...(artist.value?.genres ?? []), ...(artist.value?.tags ?? [])]) {
    const key = t.trim().toLowerCase()
    if (!key || seen.has(key)) continue
    seen.add(key)
    out.push(t)
  }
  return out
})
const pillsExpanded = ref(false)
const heroPills = computed(() => heroPillsAll.value.slice(0, HERO_PILL_MAX))
const heroPillsOverflowList = computed(() => heroPillsAll.value.slice(HERO_PILL_MAX))
const heroPillOverflow = computed(() => heroPillsOverflowList.value.length)

// artist_type comes through lower-cased in prod ('group'/'person'); compare
// case-insensitively so group-only treatments (status, lifecycle, ledger label)
// don't silently miss.
const isGroup = computed(() => (artist.value?.artist_type ?? '').toLowerCase() === 'group')
// Band lifecycle — drop the empty-name junk records prod ships (MusicBrainz
// leaves placeholder rows); the original rendered them as blank chips.
const displayMembers = computed(() => (artist.value?.members ?? []).filter((m) => m.name?.trim()))
const displayGroups = computed(() => (artist.value?.groups ?? []).filter((g) => g.name?.trim()))

// Member chips collapse past this count; MusicBrainz lists 20+ people for
// long-running groups and every one of them used to render as a full row.
const MEMBER_CHIP_MAX = 10
const membersExpanded = ref(false)
const groupsExpanded = ref(false)
const visibleMembers = computed(() => (membersExpanded.value ? displayMembers.value : displayMembers.value.slice(0, MEMBER_CHIP_MAX)))
const visibleGroups = computed(() => (groupsExpanded.value ? displayGroups.value : displayGroups.value.slice(0, MEMBER_CHIP_MAX)))
function memberTitle(m: ArtistMember): string {
  if (!m.begin_year && !m.end_year) return m.name
  return `${m.name} · ${m.begin_year || '?'}–${m.end_year || 'present'}`
}

// Music videos — media_videos rows on the artist's media item (all
// video_type=music_video for artists). External YouTube content.
const musicVideos = computed<MediaVideo[]>(() => detail.value?.videos ?? [])

// Rail handles for the header scroll-controls — AppRail is generic, so
// InstanceType<> can't name it; type the exposed surface directly.
type RailHandle = { scrollByDir: (dir: number, step?: number) => void; scrollToStart: () => void; overflows: boolean }
const mvRail = ref<RailHandle | null>(null)
const sonicRail = ref<RailHandle | null>(null)
const simRail = ref<RailHandle | null>(null)

const videoModal = ref<{ key: string; title: string; description?: string } | null>(null)
function openVideo(key: string, title: string, description?: string) {
  videoModal.value = { key, title, description }
}
// Autoplay is a motion trigger — skip it under prefers-reduced-motion so
// opening the dialog doesn't immediately start moving video.
function videoEmbedSrc(key: string): string {
  const reduceMotion = typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  return `https://www.youtube-nocookie.com/embed/${key}?autoplay=${reduceMotion ? 0 : 1}&rel=0`
}

// Strip MusicBrainz annotation link markup from the bio ([a=Name], [artist=
// Name|Display], [l=Label]…) → the plain display text. The raw prose shipped
// these bracket tokens; they read as noise in the About prose.
const cleanBio = computed(() => {
  const b = artist.value?.biography
  if (!b) return ''
  return b.replace(/\[(?:a|artist|b|band|l|label|r|release|rg|t|track|e|event|w|work|u|url)=([^\]|]+)(?:\|[^\]]*)?\]/gi, '$1')
})

const artistTypeLabel = computed(() => {
  const t = artist.value?.artist_type
  if (!t || t.toLowerCase() === 'person') return ''
  return t.charAt(0).toUpperCase() + t.slice(1)
})

const KIND_ORDER = ['album', 'ep', 'single', 'compilation', 'live', 'soundtrack', 'remix', 'demo', 'other']
const KIND_LABEL: Record<string, string> = {
  album: 'Albums',
  ep: 'EPs',
  single: 'Singles',
  compilation: 'Compilations',
  live: 'Live',
  soundtrack: 'Soundtracks',
  remix: 'Remixes',
  demo: 'Demos',
  other: 'Other',
}
// Per-tile corner flag — only for the non-album kinds (the "Albums" grid needs
// no flag; the mockup flags Singles/EP/etc.).
const KIND_FLAG: Record<string, string> = {
  ep: 'EP', single: 'Single', compilation: 'Comp', live: 'Live',
  soundtrack: 'OST', remix: 'Remix', demo: 'Demo', other: '',
}
function albumTypeFlag(kind: string): string { return KIND_FLAG[kind] ?? '' }

const groupedDiscography = computed(() => {
  const byKind = new Map<string, AlbumView[]>()
  for (const al of albums.value) {
    const kind = (al.album_type || 'album').toLowerCase()
    const bucket = KIND_LABEL[kind] ? kind : 'other'
    if (!byKind.has(bucket)) byKind.set(bucket, [])
    byKind.get(bucket)!.push(al)
  }
  for (const list of byKind.values()) {
    list.sort((a, b) => {
      const ay = parseInt(a.year || '0', 10) || 0
      const by = parseInt(b.year || '0', 10) || 0
      return by - ay
    })
  }
  return KIND_ORDER
    .filter((k) => byKind.has(k))
    .map((kind) => ({ kind, label: KIND_LABEL[kind] ?? kind, albums: byKind.get(kind)! }))
})


// Birthplace can come through as a Wikidata QID we don't yet resolve; only
// show when it's a human-readable token.
const originLabel = computed(() => {
  const bp = artist.value?.birthplace ?? ''
  if (!bp) return ''
  if (/^Q\d+$/.test(bp)) return ''
  return bp
})

const lifecycleLabel = computed(() => {
  const a = artist.value
  if (!a) return ''
  const start = a.begin_year ? String(a.begin_year) : (a.begin_date || '')
  const end = a.deathday || a.end_date || (a.ended ? '?' : '')
  if (!start && !end) return ''
  if (isGroup.value) {
    if (start && end) return `${start} – ${end}`
    if (start) return `Since ${start}`
    return end
  }
  if (start && a.deathday) return `${start} – ${a.deathday}`
  return start
})

// Minimal status chip — only disbanded groups get one (the mockup's
// "DISBANDED"). Living/active acts + people carry their years in lifecycleLabel.
const statusLabel = computed(() => {
  const a = artist.value
  if (!a) return ''
  if (isGroup.value && a.ended) return 'DISBANDED'
  return ''
})

// ── Ledger (user-facing facts only, PLAN cardinal rule 2) ────────────────────
const ledgerCells = computed<LedgerCell[]>(() => {
  const a = artist.value
  const cells: LedgerCell[] = []
  if (!a) return cells
  if ((a.listeners ?? 0) > 0) cells.push({ k: 'Listeners', v: formatBigInt(a.listeners!), sub: 'last.fm' })
  if ((a.followers ?? 0) > 0) cells.push({ k: 'Followers', v: formatBigInt(a.followers!), sub: 'theaudiodb' })
  if ((a.playcount ?? 0) > 0) cells.push({ k: 'Global plays', v: formatBigInt(a.playcount!) })
  if (totalAlbums.value > 0) {
    cells.push({
      k: 'In library',
      v: String(totalAlbums.value),
      unit: totalAlbums.value === 1 ? 'release' : 'releases',
      sub: `${totalTracks.value} tracks`,
    })
  }
  if (lifecycleLabel.value) cells.push({ k: isGroup.value ? 'Active' : 'Life', v: lifecycleLabel.value })
  if (originLabel.value) cells.push({ k: 'From', v: originLabel.value })
  if ((a.popularity ?? 0) > 0) cells.push({ k: 'Popularity', v: String(a.popularity), unit: '/ 100' })
  return cells
})

// ── "Around the web" dropdown ───────────────────────────────────────────
// The MusicBrainz url-rel list is a long tail (60+ links, half of them
// labeled "other databases" / "purchase for download"). Grouped into a
// dropdown: rows are labeled by SITE (hostname), with the rel type as a
// muted suffix only where it adds meaning.

interface AtwLink { label: string; sub?: string; url: string }

function linkCategory(type: string): string {
  const t = type.toLowerCase()
  if (t.includes('official') || t.includes('fanpage') || t.includes('bbc') || t.includes('discography') || t === 'blog' || t === 'image') return 'Official'
  if (t.includes('streaming') || t.includes('purchase') || t.includes('soundcloud') || t.includes('youtube music')) return 'Listen & Buy'
  if (t.includes('social') || t === 'myspace' || t.includes('video channel') || t === 'youtube' || t.includes('online community')) return 'Social'
  if (t === 'bandsintown' || t === 'songkick' || t === 'setlistfm') return 'Live'
  return 'Reference'
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

// Types that describe the link better than the bare hostname does.
const ATW_MEANINGFUL_TYPES = new Set([
  'official homepage', 'fanpage', 'discography page', 'lyrics', 'image',
  'purchase for download', 'purchase for mail-order', 'free streaming', 'streaming',
])

const CATEGORY_ORDER = ['Official', 'Listen & Buy', 'Social', 'Live', 'Reference', 'Wikipedia']

const linkGroups = computed(() => {
  const buckets = new Map<string, AtwLink[]>()
  const seen = new Set<string>()
  for (const l of (artist.value?.urls ?? [])) {
    if (!l.url || seen.has(l.url)) continue
    seen.add(l.url)
    const type = l.type || 'link'
    const cat = linkCategory(type)
    const entry: AtwLink = {
      label: hostOf(l.url),
      sub: ATW_MEANINGFUL_TYPES.has(type.toLowerCase()) ? type : undefined,
      url: l.url,
    }
    if (!buckets.has(cat)) buckets.set(cat, [])
    buckets.get(cat)!.push(entry)
  }
  const wiki = Object.entries(artist.value?.wikipedia_links ?? {})
    .map(([lang, url]) => ({ label: lang.toUpperCase(), sub: 'wikipedia', url }))
    .sort((a, b) => a.label.localeCompare(b.label))
  if (wiki.length) buckets.set('Wikipedia', wiki)

  return CATEGORY_ORDER
    .filter((c) => buckets.has(c))
    .map((c) => ({
      label: c,
      links: buckets.get(c)!.sort((a, b) => a.label.localeCompare(b.label)),
    }))
})

const hasExternalIds = computed(() => Object.keys(detail.value?.media_item?.external_ids ?? {}).length > 0)

// "Around the web" chips — the grouped dropdown flattened (group order
// kept: Official → Listen & Buy → Social → Live → Reference → Wikipedia),
// collapsed past LINK_CHIP_MAX like the member rows above them.
const LINK_CHIP_MAX = 12
const linksExpanded = ref(false)
const allLinks = computed(() => linkGroups.value.flatMap((g) => g.links))
const visibleLinks = computed(() => (linksExpanded.value ? allLinks.value : allLinks.value.slice(0, LINK_CHIP_MAX)))

const colSideVisible = computed(() =>
  displayMembers.value.length > 0 || displayGroups.value.length > 0 || allLinks.value.length > 0 || hasExternalIds.value)

function initials(name: string): string {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]?.toUpperCase() ?? '').join('')
}

function formatBigInt(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return n.toLocaleString()
}

function trackFromAlbum(album: AlbumView, t: TrackView): Track {
  const primary = t.files[0]
  return {
    id: t.id,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: album.title,
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.id}/stream`,
    album_id: album.id,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(route.params.slug as string, album.slug) ?? undefined,
    integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
    true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
  }
}

async function playAlbum(album: AlbumView, shuffle: boolean) {
  // Semantic source: server materializes (and truly shuffles) the album.
  await playContext({ kind: 'album', id: album.id }, { shuffle })
}

async function playAll(shuffle: boolean) {
  // Semantic source: the FULL discography server-side — not just the
  // albums this page happened to load — with true random shuffle.
  const artistID = artist.value?.id
  if (!artistID) return
  await playContext({ kind: 'artist', id: artistID }, { shuffle })
}

function addAllToQueue() {
  const tracks: Track[] = []
  for (const al of albums.value) {
    for (const t of al.tracks) if (isTrackPlayable(t)) tracks.push(trackFromAlbum(al, t))
  }
  void addToQueue(tracks)
}

function topTrackToTrack(t: ArtistTopTrackRow): Track {
  return {
    id: t.local_track_id!,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: t.local_album_title ?? '',
    duration: t.local_duration ?? 0,
    stream_url: `/api/music/tracks/${t.local_track_id}/stream`,
    album_id: t.local_album_id ?? 0,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(route.params.slug as string, t.local_album_slug ?? '') ?? undefined,
  }
}

// --- Popular Tracks menu / phone action sheet -------------------------------
function ttMenuItems(t: ArtistTopTrackRow) {
  if (!t.local_track_id) return []
  return trackMenuActions.forTrack({
    id: t.local_track_id,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: t.local_album_title ?? '',
    duration: t.local_duration ?? 0,
    artist_slug: artistSlugForQueries.value || undefined,
    album_slug: t.local_album_slug,
    available: isTopTrackPlayable(t),
  })
}

function discogMenuItems(album: AlbumView) {
  return trackMenuActions.forAlbum({
    id: album.id,
    title: album.title,
    artist_id: artist.value?.id,
    artist_name: artist.value?.name ?? '',
    artist_slug: artistSlugForQueries.value || route.params.slug as string,
    album_slug: album.slug,
    available: albumPlayable(album),
  })
}

const ttSheetOpen = ref(false)
const ttSheetTrack = ref<ArtistTopTrackRow | null>(null)
function openTtSheet(t: ArtistTopTrackRow) {
  ttSheetTrack.value = t
  ttSheetOpen.value = true
}

// Row click = play, everywhere. The interactive children (title/album links,
// stars, hover-play, the phone ⋯) all @click.stop, so a click that reaches the
// row really did land on empty row space.
function onTtRowTap(t: ArtistTopTrackRow) {
  if (isTopTrackPlayable(t)) void playTopTrack(t)
}

async function playTopTrack(t: ArtistTopTrackRow) {
  if (!isTopTrackPlayable(t)) return
  const built = topTrackToTrack(t)
  await playTracks([built])
}

async function playTopAll(shuffle: boolean) {
  const owned = topTracks.value.filter(isTopTrackPlayable).map(topTrackToTrack)
  if (!owned.length) return
  await playTracks(owned, undefined, { shuffle })
}

if (import.meta.client) {
  const bus = useEventBus()
  bus.connect()
  const off = bus.on('media.updated', (e) => {
    const payload = e.payload as { media_item_id?: number } | undefined
    if (payload?.media_item_id === props.mediaId) {
      queryClient.invalidateQueries({ key: ['music', 'artist', 'detail', props.slug] })
      queryClient.invalidateQueries({ key: ['music', 'artist', 'similar', artistSlugForQueries.value] })
      queryClient.invalidateQueries({ key: ['music', 'artist', 'sonic-similar', artistSlugForQueries.value, { limit: 12 }] })
      queryClient.invalidateQueries({ key: ['music', 'artist', 'top-tracks', artistSlugForQueries.value, { limit: 25 }] })
    }
  })
  onBeforeUnmount(() => { off() })
}
</script>

<style scoped>
.m-state { color: var(--fg-3); padding: 32px var(--pad-fluid); }

/* The music shell owns the scroll root; this page just publishes tone vars and
   lays out hero → ledger → body. `hero-flush` lets the artist art ride beneath
   the fixed glass topbar at every viewport; the hero-inner's own top padding
   keeps its text clear. On desktop/tablet, pages/music.vue separately re-pads
   the MusicSidebar so its first nav item still clears the bar. */
.artist2 { --oink: 233 236 242; padding-bottom: 40px; }

/* ═══ HERO ═════════════════════════════════════════════════════════════════ */
.artist-hero {
  position: relative;
  min-height: 54vh;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
}

.hero-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  padding: 88px var(--pad-fluid) 40px;
}
.hero-inner > .grow { min-width: 0; }

/* mono eyebrow — complement-colored (hue-opposite of the art tone, text-
   lifted; falls back to the tone itself pre-sample) over the dark grade */
.eyebrow {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 16px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone-comp, var(--tone));
}
.eyebrow .sep { color: rgb(var(--oink) / 0.3); }

/* Archivo display title — UPPERCASE, wdth 125 (heya2.css .title.artist). */
.title {
  font-family: var(--font-display);
  font-size: clamp(2.5rem, 6vw, 4.6rem);
  font-weight: 800;
  line-height: 0.96;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
  margin: 0;
}
.title.artist {
  text-transform: uppercase;
  font-variation-settings: "wdth" 125;
  letter-spacing: 0;
}
.title-logo-wrap { line-height: 0; }
.title-logo {
  display: block;
  width: auto;
  height: auto;
  max-width: min(520px, 100%);
  max-height: 128px;
  object-fit: contain;
  object-position: left center;
  filter: drop-shadow(0 6px 24px rgb(0 0 0 / 0.55));
}

/* aka signature line, hanging off the title's left edge */
.hero-aka {
  margin: 6px 0 0 2px;
  font-size: 12.5px;
  font-style: italic;
  color: rgb(var(--oink) / 0.72);
  text-shadow: 0 1px 8px rgb(0 0 0 / 0.5);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 60ch;
}
.hero-aka-label {
  font-family: var(--font-mono);
  font-style: normal;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: rgb(var(--oink) / 0.55);
  margin-right: 4px;
}

.metaline {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  font: 500 12.5px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--oink) / 0.72);
}
.metaline .dot { color: rgb(var(--tone-comp-rgb, var(--tone-rgb)) / 0.85); }
.metaline .status { letter-spacing: 0.1em; color: rgb(var(--oink) / 0.85); }
.metaline .genre,
.genre-flyout .genre {
  border-bottom: 1px solid rgb(var(--oink) / 0.25);
  padding-bottom: 1px;
  text-transform: lowercase;
  transition: color 0.15s, border-color 0.15s;
}
.metaline .genre:hover,
.genre-flyout .genre:hover { color: rgb(var(--oink) / 0.95); border-color: rgb(var(--tone-rgb) / 0.6); }
/* +N overflow — slides the remaining genres out below; not a browse
   link, so no underline. */
.metaline .genre-more {
  border: 0;
  background: none;
  padding: 0 0 1px;
  font: inherit;
  letter-spacing: inherit;
  color: rgb(var(--oink) / 0.55);
  cursor: pointer;
}
.metaline .genre-more:hover { color: rgb(var(--oink) / 0.95); }

/* Overflow genre row — 0fr→1fr grid row animates the reveal height (the
   identity block above nudges up as the row grows), pills stagger in from
   the left. Shares the metaline's type + complement ink. */
.genre-flyout {
  display: grid;
  grid-template-rows: 0fr;
  transition: grid-template-rows 0.38s cubic-bezier(0.22, 1, 0.36, 1);
  font: 500 12.5px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--tone-comp-rgb, var(--oink)) / 0.82);
  text-shadow: var(--hero-halo);
}
.genre-flyout.open { grid-template-rows: 1fr; }
.genre-flyout-inner {
  overflow: hidden;
  min-height: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  padding-top: 10px;
}
.genre-flyout .genre {
  opacity: 0;
  transform: translateX(-16px);
  transition: opacity 0.28s ease, transform 0.34s cubic-bezier(0.22, 1, 0.36, 1);
  transition-delay: calc(var(--i, 0) * 26ms);
}
.genre-flyout.open .genre {
  opacity: 1;
  transform: none;
}
@media (prefers-reduced-motion: reduce) {
  .genre-flyout, .genre-flyout .genre { transition: none; }
}

/* actions */
.actions {
  margin-top: 26px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.missing {
  display: inline-flex; align-items: center; gap: 5px;
  font: 600 11px var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--bad); width: 100%;
}
.primary-actions {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}

/* tone-glowing primary Play (heya2.css .btn-play) */
.btn-play {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 13px 26px 13px 20px;
  border: 0;
  border-radius: 999px;
  cursor: pointer;
  background: var(--tone);
  color: var(--tone-ink, #0a0c10);
  font: 650 14px var(--font-sans);
  letter-spacing: 0.01em;
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.45),
    0 0 24px rgb(var(--tone-rgb) / 0.4),
    6px 10px 36px -8px rgb(var(--tone-rgb) / 0.75);
  transition: transform 0.15s ease, box-shadow 0.15s ease,
    background 0.9s cubic-bezier(0.22, 1, 0.36, 1), color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.btn-play:hover:not([disabled]) {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.btn-play[disabled] { cursor: not-allowed; opacity: 0.4; box-shadow: 0 0 0 1px rgb(var(--oink) / 0.14); transform: none; }
.btn-play .tri {
  width: 0; height: 0;
  border-left: 11px solid var(--tone-ink, #0a0c10);
  border-top: 7px solid transparent;
  border-bottom: 7px solid transparent;
}
.btn-play small { font: 500 11px var(--font-mono); opacity: 0.72; letter-spacing: 0.06em; }

/* tone-tinted secondary pills (heya2.css .pill) */
.pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 11px 18px;
  border-radius: 999px;
  cursor: pointer;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(var(--oink) / 0.9);
  font: 550 13px var(--font-sans);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.14), 5px 8px 22px -10px rgb(0 0 0 / 0.7);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.15s, color 0.15s;
}
.pill:hover:not([disabled]) {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  color: rgb(var(--oink));
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.28), 6px 10px 26px -10px rgb(0 0 0 / 0.75);
  transform: translateY(-1px);
}
.pill[disabled] { cursor: not-allowed; opacity: 0.4; }
.pill.icon { width: 42px; height: 42px; padding: 0; justify-content: center; }

/* Artist taste controls use the same tone-tinted glass language as the hero's
   secondary actions, while remaining a single compact three-button cluster. */
.hero-rating {
  display: inline-flex;
  align-items: center;
  padding: 5px 8px;
  border-radius: 999px;
  background: rgb(var(--tone-rgb) / 0.08);
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.14), 5px 8px 22px -10px rgb(0 0 0 / 0.7);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s;
}
.hero-rating:hover {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.28), 6px 10px 26px -10px rgb(0 0 0 / 0.75);
}
.hero-rating :deep(.reaction-btn) { color: rgb(var(--oink) / 0.7); }
.hero-rating :deep(.reaction-btn:hover) { color: rgb(var(--oink) / 0.95); }

/* ═══ BODY ═════════════════════════════════════════════════════════════════ */
.page { padding: 0 var(--pad-fluid) 80px; }
.section { margin-top: 52px; }
.section:first-of-type { margin-top: 44px; }

.see-all {
  margin-top: 10px;
  background: none;
  border: none;
  color: var(--tone);
  cursor: pointer;
  font: 550 12px var(--font-mono);
  letter-spacing: 0.06em;
  padding: 4px 2px;
}
.see-all:hover { filter: brightness(1.15); }

/* SectionHeader #actions mini pills */
.mini-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 13px;
  border-radius: 999px;
  border: 0;
  background: var(--tone);
  color: var(--tone-ink, #0a0c10);
  font: 700 11.5px var(--font-sans);
  cursor: pointer;
  transition: filter 0.12s, background 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.mini-pill:hover:not([disabled]) { filter: brightness(1.1); }
.mini-pill[disabled] { opacity: 0.4; cursor: not-allowed; }
.mini-pill-ghost { background: rgb(var(--ink) / 0.07); color: rgb(var(--ink) / 0.82); }
.mini-pill-ghost:hover:not([disabled]) { background: rgb(var(--ink) / 0.12); filter: none; }

/* ── Popular Tracks — .trk ledger rows (heya2.css .trk / .trklist) ── */
.trklist { border-top: 1px solid var(--hair-strong); }
.trk {
  display: grid;
  grid-template-columns: 44px minmax(0, 1.5fr) minmax(0, 1fr) auto 66px 40px;
  gap: 18px;
  align-items: center;
  padding: 10px 8px;
  border-bottom: 1px solid var(--hair);
  border-radius: var(--r-sm);
  cursor: pointer; /* whole row plays (onTtRowTap) */
  transition: background 0.12s;
  min-height: 44px;
}
.trk:hover { background: rgb(var(--ink) / 0.03); }
.trk:hover .trk-rank { opacity: 0; }
.trk:hover .trk-hover-play { opacity: 1; }
.trk-missing { opacity: 0.5; }
.trk-active { background: rgb(var(--tone-rgb) / 0.1); }
.trk-active:hover { background: rgb(var(--tone-rgb) / 0.12); }
.trk-active .trk-t { color: var(--tone); }

.trk-n {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  height: 22px;
}
.trk-rank {
  font: 600 13px var(--font-mono);
  color: rgb(var(--ink) / 0.35);
  font-variant-numeric: tabular-nums;
  transition: opacity 0.12s;
}
.trk-vu { margin-left: auto; }
.trk-missing-icon { color: var(--bad); }
.trk-hover-play {
  position: absolute;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 24px;
  height: 24px;
  border-radius: 50%;
  border: 0;
  background: var(--tone);
  color: var(--tone-ink, #0a0c10);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.12s, filter 0.12s;
}
.trk-hover-play:hover { filter: brightness(1.1); }

.trk-meta { min-width: 0; overflow: hidden; }
.trk-t {
  font-size: 14.5px;
  font-weight: 600;
  color: rgb(var(--ink) / 0.92);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: block;
}
.trk-al {
  font: 500 11.5px var(--font-mono);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.5);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-decoration: none;
}
a.trk-al:hover { color: var(--tone); }
.trk-stars { display: inline-flex; justify-content: flex-end; }
.trk-d {
  font: 500 12px var(--font-mono);
  color: rgb(var(--ink) / 0.55);
  text-align: right;
  font-variant-numeric: tabular-nums;
}
/* phone-only ⋯ (see the media query below) */
.trk-more { display: none; }

/* ── Discography — album cards (heya2.css .album-card + embedded flag) ── */
.album-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(178px, 1fr));
  gap: 22px 18px;
}
.album-card {
  position: relative;
  display: block;
  text-decoration: none;
  color: inherit;
}
.album-card :deep(.mc-art) { transition: transform 0.18s ease, box-shadow 0.28s ease; }
.album-card:hover :deep(.mc-art) { transform: translateY(-4px); box-shadow: var(--shadow-card-hover); }
.album-active :deep(.mc-art) { box-shadow: var(--shadow-card), 0 0 0 2px var(--tone); }
.album-nowplaying {
  position: absolute;
  top: 10px;
  right: 10px;
  z-index: 3;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px 6px;
  border-radius: var(--r-xs);
  background: rgb(0 0 0 / 0.6); /* over artwork — literal */
  backdrop-filter: blur(6px);
}

/* ── About + Members two-column (heya2.css .cols) ── */
.cols {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
  gap: 56px;
  align-items: start;
  /* The avatar rails above carry no shadow-room below their tiles the way
     card rails do — without this the About header sat tight under them. */
  margin-top: 48px;
}
.prose { font-size: 15.5px; line-height: 1.75; color: rgb(var(--ink) / 0.82); max-width: 64ch; }

/* Height link (wide screens only — single-column below 1100px): the right
   chip column decides the row height. The About column contributes no
   height of its own (absolute fill), its bio flex-shrinks to the leftover
   space with a fade-out, and the sources/library rows never shrink. The
   More toggle drops .cols-linked entirely, releasing the row. */
@media (min-width: 1100.02px) {
  .cols-linked .col-about { position: relative; align-self: stretch; }
  .cols-linked .col-about-fill {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .cols-linked .col-about-fill .prose {
    flex: 0 1 auto;
    min-height: 0;
    overflow: hidden;
    mask-image: linear-gradient(to bottom, rgb(0 0 0) 72%, transparent 100%);
    -webkit-mask-image: linear-gradient(to bottom, rgb(0 0 0) 72%, transparent 100%);
  }
  .cols-linked .col-about-fill .bio-toggle,
  .cols-linked .col-about-fill .detail-grid { flex-shrink: 0; }
}
.bio-toggle::before { content: '▾ '; opacity: 0.7; }
.prose-empty { font-size: 14px; color: rgb(var(--ink) / 0.5); font-style: italic; }

.detail-grid { display: grid; grid-template-columns: 1fr; gap: 26px; margin-top: 30px; }
.detail-grid dt {
  font: 600 10.5px var(--font-mono);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
  margin-bottom: 10px;
}
.detail-grid dd { font-size: 13.5px; line-height: 1.8; color: rgb(var(--ink) / 0.75); }
.detail-grid dd .mbid { font-family: var(--font-mono); font-size: 11px; color: rgb(var(--ink) / 0.5); }
.atw-ext { margin-top: 12px; }

/* members — compact wrapping chips (26px avatar + name + tiny tenure). */
.member-chips { display: flex; flex-wrap: wrap; gap: 8px; }
.mchip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 12px 4px 5px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--hair);
  text-decoration: none;
  color: inherit;
  max-width: 100%;
  min-width: 0;
}
.mchip-linked { transition: background 0.15s, border-color 0.15s; }
.mchip-linked:hover { background: rgb(var(--ink) / 0.1); border-color: rgb(var(--ink) / 0.18); }
.mchip-linked:hover .mchip-nm { color: var(--tone); }
.mchip-av {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  flex-shrink: 0;
}
.mchip-av-initials {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-2);
  box-shadow: 0 0 0 1px rgb(var(--ink) / 0.12);
  font: 700 10px var(--font-mono);
  color: rgb(var(--ink) / 0.35);
}
.mchip-nm {
  font-size: 12.5px;
  font-weight: 600;
  color: rgb(var(--ink) / 0.85);
  transition: color 0.15s;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mchip-yrs { font: 500 10px var(--font-mono); color: rgb(var(--ink) / 0.45); white-space: nowrap; }
.mchip-more {
  padding: 4px 12px;
  cursor: pointer;
  font: 600 11.5px var(--font-mono);
  color: rgb(var(--ink) / 0.6);
  transition: background 0.15s, color 0.15s;
}
.mchip-more:hover { background: rgb(var(--ink) / 0.1); color: rgb(var(--ink) / 0.9); }
.mt-gap { margin-top: 36px; }

/* link chips — mchip minus the avatar slot; sub-label rides mchip-yrs */
.lchip { padding: 4px 12px; }
.atw-ext-side { margin-bottom: 12px; }

/* metadata-source provenance chips (About column) */
.src-chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.src-chip {
  padding: 3px 9px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--hair);
  font: 550 10.5px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--ink) / 0.7);
}
.src-via { font: 500 10.5px var(--font-mono); color: rgb(var(--ink) / 0.4); margin-left: 2px; }

/* music videos — YouTube thumb tiles + hover play scrim (movie page recipe) */
.video-card {
  width: 100%; text-align: left;
  background: none; border: none; cursor: pointer; color: inherit; padding: 0;
}
.video-play {
  position: absolute; inset: 0; z-index: 3;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35); opacity: 0; transition: opacity 0.15s;
  color: #fff; pointer-events: none; /* on artwork — stays literal */
}
.video-card:hover .video-play { opacity: 1; }

/* ── Sounds Like / Similar — square overlay cards (MusicCard paints the
   name/source on the art, same grammar as the discography tiles). ── */
.sim-tile {
  display: block;
  text-decoration: none;
  color: inherit;
}
.sim-tile :deep(.mc-art) { transition: transform 0.18s ease, box-shadow 0.28s ease; }
.sim-tile:hover :deep(.mc-art) { transform: translateY(-4px); box-shadow: var(--shadow-card-hover); }
.sim-external { opacity: 0.75; cursor: default; }
.sim-external:hover { opacity: 1; }
.sim-external:hover :deep(.mc-art) { transform: none; box-shadow: none; }

/* ═══ RESPONSIVE ═══════════════════════════════════════════════════════════ */
@media (max-width: 1100px) {
  .cols { grid-template-columns: 1fr; gap: 40px; }
}

@media (max-width: 720px) {
  .artist-hero { min-height: 48vh; }
  .hero-inner { padding: 64px var(--pad-fluid) 28px; }
  .title { font-size: clamp(2rem, 9vw, 3rem); }
  .title-logo { max-height: 92px; }
  .actions { gap: 8px; row-gap: 10px; }
  .primary-actions {
    flex: 1 1 100%;
    width: 100%;
    gap: 8px;
  }
  .btn-play {
    flex: 1 1 0;
    min-width: 0;
    justify-content: center;
    height: 48px;
    padding-inline: 14px;
  }
  .hero-rating {
    flex: 0 0 auto;
    height: 48px;
    padding-inline: 7px;
  }
  .pill:not(.icon) { flex: 1 1 auto; justify-content: center; height: 46px; }
  .pill.icon { width: 46px; height: 46px; }
  /* Metadata editor is a desktop-sized surface — no phone entry point. */
  .hero-edit { display: none; }

  /* Popular Tracks: title/subtitle own the first line; the three section
     actions get an evenly-sized row beneath instead of squeezing the title. */
  .popular-header {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: start;
    gap: 12px;
  }
  .popular-header :deep(.sh-actions) {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    width: 100%;
    margin-left: 0;
    gap: 8px;
  }
  .popular-header .mini-pill {
    min-width: 0;
    min-height: 40px;
    justify-content: center;
    padding-inline: 8px;
    white-space: nowrap;
  }

  /* Popular Tracks: the rating widget ate the title column at 390px — hide it
     (the ⋯ ActionSheet carries Rate + play/queue) and give the text room; the
     album drops onto its own line under the title. Row tap plays. */
  .trk {
    grid-template-columns: 34px minmax(0, 1fr) max-content 44px;
    gap: 10px;
    padding: 8px 4px;
    min-height: 54px;
    align-items: center;
  }
  .trk-stars { display: none; }
  .trk-n {
    grid-column: 1;
    grid-row: 1 / span 2;
  }
  .trk-meta {
    grid-column: 2;
    grid-row: 1;
  }
  .trk-al {
    grid-column: 2;
    grid-row: 2;
    align-self: start;
    margin-top: -2px;
  }
  .trk-d { grid-column: 3; grid-row: 1 / span 2; }
  .trk-more {
    grid-column: 4;
    grid-row: 1 / span 2;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    background: transparent;
    border: 0;
    color: rgb(var(--ink) / 0.55);
    cursor: pointer;
  }
  .trk-more:active { color: var(--tone); }

  .album-grid { grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 18px 12px; }
}
</style>

<!-- "Around the web" dropdown — the AppMenu content is portaled to <body> and
     the trigger renders inside the AppMenu child component, so none of these
     rules can live in the scoped block (docs/ui.md gotcha #2). -->
<style>
/* Video modal internals — the dialog content is portaled, so these rules
   must be unscoped to reach it. */
.video-dialog .app-dialog-body { padding: 0; }
.video-dialog-iframe { width: 100%; aspect-ratio: 16 / 9; display: block; border: 0; }
.video-dialog-desc {
  max-height: 180px;
  overflow-y: auto;
  padding: 14px 20px;
  font-size: 13.5px;
  line-height: 1.65;
  color: rgb(var(--ink) / 0.8);
  white-space: pre-wrap;
}

.atw-trigger {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--hair);
  font: 550 11px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--ink) / 0.8);
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.atw-trigger:hover,
.atw-trigger[data-state="open"] { background: rgb(var(--ink) / 0.09); color: rgb(var(--ink) / 0.95); border-color: var(--hair-strong); }
.atw-count { font-size: 10px; color: rgb(var(--ink) / 0.5); }

/* Long list — scroll inside the surface. */
.atw-scroll { max-height: min(55vh, 480px); overflow-y: auto; }
.atw-item { display: flex; align-items: center; gap: 10px; }
.atw-host { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.atw-type {
  flex-shrink: 0;
  font: 500 10px var(--font-mono);
  color: rgb(var(--ink) / 0.4);
  letter-spacing: 0.04em;
}
</style>
