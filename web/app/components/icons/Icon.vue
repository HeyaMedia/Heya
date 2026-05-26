<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  name: string
  size?: number
  weight?: 'thin' | 'light' | 'regular' | 'bold' | 'fill' | 'duotone'
}>()

// Internal alias → phosphor kebab name. Phosphor icons load as async chunks
// via nuxt-phosphor-icons, so only the icons actually rendered are fetched.
const nameMap: Record<string, string> = {
  home: 'house',
  film: 'film-slate',
  tv: 'television-simple',
  music: 'music-notes',
  book: 'book-open',
  search: 'magnifying-glass',
  settings: 'gear-six',
  play: 'play',
  pause: 'pause',
  prev: 'skip-back',
  next: 'skip-forward',
  shuffle: 'shuffle',
  repeat: 'repeat',
  vol: 'speaker-high',
  volmute: 'speaker-x',
  queue: 'queue',
  heart: 'heart',
  heartfill: 'heart',
  plus: 'plus',
  more: 'dots-three',
  star: 'star',
  check: 'check',
  chevdown: 'caret-down',
  chevright: 'caret-right',
  chevleft: 'caret-left',
  close: 'x',
  cast: 'airplay',
  download: 'download-simple',
  globe: 'globe',
  mic: 'microphone',
  radio: 'radio',
  list: 'list',
  grid: 'squares-four',
  filter: 'funnel',
  sort: 'sort-ascending',
  back: 'arrow-left',
  user: 'user',
  bell: 'bell',
  folder: 'folder',
  network: 'tree-structure',
  bookmark: 'bookmark-simple',
  reader: 'book-open-text',
  type: 'text-aa',
  brightness: 'sun',
  eq: 'sliders',
  lyrics: 'chat-text',
  expand: 'arrows-out',
  collapse: 'arrows-in',
  'chart-bar': 'chart-bar',
  chart: 'chart-bar',
  sparkle: 'sparkle',
  compass: 'compass',
  link: 'link-simple',
  'star-half': 'star-half',
  database: 'database',
  shield: 'shield-check',
  cpu: 'cpu',
  users: 'users',
  info: 'info',
  trash: 'trash',
  pencil: 'pencil-simple',
  warning: 'warning',
  clock: 'clock',
  envelope: 'envelope',
  'user-plus': 'user-plus',
  crown: 'crown',
  key: 'key',
  'arrow-right': 'arrow-right',
  eye: 'eye',
  'sign-out': 'sign-out',
  wrench: 'wrench',
  'hard-drives': 'hard-drives',
  pulse: 'pulse',
  spinner: 'spinner',
  'cloud-download': 'cloud-arrow-down',
  github: 'github-logo',
  refresh: 'arrow-clockwise',
  lightning: 'lightning',
  loading: 'circle-notch',
  'toggle-on': 'toggle-right',
  'toggle-off': 'toggle-left',
  clipboard: 'clipboard-text',
  timer: 'timer',
  hash: 'hash',
  copyright: 'copyright',
  speakerhigh: 'speaker-high',
  speakerlow: 'speaker-low',
  speakernone: 'speaker-none',
  speakerx: 'speaker-x',
  subtitles: 'subtitles',
  captions: 'closed-captioning',
  translate: 'translate',
  shrink: 'arrows-in',
}

const phosphorName = computed(() => nameMap[props.name] ?? null)

const iconWeight = computed(() => {
  if (props.weight) return props.weight
  if (props.name === 'heartfill') return 'fill'
  return 'regular'
})
</script>

<template>
  <PhosphorIcon
    v-if="phosphorName"
    :name="phosphorName"
    :size="size || 18"
    :weight="iconWeight"
  />
</template>
