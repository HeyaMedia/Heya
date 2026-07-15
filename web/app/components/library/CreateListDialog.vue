<!--
  CreateListDialog — replaces the browser-native window.prompt() the library
  sidebar used for "New List". Built on AppDialog (modeled on
  CreatePlaylistModal): a single autofocused name field, Enter submits, Escape
  cancels, non-empty/trim validation, inline error. Posts the SAME payload the
  old prompt flow did (`POST /api/me/lists` with just `{ name }`) and emits the
  created row so the sidebar can slot it into the accordion + navigate to it.

  Content is portaled out of <body> by AppDialog, so the .cld-* styles live
  unscoped (prefixed, so they don't leak) — scoped CSS can't reach them.
-->
<template>
  <AppDialog
    v-model="open"
    title="New list"
    size="sm"
    prevent-auto-focus
    content-class="cld"
  >
    <form class="cld-form" @submit.prevent="submit">
      <label class="cld-field">
        <span class="cld-label">List name</span>
        <input
          ref="nameInput"
          v-model="name"
          type="text"
          class="cld-input"
          placeholder="Weekend Watchlist"
          maxlength="80"
          @keydown.enter.prevent="submit"
        >
      </label>
      <p v-if="error" class="cld-error">{{ error }}</p>
    </form>
    <template #footer="{ close }">
      <button type="button" class="btn" @click="close">Cancel</button>
      <button
        type="button"
        class="btn btn-primary cld-create"
        :disabled="busy || !name.trim()"
        @click="submit"
      >
        {{ busy ? 'Creating…' : 'Create list' }}
      </button>
    </template>
  </AppDialog>
</template>

<script setup lang="ts">
import type { UserList } from '~~/shared/types'

const props = defineProps<{
  /** Section the list belongs to — the API requires it. */
  mediaType: string
}>()

const open = defineModel<boolean>({ default: false })
const emit = defineEmits<{ created: [row: UserList] }>()

const { $heya } = useNuxtApp()

const name = ref('')
const error = ref('')
const busy = ref(false)
const nameInput = ref<HTMLInputElement | null>(null)

// Reset + focus on every open. AppDialog's `prevent-auto-focus` hands focus
// to us so the name field wins over the header's close button.
watch(open, async (v) => {
  if (!v) return
  name.value = ''
  error.value = ''
  busy.value = false
  await nextTick()
  nameInput.value?.focus()
})

async function submit() {
  const trimmed = name.value.trim()
  if (!trimmed || busy.value) return
  busy.value = true
  error.value = ''
  try {
    // The API requires the full manual-list shape (name-only 422s) — mirror
    // the smart-list save, minus the filter (a manual list starts empty).
    const created = await $heya('/api/me/lists', {
      method: 'POST',
      body: {
        name: trimmed,
        description: '',
        list_type: 'manual',
        filter_json: null,
        media_type: props.mediaType,
      } as any,
    }) as UserList
    emit('created', created)
    open.value = false
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || 'Could not create the list.'
  } finally {
    busy.value = false
  }
}
</script>

<style>
/* Portaled content — unscoped, .cld-prefixed so nothing leaks. */
.cld-form { display: flex; flex-direction: column; gap: 12px; }
.cld-field { display: flex; flex-direction: column; gap: 6px; }
.cld-label {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-2);
}
.cld-input {
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
.cld-input:focus { border-color: var(--gold); }
.cld-input::placeholder { color: var(--fg-3); }
.cld-error { font-size: 12px; color: var(--bad); margin: 2px 0 0; }

/* Tone-glow primary — gold fill + a soft halo that deepens on hover. */
.cld-create {
  padding: 0 20px;
  height: 36px;
  border-radius: 999px;
  font-weight: 600;
  box-shadow: 0 4px 18px color-mix(in srgb, var(--gold) 34%, transparent);
  transition: box-shadow 0.18s ease, background 0.15s ease;
}
.cld-create:hover:not(:disabled) {
  box-shadow: 0 6px 26px color-mix(in srgb, var(--gold) 52%, transparent);
}
.cld-create:disabled { box-shadow: none; }
</style>
