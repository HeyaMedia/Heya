<template>
  <div class="mf-split">
    <div class="mf-col">
      <div class="mf-card">
        <div class="mf-card-head">Season Info</div>
        <div class="mf-grid">
          <div class="mf-field mf-full">
            <label class="mf-label" for="me-season-title">Title</label>
            <input id="me-season-title" v-model="form.title" type="text" class="mf-input" />
          </div>
          <div class="mf-field">
            <label class="mf-label">Season #</label>
            <div class="mf-readonly">{{ season?.season_number ?? '—' }}</div>
          </div>
          <div class="mf-field">
            <label class="mf-label">Episodes</label>
            <div class="mf-readonly">{{ (season as any)?.episodes?.length ?? season?.episode_count ?? '—' }}</div>
          </div>
          <div class="mf-field">
            <label class="mf-label" for="me-season-air-date">Air Date</label>
            <input id="me-season-air-date" v-model="form.air_date" type="date" class="mf-input" />
          </div>
        </div>
      </div>
    </div>

    <div class="mf-col">
      <div class="mf-card mf-card-fill">
        <div class="mf-card-head">Overview</div>
        <div class="mf-field mf-field-fill">
          <textarea v-model="form.overview" class="mf-textarea mf-textarea-fill" aria-label="Overview" placeholder="Season overview..." />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  season: any
}>()

const form = defineModel<Record<string, any>>('form', { required: true })
</script>

<style scoped>
.mf-split {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  height: 100%;
}

.mf-col {
  display: flex;
  flex-direction: column;
  gap: 20px;
  min-width: 0;
}

.mf-card {
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 20px;
}

.mf-card-fill {
  flex: 1;
  display: flex;
  flex-direction: column;
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
  flex-shrink: 0;
}

.mf-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.mf-full {
  grid-column: 1 / -1;
}

.mf-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.mf-field-fill {
  flex: 1;
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
  background: rgb(var(--ink) / 0.03);
  border: 1px solid rgb(var(--ink) / 0.04);
  font-size: 13px;
  color: var(--fg-2);
  font-family: var(--font-mono);
}

.mf-textarea {
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-0);
  font-size: 13px;
  padding: 10px 12px;
  outline: none;
  resize: vertical;
  min-height: 100px;
  font-family: inherit;
  line-height: 1.55;
  transition: border-color 0.15s;
}

.mf-textarea-fill {
  flex: 1;
  resize: none;
}

.mf-textarea:focus {
  border-color: var(--gold);
}
</style>
