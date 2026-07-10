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
        <button class="scroll-ctrl-btn" @click="scrollCast('left')"><Icon name="chevleft" :size="14" /></button>
        <button class="scroll-ctrl-btn" @click="scrollCast('right')"><Icon name="chevright" :size="14" /></button>
        <button v-if="cast && cast.length > 8" class="scroll-ctrl-btn expand" @click="castExpanded = !castExpanded">
          <Icon name="chevdown" :size="14" :style="{ transform: castExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
        </button>
      </div>
      <!-- Underline variant (MediaDetailView): always-visible round arrows. -->
      <div v-else-if="variant === 'underline' && peopleTab === 'cast'" style="display: flex; gap: 8px">
        <button class="scroll-arrow" @click="scrollCast('left')"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-arrow" @click="scrollCast('right')"><Icon name="chevright" :size="16" /></button>
      </div>
    </div>

    <TabsContent value="cast" style="margin-top: 16px">
      <!-- Scroll mode -->
      <div v-if="variant === 'underline' || !castExpanded" ref="castScrollEl" class="hscroll">
        <NuxtLink v-for="c in cast" :key="c.id" :to="personUrl(c)" class="cast-card">
          <MediaCard
            :idx="c.id"
            :src="c.profile_path ? `/api/person/${c.id}/image` : ''"
            aspect="2/3"
            :title="c.name"
            :subtitle="c.character"
          />
        </NuxtLink>
      </div>
      <!-- Expanded grid mode (pill variant only) -->
      <div v-else class="cast-grid">
        <NuxtLink v-for="c in cast" :key="c.id" :to="personUrl(c)" class="cast-card">
          <MediaCard
            :idx="c.id"
            :src="c.profile_path ? `/api/person/${c.id}/image` : ''"
            aspect="2/3"
            :title="c.name"
            :subtitle="c.character"
          />
        </NuxtLink>
      </div>
    </TabsContent>

    <TabsContent value="crew" style="margin-top: 16px">
      <div v-for="dept in crewByDepartment" :key="dept.name" class="crew-dept">
        <div class="crew-dept-label">{{ dept.name }}</div>
        <div class="crew-dept-grid">
          <NuxtLink v-for="c in dept.members" :key="`${c.id}-${c.job}`" :to="personUrl(c)" class="crew-card">
            <MediaCard
              :idx="c.id"
              :src="c.profile_path ? `/api/person/${c.id}/image` : ''"
              aspect="2/3"
              :title="c.name"
              :subtitle="c.job"
            />
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
.cast-card { flex-shrink: 0; text-decoration: none; color: inherit; display: block; }
.crew-card { text-decoration: none; color: inherit; display: block; }

/* ── Pill variant (movies / tv) ─────────────────────────────────────── */
.cct-pill .section-row-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.cct-pill .tab-bar { display: flex; gap: 4px; }
.cct-pill .tab-btn { padding: 8px 16px; border-radius: var(--r-md); font-size: 13px; font-weight: 500; color: var(--fg-2); background: none; border: none; cursor: pointer; transition: all 0.15s; }
.cct-pill .tab-btn:hover { background: rgb(var(--ink) / 0.04); }
.cct-pill .tab-btn[data-state="active"] { background: var(--bg-3); color: var(--fg-0); font-weight: 600; }
.cct-pill .tab-count { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); margin-left: 4px; }
.cct-pill .cast-card { width: 120px; }
.cct-pill .crew-dept { margin-bottom: 20px; }
.cct-pill .crew-dept-label {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-3);
  margin-bottom: 8px; padding-left: 2px;
}
.cct-pill .crew-dept-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 16px; }

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
.cct-underline .cast-card { width: 100px; }
.cct-underline .hscroll { gap: 14px; }
/* Crew dept label replicates the previous `.section-title` + inline styles. */
.cct-underline .crew-dept { margin-bottom: 24px; }
.cct-underline .crew-dept-label {
  font-size: 11px; font-weight: 600; letter-spacing: 0.18em;
  text-transform: uppercase; color: var(--fg-2); font-family: var(--font-mono);
  margin-bottom: 10px;
}
.cct-underline .crew-dept-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 14px; }
.cct-underline .scroll-arrow {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgb(var(--ink) / 0.06); border: 1px solid var(--border);
  color: var(--fg-2); transition: all 0.15s;
}
.cct-underline .scroll-arrow:hover { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }

/* Expanded grid (pill only) — keep after the width rules so `width: auto` wins. */
.cast-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 16px; }
.cast-grid .cast-card { width: auto; }

/* Touch: swipe replaces the mouse-only scroll arrows. The pill variant's
   fold/expand toggle (`.expand`) stays — it's a real affordance on touch too. */
@media (pointer: coarse) {
  .scroll-controls .scroll-ctrl-btn:not(.expand) { display: none; }
  .scroll-arrow { display: none; }
}
</style>
