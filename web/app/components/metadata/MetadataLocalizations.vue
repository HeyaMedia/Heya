<script setup lang="ts">
const props = defineProps<{
  titles?: { id?: number; title: string; language: string; country?: string; source?: string; title_type?: string }[] | null
  overviews?: { id?: number; overview: string; language: string; source?: string }[] | null
  libraryLanguage?: string | null
  primaryTitle?: string | null
  primaryOverview?: string | null
}>()

const LANG_LABELS: Record<string, string> = {
  en: 'English', eng: 'English', ja: 'Japanese', jpn: 'Japanese',
  de: 'German', ger: 'German', deu: 'German', fr: 'French', fre: 'French', fra: 'French',
  es: 'Spanish', spa: 'Spanish', it: 'Italian', ita: 'Italian',
  pt: 'Portuguese', por: 'Portuguese', ru: 'Russian', rus: 'Russian',
  ko: 'Korean', kor: 'Korean', zh: 'Chinese', chi: 'Chinese', zho: 'Chinese',
  ar: 'Arabic', ara: 'Arabic', hi: 'Hindi', hin: 'Hindi',
  da: 'Danish', dan: 'Danish', sv: 'Swedish', swe: 'Swedish',
  no: 'Norwegian', nor: 'Norwegian', nb: 'Norwegian', nob: 'Norwegian',
  fi: 'Finnish', fin: 'Finnish', nl: 'Dutch', dut: 'Dutch', nld: 'Dutch',
  pl: 'Polish', pol: 'Polish', tr: 'Turkish', tur: 'Turkish',
  cs: 'Czech', ces: 'Czech', hu: 'Hungarian', hun: 'Hungarian',
  ro: 'Romanian', ron: 'Romanian', he: 'Hebrew', heb: 'Hebrew',
  th: 'Thai', tha: 'Thai', vi: 'Vietnamese', vie: 'Vietnamese',
  id: 'Indonesian', ind: 'Indonesian', uk: 'Ukrainian', ukr: 'Ukrainian',
  hr: 'Croatian', hrv: 'Croatian', sr: 'Serbian', srp: 'Serbian',
  und: 'Unknown',
}

function langLabel(code: string) {
  if (!code) return 'Unknown'
  return LANG_LABELS[code.toLowerCase()] || code.toUpperCase()
}

function isLibraryLang(code: string) {
  if (!props.libraryLanguage || !code) return false
  const lib = props.libraryLanguage.toLowerCase()
  const c = code.toLowerCase()
  return lib === c || lib.startsWith(c) || c.startsWith(lib)
}

const titlesByLang = computed(() => {
  const groups: Record<string, typeof props.titles> = {}
  for (const t of (props.titles || [])) {
    const key = t.language || 'und'
    if (!groups[key]) groups[key] = []
    groups[key]!.push(t)
  }
  return Object.entries(groups)
    .map(([lang, items]) => ({ lang, items: items! }))
    .sort((a, b) => {
      if (isLibraryLang(a.lang)) return -1
      if (isLibraryLang(b.lang)) return 1
      return a.lang.localeCompare(b.lang)
    })
})

const overviewsByLang = computed(() => {
  const groups: Record<string, typeof props.overviews> = {}
  for (const o of (props.overviews || [])) {
    const key = o.language || 'und'
    if (!groups[key]) groups[key] = []
    groups[key]!.push(o)
  }
  return Object.entries(groups)
    .map(([lang, items]) => ({ lang, items: items! }))
    .sort((a, b) => {
      if (isLibraryLang(a.lang)) return -1
      if (isLibraryLang(b.lang)) return 1
      return a.lang.localeCompare(b.lang)
    })
})

const hasTitles = computed(() => titlesByLang.value.length > 0)
const hasOverviews = computed(() => overviewsByLang.value.length > 0)
</script>

<template>
  <div class="mf">
    <div v-if="!hasTitles && !hasOverviews" class="loc-empty">
      <Icon name="translate" :size="28" />
      <p>No alternate translations recorded for this item.</p>
      <p class="loc-empty-sub">Pull richer metadata via Identify or a refresh to populate localized titles and overviews.</p>
    </div>

    <div v-if="hasTitles" class="mf-card">
      <div class="loc-section-head">
        <span class="mf-card-head-inline">Titles</span>
        <span class="loc-count">{{ titlesByLang.length }} languages</span>
      </div>
      <div class="loc-grid">
        <div
          v-for="g in titlesByLang"
          :key="`t-${g.lang}`"
          class="loc-card"
          :class="{ 'loc-primary': isLibraryLang(g.lang) }"
        >
          <div class="loc-card-head">
            <span class="loc-lang">{{ langLabel(g.lang) }}</span>
            <span class="loc-code">{{ g.lang.toUpperCase() }}</span>
            <span v-if="isLibraryLang(g.lang)" class="loc-flag">Library</span>
          </div>
          <div v-for="(t, i) in g.items" :key="t.id ?? i" class="loc-row">
            <div class="loc-value">{{ t.title }}</div>
            <div v-if="t.title_type || t.source || t.country" class="loc-meta">
              <span v-if="t.title_type">{{ t.title_type }}</span>
              <span v-if="t.country">{{ t.country }}</span>
              <span v-if="t.source" class="loc-source">{{ t.source }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="hasOverviews" class="mf-card">
      <div class="loc-section-head">
        <span class="mf-card-head-inline">Overviews</span>
        <span class="loc-count">{{ overviewsByLang.length }} languages</span>
      </div>
      <div class="loc-overview-list">
        <div
          v-for="g in overviewsByLang"
          :key="`o-${g.lang}`"
          class="loc-card"
          :class="{ 'loc-primary': isLibraryLang(g.lang) }"
        >
          <div class="loc-card-head">
            <span class="loc-lang">{{ langLabel(g.lang) }}</span>
            <span class="loc-code">{{ g.lang.toUpperCase() }}</span>
            <span v-if="isLibraryLang(g.lang)" class="loc-flag">Library</span>
          </div>
          <p v-for="(o, i) in g.items" :key="o.id ?? i" class="loc-overview-text">
            {{ o.overview }}
            <span v-if="o.source" class="loc-source loc-source-inline">{{ o.source }}</span>
          </p>
        </div>
      </div>
    </div>

    <div v-if="primaryTitle || primaryOverview" class="mf-card loc-stored">
      <div class="loc-section-head">
        <span class="mf-card-head-inline">Stored on item</span>
        <span class="loc-count">canonical</span>
      </div>
      <div class="loc-stored-content">
        <div v-if="primaryTitle" class="loc-row">
          <div class="loc-label">Title</div>
          <div class="loc-value">{{ primaryTitle }}</div>
        </div>
        <div v-if="primaryOverview" class="loc-row">
          <div class="loc-label">Overview</div>
          <div class="loc-value loc-overview-text">{{ primaryOverview }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

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

.mf-card-head-inline {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
}

.loc-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 64px 24px;
  color: var(--fg-3);
  text-align: center;
  width: 100%;
}
.loc-empty p {
  margin: 0;
  font-size: 14px;
}
.loc-empty-sub {
  font-size: 12px;
  color: var(--fg-4);
}

.loc-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
  padding-bottom: 10px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border);
}

.loc-count {
  font-size: 10px;
  color: var(--fg-4);
  font-family: var(--font-mono);
}

.loc-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 10px;
}

.loc-overview-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 10px;
}

.loc-card {
  padding: 12px 14px;
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid rgba(255, 255, 255, 0.04);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.loc-card.loc-primary {
  background: var(--gold-soft);
  border-color: rgba(251, 191, 36, 0.35);
}
.loc-card.loc-primary .loc-lang {
  color: var(--gold-bright);
}

.loc-card-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 4px;
  margin-bottom: 2px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.loc-lang {
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-0);
}

.loc-code {
  font-size: 9px;
  font-weight: 700;
  font-family: var(--font-mono);
  letter-spacing: 0.06em;
  color: var(--fg-4);
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.05);
}

.loc-flag {
  font-size: 9px;
  font-weight: 700;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--gold);
  color: #000;
  margin-left: auto;
}

.loc-row {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 4px 0;
}

.loc-label {
  font-size: 9px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}

.loc-value {
  font-size: 13px;
  color: var(--fg-1);
  line-height: 1.4;
  word-break: break-word;
}

.loc-meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  font-size: 9px;
  font-family: var(--font-mono);
  color: var(--fg-4);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.loc-source {
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(96, 165, 250, 0.12);
  color: rgb(96, 165, 250);
}
.loc-source-inline {
  margin-left: 6px;
  vertical-align: middle;
}

.loc-overview-text {
  font-size: 13px;
  line-height: 1.55;
  color: var(--fg-1);
  margin: 0;
  padding: 2px 0;
}

.loc-stored-content {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
