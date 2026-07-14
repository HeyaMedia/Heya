<template>
  <AppDialog
    :model-value="show"
    :title="`Edit Album — ${album?.title || ''}`"
    size="md"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <div class="mae-grid">
      <div class="mae-field mae-full">
        <label class="mae-label">Title</label>
        <input v-model="form.title" type="text" class="mae-input" />
      </div>
      <div class="mae-field">
        <label class="mae-label">Year</label>
        <input v-model="form.year" type="text" class="mae-input" maxlength="4" />
      </div>
      <div class="mae-field">
        <label class="mae-label">Type</label>
        <select v-model="form.album_type" class="mae-input">
          <option v-for="t in albumTypes" :key="t" :value="t">{{ t }}</option>
        </select>
      </div>
      <div class="mae-field">
        <label class="mae-label">Release Date</label>
        <input v-model="form.release_date" type="date" class="mae-input" />
      </div>
      <div class="mae-field">
        <label class="mae-label">Label</label>
        <input v-model="form.label" type="text" class="mae-input" />
      </div>
      <div class="mae-field">
        <label class="mae-label">Country</label>
        <input v-model="form.country" type="text" class="mae-input" maxlength="2" placeholder="US" />
      </div>
      <div class="mae-field">
        <label class="mae-label">Barcode</label>
        <input v-model="form.barcode" type="text" class="mae-input" />
      </div>
      <div class="mae-field mae-full">
        <label class="mae-label">Genres (comma-separated)</label>
        <input v-model="genresText" type="text" class="mae-input" placeholder="rock, indie" />
      </div>
    </div>
    <template #footer="{ close }">
      <button class="btn btn-ghost-sm mae-identify-btn" title="Choose a different Heya release" @click="$emit('identify')">
        <Icon name="search" :size="13" /> Re-identify
      </button>
      <button class="btn btn-ghost-sm" @click="close()">Cancel</button>
      <button class="btn btn-primary" :disabled="saving" @click="save">
        {{ saving ? 'Saving...' : 'Save' }}
      </button>
    </template>
  </AppDialog>
</template>

<script setup lang="ts">
const props = defineProps<{
  album: any | null
  show: boolean
}>()
const emit = defineEmits<{ saved: []; identify: []; close: [] }>()

const albumTypes = ['album', 'ep', 'single', 'compilation', 'soundtrack', 'live', 'remix', 'demo', 'audiobook', 'spokenword', 'other']

const form = ref<Record<string, any>>({})
const genresText = ref('')
const saving = ref(false)

const { $heya } = useNuxtApp()
const { toast } = useToast()

watch(() => props.show, (v) => {
  if (v && props.album) {
    form.value = {
      title: props.album.title || '',
      year: props.album.year || '',
      album_type: props.album.album_type || 'album',
      release_date: formatDate(props.album.release_date),
      label: props.album.label || '',
      country: props.album.country || '',
      barcode: props.album.barcode || '',
    }
    genresText.value = (props.album.genres || []).join(', ')
  }
})

function formatDate(d: any): string {
  if (!d) return ''
  if (typeof d === 'string') return d.substring(0, 10)
  if (d.Time) return new Date(d.Time).toISOString().substring(0, 10)
  return ''
}

async function save() {
  if (!props.album) return
  saving.value = true
  try {
    await $heya('/api/music/albums/{id}', {
      method: 'PUT',
      path: { id: props.album.id },
      body: {
        ...form.value,
        genres: genresText.value.split(',').map(g => g.trim()).filter(Boolean),
      } as any,
    })
    emit('saved')
    toast.ok('Album metadata saved')
  } catch (error) {
    toast.err(apiErrorMessage(error, 'Could not save album metadata'), { duration: 7000 })
  }
  saving.value = false
}
</script>

<style scoped>
.mae-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.mae-full {
  grid-column: 1 / -1;
}

.mae-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.mae-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}

.mae-input {
  height: 38px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-0);
  font-size: 13px;
  padding: 0 12px;
  outline: none;
  transition: border-color 0.15s;
}
.mae-input:focus {
  border-color: var(--gold);
}

.mae-identify-btn {
  margin-right: auto;
}
</style>
