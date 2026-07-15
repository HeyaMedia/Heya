<!--
  TextPromptDialog — the in-app replacement for window.prompt(). Driven by the
  usePrompt() singleton (state lives module-level), mounted once in app.vue next
  to TrackInfoDialog so any surface — including composables that can't render
  components (usePlaylistMenu, useMusicActions) — can open it via promptText().

  Built on AppDialog (like CreateListDialog): single autofocused input, Enter
  submits, Escape / overlay / Cancel resolve null. `allowEmpty` lets an empty
  submit resolve '' (tags-clear) rather than being blocked.

  Content is portaled out of <body> by AppDialog, so .tpd-* styles live
  unscoped (prefixed so nothing leaks) — scoped CSS can't reach them.
-->
<template>
  <AppDialog
    :model-value="state.open"
    :title="state.title"
    size="sm"
    prevent-auto-focus
    content-class="tpd"
    @update:model-value="onOpenChange"
  >
    <form class="tpd-form" @submit.prevent="submit">
      <label class="tpd-field">
        <span v-if="state.label" class="tpd-label">{{ state.label }}</span>
        <input
          ref="inputEl"
          v-model="value"
          type="text"
          class="tpd-input"
          :placeholder="state.placeholder || ''"
          :maxlength="state.maxLength || 200"
          @keydown.enter.prevent="submit"
        >
      </label>
      <p v-if="state.message" class="tpd-hint">{{ state.message }}</p>
    </form>
    <template #footer>
      <button type="button" class="btn" @click="cancel">{{ state.cancelLabel }}</button>
      <button
        type="button"
        class="btn btn-primary tpd-confirm"
        :disabled="!canSubmit"
        @click="submit"
      >
        {{ state.confirmLabel }}
      </button>
    </template>
  </AppDialog>
</template>

<script setup lang="ts">
const { state, _resolve } = usePrompt()

const value = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

const canSubmit = computed(() =>
  state.value.allowEmpty ? true : value.value.trim().length > 0,
)

// Reset + focus on every open. AppDialog's `prevent-auto-focus` hands focus to
// us; selecting the text means a pre-filled value can be typed straight over.
watch(() => state.value.open, async (open) => {
  if (!open) return
  value.value = state.value.initial ?? ''
  await nextTick()
  inputEl.value?.focus()
  inputEl.value?.select()
})

function submit() {
  if (!canSubmit.value) return
  // Non-empty prompts trim (matches the old `prompt(...)?.trim()` sites);
  // allowEmpty (tags) preserves the raw string so the caller can split it.
  _resolve(state.value.allowEmpty ? value.value : value.value.trim())
}

function cancel() {
  _resolve(null)
}

// Escape / overlay / close-button dismiss all emit update:model-value(false).
// Only cancel when the prompt is still open — a programmatic close from
// submit()/cancel() already flipped state.open to false, so this won't
// double-resolve.
function onOpenChange(v: boolean) {
  if (!v && state.value.open) cancel()
}
</script>

<style>
/* Portaled content — unscoped, .tpd-prefixed so nothing leaks. */
.tpd-form { display: flex; flex-direction: column; gap: 12px; }
.tpd-field { display: flex; flex-direction: column; gap: 6px; }
.tpd-label {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-2);
}
.tpd-input {
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  padding: 10px 12px;
  font-size: 14px;
  font-family: inherit;
  outline: none;
  transition: border-color 0.15s;
}
.tpd-input:focus { border-color: var(--gold); }
.tpd-input::placeholder { color: var(--fg-3); }
.tpd-hint { font-size: 12px; color: var(--fg-2); margin: 2px 0 0; line-height: 1.5; }

/* Tone-glow primary — gold fill + a soft halo that deepens on hover. Mirrors
   CreateListDialog's .cld-create so every text-prompt reads the same. */
.tpd-confirm {
  padding: 0 20px;
  height: 36px;
  border-radius: 999px;
  font-weight: 600;
  box-shadow: 0 4px 18px color-mix(in srgb, var(--gold) 34%, transparent);
  transition: box-shadow 0.18s ease, background 0.15s ease;
}
.tpd-confirm:hover:not(:disabled) {
  box-shadow: 0 6px 26px color-mix(in srgb, var(--gold) 52%, transparent);
}
.tpd-confirm:disabled { box-shadow: none; opacity: 0.55; }
</style>
