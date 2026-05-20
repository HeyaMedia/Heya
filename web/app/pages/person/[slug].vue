<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div class="page-pad"><div style="height: 200px; background: var(--bg-2); border-radius: var(--r-md)" /></div>
  </div>

  <div v-else-if="data" class="scroll" style="height: 100%">
    <div class="page-pad" style="max-width: 1200px">
      <div class="person-header">
        <img
          v-if="data.person.profile_path && !data.person.profile_path.startsWith('http')"
          :src="`/api/person/${data.person.id}/image`"
          class="person-photo"
          @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
        />
        <div v-else class="person-photo-placeholder">
          {{ data.person.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}
        </div>

        <div class="person-info">
          <h1 class="person-name">{{ data.person.name }}</h1>
          <div class="person-meta">
            <span v-if="data.person.birthday">Born {{ formatDate(data.person.birthday) }}</span>
            <template v-if="data.person.birthday && data.person.place_of_birth"><span class="dot" /></template>
            <span v-if="data.person.place_of_birth">{{ data.person.place_of_birth }}</span>
            <template v-if="data.person.deathday"><span class="dot" /><span>Died {{ formatDate(data.person.deathday) }}</span></template>
          </div>
          <div v-if="data.person.also_known_as?.length" class="person-aka">
            Also known as: {{ data.person.also_known_as.slice(0, 5).join(', ') }}
          </div>
          <div v-if="data.person.imdb_id" style="margin-top: 12px">
            <a :href="`https://www.imdb.com/name/${data.person.imdb_id}`" target="_blank" class="btn-ghost-sm">
              <Icon name="globe" :size="14" /> IMDB
            </a>
          </div>
        </div>
      </div>

      <div v-if="data.person.biography" class="person-bio">
        <h3 class="section-title" style="margin-bottom: 12px">Biography</h3>
        <p v-for="(para, i) in data.person.biography.split('\n\n').filter(Boolean)" :key="i" class="bio-para">{{ para }}</p>
      </div>

      <div v-if="data.cast_credits?.length" class="detail-section">
        <h3 class="section-title" style="margin-bottom: 16px">Known For</h3>
        <div class="credits-grid">
          <NuxtLink
            v-for="c in data.cast_credits"
            :key="`cast-${c.media_item_id}`"
            :to="mediaUrl({ id: c.media_item_id, title: c.title, year: c.year, media_type: c.media_type })"
            class="credit-card card-tile"
          >
            <Poster :idx="c.media_item_id" :src="usePosterUrl(c.media_item_id)" aspect="2/3" :title="c.title" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ c.title }}</div>
              <div class="grid-tile-sub">{{ c.year }} · {{ c.character }}</div>
            </div>
          </NuxtLink>
        </div>
      </div>

      <div v-if="data.crew_credits?.length" class="detail-section">
        <h3 class="section-title" style="margin-bottom: 16px">Crew</h3>
        <div class="credits-grid">
          <NuxtLink
            v-for="c in data.crew_credits"
            :key="`crew-${c.media_item_id}-${c.job}`"
            :to="mediaUrl({ id: c.media_item_id, title: c.title, year: c.year, media_type: c.media_type })"
            class="credit-card card-tile"
          >
            <Poster :idx="c.media_item_id" :src="usePosterUrl(c.media_item_id)" aspect="2/3" :title="c.title" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ c.title }}</div>
              <div class="grid-tile-sub">{{ c.year }} · {{ c.job }}</div>
            </div>
          </NuxtLink>
        </div>
      </div>
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

const data = ref<PersonResponse | null>(null)
const loading = ref(true)

function formatDate(d: string) {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

onMounted(async () => {
  try {
    data.value = await apiFetch<PersonResponse>(`/api/person/${slug.value}`)
  } catch { /* empty */ }
  loading.value = false
})
</script>

<style scoped>
.person-header { display: flex; gap: 32px; margin-bottom: 40px; }
.person-photo { width: 180px; height: 180px; border-radius: 50%; object-fit: cover; flex-shrink: 0; }
.person-photo-placeholder {
  width: 180px; height: 180px; border-radius: 50%;
  background: linear-gradient(135deg, var(--bg-4), var(--bg-3));
  display: flex; align-items: center; justify-content: center;
  font-size: 48px; font-weight: 600; color: var(--fg-2); flex-shrink: 0;
}
.person-info { display: flex; flex-direction: column; justify-content: center; }
.person-name { font-size: 36px; font-weight: 600; letter-spacing: -0.02em; margin: 0 0 8px; }
.person-meta { display: flex; align-items: center; gap: 8px; color: var(--fg-2); font-size: 14px; flex-wrap: wrap; }
.person-meta .dot { width: 3px; height: 3px; background: var(--fg-3); border-radius: 50%; }
.person-aka { font-size: 12px; color: var(--fg-3); font-family: var(--font-mono); margin-top: 8px; }
.person-bio { margin-bottom: 40px; }
.bio-para { font-size: 15px; line-height: 1.7; color: var(--fg-1); margin: 0 0 16px; max-width: 800px; }
.credits-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 18px; }
.credit-card { text-decoration: none; color: inherit; }
.credit-card:hover .grid-tile-title { color: var(--gold); }
</style>
