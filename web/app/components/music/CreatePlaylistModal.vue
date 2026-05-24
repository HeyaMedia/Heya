<template>
  <Teleport to="body">
    <Transition name="cpm">
      <div v-if="open" class="cpm-overlay" @click.self="$emit('close')">
        <div class="cpm-modal">
          <header class="cpm-header">
            <h3>Create Playlist</h3>
            <button class="btn-icon" @click="$emit('close')" title="Close">
              <Icon name="close" :size="18" />
            </button>
          </header>

          <form class="cpm-form" @submit.prevent="submit">
            <label class="cpm-field">
              <span class="cpm-label">Name</span>
              <input
                ref="nameInput"
                v-model="name"
                type="text"
                class="cpm-input"
                placeholder="Late Night Coding"
                maxlength="80"
                required
              />
            </label>
            <label class="cpm-field">
              <span class="cpm-label">Description <span class="cpm-optional">(optional)</span></span>
              <textarea
                v-model="description"
                class="cpm-input cpm-textarea"
                placeholder="What's this playlist for?"
                maxlength="500"
                rows="3"
              />
            </label>
            <div v-if="error" class="cpm-error">{{ error }}</div>
            <div class="cpm-actions">
              <button type="button" class="btn" @click="$emit('close')">Cancel</button>
              <button type="submit" class="btn btn-primary" :disabled="busy || !name.trim()">
                {{ busy ? 'Creating…' : 'Create' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; created: [id: number] }>()

const { create } = usePlaylists()

const name = ref('')
const description = ref('')
const error = ref('')
const busy = ref(false)
const nameInput = ref<HTMLInputElement | null>(null)

// Reset state + focus the name field every time the modal opens.
watch(() => props.open, (open) => {
  if (open) {
    name.value = ''
    description.value = ''
    error.value = ''
    busy.value = false
    nextTick(() => nameInput.value?.focus())
  }
})

async function submit() {
  if (!name.value.trim()) return
  busy.value = true
  error.value = ''
  try {
    const pl = await create(name.value.trim(), description.value.trim())
    emit('created', pl.id)
    emit('close')
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to create playlist'
  } finally {
    busy.value = false
  }
}

// Esc closes.
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open) emit('close')
}
onMounted(() => { window.addEventListener('keydown', onKey) })
onBeforeUnmount(() => { window.removeEventListener('keydown', onKey) })
</script>

<style scoped>
.cpm-overlay {
  position: fixed; inset: 0; z-index: 220;
  background: rgba(0,0,0,0.6);
  backdrop-filter: blur(8px);
  display: flex; align-items: center; justify-content: center;
}
.cpm-modal {
  width: 440px;
  max-width: 92vw;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: 22px 24px 20px;
  box-shadow: var(--shadow-3);
}
.cpm-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.cpm-header h3 { font-size: 16px; font-weight: 700; }

.cpm-form { display: flex; flex-direction: column; gap: 14px; }
.cpm-field { display: flex; flex-direction: column; gap: 6px; }
.cpm-label {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-2);
}
.cpm-optional { color: var(--fg-3); letter-spacing: 0; text-transform: none; font-family: inherit; }
.cpm-input {
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
.cpm-input:focus { border-color: var(--gold); }
.cpm-textarea { resize: vertical; min-height: 64px; }
.cpm-error {
  font-size: 12px;
  color: #e34;
  margin-top: 4px;
}
.cpm-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 4px;
}
.cpm-actions :deep(.btn-primary) {
  padding: 0 20px;
  height: 36px;
  border-radius: 999px;
  font-weight: 600;
}

.cpm-enter-active, .cpm-leave-active { transition: opacity 0.15s ease; }
.cpm-enter-from, .cpm-leave-to { opacity: 0; }
</style>
