<template>
  <div v-if="loading" class="scroll hero-flush" style="height: 100%">
    <div style="height: 420px; background: var(--bg-2)" />
  </div>

  <!-- `hero-flush` opts this page out of the topbar offset so the hero art
       rides up under the glass topbar (the hero's own inner padding keeps text
       clear of the bar). Person pages have no owned landscape backdrop, so the
       hero art is sourced from the BACKDROPS of the titles this person is
       credited in (crossfaded, sharp); when the person has no in-library
       credits with backdrops it falls back to the profile photos, softened
       page-side (`photos-mode`) so the sharp headshot in the record-card stays
       the focal point. Either way HeroCanvas publishes a graded (v2) art claim
       so the blurred site-wide underlay follows along. Tone vars are published
       on the scroll root, mirroring the movie/artist ports. -->
  <div v-else-if="person" class="scroll person2 hero-flush" :style="toneStyle" style="height: 100%">
    <section class="hero-section person-hero" :class="{ 'photos-mode': heroPhotosMode }">
      <HeroCanvas
        :src="backdropA || ''"
        :src-b="backdropB"
        :show-a="showA"
        object-position="center 24%"
      />

      <!-- Backdrop tools — expand-to-lightbox + the shared prev/pause/next
           ring together, top-right. -->
      <div v-if="creditBackdrops.length > 0 || heroArtPool.length > 1" class="hero-tools">
        <button v-if="creditBackdrops.length > 0" class="hero-expand" aria-label="Expand backdrop" @click="openBackdropLightbox">
          <Icon name="expand" :size="13" />
        </button>
        <CycleControls
          v-if="heroArtPool.length > 1"
          v-model:paused="carouselPaused"
          :cycle-key="cycleKey"
          :duration="BACKDROP_INTERVAL"
          item-label="backdrop"
          @prev="retreatBackdrop"
          @next="advanceBackdrop"
        />
      </div>

      <div class="hero-inner">
        <!-- Portrait record-card — layered directional shadow, initials fallback
             baked into Poster, gallery lightbox zoom. Hidden ≤tablet. -->
        <div class="hero-left">
          <div class="hero-poster postercard">
            <Poster :idx="person.id" :src="portraitSrc" :title="person.name" aspect="2/3" :width="600" />
            <button v-if="galleryUrls.length > 0" class="zoom-btn" aria-label="Expand photo" @click="openGallery(0)">
              <Icon name="expand" :size="14" />
            </button>
          </div>
        </div>

        <div class="grow hero-ink">
          <div class="eyebrow">
            <span>Person</span>
            <template v-if="person.known_for_department">
              <span class="sep">&middot;</span><span>{{ person.known_for_department }}</span>
            </template>
            <template v-if="person.place_of_birth">
              <span class="sep">&middot;</span><span>{{ person.place_of_birth }}</span>
            </template>
          </div>

          <h1 class="title">{{ person.name }}</h1>

          <p v-if="metaParts.length" class="metaline">
            <template v-for="(part, i) in metaParts" :key="part">
              <span v-if="i > 0" class="dot">&middot;</span>
              <span>{{ part }}</span>
            </template>
          </p>

          <!-- Around the web — same tone-tinted pill family as the movie hero
               actions row (a person has no primary Play CTA). -->
          <div v-if="socialLinks.length" class="actions">
            <a
              v-for="link in socialLinks"
              :key="link.platform"
              :href="link.url"
              target="_blank"
              rel="noopener noreferrer"
              class="pill link-pill"
              :title="link.label"
            >
              <Icon :name="link.icon" :size="15" />
              <span>{{ link.label }}</span>
            </a>
          </div>
        </div>
      </div>
    </section>

    <!-- ── LEDGER at the hard-clip seam — user-facing facts only. ── -->
    <LedgerStrip :cells="ledgerCells" />

    <!-- ── BODY ── -->
    <main class="page">
      <!-- Biography + side panel (departments breakdown, gallery). -->
      <section v-if="hasStorySection" class="section cols">
        <div>
          <SectionHeader title="Biography" />

          <div v-if="activeBioText" class="prose">
            <div v-if="bioLanguageOptions.length > 1" class="bio-lang-pills">
              <button
                v-for="opt in bioLanguageOptions"
                :key="opt.code"
                type="button"
                class="bio-lang-pill"
                :class="{ active: selectedBioLang === opt.code }"
                :aria-pressed="selectedBioLang === opt.code"
                @click="selectedBioLang = opt.code"
              >{{ opt.label }}</button>
            </div>
            <p v-for="(para, i) in bioParas" :key="i" class="bio-para">{{ para }}</p>
            <button v-if="bioTruncated" class="see-all" @click="bioExpanded = !bioExpanded">
              {{ bioExpanded ? 'Less' : 'More' }}
            </button>
          </div>
          <p v-else class="prose-empty">No biography available.</p>

          <dl v-if="person.also_known_as?.length" class="detail-grid">
            <div>
              <dt>Also known as</dt>
              <dd>{{ person.also_known_as.slice(0, 8).join(' · ') }}</dd>
            </div>
          </dl>
        </div>

        <div v-if="departmentStats.length > 1 || profileThumbs.length > 0" class="col-side">
          <template v-if="departmentStats.length > 1">
            <SectionHeader title="Departments" :subtitle="String(departmentStats.length)" />
            <div class="dept-list">
              <div v-for="d in departmentStats" :key="d.name" class="dept-row">
                <div class="dept-label">{{ d.name }}</div>
                <div class="dept-bar"><div class="dept-fill" :style="{ width: d.pct + '%' }" /></div>
                <div class="dept-count">{{ d.count }}</div>
              </div>
            </div>
          </template>

          <template v-if="profileThumbs.length > 0">
            <SectionHeader title="Photos" :subtitle="String((data?.profiles?.length || 0))" :class="{ 'mt-gap': departmentStats.length > 1 }" />
            <div class="photo-strip">
              <button
                v-for="(url, idx) in profileThumbs"
                :key="idx"
                type="button"
                class="photo-thumb"
                :aria-label="`Open photo ${idx + 1}`"
                @click="openGallery(galleryHeroOffset + idx)"
              >
                <Poster :idx="idx" :src="url" aspect="2/3" :width="160" />
              </button>
              <button v-if="extraProfileCount > 0" type="button" class="photo-thumb photo-more" @click="openGallery(galleryHeroOffset)">
                +{{ extraProfileCount }}
              </button>
            </div>
          </template>
        </div>
      </section>

      <!-- ── FILMOGRAPHY ── -->
      <section v-if="hasAnyCredits" class="section filmo">
        <SectionHeader title="Filmography">
          <template #actions>
            <div class="scope-toggle">
              <button class="scope-btn" :class="{ active: scope === 'library' }" @click="scope = 'library'">
                In Library <span class="scope-count">{{ libraryCount }}</span>
              </button>
              <button v-if="knownForCount > 0" class="scope-btn" :class="{ active: scope === 'known' }" @click="scope = 'known'">
                Known For <span class="scope-count">{{ knownForCount }}</span>
              </button>
            </div>
          </template>
        </SectionHeader>

        <!-- Role/department filter tabs within the active scope. -->
        <div v-if="activeFilterOptions.length > 1" class="filmo-filters">
          <button
            v-for="f in activeFilterOptions"
            :key="f.key"
            class="filter-tab"
            :class="{ active: activeFilter === f.key }"
            @click="activeFilter = f.key"
          >
            {{ f.label }}<span class="filter-count">{{ f.count }}</span>
          </button>
        </div>

        <!-- IN LIBRARY scope -->
        <template v-if="scope === 'library'">
          <div v-if="(activeFilter === 'all' || activeFilter === 'acting') && sortedCast.length" class="filmo-group">
            <div class="filmo-group-head">Acting <span class="filmo-group-n">{{ sortedCast.length }}</span></div>
            <div class="filmo-grid">
              <NuxtLink
                v-for="c in sortedCast"
                :key="`cast-${c.media_item_id}`"
                :to="mediaUrl({ id: c.media_item_id, public_id: c.media_item_public_id, title: c.title, year: c.year, media_type: c.media_type })"
                class="filmo-card"
              >
                <MediaCard
                  :idx="c.media_item_id"
                  :src="usePosterUrl({ id: c.media_item_id, public_id: c.media_item_public_id })"
                  aspect="2/3"
                  :width="300"
                  :title="c.title"
                  :subtitle="creditSub(c.year, c.character)"
                />
              </NuxtLink>
            </div>
          </div>

          <div v-for="dept in filteredCrewDepts" :key="`crew-${dept.name}`" class="filmo-group">
            <div class="filmo-group-head">{{ dept.name }} <span class="filmo-group-n">{{ dept.credits.length }}</span></div>
            <div class="filmo-grid">
              <NuxtLink
                v-for="c in dept.credits"
                :key="`crew-${c.media_item_id}-${c.job}`"
                :to="mediaUrl({ id: c.media_item_id, public_id: c.media_item_public_id, title: c.title, year: c.year, media_type: c.media_type })"
                class="filmo-card"
              >
                <MediaCard
                  :idx="c.media_item_id"
                  :src="usePosterUrl({ id: c.media_item_id, public_id: c.media_item_public_id })"
                  aspect="2/3"
                  :width="300"
                  :title="c.title"
                  :subtitle="creditSub(c.year, c.job)"
                />
              </NuxtLink>
            </div>
          </div>
        </template>

        <!-- KNOWN FOR scope — external credits, dimmed + non-linking. -->
        <template v-else>
          <div v-if="(activeFilter === 'all' || activeFilter === 'acting') && externalCast.length" class="filmo-group">
            <div class="filmo-group-head">Acting <span class="filmo-group-n">{{ externalCast.length }}</span></div>
            <div class="filmo-grid">
              <div v-for="c in externalCast" :key="`ext-cast-${c.id}`" class="filmo-card is-external">
                <MediaCard :idx="c.id" :src="c.poster_url" aspect="2/3" :width="300" :title="c.title" :subtitle="creditSub(c.year, c.character)" />
              </div>
            </div>
          </div>

          <template v-for="dept in externalCrewDepts" :key="`ext-${dept.name}`">
            <div v-if="activeFilter === 'all' || activeFilter === dept.key" class="filmo-group">
              <div class="filmo-group-head">{{ dept.name }} <span class="filmo-group-n">{{ dept.credits.length }}</span></div>
              <div class="filmo-grid">
                <div v-for="c in dept.credits" :key="`ext-crew-${c.id}`" class="filmo-card is-external">
                  <MediaCard :idx="c.id" :src="c.poster_url" aspect="2/3" :width="300" :title="c.title" :subtitle="creditSub(c.year, c.job)" />
                </div>
              </div>
            </div>
          </template>
        </template>
      </section>
    </main>
  </div>

  <div v-else class="scroll" style="height: 100%; display: flex; align-items: center; justify-content: center">
    <div style="text-align: center; color: var(--fg-2)">
      <p style="font-size: 18px">Person not found</p>
      <button class="btn btn-secondary" style="margin-top: 16px" @click="$router.back()">Go back</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { PersonResponse, PersonExternalCredit } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { useQuery } from '@pinia/colada'
import { personDetailQuery } from '~/queries/discovery'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const lightbox = useLightbox()

const detailQuery = useQuery(() => personDetailQuery(slug.value))
await waitForQuery(detailQuery)
const data = computed<PersonResponse | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)
const person = computed(() => data.value?.person ?? null)

const activeFilter = ref('all')
const bioExpanded = ref(false)
const selectedBioLang = ref('en')

// ── Portrait + gallery ──────────────────────────────────────────────────────
// Poster paints big centred initials from the name when src is null or the
// image errors, so we just hand it the endpoint (which materialises remote
// headshots on demand) or null when there's no profile at all.
const portraitSrc = computed(() => person.value?.profile_path ? `/api/person/${person.value.id}/image` : null)

// Lightbox gallery: the hero headshot first (index 0), then the profile set.
const galleryUrls = computed<string[]>(() => {
  if (!data.value) return []
  const urls: string[] = []
  if (data.value.person.profile_path) urls.push(`/api/person/${data.value.person.id}/image`)
  if (data.value.profiles) urls.push(...data.value.profiles.map(p => p.url))
  return urls
})
const galleryHeroOffset = computed(() => (data.value?.person.profile_path ? 1 : 0))
const profileThumbs = computed(() => (data.value?.profiles || []).slice(0, 8).map(p => p.url))
const extraProfileCount = computed(() => Math.max(0, (data.value?.profiles?.length || 0) - 8))

function openGallery(idx: number) {
  if (galleryUrls.value.length === 0) return
  lightbox.open(galleryUrls.value, idx)
}

// ── Hero art — credit backdrops, falling back to profile photos ─────────────
// The person's credited local titles supply landscape backdrops; each is probed
// at thumbnail size first (not every credit has one; the endpoint 404s). When
// none resolve we ride the profile photos instead, softened page-side so the
// record-card headshot stays the focal point (see .photos-mode below).
const creditBackdropCandidates = computed<string[]>(() => {
  const seen = new Set<number>()
  const urls: string[] = []
  for (const c of [...(data.value?.cast_credits || []), ...(data.value?.crew_credits || [])]) {
    if (seen.has(c.media_item_id)) continue
    seen.add(c.media_item_id)
    const u = useBackdropUrl({ id: c.media_item_id, public_id: c.media_item_public_id })
    if (u) urls.push(u)
  }
  return urls.slice(0, 14)
})

function probeImage(url: string) {
  return new Promise<boolean>((resolve) => {
    if (import.meta.server) return resolve(false)
    const img = new Image()
    img.onload = () => resolve(true)
    img.onerror = () => resolve(false)
    img.src = `${url}?w=64`
  })
}

const creditBackdrops = ref<string[]>([])
let probeToken = 0
watch(creditBackdropCandidates, async (cands) => {
  const token = ++probeToken
  const ok: string[] = []
  for (const u of cands) {
    if (await probeImage(u)) ok.push(u)
    if (token !== probeToken) return // newer credit list superseded this pass
    if (ok.length >= 8) break
  }
  creditBackdrops.value = ok
}, { immediate: true })

const profilePhotoUrls = computed<string[]>(() => {
  const urls: string[] = []
  if (person.value?.profile_path) urls.push(`/api/person/${person.value.id}/image`)
  urls.push(...(data.value?.profiles || []).map(p => p.url))
  return urls
})

// Pool the hero crossfades over: real backdrops when we have them, else photos.
const heroArtPool = computed<string[]>(() => (creditBackdrops.value.length ? creditBackdrops.value : profilePhotoUrls.value))
const heroPhotosMode = computed(() => creditBackdrops.value.length === 0 && profilePhotoUrls.value.length > 0)

const showA = ref(true)
const backdropA = ref<string | null>(null)
const backdropB = ref<string | null>(null)
const bdIdx = ref(0)
const cycleKey = ref(0)
const carouselPaused = ref(false)

function showBdIdx(idx: number) {
  bdIdx.value = idx
  const url = heroArtPool.value[idx] || null
  if (showA.value) backdropB.value = url
  else backdropA.value = url
  showA.value = !showA.value
  cycleKey.value++
}
function advanceBackdrop() {
  const n = heroArtPool.value.length
  if (n <= 1) return
  showBdIdx((bdIdx.value + 1) % n)
}
function retreatBackdrop() {
  const n = heroArtPool.value.length
  if (n <= 1) return
  showBdIdx((bdIdx.value - 1 + n) % n)
}

// (Re)seed whenever the pool changes — photos on first paint, swapping to the
// credit backdrops once the probe pass lands. Second image preloads into the
// hidden slot so the first crossfade is instant.
watch(heroArtPool, (pool) => {
  bdIdx.value = 0
  backdropA.value = pool[0] || null
  backdropB.value = pool[1] || pool[0] || null
  showA.value = true
  cycleKey.value++
}, { immediate: true })

const currentHeroBackdrop = computed(() => (showA.value ? backdropA.value : backdropB.value) || null)
function openBackdropLightbox() {
  if (heroArtPool.value.length === 0) return
  lightbox.open(heroArtPool.value, bdIdx.value)
}

// ── Tone follow — publish --tone/--tone-rgb/--tone-ink on the page root ──────
// Primary source is the AmbientBackdrop's own sampled tone; a direct sample of
// the current hero art (or the portrait) is the ambient-off fallback,
// sequence-guarded against a slow sample landing after the route changed.
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
const toneSource = computed(() => currentHeroBackdrop.value || portraitSrc.value)
watch(toneSource, (src) => {
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

// ── Derived facts ────────────────────────────────────────────────────────────
const age = computed(() => {
  if (!person.value?.birthday) return 0
  const birth = new Date(person.value.birthday + 'T00:00:00')
  if (Number.isNaN(birth.getTime())) return 0
  const end = person.value.deathday ? new Date(person.value.deathday + 'T00:00:00') : new Date()
  let a = end.getFullYear() - birth.getFullYear()
  const m = end.getMonth() - birth.getMonth()
  if (m < 0 || (m === 0 && end.getDate() < birth.getDate())) a--
  return a
})

const birthYear = computed(() => {
  const p = person.value
  if (p?.birth_year) return p.birth_year
  if (p?.birthday) { const y = parseInt(p.birthday.slice(0, 4), 10); return Number.isFinite(y) ? y : 0 }
  return 0
})
const deathYear = computed(() => {
  const d = person.value?.deathday
  if (!d) return 0
  const y = parseInt(d.slice(0, 4), 10)
  return Number.isFinite(y) ? y : 0
})

// "Years active" — first to most-recent credit year across all sources.
const yearsActive = computed(() => {
  const years: number[] = []
  const push = (y: string | number | undefined | null) => {
    if (!y) return
    const n = typeof y === 'string' ? parseInt(y.slice(0, 4), 10) : y
    if (Number.isFinite(n) && n > 1800 && n < 2200) years.push(n)
  }
  for (const c of data.value?.cast_credits || []) push(c.year)
  for (const c of data.value?.crew_credits || []) push(c.year)
  for (const c of data.value?.external_cast || []) push(c.year)
  for (const c of data.value?.external_crew || []) push(c.year)
  if (years.length < 2) return ''
  const min = Math.min(...years)
  const max = Math.max(...years)
  return min === max ? `${min}` : `${min}–${max}`
})

// Hero metaline parts (mono, uppercased by CSS).
const metaParts = computed<string[]>(() => {
  const p = person.value
  if (!p) return []
  const parts: string[] = []
  if (p.birthday) parts.push(`Born ${formatDateLong(p.birthday)}`)
  else if (birthYear.value) parts.push(`Born ${birthYear.value}`)
  if (age.value) parts.push(p.deathday ? `Age ${age.value} at death` : `Age ${age.value}`)
  if (p.deathday) parts.push(`Died ${formatDateLong(p.deathday)}`)
  return parts
})

// Distinct in-library titles this person appears in (cast ∪ crew).
const distinctLibraryTitles = computed(() => {
  const s = new Set<number>()
  for (const c of data.value?.cast_credits || []) s.add(c.media_item_id)
  for (const c of data.value?.crew_credits || []) s.add(c.media_item_id)
  return s.size
})

// ── Ledger (user-facing facts only, PLAN cardinal rule 2) ────────────────────
const ledgerCells = computed<LedgerCell[]>(() => {
  const p = person.value
  const cells: LedgerCell[] = []
  if (!p) return cells
  if (p.known_for_department) cells.push({ k: 'Known for', v: p.known_for_department })
  if (birthYear.value) cells.push({ k: 'Born', v: String(birthYear.value) })
  if (age.value) cells.push({ k: 'Age', v: String(age.value), sub: p.deathday ? 'at death' : undefined })
  if (deathYear.value) cells.push({ k: 'Died', v: String(deathYear.value) })
  if (distinctLibraryTitles.value > 0) {
    cells.push({ k: 'In library', v: String(distinctLibraryTitles.value), sub: distinctLibraryTitles.value === 1 ? 'title' : 'titles' })
  }
  if (yearsActive.value) cells.push({ k: 'Active', v: yearsActive.value })
  return cells
})

// ── Biography ────────────────────────────────────────────────────────────────
const langNames: Record<string, string> = {
  en: 'English', de: 'Deutsch', fr: 'Francais', es: 'Espanol', it: 'Italiano',
  pt: 'Portugues', ja: 'Japanese', ko: 'Korean', zh: 'Chinese', ru: 'Russian',
  nl: 'Nederlands', sv: 'Svenska', da: 'Dansk', no: 'Norsk', fi: 'Suomi',
  pl: 'Polski', cs: 'Cestina', hu: 'Magyar', ro: 'Romana', tr: 'Turkce',
  ar: 'Arabic', he: 'Hebrew', th: 'Thai', vi: 'Tieng Viet', id: 'Indonesian',
}

const bioLanguageOptions = computed(() => {
  const opts: { code: string; label: string }[] = []
  if (person.value?.biography) opts.push({ code: '_primary', label: 'Primary' })
  for (const b of data.value?.biographies || []) {
    if (b.biography === person.value?.biography) continue
    opts.push({ code: b.language, label: langNames[b.language] || b.language.toUpperCase() })
  }
  return opts
})

watch(() => data.value, (val) => {
  if (!val) return
  selectedBioLang.value = val.biographies?.some(b => b.language === 'en') ? 'en' : '_primary'
}, { immediate: true })

const activeBioText = computed(() => {
  if (!data.value) return ''
  if (selectedBioLang.value === '_primary') return person.value?.biography || ''
  const match = data.value.biographies?.find(b => b.language === selectedBioLang.value)
  return match?.biography || person.value?.biography || ''
})

const allParas = computed(() => activeBioText.value.split('\n\n').filter(Boolean))
const bioTruncated = computed(() => allParas.value.length > 3)
const bioParas = computed(() => {
  if (bioExpanded.value || !bioTruncated.value) return allParas.value
  return allParas.value.slice(0, 2)
})

// ── Social / external links ──────────────────────────────────────────────────
interface SocialLink { platform: string; url: string; icon: string; label: string }
const socialLinks = computed<SocialLink[]>(() => {
  const p = person.value
  const ids = p?.external_ids
  if (!p || !ids) return []
  const links: SocialLink[] = []
  if (p.imdb_id) links.push({ platform: 'imdb', url: `https://www.imdb.com/name/${p.imdb_id}`, icon: 'globe', label: 'IMDb' })
  if (ids.twitter) links.push({ platform: 'twitter', url: `https://twitter.com/${ids.twitter}`, icon: 'globe', label: 'Twitter / X' })
  if (ids.instagram) links.push({ platform: 'instagram', url: `https://instagram.com/${ids.instagram}`, icon: 'globe', label: 'Instagram' })
  if (ids.facebook) links.push({ platform: 'facebook', url: `https://facebook.com/${ids.facebook}`, icon: 'globe', label: 'Facebook' })
  if (ids.tiktok) links.push({ platform: 'tiktok', url: `https://tiktok.com/@${ids.tiktok}`, icon: 'globe', label: 'TikTok' })
  if (ids.wikidata) links.push({ platform: 'wikidata', url: `https://www.wikidata.org/wiki/${ids.wikidata}`, icon: 'globe', label: 'Wikidata' })
  return links
})

// ── Department breakdown ─────────────────────────────────────────────────────
interface DeptStat { name: string; count: number; pct: number }
const departmentStats = computed<DeptStat[]>(() => {
  const counts = new Map<string, number>()
  const add = (name: string, n: number) => counts.set(name, (counts.get(name) || 0) + n)
  add('Acting', (data.value?.cast_credits?.length || 0) + (data.value?.external_cast?.length || 0))
  for (const c of data.value?.crew_credits || []) add(c.department || 'Other', 1)
  for (const c of data.value?.external_crew || []) add(c.department || 'Other', 1)
  const rows: DeptStat[] = []
  for (const [name, count] of counts) {
    if (count <= 0) continue
    rows.push({ name, count, pct: 0 })
  }
  rows.sort((a, b) => b.count - a.count)
  const max = rows[0]?.count || 1
  for (const r of rows) r.pct = Math.round((r.count / max) * 100)
  return rows.slice(0, 6)
})

const hasStorySection = computed(() =>
  !!(activeBioText.value || person.value?.also_known_as?.length || departmentStats.value.length > 1 || profileThumbs.value.length > 0),
)

function creditSub(year: string | number | undefined | null, role?: string): string {
  const y = year ? String(year).slice(0, 4) : '—'
  return role ? `${y} · ${role}` : y
}

// ── Filmography — in-library ─────────────────────────────────────────────────
const sortedCast = computed(() => {
  if (!data.value?.cast_credits) return []
  return [...data.value.cast_credits].sort((a, b) => (b.year || '').localeCompare(a.year || ''))
})

interface DeptGroup { name: string; credits: any[] }
const crewDepts = computed<DeptGroup[]>(() => {
  if (!data.value?.crew_credits) return []
  const deptMap = new Map<string, any[]>()
  for (const c of data.value.crew_credits) {
    const dept = c.department || 'Other'
    if (!deptMap.has(dept)) deptMap.set(dept, [])
    deptMap.get(dept)!.push(c)
  }
  return Array.from(deptMap.entries())
    .map(([name, credits]) => ({ name, credits: credits.sort((a: any, b: any) => (b.year || '').localeCompare(a.year || '')) }))
    .sort((a, b) => b.credits.length - a.credits.length)
})

const filteredCrewDepts = computed(() => {
  if (activeFilter.value === 'all') return crewDepts.value
  if (activeFilter.value === 'acting') return []
  return crewDepts.value.filter(d => d.name === activeFilter.value)
})

const filterOptions = computed(() => {
  const opts: { key: string; label: string; count: number }[] = []
  const castCount = data.value?.cast_credits?.length || 0
  const crewCount = data.value?.crew_credits?.length || 0
  opts.push({ key: 'all', label: 'All', count: castCount + crewCount })
  if (castCount > 0) opts.push({ key: 'acting', label: 'Acting', count: castCount })
  for (const d of crewDepts.value) opts.push({ key: d.name, label: d.name, count: d.credits.length })
  return opts
})

// ── Scope toggle (In Library vs Known For) ───────────────────────────────────
const scope = ref<'library' | 'known'>('library')
watch(scope, () => { activeFilter.value = 'all' })

const libraryCount = computed(() =>
  (data.value?.cast_credits?.length || 0) + (data.value?.crew_credits?.length || 0),
)

const externalCast = computed(() => {
  const list = [...(data.value?.external_cast || [])]
  return list.sort((a, b) => (b.year || 0) - (a.year || 0) || a.display_order - b.display_order)
})

interface ExtDeptGroup { key: string; name: string; credits: PersonExternalCredit[] }
const externalCrewDepts = computed<ExtDeptGroup[]>(() => {
  const src = data.value?.external_crew || []
  if (!src.length) return []
  const deptMap = new Map<string, PersonExternalCredit[]>()
  for (const c of src) {
    const dept = c.department || 'Other'
    if (!deptMap.has(dept)) deptMap.set(dept, [])
    deptMap.get(dept)!.push(c)
  }
  return Array.from(deptMap.entries())
    .map(([name, credits]) => ({ key: name, name, credits: [...credits].sort((a, b) => (b.year || 0) - (a.year || 0)) }))
    .sort((a, b) => b.credits.length - a.credits.length)
})

const knownForCount = computed(() =>
  (data.value?.external_cast?.length || 0) + (data.value?.external_crew?.length || 0),
)
const hasAnyCredits = computed(() => libraryCount.value + knownForCount.value > 0)

const knownForFilterOptions = computed(() => {
  const opts: { key: string; label: string; count: number }[] = []
  const castCount = data.value?.external_cast?.length || 0
  const crewCount = data.value?.external_crew?.length || 0
  opts.push({ key: 'all', label: 'All', count: castCount + crewCount })
  if (castCount > 0) opts.push({ key: 'acting', label: 'Acting', count: castCount })
  for (const d of externalCrewDepts.value) opts.push({ key: d.key, label: d.name, count: d.credits.length })
  return opts
})

const activeFilterOptions = computed(() =>
  scope.value === 'library' ? filterOptions.value : knownForFilterOptions.value,
)
</script>

<style scoped>
/* ═══ HERO ═══════════════════════════════════════════════════════════════════
   Shared backdrop/carousel/zoom chrome (.hero-cycle, .hero-expand, .zoom-btn,
   .hero-section) lives in heya.css; only per-page deltas here. Hero text rides
   HeroCanvas's literal-dark grade, so --oink keeps it light in every theme. */
.person2 { --oink: 233 236 242; }

.person-hero {
  position: relative;
  min-height: 56vh;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
}

/* Photos fallback: no owned landscape art, so the profile headshots ride behind
   the hero — softened here (page-side) so the sharp record-card headshot stays
   the focal point. Reaches into HeroCanvas's own <img> via :deep (page-scoped,
   the shared component keeps its sharp default for backdrop-mode pages). */
.person-hero.photos-mode :deep(.hc-img) {
  filter: blur(26px) brightness(0.82) saturate(0.9);
  transform: scale(1.14);
}

.hero-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  padding: 120px var(--pad-fluid) 44px;
  display: flex;
  align-items: flex-end;
  gap: 44px;
}
.hero-inner > .grow { flex: 1; min-width: 0; }
.hero-left { flex: 0 0 232px; align-self: flex-end; }

/* portrait record-card — layered directional shadow (heya2.css .postercard) */
.postercard { position: relative; }
.postercard :deep(.poster) {
  width: 100%;
  border-radius: var(--r-md);
  overflow: hidden;
  box-shadow:
    0 0 0 1px rgb(var(--oink) / 0.16),
    10px 18px 34px -12px rgb(0 0 0 / 0.8),
    24px 44px 90px -20px rgb(0 0 0 / 0.95);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.postercard:hover :deep(.poster) { transform: translateY(-3px); }
.postercard :deep(.poster-initials) {
  font-family: var(--font-display);
  font-weight: 800;
  font-size: clamp(48px, 6vw, 76px);
  color: rgb(var(--oink) / 0.4);
}

/* mono eyebrow */
.eyebrow {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 18px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
}
.eyebrow .sep { color: rgb(var(--oink) / 0.3); }

/* Archivo display title */
.title {
  font-family: var(--font-display);
  font-size: clamp(2.5rem, 5.2vw, 4.4rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 0.99;
  text-wrap: balance;
  max-width: 18ch;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
  margin: 0;
}

.metaline {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  font: 500 12.5px var(--font-mono);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: rgb(var(--oink) / 0.72);
}
.metaline .dot { color: rgb(var(--tone-rgb) / 0.85); }

/* around-the-web pills (heya2.css .pill) */
.actions {
  margin-top: 26px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border-radius: 999px;
  cursor: pointer;
  text-decoration: none;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(var(--oink) / 0.9);
  font: 550 12.5px var(--font-sans);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.14), 5px 8px 22px -10px rgb(0 0 0 / 0.7);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.15s, color 0.15s;
}
.pill:hover {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  color: rgb(var(--oink));
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.28), 6px 10px 26px -10px rgb(0 0 0 / 0.75);
  transform: translateY(-1px);
}

/* ═══ BODY ════════════════════════════════════════════════════════════════════ */
.page { padding: 0 var(--pad-fluid) 90px; }
.section { margin-top: 52px; }
.section:first-of-type { margin-top: 44px; }

.cols {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
  gap: 56px;
  align-items: start;
}

.prose { font-size: 15.5px; line-height: 1.75; color: rgb(var(--ink) / 0.82); max-width: 64ch; }
.bio-para { margin: 0 0 14px; }
.prose-empty { font-size: 14px; color: rgb(var(--ink) / 0.5); font-style: italic; }

.bio-lang-pills { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 16px; }
.bio-lang-pill {
  font: 700 10px var(--font-mono);
  padding: 4px 10px;
  border-radius: 999px;
  color: rgb(var(--ink) / 0.6);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--hair);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  cursor: pointer;
  transition: color 0.15s, background 0.15s, border-color 0.15s;
}
.bio-lang-pill:hover { color: rgb(var(--ink) / 0.9); border-color: var(--hair-strong); }
.bio-lang-pill.active { color: var(--tone); background: rgb(var(--tone-rgb) / 0.12); border-color: rgb(var(--tone-rgb) / 0.35); }

.see-all {
  margin-top: 6px;
  background: none;
  border: none;
  color: var(--tone);
  cursor: pointer;
  font: 550 12px var(--font-mono);
  letter-spacing: 0.06em;
  padding: 4px 2px;
}
.see-all::before { content: '▾ '; opacity: 0.7; }
.see-all:hover { filter: brightness(1.15); }

.detail-grid { display: grid; grid-template-columns: 1fr; gap: 26px; margin-top: 30px; }
.detail-grid dt {
  font: 600 10.5px var(--font-mono);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
  margin-bottom: 10px;
}
.detail-grid dd { font-size: 13.5px; line-height: 1.8; color: rgb(var(--ink) / 0.75); }

/* Departments — a compact bar list (heya2.css side panel). */
.dept-list { display: flex; flex-direction: column; gap: 10px; }
.dept-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 90px auto;
  gap: 12px;
  align-items: center;
}
.dept-label {
  font: 500 12.5px var(--font-sans);
  color: rgb(var(--ink) / 0.78);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.dept-bar { height: 5px; border-radius: 3px; background: rgb(var(--ink) / 0.07); overflow: hidden; }
.dept-fill { height: 100%; border-radius: 3px; background: var(--tone); transition: width 0.4s ease; }
.dept-count {
  font: 600 11px var(--font-mono);
  color: rgb(var(--ink) / 0.5);
  text-align: right;
  min-width: 22px;
  font-variant-numeric: tabular-nums;
}
.mt-gap { margin-top: 34px; }

/* Photos strip — small 2:3 chips opening the gallery lightbox. */
.photo-strip { display: flex; flex-wrap: wrap; gap: 10px; }
.photo-thumb {
  width: 66px;
  border: 0; padding: 0; background: none; cursor: pointer;
  border-radius: var(--r-sm); overflow: hidden;
  transition: transform 0.15s ease, box-shadow 0.28s ease;
  box-shadow: var(--shadow-card);
}
.photo-thumb :deep(.poster) { border-radius: var(--r-sm); }
.photo-thumb:hover { transform: translateY(-3px); box-shadow: var(--shadow-card-hover); }
.photo-more {
  display: inline-flex; align-items: center; justify-content: center;
  aspect-ratio: 2/3;
  background: rgb(var(--ink) / 0.05);
  color: rgb(var(--ink) / 0.6);
  font: 700 12px var(--font-mono);
}

/* ═══ FILMOGRAPHY ═════════════════════════════════════════════════════════════ */
.filmo :deep(.sh-actions) { flex-wrap: wrap; }

/* Scope toggle — segmented control living in the SectionHeader actions slot. */
.scope-toggle {
  display: inline-flex;
  padding: 3px;
  background: rgb(var(--ink) / 0.05);
  border-radius: 999px;
  border: 1px solid var(--hair);
}
.scope-btn {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 6px 15px; border-radius: 999px;
  font: 600 12px var(--font-sans);
  color: rgb(var(--ink) / 0.55); background: transparent; border: 0;
  cursor: pointer; transition: color 0.15s, background 0.15s;
}
.scope-btn:hover { color: rgb(var(--ink) / 0.9); }
.scope-btn.active { background: var(--bg-1); color: rgb(var(--ink) / 0.95); box-shadow: 0 1px 3px rgb(var(--shade) / 0.3); }
.scope-count {
  font: 600 10.5px var(--font-mono);
  color: rgb(var(--ink) / 0.45);
  padding: 1px 6px; border-radius: 999px;
  background: rgb(var(--ink) / 0.06);
}
.scope-btn.active .scope-count { color: var(--tone); background: rgb(var(--tone-rgb) / 0.12); }

/* Role/department filter tabs — tone pills. */
.filmo-filters {
  display: flex; gap: 6px; flex-wrap: wrap;
  margin: -6px 0 26px;
}
.filter-tab {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 14px; border-radius: 999px;
  font: 600 11.5px var(--font-sans);
  color: rgb(var(--ink) / 0.6);
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--hair);
  white-space: nowrap; cursor: pointer;
  transition: color 0.15s, background 0.15s, border-color 0.15s;
}
.filter-tab:hover { color: rgb(var(--ink) / 0.92); border-color: var(--hair-strong); }
.filter-tab.active { color: var(--tone); background: rgb(var(--tone-rgb) / 0.1); border-color: rgb(var(--tone-rgb) / 0.35); }
.filter-count { font: 600 10px var(--font-mono); opacity: 0.7; }

.filmo-group { margin-top: 30px; }
.filmo-group:first-of-type { margin-top: 4px; }
.filmo-group-head {
  font: 600 11px var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.6);
  margin-bottom: 16px;
}
.filmo-group-n { color: var(--tone); margin-left: 4px; }

.filmo-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
  gap: 24px 18px;
}
.filmo-card { text-decoration: none; color: inherit; display: block; }
.filmo-card :deep(.poster) {
  box-shadow: var(--shadow-card);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.filmo-card:hover :deep(.poster) { transform: translateY(-4px); box-shadow: var(--shadow-card-hover); }
.filmo-card.is-external { opacity: 0.62; cursor: default; }
.filmo-card.is-external:hover { opacity: 1; }
.filmo-card.is-external:hover :deep(.poster) { transform: none; box-shadow: var(--shadow-card); }

/* ═══ RESPONSIVE ══════════════════════════════════════════════════════════════ */
@media (max-width: 1100px) {
  .cols { grid-template-columns: 1fr; gap: 40px; }
}

/* Tablet: hide the portrait record-card (mockup convention). */
@media (max-width: 960px) {
  .hero-left { display: none; }
  .hero-inner { padding: 96px var(--pad-fluid) 32px; gap: 28px; }
}

@media (max-width: 720px) {
  .person-hero { min-height: 48vh; }
  .hero-inner { padding: 84px var(--pad-fluid) 26px; }
  .title { font-size: clamp(2rem, 9vw, 3rem); }
  .actions { gap: 8px; row-gap: 10px; }
  .pill { flex: 1 1 auto; justify-content: center; height: 46px; }
  .filmo-grid { grid-template-columns: repeat(auto-fill, minmax(112px, 1fr)); gap: 16px 12px; }
  .filmo :deep(.sh-actions) { margin-left: 0; width: 100%; }
  .scope-toggle { flex: 1; }
  .scope-btn { flex: 1; justify-content: center; }
}
</style>
