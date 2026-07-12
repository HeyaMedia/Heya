<!--
  ReactionControl — the one-tap music taste widget: Thumbs Down / Thumbs Up /
  Heart. Replaces StarRating on music surfaces (video keeps its own system).

  Storage stays the 1–10 rating scale; the control writes sentinel values
  (down=1, up=7, heart=10) and reads BANDS, so ratings arriving from Subsonic
  clients (1–5 stars, stored ×2) light up the matching reaction:
    ≤3 → down · 6–8 → up · ≥9 → heart · 4–5/0 → none
  Clicking the active reaction clears back to 0. The taste model reads the
  same bands, so every client feeds one signal store.
-->
<script setup lang="ts">
const props = withDefaults(defineProps<{
  modelValue: number // 0..10 rating
  size?: 'sm' | 'md'
}>(), {
  size: 'md',
})

const emit = defineEmits<{ 'update:modelValue': [value: number] }>()

export type Reaction = 'down' | 'up' | 'heart' | null

const REACTION_DOWN = 1
const REACTION_UP = 7
const REACTION_HEART = 10

const active = computed<Reaction>(() => {
  const r = props.modelValue
  if (r >= 9) return 'heart'
  if (r >= 6 && r <= 8) return 'up'
  if (r >= 1 && r <= 3) return 'down'
  return null
})

function pick(reaction: Exclude<Reaction, null>) {
  if (active.value === reaction) {
    emit('update:modelValue', 0) // tap the active one to clear
    return
  }
  emit('update:modelValue', reaction === 'heart' ? REACTION_HEART : reaction === 'up' ? REACTION_UP : REACTION_DOWN)
}

const iconSize = computed(() => (props.size === 'sm' ? 14 : 17))
</script>

<template>
  <div class="reaction" :class="[`reaction--${size}`]" @click.stop>
    <button
      class="reaction-btn reaction-down"
      :class="{ active: active === 'down' }"
      :aria-pressed="active === 'down'"
      title="Not for me"
      aria-label="Thumbs down"
      @click="pick('down')"
    >
      <Icon :name="active === 'down' ? 'thumbsdownfill' : 'thumbsdown'" :size="iconSize" />
    </button>
    <button
      class="reaction-btn reaction-up"
      :class="{ active: active === 'up' }"
      :aria-pressed="active === 'up'"
      title="Like"
      aria-label="Thumbs up"
      @click="pick('up')"
    >
      <Icon :name="active === 'up' ? 'thumbsupfill' : 'thumbsup'" :size="iconSize" />
    </button>
    <button
      class="reaction-btn reaction-heart"
      :class="{ active: active === 'heart' }"
      :aria-pressed="active === 'heart'"
      title="Love"
      aria-label="Heart"
      @click="pick('heart')"
    >
      <Icon :name="active === 'heart' ? 'heartfill' : 'heart'" :size="iconSize" />
    </button>
  </div>
</template>

<style scoped>
.reaction { display: inline-flex; align-items: center; gap: 2px; }
.reaction-btn {
  display: inline-flex; align-items: center; justify-content: center;
  padding: 5px; border-radius: 999px;
  color: var(--fg-3); cursor: pointer;
  transition: color 0.12s ease, background 0.12s ease, transform 0.1s ease;
}
.reaction--sm .reaction-btn { padding: 3px; }
.reaction-btn:hover { color: var(--fg-0); background: rgba(255, 255, 255, 0.06); }
.reaction-btn:active { transform: scale(0.9); }
.reaction-down.active { color: #e06c5c; }
.reaction-up.active { color: #6fbf73; }
.reaction-heart.active { color: var(--gold-bright, #e0b45c); }
</style>
