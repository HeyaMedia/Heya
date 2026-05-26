<script setup lang="ts">
// 5-star rating widget with true half-star precision. Internally tracks
// 1..10 (matches the SMALLINT column); 0 = unrated. Mouse hover previews;
// click commits.
//
// Each star slot renders one of three Phosphor glyphs — outline / half-fill
// / full-fill — so the half-state is pixel-perfect (no overlay clipping).
//
// Half-stars: hovering or clicking the LEFT half of a star sets the
// half-step (odd value), the RIGHT half sets the whole-step (even value).
//
// Clearing: a small dim "0" sits to the left of the first star — hover
// fades it gold + the stars all empty as preview; click commits to 0.

const props = withDefaults(defineProps<{
  /** Current rating 0..10. 0 = unrated. */
  modelValue: number
  /** Render small (14px stars) for inline use, medium (18px) for hero. */
  size?: 'sm' | 'md'
  /** Disable interaction (read-only display). */
  readonly?: boolean
}>(), {
  size: 'md',
  readonly: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: number]
}>()

const hoverValue = ref<number | null>(null)

const displayValue = computed(() => hoverValue.value ?? props.modelValue)
const starSize = computed(() => (props.size === 'sm' ? 15 : 19))
const clearSize = computed(() => (props.size === 'sm' ? 9 : 11))

type Fill = 'empty' | 'half' | 'full'
function starFill(starIdx: number): Fill {
  const v = displayValue.value
  const fullThreshold = starIdx * 2
  const halfThreshold = starIdx * 2 - 1
  if (v >= fullThreshold) return 'full'
  if (v >= halfThreshold) return 'half'
  return 'empty'
}

function valueForClick(starIdx: number, isLeftHalf: boolean): number {
  return isLeftHalf ? starIdx * 2 - 1 : starIdx * 2
}

function onClickStar(starIdx: number, e: MouseEvent) {
  if (props.readonly) return
  const target = e.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()
  const isLeftHalf = (e.clientX - rect.left) < rect.width / 2
  emit('update:modelValue', valueForClick(starIdx, isLeftHalf))
}

function onMouseMoveStar(starIdx: number, e: MouseEvent) {
  if (props.readonly) return
  const target = e.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()
  const isLeftHalf = (e.clientX - rect.left) < rect.width / 2
  hoverValue.value = valueForClick(starIdx, isLeftHalf)
}

function onZeroEnter() {
  if (props.readonly) return
  hoverValue.value = 0
}

function onZeroClick() {
  if (props.readonly) return
  emit('update:modelValue', 0)
}

function onMouseLeave() {
  hoverValue.value = null
}
</script>

<template>
  <div
    class="sr"
    :class="[`sr-${size}`, { 'sr-readonly': readonly, 'sr-hovering': hoverValue !== null }]"
    @mouseleave="onMouseLeave"
  >
    <!-- Clear affordance: small "0" glyph always reserved on the left so
         the widget width never shifts. Visually dim at rest; brightens on
         hover. Disabled visually + functionally when unrated. -->
    <button
      type="button"
      class="sr-clear"
      :class="{ 'sr-clear-disabled': modelValue === 0 && hoverValue !== 0, 'sr-clear-active': hoverValue === 0 }"
      :tabindex="readonly || modelValue === 0 ? -1 : 0"
      title="Clear rating"
      @mouseenter="onZeroEnter"
      @click.stop="onZeroClick"
    >
      <svg :width="clearSize" :height="clearSize" viewBox="0 0 16 16" aria-hidden="true">
        <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </button>

    <span
      v-for="i in 5"
      :key="i"
      class="sr-star"
      :class="`sr-${starFill(i)}`"
      @click.stop="onClickStar(i, $event)"
      @mousemove="onMouseMoveStar(i, $event)"
    >
      <Icon
        :name="starFill(i) === 'half' ? 'star-half' : 'star'"
        :size="starSize"
        :weight="starFill(i) === 'empty' ? 'regular' : 'fill'"
      />
    </span>
  </div>
</template>

<style scoped>
.sr {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}

.sr-clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px; height: 16px;
  margin-right: 2px;
  padding: 0;
  background: transparent;
  border: 0;
  cursor: pointer;
  color: var(--fg-3);
  opacity: 0.45;
  border-radius: 50%;
  transition: opacity 0.15s, color 0.15s, background 0.15s, transform 0.12s;
}
.sr-clear:hover { opacity: 1; color: var(--fg-1); background: rgba(255,255,255,0.06); }
.sr-clear-active { opacity: 1; color: var(--gold) !important; background: var(--gold-soft) !important; transform: scale(1.08); }
/* When the row is unrated the clear button is non-actionable — render it
   invisible so the gutter still reserves width but isn't a visual hit. */
.sr-clear-disabled { opacity: 0 !important; pointer-events: none; }
.sr-readonly .sr-clear { display: none; }

.sr-star {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: rgba(255, 255, 255, 0.22);
  transition: color 0.12s, transform 0.12s;
}
.sr-readonly .sr-star { cursor: default; }
.sr-hovering .sr-star { color: rgba(255, 255, 255, 0.32); }
.sr-half, .sr-full { color: var(--gold) !important; }
.sr-star:hover { transform: scale(1.06); }
.sr-readonly .sr-star:hover { transform: none; }

.sr-md .sr-clear { width: 18px; height: 18px; }
</style>
