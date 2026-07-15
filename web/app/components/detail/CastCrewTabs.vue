<template>
  <TabsRoot v-model="peopleTab" class="detail-section" :class="variant === 'underline' ? 'cct-underline' : 'cct-pill'">
    <div class="section-row-head" style="margin-bottom: 0">
      <TabsList class="tab-bar" style="margin-bottom: 0">
        <TabsTrigger value="cast" class="tab-btn">
          Cast <span class="tab-count">{{ cast?.length || 0 }}</span>
        </TabsTrigger>
        <TabsTrigger value="crew" class="tab-btn">
          Crew <span class="tab-count">{{ crew?.length || 0 }}</span>
        </TabsTrigger>
      </TabsList>
      <!-- Pill variant (movies/tv): overflow-gated scroll controls + expand toggle. -->
      <div v-if="variant !== 'underline' && peopleTab === 'cast' && castOverflows" class="scroll-controls">
        <button class="scroll-ctrl-btn" aria-label="Scroll left" @click="scrollCast('left')"><Icon name="chevleft" :size="14" /></button>
        <button class="scroll-ctrl-btn" aria-label="Scroll right" @click="scrollCast('right')"><Icon name="chevright" :size="14" /></button>
        <button
          v-if="cast && cast.length > 8" class="scroll-ctrl-btn expand" aria-label="Toggle expanded view"
          :aria-expanded="castExpanded"
          @click="castExpanded = !castExpanded"
        >
          <Icon name="chevdown" :size="14" :style="{ transform: castExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
        </button>
      </div>
      <!-- Underline variant (MediaDetailView): always-visible round arrows. -->
      <div v-else-if="variant === 'underline' && peopleTab === 'cast'" style="display: flex; gap: 8px">
        <button class="scroll-arrow" aria-label="Scroll left" @click="scrollCast('left')"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-arrow" aria-label="Scroll right" @click="scrollCast('right')"><Icon name="chevright" :size="16" /></button>
      </div>
    </div>

    <TabsContent value="cast" style="margin-top: 16px">
      <!-- Scroll mode -->
      <div v-if="variant === 'underline' || !castExpanded" ref="castScrollEl" class="hscroll">
        <NuxtLink v-for="c in cast" :key="c.id" :to="personUrl(c)" class="person">
          <LoadingImage v-if="showImg(c)" :src="`/api/person/${c.id}/image`" :width="264" :quality="80" :alt="c.name" class="person-img" @error="failedImg.add(c.id)" />
          <div v-else class="person-noimg">{{ personInitials(c.name) }}</div>
          <div class="person-nm">{{ c.name }}</div>
          <div v-if="c.character" class="person-as">as {{ c.character }}</div>
        </NuxtLink>
      </div>
      <!-- Expanded grid mode (pill variant only) -->
      <div v-else class="cast-grid">
        <NuxtLink v-for="c in cast" :key="c.id" :to="personUrl(c)" class="person">
          <LoadingImage v-if="showImg(c)" :src="`/api/person/${c.id}/image`" :width="264" :quality="80" :alt="c.name" class="person-img" @error="failedImg.add(c.id)" />
          <div v-else class="person-noimg">{{ personInitials(c.name) }}</div>
          <div class="person-nm">{{ c.name }}</div>
          <div v-if="c.character" class="person-as">as {{ c.character }}</div>
        </NuxtLink>
      </div>
    </TabsContent>

    <TabsContent value="crew" style="margin-top: 16px">
      <div v-for="dept in crewByDepartment" :key="dept.name" class="crew-dept">
        <div class="crew-dept-label">{{ dept.name }}</div>
        <div class="crew-dept-grid">
          <NuxtLink v-for="c in dept.members" :key="`${c.id}-${c.job}`" :to="personUrl(c)" class="person">
            <LoadingImage v-if="showImg(c)" :src="`/api/person/${c.id}/image`" :width="264" :quality="80" :alt="c.name" class="person-img" @error="failedImg.add(c.id)" />
            <div v-else class="person-noimg">{{ personInitials(c.name) }}</div>
            <div class="person-nm">{{ c.name }}</div>
            <div v-if="c.job" class="person-as">{{ c.job }}</div>
          </NuxtLink>
        </div>
      </div>
    </TabsContent>
  </TabsRoot>
</template>

<script setup lang="ts">
import type { CastMember, CrewMember } from '~~/shared/types'
import { TabsRoot, TabsList, TabsTrigger, TabsContent } from 'reka-ui'

const props = defineProps<{
  cast?: CastMember[] | null
  crew?: CrewMember[] | null
  /** 'pill' = movies/tv look (default); 'underline' = MediaDetailView look. */
  variant?: 'pill' | 'underline'
}>()

const peopleTab = ref<'cast' | 'crew'>('cast')
const castExpanded = ref(false)
const castScrollEl = ref<HTMLElement | null>(null)
const castOverflows = ref(false)

// Per-person portrait fallback: a person shows their profile image when it
// exists AND hasn't 404'd; otherwise a mono-initials tile (heya2.css .noimg).
// (Person images aren't backed by media_assets, so gating on profile_path —
// unlike the "image URLs unconditional" rule for media items — is correct.)
const failedImg = reactive(new Set<number>())
function showImg(c: { id: number; profile_path?: string | null }) {
  return !!c.profile_path && !failedImg.has(c.id)
}
function personInitials(name: string): string {
  const t = (name || '').trim()
  if (!t) return '?'
  const words = t.split(/\s+/).filter(Boolean)
  return (words.length >= 2 ? words[0]!.charAt(0) + words[1]!.charAt(0) : t.slice(0, 2)).toUpperCase()
}

function checkCastOverflow() {
  nextTick(() => {
    if (castScrollEl.value) {
      castOverflows.value = castScrollEl.value.scrollWidth > castScrollEl.value.clientWidth
    } else {
      castOverflows.value = (props.cast?.length || 0) > 8
    }
  })
}

watch(() => props.cast, () => checkCastOverflow(), { immediate: true })

function scrollCast(dir: 'left' | 'right') {
  if (!castScrollEl.value) return
  const amount = props.variant === 'underline' ? 500 : castScrollEl.value.clientWidth * 0.75
  castScrollEl.value.scrollBy({ left: dir === 'left' ? -amount : amount, behavior: 'smooth' })
}

// Department-ordered crew grouping (Directing first, then Writing, …).
// Departments outside the known list append in encounter order.
const crewByDepartment = computed(() => {
  const crew = props.crew || []
  const depts = new Map<string, CrewMember[]>()
  for (const c of crew) {
    const d = c.department || 'Other'
    if (!depts.has(d)) depts.set(d, [])
    depts.get(d)!.push(c)
  }
  const order = ['Directing', 'Writing', 'Production', 'Camera', 'Sound', 'Editing', 'Art', 'Costume & Make-Up', 'Visual Effects', 'Lighting', 'Crew']
  const sorted: { name: string; members: CrewMember[] }[] = []
  for (const name of order) {
    if (depts.has(name)) sorted.push({ name, members: depts.get(name)! })
  }
  for (const [name, members] of depts.entries()) {
    if (!order.includes(name)) sorted.push({ name, members })
  }
  return sorted
})
</script>

<style scoped>
/* Shared — both variants. `.hscroll` base + `.scroll-controls`/`.scroll-ctrl-btn`
   come from heya.css globals. */

/* ── Person portrait tiles (heya2.css `.person`): 132×176 rounded-rect
   portrait, name + mono role line BELOW the art, hairline ring + top-left
   key-light directional shadow. Used by the cast rail, the expand-to-grid
   view, and the crew department grids. NOT the MediaCard embedded-label look. */
.person { text-decoration: none; color: inherit; display: block; }
.person-img,
.person-noimg {
  width: 100%;
  aspect-ratio: 132 / 176;
  object-fit: cover;
  border-radius: 8px;
  background: var(--bg-2);
  box-shadow: 0 0 0 1px rgb(var(--ink) / 0.1), 7px 14px 30px -12px rgb(0 0 0 / 0.8);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.person-noimg {
  display: flex; align-items: center; justify-content: center;
  font: 700 26px var(--font-mono); color: rgb(var(--ink) / 0.25);
}
.person:hover .person-img,
.person:hover .person-noimg {
  transform: translateY(-4px);
  box-shadow: 0 0 0 1px rgb(var(--ink) / 0.16), 10px 18px 34px -12px rgb(0 0 0 / 0.85), 0 0 26px rgb(var(--tone-rgb) / 0.12);
}
.person-nm { margin-top: 9px; font-size: 13px; font-weight: 600; line-height: 1.3; color: var(--fg-0); }
.person-as {
  margin-top: 2px;
  font: 500 10.5px var(--font-mono); letter-spacing: 0.06em; color: var(--fg-3);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
/* Rail tiles get a fixed basis; grid tiles fill their column (width auto). */
.hscroll .person { flex: 0 0 132px; width: 132px; }

/* ── Pill variant (movies / tv) ─────────────────────────────────────── */
.cct-pill .section-row-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.cct-pill .tab-bar { display: flex; gap: 4px; }
/* Glass pills with a float shadow — bare ink washes vanished over the
   ambient artwork behind the page. */
.cct-pill .tab-btn {
  padding: 8px 16px; border-radius: var(--r-md); font-size: 13px; font-weight: 500;
  color: var(--fg-2); cursor: pointer; transition: all 0.15s;
  background: color-mix(in oklab, var(--bg-2) 70%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
}
.cct-pill .tab-btn:hover { background: color-mix(in oklab, var(--bg-2) 88%, transparent); color: var(--fg-0); }
.cct-pill .tab-btn[data-state="active"] { background: var(--bg-3); color: var(--fg-0); font-weight: 600; border-color: var(--border-strong); }
.cct-pill .tab-count { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); margin-left: 4px; }
.cct-pill .crew-dept { margin-bottom: 20px; }
.cct-pill .crew-dept-label {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-3);
  margin-bottom: 8px; padding-left: 2px;
}
.cct-pill .crew-dept-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 18px; }

/* ── Underline variant (MediaDetailView) ────────────────────────────── */
/* No .section-row-head rule here: MediaDetailView relied on the heya.css
   global (align-items: baseline), so the underline variant does too. */
.cct-underline .tab-bar { display: flex; gap: 0; border-bottom: 1px solid var(--border); margin-bottom: 20px; }
.cct-underline .tab-btn {
  padding: 10px 20px; font-size: 13px; font-weight: 500; color: var(--fg-2);
  border-bottom: 2px solid transparent; transition: color 0.15s, border-color 0.15s;
}
.cct-underline .tab-btn:hover { color: var(--fg-0); }
.cct-underline .tab-btn[data-state="active"] { color: var(--gold); border-bottom-color: var(--gold); }
.cct-underline .tab-count { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); margin-left: 6px; }
.cct-underline .hscroll { gap: 14px; }
/* Crew dept label replicates the previous `.section-title` + inline styles. */
.cct-underline .crew-dept { margin-bottom: 24px; }
.cct-underline .crew-dept-label {
  font-size: 11px; font-weight: 600; letter-spacing: 0.18em;
  text-transform: uppercase; color: var(--fg-2); font-family: var(--font-mono);
  margin-bottom: 10px;
}
.cct-underline .crew-dept-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 16px; }
.cct-underline .scroll-arrow {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgb(var(--ink) / 0.06); border: 1px solid var(--border);
  color: var(--fg-2); transition: all 0.15s;
}
.cct-underline .scroll-arrow:hover { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }

/* Expanded grid (pill only). `.person` (no `.hscroll` ancestor) fills each
   grid cell at width auto. */
.cast-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 18px; }

/* Phone: smaller portraits (heya2.css ≤560 .person). */
@media (max-width: 560px) {
  .hscroll .person { flex-basis: 104px; width: 104px; }
  .person-nm { font-size: 12.5px; }
}

/* Touch: swipe replaces the mouse-only scroll arrows. The pill variant's
   fold/expand toggle (`.expand`) stays — it's a real affordance on touch too. */
@media (pointer: coarse) {
  .scroll-controls .scroll-ctrl-btn:not(.expand) { display: none; }
  .scroll-arrow { display: none; }
}
</style>
