<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div class="page-pad"><div style="height: 200px; background: var(--bg-2); border-radius: var(--r-md)" /></div>
  </div>

  <div v-else-if="data" class="scroll" style="height: 100%">
    <div class="page-pad" style="max-width: 1200px">
      <div class="person-header">
        <div class="person-photo-column">
          <div v-if="data.person.profile_path && !data.person.profile_path.startsWith('http')" class="person-photo-wrap">
            <img
              :src="`/api/person/${data.person.id}/image`"
              class="person-photo"
              @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
            />
            <button class="zoom-btn" @click="lightbox.open(`/api/person/${data.person.id}/image`)"><Icon name="expand" :size="14" /></button>
          </div>
          <div v-else class="person-photo-placeholder">
            {{ data.person.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}
          </div>

          <!-- Profile gallery -->
          <div v-if="data.profiles && data.profiles.length > 1" class="profile-gallery">
            <div
              v-for="(prof, idx) in data.profiles.slice(0, 8)"
              :key="prof.id"
              class="profile-thumb-wrap"
              @click="lightbox.open(prof.url)"
            >
              <img :src="prof.url" :alt="`Profile ${idx + 1}`" class="profile-thumb" />
            </div>
          </div>
        </div>

        <div class="person-info">
          <h1 class="person-name">{{ data.person.name }}</h1>
          <span v-if="data.person.known_for_department" class="department-badge">{{ data.person.known_for_department }}</span>

          <!-- Social links -->
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
              <Icon :name="link.icon" :size="20" />
            </a>
          </div>

          <div class="person-meta">
            <span v-if="data.person.birthday">Born {{ formatDate(data.person.birthday) }}</span>
            <template v-if="data.person.birthday && data.person.place_of_birth"><span class="dot" /></template>
            <span v-if="data.person.place_of_birth">{{ data.person.place_of_birth }}</span>
            <template v-if="data.person.deathday"><span class="dot" /><span>Died {{ formatDate(data.person.deathday) }}</span></template>
          </div>
          <div class="person-stats">
            <span v-if="data.cast_credits?.length" class="stat">{{ data.cast_credits.length }} role{{ data.cast_credits.length !== 1 ? 's' : '' }}</span>
            <span v-if="data.crew_credits?.length" class="stat">{{ data.crew_credits.length }} crew credit{{ data.crew_credits.length !== 1 ? 's' : '' }}</span>
          </div>
          <div v-if="data.person.also_known_as?.length" class="person-aka">
            Also known as: {{ data.person.also_known_as.slice(0, 5).join(', ') }}
          </div>
        </div>
      </div>

      <!-- Biography with multi-language selector -->
      <div v-if="activeBioText" class="person-bio">
        <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 12px">
          <h3 class="section-title" style="margin: 0">Biography</h3>
          <select
            v-if="bioLanguageOptions.length > 1"
            v-model="selectedBioLang"
            class="bio-lang-select"
          >
            <option v-for="opt in bioLanguageOptions" :key="opt.code" :value="opt.code">
              {{ opt.label }}
            </option>
          </select>
        </div>
        <p v-for="(para, i) in bioParas" :key="i" class="bio-para">{{ para }}</p>
        <button v-if="bioTruncated" class="bio-toggle" @click="bioExpanded = !bioExpanded">
          {{ bioExpanded ? 'Show less' : 'Read more' }}
        </button>
      </div>

      <!-- Filter tabs -->
      <div v-if="data.cast_credits?.length || data.crew_credits?.length" class="filmography-filters">
        <button
          v-for="f in filterOptions"
          :key="f.key"
          class="filter-tab"
          :class="{ active: activeFilter === f.key }"
          @click="activeFilter = f.key"
        >
          {{ f.label }}
          <span class="filter-count">{{ f.count }}</span>
        </button>
      </div>

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
              <Poster :idx="c.media_item_id" :src="usePosterUrl(c.media_item_id)" aspect="2/3" :title="c.title" />
              <div class="grid-tile-meta">
                <div class="grid-tile-title">{{ c.title }}</div>
                <div class="grid-tile-sub">
                  {{ c.year || '?' }}
                  <template v-if="c.character"> · {{ c.character }}</template>
                </div>
              </div>
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
              <Poster :idx="c.media_item_id" :src="usePosterUrl(c.media_item_id)" aspect="2/3" :title="c.title" />
              <div class="grid-tile-meta">
                <div class="grid-tile-title">{{ c.title }}</div>
                <div class="grid-tile-sub">
                  {{ c.year || '?' }}
                  <template v-if="c.job"> · {{ c.job }}</template>
                </div>
              </div>
            </NuxtLink>
          </div>
        </div>
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

function formatDate(d: string) {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

onMounted(async () => {
  try {
    const { $heya } = useNuxtApp()
    data.value = await $heya('/api/person/{id}', { path: { id: slug.value } }) as PersonResponse
  } catch { /* empty */ }
  loading.value = false
})
</script>

<style scoped>
.person-header { display: flex; gap: 32px; margin-bottom: 40px; }
.person-photo-column { display: flex; flex-direction: column; align-items: center; gap: 12px; flex-shrink: 0; }
.person-photo-wrap { position: relative; width: 180px; height: 180px; border-radius: 50%; overflow: hidden; flex-shrink: 0; }
.person-photo { width: 100%; height: 100%; object-fit: cover; }
.zoom-btn {
  position: absolute; top: 8px; right: 8px;
  width: 28px; height: 28px; border-radius: var(--r-sm);
  background: rgba(0,0,0,0.55); color: rgba(255,255,255,0.7);
  display: flex; align-items: center; justify-content: center;
  opacity: 0; transition: opacity 0.15s, background 0.15s;
  cursor: zoom-in; z-index: 2;
}
.zoom-btn:hover { background: rgba(0,0,0,0.8); color: #fff; }
.person-photo-wrap:hover .zoom-btn { opacity: 1; }
.person-photo-placeholder {
  width: 180px; height: 180px; border-radius: 50%;
  background: linear-gradient(135deg, var(--bg-4), var(--bg-3));
  display: flex; align-items: center; justify-content: center;
  font-size: 48px; font-weight: 600; color: var(--fg-2); flex-shrink: 0;
}
.profile-gallery {
  display: flex; gap: 6px; overflow-x: auto; max-width: 180px;
  scrollbar-width: none; padding: 2px 0;
}
.profile-gallery::-webkit-scrollbar { display: none; }
.profile-thumb-wrap {
  flex-shrink: 0; width: 40px; height: 60px; border-radius: var(--r-sm);
  overflow: hidden; cursor: pointer;
  border: 2px solid transparent; transition: border-color 0.15s;
}
.profile-thumb-wrap:hover { border-color: var(--gold); }
.profile-thumb { width: 100%; height: 100%; object-fit: cover; }

.person-info { display: flex; flex-direction: column; justify-content: center; }
.person-name { font-size: 36px; font-weight: 600; letter-spacing: -0.02em; margin: 0 0 6px; }
.department-badge {
  display: inline-block; font-size: 11px; font-weight: 600; font-family: var(--font-mono);
  color: var(--gold); background: var(--gold-soft); padding: 3px 10px; border-radius: 100px;
  margin-bottom: 10px; width: fit-content; text-transform: uppercase; letter-spacing: 0.04em;
}
.social-links { display: flex; gap: 6px; margin-bottom: 10px; }
.social-link {
  width: 32px; height: 32px; border-radius: var(--r-sm);
  background: rgba(255,255,255,0.04); border: 1px solid var(--border);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-2); transition: all 0.15s; text-decoration: none;
}
.social-link:hover { color: var(--fg-0); border-color: var(--fg-3); background: rgba(255,255,255,0.08); }
.person-meta { display: flex; align-items: center; gap: 8px; color: var(--fg-2); font-size: 14px; flex-wrap: wrap; }
.person-meta .dot { width: 3px; height: 3px; background: var(--fg-3); border-radius: 50%; }
.person-stats {
  display: flex; gap: 12px; margin-top: 8px;
}
.person-stats .stat {
  font-size: 12px; font-family: var(--font-mono); color: var(--fg-3);
  padding: 3px 10px; border-radius: 100px;
  background: rgba(255,255,255,0.04); border: 1px solid var(--border);
}
.person-aka { font-size: 12px; color: var(--fg-3); font-family: var(--font-mono); margin-top: 8px; }
.person-bio { margin-bottom: 40px; }
.bio-lang-select {
  font-size: 12px; font-family: var(--font-mono); color: var(--fg-1);
  background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-sm);
  padding: 4px 8px; cursor: pointer; outline: none;
}
.bio-lang-select:hover { border-color: var(--fg-3); }
.bio-lang-select:focus { border-color: var(--gold); }
.bio-para { font-size: 15px; line-height: 1.7; color: var(--fg-1); margin: 0 0 16px; max-width: 800px; }
.bio-toggle {
  font-size: 12px; font-weight: 600; color: var(--gold);
  font-family: var(--font-mono); cursor: pointer;
  transition: opacity 0.12s;
}
.bio-toggle:hover { opacity: 0.8; }

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
</style>
