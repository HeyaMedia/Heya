<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 520px; background: var(--bg-2)" />
  </div>

  <div v-else-if="data" class="scroll" style="height: 100%">
    <!-- Hero with crossfade backdrops. Person pages don't have proper
         landscape backdrops, so we use the same profile gallery photos —
         blurred + darkened so the headshot still reads as the focal point. -->
    <div class="hero-section">
      <div class="hero-bg">
        <NuxtImg v-if="backdropA" :src="backdropA" :width="1920" :quality="60" class="hero-bg-img" :class="{ visible: showA }" />
        <NuxtImg v-if="backdropB" :src="backdropB" :width="1920" :quality="60" class="hero-bg-img" :class="{ visible: !showA }" />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <!-- Left column: portrait + thumbnail strip -->
        <div class="hero-left">
          <div class="hero-portrait">
            <NuxtImg
              v-if="data.person.profile_path && !data.person.profile_path.startsWith('http')"
              :src="`/api/person/${data.person.id}/image`"
              :width="600"
              :quality="80"
              class="hero-portrait-img"
              @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
            />
            <div v-else class="hero-portrait-placeholder">
              {{ data.person.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}
            </div>
            <button v-if="galleryUrls.length > 0" class="zoom-btn" @click="openGallery(0)"><Icon name="expand" :size="14" /></button>
          </div>
        </div>

        <!-- Centre column: identity, bio, links -->
        <div class="hero-info">
          <div class="detail-badges">
            <Chip v-if="data.person.known_for_department" gold>{{ data.person.known_for_department }}</Chip>
            <Chip v-if="yearsActive">{{ yearsActive }}</Chip>
            <Chip v-if="totalCreditsCount">{{ totalCreditsCount }} credits</Chip>
          </div>

          <h1 class="detail-title">{{ data.person.name }}</h1>

          <div class="hero-meta-row" v-if="data.person.birthday || data.person.place_of_birth || data.person.deathday">
            <template v-if="data.person.birthday">
              <Icon name="cake" :size="14" style="color: var(--fg-3)" />
              <span>{{ formatDateLong(data.person.birthday) }}<template v-if="age">, age {{ age }}</template></span>
            </template>
            <template v-if="data.person.place_of_birth">
              <span class="dot" />
              <Icon name="globe" :size="14" style="color: var(--fg-3)" />
              <span>{{ data.person.place_of_birth }}</span>
            </template>
            <template v-if="data.person.deathday">
              <span class="dot" />
              <span style="color: var(--fg-3)">Died {{ formatDateLong(data.person.deathday) }}</span>
            </template>
          </div>

          <!-- Social / external links — same surface chrome as media detail. -->
          <div v-if="socialLinks.length" class="social-links">
            <a
              v-for="link in socialLinks"
              :key="link.platform"
              :href="link.url"
              target="_blank"
              rel="noopener noreferrer"
              class="social-link"
              :title="link.label"
            >
              <Icon :name="link.icon" :size="16" />
              <span class="social-label">{{ link.label }}</span>
            </a>
          </div>

          <!-- Biography. The language pill row sits inline above the text
               when more than one bio is available; otherwise it's hidden. -->
          <div v-if="activeBioText" class="bio-block">
            <div v-if="bioLanguageOptions.length > 1" class="bio-lang-pills">
              <button
                v-for="opt in bioLanguageOptions"
                :key="opt.code"
                type="button"
                class="bio-lang-pill"
                :class="{ active: selectedBioLang === opt.code }"
                @click="selectedBioLang = opt.code"
              >{{ opt.label }}</button>
            </div>
            <p v-for="(para, i) in bioParas" :key="i" class="bio-para">{{ para }}</p>
            <button v-if="bioTruncated" class="bio-toggle" @click="bioExpanded = !bioExpanded">
              {{ bioExpanded ? 'Show less' : 'Read more' }}
            </button>
          </div>

          <div v-if="data.person.also_known_as?.length" class="person-aka">
            <span class="aka-label">Also known as</span>
            <span class="aka-names">{{ data.person.also_known_as.slice(0, 6).join(' · ') }}</span>
          </div>

          <!-- Profile thumbnail strip — moved from under the portrait so
               the filmography section can sit higher on the page. Renders
               as a horizontal row of small 2:3 chips that open the
               gallery lightbox at the clicked index. -->
          <div v-if="galleryUrls.length > 1" class="profile-strip">
            <button
              v-for="(url, idx) in galleryThumbs"
              :key="idx"
              type="button"
              class="profile-thumb-wrap"
              :aria-label="`Open photo ${idx + 1}`"
              @click="openGallery(galleryHeroOffset + idx)"
            >
              <img :src="url" :alt="`Profile ${idx + 1}`" class="profile-thumb" />
            </button>
            <button v-if="extraProfileCount > 0" type="button" class="profile-thumb-wrap profile-more" @click="openGallery(galleryHeroOffset)">
              +{{ extraProfileCount }}
            </button>
          </div>
        </div>

        <!-- Right column: department breakdown sidebar (only renders
             when the person has more than one department or substantial
             credits — otherwise it just clutters the layout). -->
        <div v-if="departmentStats.length > 1" class="hero-side">
          <div class="stat-card">
            <div class="stat-card-head">Departments</div>
            <div class="stat-row" v-for="d in departmentStats" :key="d.name">
              <div class="stat-row-label">{{ d.name }}</div>
              <div class="stat-row-bar">
                <div class="stat-row-fill" :style="{ width: d.pct + '%' }" />
              </div>
              <div class="stat-row-count">{{ d.count }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Filmography body below the hero -->
    <div class="filmography-body">

      <!-- Scope toggle (In Library vs Known For). Known-for tab only
           shows when the upstream actually reported credits that aren't
           in the library — no point dangling an empty tab. -->
      <div v-if="hasAnyCredits" class="scope-toggle">
        <button
          class="scope-btn"
          :class="{ active: scope === 'library' }"
          @click="scope = 'library'"
        >
          In Library
          <span class="scope-count">{{ libraryCount }}</span>
        </button>
        <button
          v-if="knownForCount > 0"
          class="scope-btn"
          :class="{ active: scope === 'known' }"
          @click="scope = 'known'"
        >
          Known For
          <span class="scope-count">{{ knownForCount }}</span>
        </button>
      </div>

      <!-- Filter tabs (acting/department slicing within the active scope) -->
      <div v-if="activeFilterOptions.length > 1" class="filmography-filters">
        <button
          v-for="f in activeFilterOptions"
          :key="f.key"
          class="filter-tab"
          :class="{ active: activeFilter === f.key }"
          @click="activeFilter = f.key"
        >
          {{ f.label }}
          <span class="filter-count">{{ f.count }}</span>
        </button>
      </div>

      <!-- IN LIBRARY scope -->
      <template v-if="scope === 'library'">
        <!-- Cast / Acting credits -->
        <div v-if="activeFilter === 'all' || activeFilter === 'acting'" class="detail-section">
          <template v-if="sortedCast.length">
            <h3 class="section-title" style="margin-bottom: 16px">Acting</h3>
            <div class="credits-grid">
              <NuxtLink
                v-for="c in sortedCast"
                :key="`cast-${c.media_item_id}`"
                :to="mediaUrl({ id: c.media_item_id, title: c.title, year: c.year, media_type: c.media_type })"
                class="credit-card card-tile"
              >
                <MediaCard
                  :idx="c.media_item_id"
                  :src="usePosterUrl(c.media_item_id)"
                  aspect="2/3"
                  :title="c.title"
                  :subtitle="(c.year || '?') + (c.character ? ` · ${c.character}` : '')"
                />
              </NuxtLink>
            </div>
          </template>
        </div>

        <!-- Crew credits grouped by department -->
        <template v-if="activeFilter === 'all' || activeFilter !== 'acting'">
          <div v-for="dept in filteredCrewDepts" :key="dept.name" class="detail-section">
            <h3 class="section-title" style="margin-bottom: 16px">{{ dept.name }}</h3>
            <div class="credits-grid">
              <NuxtLink
                v-for="c in dept.credits"
                :key="`crew-${c.media_item_id}-${c.job}`"
                :to="mediaUrl({ id: c.media_item_id, title: c.title, year: c.year, media_type: c.media_type })"
                class="credit-card card-tile"
              >
                <MediaCard
                  :idx="c.media_item_id"
                  :src="usePosterUrl(c.media_item_id)"
                  aspect="2/3"
                  :title="c.title"
                  :subtitle="(c.year || '?') + (c.job ? ` · ${c.job}` : '')"
                />
              </NuxtLink>
            </div>
          </div>
        </template>
      </template>

      <!-- KNOWN FOR scope -->
      <template v-else-if="scope === 'known'">
        <!-- External cast (acting) — only when the role filter is "all" or "acting" -->
        <div v-if="(activeFilter === 'all' || activeFilter === 'acting') && externalCast.length" class="detail-section">
          <h3 class="section-title" style="margin-bottom: 16px">Acting</h3>
          <div class="credits-grid">
            <div
              v-for="c in externalCast"
              :key="`ext-cast-${c.id}`"
              class="credit-card card-tile credit-external"
            >
              <MediaCard
                :idx="c.id"
                :src="c.poster_url"
                aspect="2/3"
                :title="c.title"
                :subtitle="(c.year || '?') + (c.character ? ` · ${c.character}` : '')"
              />
            </div>
          </div>
        </div>

        <!-- External crew grouped by department -->
        <template v-for="dept in externalCrewDepts" :key="`ext-${dept.name}`">
          <div v-if="activeFilter === 'all' || activeFilter === dept.key" class="detail-section">
            <h3 class="section-title" style="margin-bottom: 16px">{{ dept.name }}</h3>
            <div class="credits-grid">
              <div
                v-for="c in dept.credits"
                :key="`ext-crew-${c.id}`"
                class="credit-card card-tile credit-external"
              >
                <MediaCard
                  :idx="c.id"
                  :src="c.poster_url"
                  aspect="2/3"
                  :title="c.title"
                  :subtitle="(c.year || '?') + (c.job ? ` · ${c.job}` : '')"
                />
              </div>
            </div>
          </div>
        </template>
      </template>
    </div>
  </div>

  <div v-else class="scroll" style="height: 100%; display: flex; align-items: center; justify-content: center">
    <div style="text-align: center; color: var(--fg-2)">
      <p style="font-size: 18px">Person not found</p>
      <button class="btn btn-secondary" style="margin-top: 16px" @click="$router.back()">Go back</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { PersonResponse } from '~~/shared/types'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const lightbox = useLightbox()

const data = ref<PersonResponse | null>(null)
const loading = ref(true)
const activeFilter = ref('all')
const bioExpanded = ref(false)
const selectedBioLang = ref('en')

// All available photos for the lightbox. The hero (`/api/person/{id}/image`)
// goes first so opening from the hero zoom-btn lands on index 0, then the
// rest of the `profiles[]` follow. `galleryHeroOffset` is the position the
// hero occupies — 1 if we have one, 0 if we don't — so the strip's "+N more"
// shortcut can open the hero when it exists.
const galleryUrls = computed<string[]>(() => {
  if (!data.value) return []
  const urls: string[] = []
  if (data.value.person.profile_path && !data.value.person.profile_path.startsWith('http')) {
    urls.push(`/api/person/${data.value.person.id}/image`)
  }
  if (data.value.profiles) urls.push(...data.value.profiles.map(p => p.url))
  return urls
})
const galleryHeroOffset = computed(() => (data.value?.person.profile_path && !data.value.person.profile_path.startsWith('http')) ? 1 : 0)
const galleryThumbs = computed(() => (data.value?.profiles || []).slice(0, 8).map(p => p.url))
const extraProfileCount = computed(() => Math.max(0, (data.value?.profiles?.length || 0) - 8))

function openGallery(idx: number) {
  if (galleryUrls.value.length === 0) return
  lightbox.open(galleryUrls.value, idx)
}

// -- Backdrop crossfade --------------------------------------------------
// Person pages don't have proper landscape backdrops, but the profile
// gallery often contains a few stylized portraits. We re-use them as the
// hero background — heavily blurred + darkened via CSS so the focal-point
// portrait still reads as the subject. Pool = profiles minus the first
// (hero) image so the backdrop never just mirrors the foreground.
const backdropPool = computed<string[]>(() => {
  const profs = data.value?.profiles || []
  if (profs.length <= 1) return profs.map(p => p.url)
  return profs.slice(1).map(p => p.url)
})
const showA = ref(true)
const backdropA = ref<string | null>(null)
const backdropB = ref<string | null>(null)
const bdIdx = ref(0)
const BACKDROP_INTERVAL = 8000
let bdTimer: ReturnType<typeof setTimeout> | null = null

function advanceBackdrop() {
  if (backdropPool.value.length <= 1) return
  bdIdx.value = (bdIdx.value + 1) % backdropPool.value.length
  const url = backdropPool.value[bdIdx.value] || null
  if (showA.value) backdropB.value = url
  else backdropA.value = url
  showA.value = !showA.value
}

function startBackdropTimer() {
  if (backdropPool.value.length <= 1) return
  bdTimer = setTimeout(() => { advanceBackdrop(); startBackdropTimer() }, BACKDROP_INTERVAL)
}

onUnmounted(() => { if (bdTimer) clearTimeout(bdTimer) })

// Age & years active — small derived facts that surface as chips on the
// hero. Age compares birthday to deathday (if dead) or today.
const age = computed(() => {
  if (!data.value?.person.birthday) return 0
  const birth = new Date(data.value.person.birthday + 'T00:00:00')
  if (Number.isNaN(birth.getTime())) return 0
  const end = data.value.person.deathday
    ? new Date(data.value.person.deathday + 'T00:00:00')
    : new Date()
  let a = end.getFullYear() - birth.getFullYear()
  const m = end.getMonth() - birth.getMonth()
  if (m < 0 || (m === 0 && end.getDate() < birth.getDate())) a--
  return a
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
  if (min === max) return `${min}`
  return `${min}–${max}`
})

const totalCreditsCount = computed(() => {
  return (data.value?.cast_credits?.length || 0)
    + (data.value?.crew_credits?.length || 0)
    + (data.value?.external_cast?.length || 0)
    + (data.value?.external_crew?.length || 0)
})

// Department breakdown — counts every credit (in-library + known-for) by
// department, normalized to %s for the bar chart. Acting collapses
// cast credits into one row even though they don't carry a department
// upstream. Limits to the top 6 departments.
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

// Language display names
const langNames: Record<string, string> = {
  en: 'English', de: 'Deutsch', fr: 'Francais', es: 'Espanol', it: 'Italiano',
  pt: 'Portugues', ja: 'Japanese', ko: 'Korean', zh: 'Chinese', ru: 'Russian',
  nl: 'Nederlands', sv: 'Svenska', da: 'Dansk', no: 'Norsk', fi: 'Suomi',
  pl: 'Polski', cs: 'Cestina', hu: 'Magyar', ro: 'Romana', tr: 'Turkce',
  ar: 'Arabic', he: 'Hebrew', th: 'Thai', vi: 'Tieng Viet', id: 'Indonesian',
}

// Biography language options
const bioLanguageOptions = computed(() => {
  const opts: { code: string; label: string }[] = []
  // Primary bio is always "primary" option
  if (data.value?.person.biography) {
    opts.push({ code: '_primary', label: 'Primary' })
  }
  if (data.value?.biographies) {
    for (const b of data.value.biographies) {
      // Skip if same as primary
      if (b.biography === data.value.person.biography) continue
      opts.push({ code: b.language, label: langNames[b.language] || b.language.toUpperCase() })
    }
  }
  return opts
})

// Initialize selected language when data loads
watch(() => data.value, (val) => {
  if (!val) return
  // Prefer English bio from biographies, else primary
  if (val.biographies?.some(b => b.language === 'en')) {
    selectedBioLang.value = 'en'
  } else {
    selectedBioLang.value = '_primary'
  }
}, { immediate: true })

// Active biography text based on selected language
const activeBioText = computed(() => {
  if (!data.value) return ''
  if (selectedBioLang.value === '_primary') {
    return data.value.person.biography || ''
  }
  const match = data.value.biographies?.find(b => b.language === selectedBioLang.value)
  return match?.biography || data.value.person.biography || ''
})

const allParas = computed(() => activeBioText.value.split('\n\n').filter(Boolean))
const bioTruncated = computed(() => allParas.value.length > 3)
const bioParas = computed(() => {
  if (bioExpanded.value || !bioTruncated.value) return allParas.value
  return allParas.value.slice(0, 2)
})

// Social links from external_ids
interface SocialLink {
  platform: string
  url: string
  icon: string
  label: string
}

const socialLinks = computed<SocialLink[]>(() => {
  const ids = data.value?.person.external_ids
  if (!ids) return []
  const links: SocialLink[] = []
  if (data.value?.person.imdb_id) {
    links.push({ platform: 'imdb', url: `https://www.imdb.com/name/${data.value.person.imdb_id}`, icon: 'globe', label: 'IMDb' })
  }
  if (ids.twitter) {
    links.push({ platform: 'twitter', url: `https://twitter.com/${ids.twitter}`, icon: 'globe', label: 'Twitter / X' })
  }
  if (ids.instagram) {
    links.push({ platform: 'instagram', url: `https://instagram.com/${ids.instagram}`, icon: 'globe', label: 'Instagram' })
  }
  if (ids.facebook) {
    links.push({ platform: 'facebook', url: `https://facebook.com/${ids.facebook}`, icon: 'globe', label: 'Facebook' })
  }
  if (ids.tiktok) {
    links.push({ platform: 'tiktok', url: `https://tiktok.com/@${ids.tiktok}`, icon: 'globe', label: 'TikTok' })
  }
  if (ids.wikidata) {
    links.push({ platform: 'wikidata', url: `https://www.wikidata.org/wiki/${ids.wikidata}`, icon: 'globe', label: 'Wikidata' })
  }
  return links
})

const sortedCast = computed(() => {
  if (!data.value?.cast_credits) return []
  return [...data.value.cast_credits].sort((a, b) => (b.year || '').localeCompare(a.year || ''))
})

interface DeptGroup {
  name: string
  credits: any[]
}

const crewDepts = computed<DeptGroup[]>(() => {
  if (!data.value?.crew_credits) return []
  const deptMap = new Map<string, any[]>()
  for (const c of data.value.crew_credits) {
    const dept = c.department || 'Other'
    if (!deptMap.has(dept)) deptMap.set(dept, [])
    deptMap.get(dept)!.push(c)
  }
  return Array.from(deptMap.entries())
    .map(([name, credits]) => ({
      name,
      credits: credits.sort((a: any, b: any) => (b.year || '').localeCompare(a.year || '')),
    }))
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
  for (const d of crewDepts.value) {
    opts.push({ key: d.name, label: d.name, count: d.credits.length })
  }
  return opts
})

// -- Scope toggle (In Library vs Known For) -----------------------------
const scope = ref<'library' | 'known'>('library')

// Reset the filter when the user toggles scope so we don't carry an
// "Acting" filter into a scope that has only Camera credits, etc.
watch(scope, () => { activeFilter.value = 'all' })

const libraryCount = computed(() =>
  (data.value?.cast_credits?.length || 0) + (data.value?.crew_credits?.length || 0),
)

const externalCast = computed(() => {
  const list = [...(data.value?.external_cast || [])]
  return list.sort((a, b) => (b.year || 0) - (a.year || 0) || a.display_order - b.display_order)
})

// Group external crew by department, sorted by credit count desc — same
// shape as `crewDepts` for the library scope so the FE renders identical
// section headers.
interface ExtDeptGroup {
  key: string
  name: string
  credits: import('~~/shared/types').PersonExternalCredit[]
}

const externalCrewDepts = computed<ExtDeptGroup[]>(() => {
  const src = data.value?.external_crew || []
  if (!src.length) return []
  const deptMap = new Map<string, typeof src>()
  for (const c of src) {
    const dept = c.department || 'Other'
    if (!deptMap.has(dept)) deptMap.set(dept, [])
    deptMap.get(dept)!.push(c)
  }
  return Array.from(deptMap.entries())
    .map(([name, credits]) => ({
      key: name,
      name,
      credits: [...credits].sort((a, b) => (b.year || 0) - (a.year || 0)),
    }))
    .sort((a, b) => b.credits.length - a.credits.length)
})

const knownForCount = computed(() =>
  (data.value?.external_cast?.length || 0) + (data.value?.external_crew?.length || 0),
)

const hasAnyCredits = computed(() => libraryCount.value + knownForCount.value > 0)

// Filter options vary by scope: library scope uses cast_credits/crew_credits
// counts, known-for scope uses external_cast/external_crew counts. Both
// reuse the "All / Acting / <department>" shape.
const knownForFilterOptions = computed(() => {
  const opts: { key: string; label: string; count: number }[] = []
  const castCount = data.value?.external_cast?.length || 0
  const crewCount = data.value?.external_crew?.length || 0
  opts.push({ key: 'all', label: 'All', count: castCount + crewCount })
  if (castCount > 0) opts.push({ key: 'acting', label: 'Acting', count: castCount })
  for (const d of externalCrewDepts.value) {
    opts.push({ key: d.key, label: d.name, count: d.credits.length })
  }
  return opts
})

const activeFilterOptions = computed(() =>
  scope.value === 'library' ? filterOptions.value : knownForFilterOptions.value,
)

onMounted(async () => {
  try {
    const { $heya } = useNuxtApp()
    data.value = await $heya('/api/person/{id}', { path: { id: slug.value } }) as PersonResponse
  } catch { /* empty */ }
  loading.value = false

  // Seed the backdrop crossfade once the gallery is known. The first
  // backdrop is visible on mount; the second is preloaded into the
  // hidden img so the first transition is instant rather than blank.
  if (backdropPool.value.length > 0) {
    backdropA.value = backdropPool.value[0] || null
    if (backdropPool.value.length > 1) {
      backdropB.value = backdropPool.value[1] || null
      startBackdropTimer()
    }
  }
})
</script>

<style scoped>
/* Hero — mirrors movies/[slug] + tv/[slug] hero pattern. The shared
   backdrop chrome (.hero-section, .hero-bg*, .hero-side) lives in heya.css.
   Person pages deliberately have NO min-height on .hero-section — the hero
   shrinks to fit the actual content so we don't leave a 100px void between
   the portrait and the filmography section when the bio is short.
   Person pages don't have proper backdrops so we recycle the profile
   gallery photos with heavy blur via the `.hero-bg-img` filter below. */
.hero-bg-img {
  /* Heavy blur + darkening — the foreground portrait remains the focal
     point. Scale slightly so the blur doesn't expose the photo edge. */
  filter: blur(30px) brightness(0.45) saturate(0.85);
  transform: scale(1.08);
}

.hero-content {
  position: relative; z-index: 2;
  display: grid; grid-template-columns: 240px minmax(0, 1fr) 260px;
  gap: 36px; padding: 40px 40px 24px;
}
.hero-left { display: flex; flex-direction: column; gap: 14px; align-self: start; min-width: 0; }
.hero-info { display: flex; flex-direction: column; min-width: 0; }

/* Portrait photo — same poster treatment as movie/tv hero. 2:3 aspect
   with a subtle shadow + frame so it pops against the blurred backdrop. */
.hero-portrait {
  position: relative; aspect-ratio: 2/3;
  border-radius: var(--r-md); overflow: hidden;
  box-shadow: 0 24px 60px rgba(0,0,0,0.6), 0 0 0 1px rgba(255,255,255,0.06);
  background: var(--bg-3);
}
.hero-portrait-img { width: 100%; height: 100%; object-fit: cover; display: block; }
.hero-portrait-placeholder {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: linear-gradient(135deg, var(--bg-4), var(--bg-3));
  font-size: 56px; font-weight: 600; color: var(--fg-2);
}
/* Base .zoom-btn chrome comes from heya.css; nudge it off the portrait
   frame a touch further than the poster default (8px). */
.zoom-btn { top: 10px; right: 10px; }
.hero-portrait:hover .zoom-btn { opacity: 1; }

/* Profile strip — horizontal row inside the hero-info column, below the
   AKA divider. Small 2:3 chips, fixed height so the filmography section
   below the hero sits at a consistent baseline regardless of how many
   thumbs render. Clicks open the gallery lightbox at the matching idx. */
.profile-strip {
  display: flex; gap: 6px; flex-wrap: wrap; margin-top: 14px;
}
.profile-thumb-wrap {
  position: relative; width: 40px; height: 60px;
  border-radius: var(--r-sm); overflow: hidden;
  border: 2px solid transparent; background: var(--bg-3);
  cursor: pointer; padding: 0; transition: border-color 0.15s, transform 0.15s;
  flex-shrink: 0;
}
.profile-thumb-wrap:hover { border-color: var(--gold); transform: translateY(-2px); }
.profile-thumb { width: 100%; height: 100%; object-fit: cover; display: block; }
.profile-more {
  display: flex; align-items: center; justify-content: center;
  background: var(--bg-3); color: var(--fg-2);
  font-family: var(--font-mono); font-size: 11px; font-weight: 600;
}

/* Identity column — same `.detail-*` token family as movie/tv hero so the
   typography reads as one design system. */
.detail-badges { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
.detail-title { font-size: 44px; font-weight: 600; letter-spacing: -0.025em; line-height: 1.05; margin: 0 0 4px; }
.hero-meta-row {
  display: flex; align-items: center; gap: 8px;
  font-size: 13px; color: var(--fg-1); margin: 12px 0 4px; flex-wrap: wrap;
}
.hero-meta-row .dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }

/* Social links — surface-style chips so they sit nicely on the glass.
   Compact icon-only on mobile / narrow widths, icon + label on wider. */
.social-links { display: flex; gap: 6px; flex-wrap: wrap; margin: 14px 0 18px; }
.social-link {
  display: inline-flex; align-items: center; gap: 6px;
  height: 30px; padding: 0 12px;
  border-radius: 100px;
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  color: var(--fg-2); text-decoration: none;
  font-size: 12px; font-weight: 500;
  transition: all 0.15s;
}
.social-link:hover {
  background: var(--gold-soft); color: var(--gold); border-color: transparent;
}
.social-label { font-family: var(--font-mono); letter-spacing: 0.02em; }

/* Biography. The language picker is a compact pill row above the text
   that lets users glance the available langs without opening a select. */
.bio-block { margin-top: 4px; max-width: 760px; }
.bio-lang-pills { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 12px; }
.bio-lang-pill {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  padding: 3px 9px; border-radius: 100px;
  color: var(--fg-3); background: transparent;
  border: 1px solid var(--border);
  text-transform: uppercase; letter-spacing: 0.06em;
  cursor: pointer; transition: all 0.15s;
}
.bio-lang-pill:hover { color: var(--fg-0); border-color: var(--fg-3); }
.bio-lang-pill.active { color: var(--gold); background: var(--gold-soft); border-color: transparent; }
.bio-para { font-size: 14.5px; line-height: 1.7; color: var(--fg-1); margin: 0 0 14px; }
.bio-toggle {
  font-size: 11px; font-weight: 700; color: var(--gold);
  font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.06em;
  background: none; border: 0; cursor: pointer; padding: 0;
  transition: opacity 0.12s;
}
.bio-toggle:hover { opacity: 0.8; }

.person-aka {
  margin-top: 18px; padding-top: 14px;
  border-top: 1px solid var(--border);
  font-size: 12px; color: var(--fg-3);
}
.aka-label {
  font-family: var(--font-mono); text-transform: uppercase;
  letter-spacing: 0.06em; color: var(--fg-3); margin-right: 8px;
}
.aka-names { color: var(--fg-2); }

/* Sidebar stat card — same surface chrome as the rest of the app. The
   bar chart is a tiny inline visualization rather than a separate chart
   library; matches how settings panels render usage bars. */
.stat-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 16px 18px;
}
.stat-card-head {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--fg-3); margin-bottom: 14px;
}
.stat-row {
  display: grid; grid-template-columns: minmax(0, 1fr) 60px auto;
  gap: 10px; align-items: center; margin-bottom: 8px;
}
.stat-row-label {
  font-size: 12px; color: var(--fg-1);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.stat-row-bar {
  height: 4px; border-radius: 2px;
  background: rgba(255,255,255,0.06); overflow: hidden;
}
.stat-row-fill {
  height: 100%; background: var(--gold); border-radius: 2px;
  transition: width 0.4s ease;
}
.stat-row-count {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-3);
  text-align: right; min-width: 24px;
}

/* Filmography body — sits below the hero, inherits the page padding the
   hero established (40px L/R). 0 top padding so the scope toggle hugs the
   bottom of the hero — keeps the cards visible above the fold. */
.filmography-body { padding: 0 40px 80px; max-width: 1400px; }

/* Scope toggle — bigger segmented control above the role-filter strip.
   Toggles between "In Library" (credits the user owns) and "Known For"
   (upstream-reported credits the user does not own). Hidden when neither
   side has anything to show. */
.scope-toggle {
  display: inline-flex; gap: 0;
  margin-bottom: 16px;
  padding: 3px;
  background: var(--bg-3);
  border-radius: 100px;
  border: 1px solid var(--border);
}
.scope-btn {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 8px 18px; border-radius: 999px;
  font-size: 13px; font-weight: 600;
  color: var(--fg-2); background: transparent; border: 0;
  cursor: pointer; transition: all 0.15s;
}
.scope-btn:hover { color: var(--fg-0); }
.scope-btn.active { background: var(--bg-1); color: var(--fg-0); box-shadow: 0 1px 3px rgba(0,0,0,0.3); }
.scope-count {
  font-size: 11px; font-family: var(--font-mono);
  color: var(--fg-3); padding: 1px 7px; border-radius: 999px;
  background: rgba(255,255,255,0.06);
}
.scope-btn.active .scope-count { background: rgba(255,196,50,0.12); color: var(--gold); }

/* Known-for credits get a slightly dimmed look so users can tell at a
   glance they're not clickable / not local. */
.credit-external { cursor: default; opacity: 0.85; }
.credit-external:hover { opacity: 1; }

.filmography-filters {
  display: flex; gap: 4px; padding-bottom: 20px;
  border-bottom: 1px solid var(--border); margin-bottom: 24px;
  overflow-x: auto; scrollbar-width: none;
}
.filmography-filters::-webkit-scrollbar { display: none; }
.filter-tab {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 14px; border-radius: 100px; font-size: 12px; font-weight: 600;
  color: var(--fg-2); background: transparent;
  border: 1px solid var(--border); white-space: nowrap;
  transition: all 0.15s;
}
.filter-tab:hover { border-color: var(--fg-3); color: var(--fg-0); }
.filter-tab.active { background: var(--gold-soft); border-color: transparent; color: var(--gold); }
.filter-count {
  font-size: 10px; font-family: var(--font-mono); opacity: 0.6;
}

.credits-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 18px; }
.credit-card { text-decoration: none; color: inherit; }
.credit-card:hover .grid-tile-title { color: var(--gold); }

/* Tablet: single-column hero, portrait shrinks and centers, department
   sidebar (when present) drops below and centers as its own block. */
@media (max-width: 960px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; padding: 32px 24px 20px; }
  .hero-left { align-self: center; }
  .hero-portrait { width: 200px; margin: 0 auto; }
  .hero-info { align-items: center; }
  .detail-badges, .hero-meta-row, .social-links, .profile-strip { justify-content: center; }
  .hero-side { grid-column: 1 / -1; }
  .stat-card { max-width: 420px; margin: 0 auto; }
  .filmography-body { padding: 0 24px 60px; }
}

/* Phone: portrait shrinks further, everything in the identity column
   centers except the prose (bio / AKA), which stays left-aligned for
   readability — `align-items: center` on `.hero-info` only affects each
   child's own box, not the text inside it, so a wide `.bio-block` still
   reads left-to-right normally. Filmography grid density matches the
   `.grid-posters` phone convention (heya.css) since this is a page-local
   grid, not that shared class. */
@media (max-width: 720px) {
  .hero-content { padding: 24px 16px 20px; gap: 16px; }
  .hero-portrait { width: 140px; }
  .detail-title { font-size: 28px; }
  .filmography-body { padding: 0 16px 60px; }
  .credits-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .scope-toggle { display: flex; justify-content: center; }
}
</style>
