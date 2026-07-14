<template>
  <div class="mf">
    <div class="mf-card">
      <div class="mf-card-head">Publication</div>
      <div class="mf-grid">
        <div class="mf-field"><label class="mf-label" for="me-book-isbn">ISBN</label><input id="me-book-isbn" v-model="form.isbn" class="mf-input" /></div>
        <div class="mf-field"><label class="mf-label" for="me-book-publisher">Publisher</label><input id="me-book-publisher" v-model="form.publisher" class="mf-input" /></div>
        <div class="mf-field"><label class="mf-label" for="me-book-publish-date">Publication Date</label><input id="me-book-publish-date" v-model="form.publish_date" type="date" class="mf-input" /></div>
        <div class="mf-field"><label class="mf-label" for="me-book-pages">Pages</label><input id="me-book-pages" v-model.number="form.page_count" type="number" min="0" class="mf-input" /></div>
        <div class="mf-field"><label class="mf-label" for="me-book-language">Language</label><input id="me-book-language" v-model="form.language" class="mf-input" /></div>
        <div class="mf-field"><label class="mf-label" for="me-book-format">Format</label><input id="me-book-format" v-model="form.format" class="mf-input" /></div>
      </div>
    </div>
    <div class="mf-card">
      <div class="mf-card-head">Subjects</div>
      <div class="mf-chips-wrap">
        <button v-for="(subject, i) in visibleSubjects" :key="`${subject}-${i}`" type="button" class="mf-chip" @click="removeSubject(subject)">
          {{ subject }} <span>&times;</span>
        </button>
        <button v-if="form.subjects.length > subjectLimit" type="button" class="mf-more" @click="showAllSubjects = !showAllSubjects">
          {{ showAllSubjects ? 'Show fewer' : `+${form.subjects.length - subjectLimit} more` }}
        </button>
        <input v-model="newSubject" class="mf-chip-input" placeholder="Add subject..." @keydown.enter.prevent="addSubject" />
      </div>
    </div>
    <div class="mf-card">
      <div class="mf-card-head">Heya Identity</div>
      <div class="mf-identity">
        <span class="mf-identity-kind">{{ detail?.metadata_binding?.entity_kind || 'book' }}</span>
        <code>{{ detail?.metadata_binding?.entity_id || 'Not linked to Heya yet' }}</code>
      </div>
      <p class="mf-hint">Identify changes the canonical Heya record. Catalog-specific IDs remain transparent compatibility evidence.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ detail: any }>()
const form = defineModel<Record<string, any>>('form', { required: true })
const newSubject = ref('')
const showAllSubjects = ref(false)
const subjectLimit = 24
const visibleSubjects = computed(() => showAllSubjects.value ? form.value.subjects : form.value.subjects.slice(0, subjectLimit))
function addSubject() {
  const value = newSubject.value.trim()
  if (value && !form.value.subjects.includes(value)) form.value.subjects.push(value)
  newSubject.value = ''
}
function removeSubject(subject: string) {
  const index = form.value.subjects.indexOf(subject)
  if (index !== -1) form.value.subjects.splice(index, 1)
}
</script>

<style scoped>
.mf { display: flex; flex-direction: column; gap: 20px; }
.mf-card { background: var(--bg-1); border: 1px solid var(--border); border-radius: var(--r-md); padding: 20px; }
.mf-card-head { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .08em; color: var(--fg-2); margin-bottom: 16px; padding-bottom: 10px; border-bottom: 1px solid var(--border); }
.mf-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.mf-field { display: flex; flex-direction: column; gap: 6px; }
.mf-label { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .06em; color: var(--fg-3); }
.mf-input { height: 38px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-3); color: var(--fg-0); font-size: 13px; padding: 0 12px; outline: none; }
.mf-input:focus { border-color: var(--gold); }
.mf-chips-wrap { display: flex; flex-wrap: wrap; gap: 6px; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-3); min-height: 38px; }
.mf-chip { padding: 3px 10px; border-radius: 12px; background: var(--gold-soft); color: var(--gold-bright); font-size: 11px; }
.mf-more { padding: 3px 10px; border-radius: 12px; border: 1px solid var(--border); color: var(--fg-2); font-size: 11px; }
.mf-chip-input { flex: 1; min-width: 120px; border: 0; outline: 0; background: transparent; color: var(--fg-0); }
.mf-identity { display: flex; align-items: center; gap: 10px; min-width: 0; }
.mf-identity code { color: var(--fg-1); font-size: 12px; overflow-wrap: anywhere; }
.mf-identity-kind { padding: 2px 7px; border-radius: 4px; background: var(--gold-soft); color: var(--gold-bright); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.mf-hint { margin: 12px 0 0; color: var(--fg-3); font-size: 12px; line-height: 1.5; }
@media (max-width: 720px) { .mf-grid { grid-template-columns: 1fr; } }
</style>
