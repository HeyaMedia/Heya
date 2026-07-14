<template>
  <div class="mf-split">
    <!-- Left column: identity fields -->
    <div class="mf-col">
      <div class="mf-card">
        <div class="mf-card-head">Artist Identity</div>
        <div class="mf-grid">
          <div class="mf-field mf-full">
            <label class="mf-label" for="me-artist-name">Name</label>
            <input id="me-artist-name" v-model="form.title" type="text" class="mf-input" />
          </div>
          <div class="mf-field">
            <label class="mf-label" for="me-artist-sort-name">Sort Name</label>
            <input id="me-artist-sort-name" v-model="form.sort_name" type="text" class="mf-input" placeholder="Beatles, The" />
          </div>
          <div class="mf-field">
            <label class="mf-label" for="me-artist-disambiguation">Disambiguation</label>
            <input id="me-artist-disambiguation" v-model="form.disambiguation" type="text" class="mf-input" placeholder="UK rock band" />
          </div>
        </div>
      </div>

      <div class="mf-card">
        <div class="mf-card-head">Heya Identity</div>
        <div class="mf-grid">
          <div class="mf-field mf-full">
            <label class="mf-label" for="me-artist-heya-id">Heya ID</label>
            <input id="me-artist-heya-id" :value="detail.metadata_binding?.entity_id || ''" type="text" class="mf-input mf-input-readonly" readonly placeholder="Not linked to Heya yet" />
          </div>
        </div>
        <p class="mf-hint">
          Identify selects the canonical Heya artist. Albums, top tracks and images
          then refresh from Heya without exposing individual catalog identities.
        </p>
      </div>
    </div>

    <!-- Right column: biography -->
    <div class="mf-col">
      <div class="mf-card mf-card-fill">
        <div class="mf-card-head">Biography</div>
        <div class="mf-field mf-field-fill">
          <textarea v-model="form.biography" class="mf-textarea mf-textarea-fill" aria-label="Biography" placeholder="Artist biography..." />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
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

.mf-input-readonly {
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 12px;
}
.mf-input-readonly:focus {
  border-color: var(--border);
}

.mf-hint {
  margin: 12px 0 0;
  font-size: 12px;
  color: var(--fg-3);
  line-height: 1.5;
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
