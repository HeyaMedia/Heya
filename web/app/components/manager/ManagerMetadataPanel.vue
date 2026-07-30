<script setup lang="ts">
// The metadata ledger: everything Heya knows about this item, grouped into
// sections, each field wearing its provenance — a gold "edited" chip marks
// manually-locked fields that enrichment will never overwrite. The Edit
// button opens the full metadata editor (same one the public pages use).
import { useQuery, useQueryCache } from '@pinia/colada'
import { managerMetadataQuery } from '~/queries/manager'
import type { ManagerMetadataField } from '~~/shared/api/types.gen'

const props = defineProps<{ mediaItemId: number }>()

const { data, asyncStatus } = useQuery(() => managerMetadataQuery(props.mediaItemId))

const editorOpen = ref(false)
const queryCache = useQueryCache()
function onEditorClose() {
  editorOpen.value = false
  queryCache.invalidateQueries({ key: ['manager', 'metadata', props.mediaItemId] })
  queryCache.invalidateQueries({ key: ['manager', 'media', props.mediaItemId] })
  queryCache.invalidateQueries({ key: ['manager', 'library-items'] })
}

// Timestamps render as relative age with the absolute value on hover; other
// values pass through.
const TS_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/
function displayValue(f: ManagerMetadataField): string {
  if (!f.value) return '—'
  if (TS_RE.test(f.value)) {
    const rest = f.value.includes(' · ') ? ' · ' + f.value.split(' · ').slice(1).join(' · ') : ''
    return timeAgoShort(f.value.split(' · ')[0]!) + rest
  }
  return f.value
}
function titleValue(f: ManagerMetadataField): string | undefined {
  if (f.value && TS_RE.test(f.value)) return new Date(f.value.split(' · ')[0]!).toLocaleString()
  return undefined
}

// Long prose and chip-lists render full-width; everything else is a ledger
// row. Sections keep their server order.
const sections = computed(() => data.value?.sections ?? [])
const altTitles = computed(() => data.value?.alt_titles ?? [])
</script>

<template>
  <section class="mmp">
    <div class="mmp-head">
      <h2 class="mmp-title">Metadata</h2>
      <span class="mmp-hint"><span class="mmp-prov user">edited</span> fields are locked against enrichment</span>
      <button type="button" class="mgr-btn-gold mmp-edit" @click="editorOpen = true">
        <Icon name="pencil" :size="13" /> Edit metadata
      </button>
    </div>

    <div v-if="asyncStatus === 'loading' && !data" class="mmp-loading"><span class="mgr-spin" /> Loading metadata…</div>

    <div v-else-if="data" class="mmp-grid">
      <div v-for="section in sections" :key="section.title" class="mmp-section" :class="{ wide: (section.fields ?? []).some(f => f.long) }">
        <h3 class="mmp-section-title mono">{{ section.title }}</h3>
        <dl class="mmp-fields">
          <template v-for="f in section.fields ?? []" :key="f.key + f.label">
            <div v-if="f.long" class="mmp-row long">
              <dt class="mmp-label mono">{{ f.label }}</dt>
              <dd class="mmp-value prose" :class="{ bad: f.tone === 'bad', empty: !f.value }">
                {{ f.value || '—' }}
                <span v-if="f.provenance" class="mmp-prov" :class="f.provenance">{{ f.provenance === 'user' ? 'edited' : f.provenance }}</span>
              </dd>
            </div>
            <div v-else-if="f.values?.length" class="mmp-row">
              <dt class="mmp-label mono">{{ f.label }}</dt>
              <dd class="mmp-value">
                <span class="mmp-chips">
                  <span v-for="v in f.values" :key="v" class="mmp-chip">{{ v }}</span>
                </span>
                <span v-if="f.provenance" class="mmp-prov" :class="f.provenance">{{ f.provenance === 'user' ? 'edited' : f.provenance }}</span>
              </dd>
            </div>
            <div v-else class="mmp-row">
              <dt class="mmp-label mono">{{ f.label }}</dt>
              <dd class="mmp-value" :class="{ mono: f.mono, bad: f.tone === 'bad' }">
                <a v-if="f.href && f.value" :href="f.href" target="_blank" rel="noopener" class="mmp-link">{{ displayValue(f) }}</a>
                <span v-else :class="{ 'mmp-empty': !f.value }" :title="titleValue(f)">{{ displayValue(f) }}</span>
                <span v-if="f.provenance" class="mmp-prov" :class="f.provenance">{{ f.provenance === 'user' ? 'edited' : f.provenance }}</span>
              </dd>
            </div>
          </template>
        </dl>
      </div>

      <div v-if="altTitles.length" class="mmp-section wide">
        <h3 class="mmp-section-title mono">Alternate titles</h3>
        <div class="mmp-titles">
          <span v-for="(t, i) in altTitles" :key="i" class="mmp-alt" :title="[t.kind, t.source].filter(Boolean).join(' · ')">
            {{ t.title }}
            <span v-if="t.language || t.country" class="mmp-alt-lang mono">{{ [t.language, t.country].filter(Boolean).join('-') }}</span>
          </span>
        </div>
      </div>
    </div>

    <MetadataEditorModal :media-id="mediaItemId" :show="editorOpen" @close="onEditorClose" />
  </section>
</template>

<style scoped>
.mmp { margin-top: 26px; }
.mmp-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}
.mmp-title {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-0);
  margin: 0;
}
.mmp-hint { font-size: 11px; color: var(--fg-3); display: inline-flex; align-items: center; gap: 5px; }
.mmp-edit { margin-left: auto; }
.mmp-loading { display: flex; align-items: center; gap: 8px; color: var(--fg-3); font-size: 12.5px; }

.mmp-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 980px) {
  .mmp-grid { grid-template-columns: minmax(0, 1fr); }
}
.mmp-section {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 16px;
  min-width: 0;
}
.mmp-section.wide { grid-column: 1 / -1; }
.mmp-section-title {
  margin: 0 0 10px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold-bright);
}
.mmp-fields { margin: 0; display: flex; flex-direction: column; }
.mmp-row {
  display: grid;
  grid-template-columns: 150px minmax(0, 1fr);
  gap: 12px;
  align-items: baseline;
  padding: 6px 0;
  border-top: 1px solid var(--hair);
}
.mmp-row:first-child { border-top: 0; }
.mmp-row.long { grid-template-columns: minmax(0, 1fr); }
.mmp-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.mmp-value {
  margin: 0;
  font-size: 12.5px;
  color: var(--fg-1);
  overflow-wrap: anywhere;
  display: flex;
  align-items: baseline;
  gap: 8px;
  flex-wrap: wrap;
}
.mmp-value.mono { font-family: var(--font-mono); font-size: 11.5px; }
.mmp-value.bad { color: var(--bad); }
.mmp-value.prose { display: block; line-height: 1.6; font-size: 12.5px; }
.mmp-empty { color: var(--fg-4); }
.mmp-link { color: var(--fg-1); text-decoration: none; border-bottom: 1px dotted var(--fg-4); }
.mmp-link:hover { color: var(--gold-bright); border-bottom-color: var(--gold); }

.mmp-chips { display: inline-flex; gap: 5px; flex-wrap: wrap; }
.mmp-chip {
  display: inline-flex;
  padding: 1px 8px;
  border-radius: 999px;
  font-size: 11px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-1);
}

.mmp-prov {
  display: inline-flex;
  align-items: center;
  padding: 0 7px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  flex-shrink: 0;
}
.mmp-prov.user,
.mmp-prov.locked {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 45%, transparent);
}

.mmp-titles { display: flex; gap: 6px; flex-wrap: wrap; }
.mmp-alt {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 12px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-1);
}
.mmp-alt-lang {
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.mono { font-family: var(--font-mono); }
</style>
