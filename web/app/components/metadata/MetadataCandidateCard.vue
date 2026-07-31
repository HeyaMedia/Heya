<script setup lang="ts">
import type { SearchEvidence, SearchPresentation } from '~~/shared/api/types.gen'

type CandidateResult = {
  provider_id: string
  provider_name: string
  title: string
  year?: string
  description?: string
  poster_url?: string
  confidence?: number
  recommendation?: string
  requires_review?: boolean
  evidence?: SearchEvidence[] | null
  external_ids?: Record<string, string>
  heya_slug?: string
  presentation?: SearchPresentation
}

const props = withDefaults(defineProps<{
  result: CandidateResult
  compact?: boolean
  kind?: string
}>(), {
  compact: false,
  kind: '',
})

const presentation = computed(() => props.result.presentation ?? {})
const candidateKind = computed(() => presentation.value.kind || props.kind)

const artworkStyle = computed(() => {
  const width = presentation.value.image_width
  const height = presentation.value.image_height
  if (width && height) return { aspectRatio: `${width} / ${height}` }
  if (['artist', 'music', 'release_group', 'release', 'recording'].includes(candidateKind.value)) {
    return { aspectRatio: '1 / 1' }
  }
  return { aspectRatio: '2 / 3' }
})

const typeLabel = computed(() => humanize(presentation.value.type || candidateKind.value))

const locationLabel = computed(() => {
  const values = [
    presentation.value.area,
    presentation.value.country,
    ...(presentation.value.countries ?? []),
  ].filter((value): value is string => Boolean(value))
    .map(countryName)
  return unique(values).join(' · ')
})

const activityLabel = computed(() => {
  const begin = presentation.value.begin_date
  const end = presentation.value.end_date
  if (!begin && !end) return ''
  if (begin && end) return `${begin}–${end}`
  if (begin && presentation.value.ended === false) return `${begin}–present`
  return begin || end || ''
})

const contributors = computed(() => {
  const artists = presentation.value.artists ?? []
  const authors = presentation.value.authors ?? []
  if (artists.length) return { label: 'By', values: artists }
  if (authors.length) return { label: 'By', values: authors }
  return null
})

const facts = computed(() => {
  const p = presentation.value
  const values: string[] = []
  if (p.release_count) values.push(`${formatCount(p.release_count)} releases`)
  if (p.fan_count) values.push(`${formatCount(p.fan_count)} fans`)
  if (p.episode_count) values.push(`${formatCount(p.episode_count)} episodes`)
  if (p.edition_count) values.push(`${formatCount(p.edition_count)} editions`)
  if (p.network) values.push(p.network)
  if (p.studios?.length) values.push(p.studios.slice(0, 2).join(', '))
  if (p.status) values.push(humanize(p.status))
  if (p.language) values.push(p.language.toUpperCase())
  if (p.catalogue) values.push(p.catalogue)
  return unique(values)
})

const aliases = computed(() => unique([
  ...(presentation.value.aliases ?? []),
]).filter(value => value.toLocaleLowerCase() !== props.result.title.toLocaleLowerCase()))

const matchedReleaseLabel = computed(() => {
  const releases = presentation.value.matched_releases ?? []
  if (!releases.length) return ''
  return releases.slice(0, 3).map(release => (
    release.year ? `${release.title} (${release.year})` : release.title
  )).join(', ')
})

const linkedProviders = computed(() => {
  const providers = Object.keys(props.result.external_ids ?? {}).map((key) => {
    const normalized = key.toLocaleLowerCase().split(':')[0]!
    if (normalized === 'mbid' || normalized.startsWith('musicbrainz')) return 'musicbrainz'
    if (normalized.startsWith('apple')) return 'apple'
    if (normalized.startsWith('openlibrary') || normalized === 'ol_work_id') return 'openlibrary'
    if (normalized.startsWith('isbn')) return 'isbn'
    return normalized.replace(/_(?:artist|album|release_group|release|recording|id)$/i, '')
  })
  return unique(providers).map(providerLabel)
})

const matchLabel = computed(() => {
  const labels: Record<string, string> = {
    existing_entity: 'Linked identity',
    corroborated_identity: 'Corroborated identity',
    strong_match: 'Strong match',
    likely_match: 'Likely match',
    ambiguous: 'Needs review',
    no_match: 'Possible match',
  }
  return labels[props.result.recommendation ?? '']
    || (props.result.requires_review ? 'Needs review' : '')
})

const evidence = computed(() => (props.result.evidence ?? [])
  .filter(item => item.field && item.outcome && item.outcome !== 'unused')
  .slice(0, 4))

function humanize(value?: string): string {
  if (!value) return ''
  return value
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, letter => letter.toUpperCase())
}

function countryName(value: string): string {
  const trimmed = value.trim()
  if (!/^[A-Za-z]{2}$/.test(trimmed)) return trimmed
  try {
    return new Intl.DisplayNames(['en'], { type: 'region' }).of(trimmed.toUpperCase()) || trimmed
  } catch {
    return trimmed.toUpperCase()
  }
}

function unique(values: string[]): string[] {
  const seen = new Set<string>()
  return values.filter((value) => {
    const normalized = value.trim().toLocaleLowerCase()
    if (!normalized || seen.has(normalized)) return false
    seen.add(normalized)
    return true
  })
}

function formatCount(value: number): string {
  return new Intl.NumberFormat('en', {
    notation: value >= 1000 ? 'compact' : 'standard',
    maximumFractionDigits: 1,
  }).format(value)
}
</script>

<template>
  <article class="candidate" :class="{ compact }">
    <div class="candidate-art" :style="artworkStyle">
      <LoadingImage
        v-if="result.poster_url"
        :src="result.poster_url"
        :alt="`${result.title} artwork`"
        loading="lazy"
      />
      <span v-else aria-hidden="true">{{ result.title.trim().charAt(0).toUpperCase() || '?' }}</span>
    </div>

    <div class="candidate-body">
      <div class="candidate-heading">
        <h3>{{ result.title }}</h3>
        <span v-if="result.year" class="candidate-year">{{ result.year }}</span>
      </div>

      <div v-if="typeLabel || locationLabel || activityLabel || presentation.date" class="candidate-identity">
        <span v-if="typeLabel">{{ typeLabel }}</span>
        <span v-if="locationLabel">{{ locationLabel }}</span>
        <span v-if="activityLabel">{{ activityLabel }}</span>
        <span v-else-if="presentation.date">{{ presentation.date }}</span>
      </div>

      <p v-if="contributors" class="candidate-line">
        <b>{{ contributors.label }}</b> {{ contributors.values.join(', ') }}
      </p>
      <p v-if="result.description" class="candidate-description">{{ result.description }}</p>

      <div v-if="presentation.genres?.length || presentation.secondary_types?.length" class="candidate-chips">
        <span v-for="genre in unique([...(presentation.genres ?? []), ...(presentation.secondary_types ?? [])]).slice(0, 7)" :key="genre">
          {{ humanize(genre) }}
        </span>
      </div>

      <div v-if="facts.length" class="candidate-facts">
        <span v-for="fact in facts" :key="fact">{{ fact }}</span>
      </div>

      <p v-if="matchedReleaseLabel" class="candidate-line">
        <b>Matched releases</b> {{ matchedReleaseLabel }}
      </p>
      <p v-if="aliases.length" class="candidate-line">
        <b>Also known as</b> {{ aliases.slice(0, 4).join(', ') }}
      </p>

      <div v-if="linkedProviders.length" class="candidate-links">
        <span class="candidate-links-label">Linked across</span>
        <span v-for="provider in linkedProviders" :key="provider" class="candidate-source">{{ provider }}</span>
      </div>

      <div v-if="evidence.length || matchLabel" class="candidate-match">
        <span v-if="matchLabel" class="candidate-match-label">
          {{ matchLabel }}<template v-if="result.confidence && result.confidence < 1"> · {{ Math.round(result.confidence * 100) }}%</template>
        </span>
        <span
          v-for="item in evidence"
          :key="`${item.field}-${item.outcome}`"
          class="candidate-evidence"
          :title="item.detail"
        >
          {{ humanize(item.field.replace(/^identifier:/, '')) }} {{ humanize(item.outcome).toLocaleLowerCase() }}
        </span>
      </div>
    </div>

    <div class="candidate-actions">
      <slot name="actions" />
    </div>
  </article>
</template>

<style scoped>
.candidate {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr) auto;
  gap: 16px;
  align-items: start;
  padding: 16px;
  color: var(--fg-1);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.candidate.compact {
  grid-template-columns: 76px minmax(0, 1fr) auto;
  padding: 14px;
  border-width: 0 0 1px;
  border-radius: 0;
  background: transparent;
}
.candidate-art {
  width: 100%;
  min-height: 76px;
  max-height: 138px;
  display: grid;
  place-items: center;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-3);
  background: var(--bg-3);
  font: 500 27px/1 var(--font-display);
}
.candidate-art :deep(img) {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.candidate-body {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.candidate-heading {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.candidate-heading h3 {
  margin: 0;
  color: var(--fg-0);
  font-size: 15px;
  font-weight: 650;
  line-height: 1.25;
}
.candidate-year {
  color: var(--fg-2);
  font-size: 12.5px;
}
.candidate-identity,
.candidate-facts,
.candidate-links,
.candidate-match {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 5px 10px;
}
.candidate-identity {
  color: var(--fg-2);
  font-size: 11.5px;
}
.candidate-identity span + span::before,
.candidate-facts span + span::before {
  content: '·';
  margin-right: 10px;
  color: var(--fg-3);
}
.candidate-description,
.candidate-line {
  margin: 0;
  color: var(--fg-2);
  font-size: 12px;
  line-height: 1.45;
}
.candidate-description {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}
.candidate-line b {
  margin-right: 4px;
  color: var(--fg-1);
  font-weight: 600;
}
.candidate-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.candidate-chips span,
.candidate-source,
.candidate-evidence,
.candidate-match-label {
  padding: 2px 7px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.025);
  font-size: 10px;
  line-height: 1.35;
}
.candidate-facts {
  color: var(--fg-2);
  font-size: 11px;
}
.candidate-links-label {
  color: var(--fg-3);
  font-size: 10.5px;
  text-transform: uppercase;
  letter-spacing: .05em;
}
.candidate-source {
  color: var(--fg-1);
}
.candidate-match {
  padding-top: 1px;
}
.candidate-match-label {
  border-color: color-mix(in srgb, var(--gold) 30%, var(--border));
  color: var(--gold);
}
.candidate-evidence {
  color: var(--fg-3);
}
.candidate-actions {
  display: flex;
  align-self: center;
  flex-shrink: 0;
}
@media (max-width: 640px) {
  .candidate,
  .candidate.compact {
    grid-template-columns: 68px minmax(0, 1fr);
    gap: 12px;
  }
  .candidate-actions {
    grid-column: 2;
    justify-self: start;
  }
}
</style>
