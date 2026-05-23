<template>
  <div class="mf">
    <!-- Genres -->
    <div class="mf-card">
      <div class="mf-card-head">Genres</div>
      <div class="mf-chips-wrap">
        <span v-for="(g, i) in (form.genres as string[])" :key="g" class="mf-chip" @click="removeGenre(i)">
          {{ g }} <span class="mf-chip-x">&times;</span>
        </span>
        <input
          v-model="newGenre"
          type="text"
          class="mf-chip-input"
          placeholder="Add genre..."
          @keydown.enter.prevent="addGenre"
        />
      </div>
    </div>

    <!-- Dates & Status -->
    <div class="mf-card">
      <div class="mf-card-head">Dates & Status</div>
      <div class="mf-grid">
        <template v-if="mediaType === 'movie'">
          <div class="mf-field">
            <label class="mf-label">Release Date</label>
            <input v-model="form.release_date" type="date" class="mf-input" />
          </div>
          <div class="mf-field">
            <label class="mf-label">Rating</label>
            <div class="mf-readonly">{{ formatRating(detail?.movie?.rating) }}</div>
          </div>
        </template>
        <template v-if="mediaType === 'tv'">
          <div class="mf-field">
            <label class="mf-label">Status</label>
            <select v-model="form.status" class="mf-input">
              <option value="">—</option>
              <option value="Returning Series">Returning Series</option>
              <option value="Ended">Ended</option>
              <option value="Canceled">Canceled</option>
              <option value="In Production">In Production</option>
            </select>
          </div>
          <div class="mf-field">
            <label class="mf-label">Rating</label>
            <div class="mf-readonly">{{ formatRating(detail?.tv_series?.rating) }}</div>
          </div>
          <div class="mf-field">
            <label class="mf-label">First Air Date</label>
            <input v-model="form.first_air_date" type="date" class="mf-input" />
          </div>
          <div class="mf-field">
            <label class="mf-label">Last Air Date</label>
            <input v-model="form.last_air_date" type="date" class="mf-input" />
          </div>
        </template>
      </div>
    </div>

    <!-- Networks (TV only) -->
    <div v-if="mediaType === 'tv'" class="mf-card">
      <div class="mf-card-head">Networks</div>
      <div class="mf-chips-wrap">
        <span v-for="(n, i) in (form.networks as string[])" :key="n" class="mf-chip" @click="removeNetwork(i)">
          {{ n }} <span class="mf-chip-x">&times;</span>
        </span>
        <input
          v-model="newNetwork"
          type="text"
          class="mf-chip-input"
          placeholder="Add network..."
          @keydown.enter.prevent="addNetwork"
        />
      </div>
    </div>

    <!-- External IDs -->
    <div class="mf-card">
      <div class="mf-card-head">External IDs</div>
      <div class="mf-ids">
        <div v-for="key in ['tmdb', 'imdb', 'tvdb']" :key="key" class="mf-id-row">
          <span class="mf-id-label">{{ key.toUpperCase() }}</span>
          <input v-model="form.external_ids[key]" type="text" class="mf-input mf-id-input" :placeholder="`${key} ID`" />
        </div>
      </div>
    </div>

    <!-- Read-only metadata -->
    <div v-if="detail?.production_companies?.length || detail?.keywords?.length || detail?.certifications?.length || detail?.external_ratings?.length" class="mf-card">
      <div class="mf-card-head">Additional Info</div>
      <div class="mf-sections">
        <div v-if="detail?.production_companies?.length" class="mf-section">
          <div class="mf-section-label">Production Companies</div>
          <div class="mf-tag-list">
            <span v-for="c in detail.production_companies" :key="c.id" class="mf-tag">{{ c.name }}</span>
          </div>
        </div>

        <div v-if="detail?.keywords?.length" class="mf-section">
          <div class="mf-section-label">Keywords</div>
          <div class="mf-tag-list">
            <span v-for="k in detail.keywords" :key="k.id" class="mf-tag mf-tag-subtle">{{ k.name }}</span>
          </div>
        </div>

        <div v-if="detail?.certifications?.length" class="mf-section">
          <div class="mf-section-label">Certifications</div>
          <div class="mf-tag-list">
            <span v-for="c in detail.certifications" :key="`${c.country}-${c.certification}`" class="mf-tag mf-tag-mono">
              {{ c.country }}: {{ c.certification }}
            </span>
          </div>
        </div>

        <div v-if="detail?.external_ratings?.length" class="mf-section">
          <div class="mf-section-label">External Ratings</div>
          <div class="mf-ratings">
            <div v-for="r in detail.external_ratings" :key="r.source" class="mf-rating">
              <span class="mf-rating-src">{{ r.source }}</span>
              <span class="mf-rating-val">{{ r.value }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaType } from '~~/shared/types'

defineProps<{
  mediaType: MediaType
  detail: any
}>()

const form = defineModel<Record<string, any>>('form', { required: true })

const newGenre = ref('')
const newNetwork = ref('')

function addGenre() {
  const g = newGenre.value.trim()
  if (g && !form.value.genres.includes(g)) {
    form.value.genres.push(g)
  }
  newGenre.value = ''
}

function removeGenre(i: number) {
  form.value.genres.splice(i, 1)
}

function addNetwork() {
  const n = newNetwork.value.trim()
  if (n && !form.value.networks.includes(n)) {
    form.value.networks.push(n)
  }
  newNetwork.value = ''
}

function removeNetwork(i: number) {
  form.value.networks.splice(i, 1)
}

function formatRating(r: any): string {
  if (r === null || r === undefined || r === '') return '—'
  const n = typeof r === 'number' ? r : parseFloat(String(r))
  return isNaN(n) || n === 0 ? '—' : n.toFixed(1)
}
</script>

<style scoped>
.mf {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.mf-card {
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 20px;
}

.mf-card-head {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
  margin-bottom: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.mf-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
@media (max-width: 720px) {
  .mf-grid { grid-template-columns: 1fr; }
}

.mf-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.mf-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}

.mf-input {
  height: 38px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-0);
  font-size: 13px;
  padding: 0 12px;
  outline: none;
  transition: border-color 0.15s;
}
.mf-input:focus {
  border-color: var(--gold);
}

.mf-readonly {
  height: 38px;
  display: flex;
  align-items: center;
  padding: 0 12px;
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.04);
  font-size: 13px;
  color: var(--fg-2);
  font-family: var(--font-mono);
}

/* ── Chips ── */
.mf-chips-wrap {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  min-height: 38px;
}

.mf-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px;
  border-radius: 12px;
  font-size: 11px;
  font-weight: 500;
  background: var(--gold-soft);
  color: var(--gold-bright);
  cursor: pointer;
  transition: background 0.12s;
}
.mf-chip:hover {
  background: var(--gold-glow);
}

.mf-chip-x {
  font-size: 14px;
  line-height: 1;
}

.mf-chip-input {
  flex: 1;
  min-width: 80px;
  border: none;
  background: transparent;
  color: var(--fg-0);
  font-size: 12px;
  outline: none;
}

/* ── External IDs ── */
.mf-ids {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mf-id-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.mf-id-label {
  width: 50px;
  font-size: 10px;
  font-weight: 700;
  color: var(--fg-3);
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

.mf-id-input {
  flex: 1;
}

/* ── Read-only sections ── */
.mf-sections {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.mf-section-label {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
  margin-bottom: 8px;
}

.mf-tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.mf-tag {
  font-size: 12px;
  color: var(--fg-1);
  padding: 4px 10px;
  background: rgba(255, 255, 255, 0.04);
  border-radius: var(--r-sm);
}

.mf-tag-subtle {
  background: rgba(255, 255, 255, 0.03);
  color: var(--fg-2);
}

.mf-tag-mono {
  font-size: 11px;
  font-family: var(--font-mono);
}

.mf-ratings {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.mf-rating {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  background: rgba(255, 255, 255, 0.04);
  border-radius: var(--r-sm);
}

.mf-rating-src {
  font-size: 10px;
  font-weight: 700;
  color: var(--fg-3);
  text-transform: uppercase;
  font-family: var(--font-mono);
}

.mf-rating-val {
  font-size: 14px;
  color: var(--gold-bright);
  font-weight: 600;
}
</style>
