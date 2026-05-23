<template>
  <div class="mf-split">
    <!-- Left column: fields -->
    <div class="mf-col">
      <div class="mf-card">
        <div class="mf-card-head">Title & Identity</div>
        <div class="mf-grid">
          <div class="mf-field mf-full">
            <label class="mf-label">Title</label>
            <input v-model="form.title" type="text" class="mf-input" />
          </div>
          <div class="mf-field">
            <label class="mf-label">Sort Title</label>
            <input v-model="form.sort_title" type="text" class="mf-input" />
          </div>
          <div class="mf-field">
            <label class="mf-label">Year</label>
            <input v-model="form.year" type="text" class="mf-input" maxlength="4" />
          </div>
          <div class="mf-field">
            <label class="mf-label">Original {{ mediaType === 'tv' ? 'Name' : 'Title' }}</label>
            <input v-model="form.original_title" type="text" class="mf-input" />
          </div>
          <div class="mf-field">
            <label class="mf-label">Original Language</label>
            <input v-model="form.original_language" type="text" class="mf-input" maxlength="5" />
          </div>
        </div>
      </div>

      <div v-if="mediaType === 'movie'" class="mf-card">
        <div class="mf-card-head">Movie Info</div>
        <div class="mf-grid">
          <div class="mf-field">
            <label class="mf-label">Runtime (min)</label>
            <input v-model.number="form.runtime_minutes" type="number" class="mf-input" />
          </div>
          <div class="mf-field mf-full">
            <label class="mf-label">Tagline</label>
            <input v-model="form.tagline" type="text" class="mf-input" />
          </div>
        </div>
      </div>
    </div>

    <!-- Right column: overview -->
    <div class="mf-col">
      <div class="mf-card mf-card-fill">
        <div class="mf-card-head">Overview</div>
        <div class="mf-field mf-field-fill">
          <textarea v-model="form.description" class="mf-textarea mf-textarea-fill" placeholder="Synopsis or description..." />
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
