<script setup lang="ts">
import { computed } from 'vue'
import {
  PhHouse, PhFilmSlate, PhTelevisionSimple, PhMusicNotes, PhBookOpen,
  PhMagnifyingGlass, PhGearSix, PhPlay, PhPause, PhSkipBack, PhSkipForward,
  PhShuffle, PhRepeat, PhSpeakerHigh, PhSpeakerLow, PhSpeakerNone, PhSpeakerX, PhQueue, PhHeart,
  PhPlus, PhDotsThree, PhStar, PhCheck, PhCaretDown, PhCaretRight,
  PhCaretLeft, PhX, PhAirplay, PhDownloadSimple, PhGlobe, PhMicrophone,
  PhRadio, PhList, PhSquaresFour, PhFunnel, PhSortAscending, PhArrowLeft,
  PhUser, PhBell, PhFolder, PhTreeStructure, PhBookmarkSimple,
  PhBookOpenText, PhTextAa, PhSun, PhSliders, PhChatText,
  PhArrowsOut, PhArrowsIn,
  PhChartBar, PhDatabase, PhShieldCheck, PhCpu, PhUsers, PhInfo,
  PhTrash, PhPencilSimple, PhWarning, PhClock, PhEnvelope, PhUserPlus,
  PhCrown, PhKey, PhArrowRight, PhEye, PhSignOut, PhWrench,
  PhHardDrives, PhPulse, PhSpinner, PhCloudArrowDown, PhGithubLogo,
  PhArrowClockwise, PhLightning, PhCircleNotch, PhToggleRight, PhToggleLeft,
  PhClipboardText, PhTimer, PhHash, PhCopyright,
  PhSubtitles, PhClosedCaptioning, PhTranslate,
} from '@phosphor-icons/vue'

const props = defineProps<{
  name: string
  size?: number
  weight?: 'thin' | 'light' | 'regular' | 'bold' | 'fill' | 'duotone'
}>()

const nameMap: Record<string, any> = {
  home: PhHouse,
  film: PhFilmSlate,
  tv: PhTelevisionSimple,
  music: PhMusicNotes,
  book: PhBookOpen,
  search: PhMagnifyingGlass,
  settings: PhGearSix,
  play: PhPlay,
  pause: PhPause,
  prev: PhSkipBack,
  next: PhSkipForward,
  shuffle: PhShuffle,
  repeat: PhRepeat,
  vol: PhSpeakerHigh,
  volmute: PhSpeakerX,
  queue: PhQueue,
  heart: PhHeart,
  heartfill: PhHeart,
  plus: PhPlus,
  more: PhDotsThree,
  star: PhStar,
  check: PhCheck,
  chevdown: PhCaretDown,
  chevright: PhCaretRight,
  chevleft: PhCaretLeft,
  close: PhX,
  cast: PhAirplay,
  download: PhDownloadSimple,
  globe: PhGlobe,
  mic: PhMicrophone,
  radio: PhRadio,
  list: PhList,
  grid: PhSquaresFour,
  filter: PhFunnel,
  sort: PhSortAscending,
  back: PhArrowLeft,
  user: PhUser,
  bell: PhBell,
  folder: PhFolder,
  network: PhTreeStructure,
  bookmark: PhBookmarkSimple,
  reader: PhBookOpenText,
  type: PhTextAa,
  brightness: PhSun,
  eq: PhSliders,
  lyrics: PhChatText,
  expand: PhArrowsOut,
  collapse: PhArrowsIn,
  'chart-bar': PhChartBar,
  database: PhDatabase,
  shield: PhShieldCheck,
  cpu: PhCpu,
  users: PhUsers,
  info: PhInfo,
  trash: PhTrash,
  pencil: PhPencilSimple,
  warning: PhWarning,
  clock: PhClock,
  envelope: PhEnvelope,
  'user-plus': PhUserPlus,
  crown: PhCrown,
  key: PhKey,
  'arrow-right': PhArrowRight,
  eye: PhEye,
  'sign-out': PhSignOut,
  wrench: PhWrench,
  'hard-drives': PhHardDrives,
  pulse: PhPulse,
  spinner: PhSpinner,
  'cloud-download': PhCloudArrowDown,
  github: PhGithubLogo,
  refresh: PhArrowClockwise,
  lightning: PhLightning,
  loading: PhCircleNotch,
  'toggle-on': PhToggleRight,
  'toggle-off': PhToggleLeft,
  clipboard: PhClipboardText,
  timer: PhTimer,
  hash: PhHash,
  copyright: PhCopyright,
  speakerhigh: PhSpeakerHigh,
  speakerlow: PhSpeakerLow,
  speakernone: PhSpeakerNone,
  speakerx: PhSpeakerX,
  subtitles: PhSubtitles,
  captions: PhClosedCaptioning,
  translate: PhTranslate,
  shrink: PhArrowsIn,
}

const iconComponent = computed(() => nameMap[props.name] || null)

const iconWeight = computed(() => {
  if (props.weight) return props.weight
  if (props.name === 'heartfill') return 'fill'
  return 'regular'
})
</script>

<template>
  <component
    v-if="iconComponent"
    :is="iconComponent"
    :size="size || 18"
    :weight="iconWeight"
  />
</template>
