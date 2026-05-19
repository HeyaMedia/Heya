<template>
  <div class="scroll page-pad" style="height: 100%">
    <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 24px">
      <h1 style="font-size: 30px; font-weight: 600">Libraries</h1>
      <button class="btn btn-primary" @click="showAdd = true">
        <Icon name="plus" :size="16" />
        Add Library
      </button>
    </div>

    <div v-if="loading" style="color: var(--fg-2)">Loading…</div>

    <div v-else-if="libraries.length" style="display: flex; flex-direction: column; gap: 16px">
      <div v-for="lib in libraries" :key="lib.id" class="lib-card">
        <div style="display: flex; align-items: center; gap: 14px">
          <div class="lib-icon">
            <Icon :name="lib.media_type === 'movie' ? 'film' : lib.media_type === 'tv' ? 'tv' : lib.media_type === 'music' ? 'music' : 'book'" :size="20" />
          </div>
          <div style="flex: 1">
            <div style="font-size: 16px; font-weight: 500">{{ lib.name }}</div>
            <div style="font-size: 12px; color: var(--fg-2); font-family: var(--font-mono); margin-top: 2px">
              {{ lib.media_type.toUpperCase() }} &middot; {{ lib.paths.join(', ') }}
            </div>
          </div>
          <div style="display: flex; gap: 8px">
            <button class="btn-ghost-sm" @click="scanLibrary(lib.id)" :disabled="scanning === lib.id">
              {{ scanning === lib.id ? 'Scanning…' : 'Scan' }}
            </button>
            <button class="btn-ghost-sm" style="color: var(--bad)" @click="deleteLibrary(lib.id)">Delete</button>
          </div>
        </div>
      </div>
    </div>

    <div v-else style="text-align: center; padding: 80px 0; color: var(--fg-2)">
      <p style="font-size: 16px">No libraries configured</p>
      <p style="font-size: 13px; margin-top: 4px">Add a library to start scanning your media</p>
    </div>

    <!-- Add Modal -->
    <Teleport to="body">
      <div v-if="showAdd" class="modal-overlay" @click.self="showAdd = false">
        <form class="modal-card" @submit.prevent="addLibrary">
          <h2 style="font-size: 18px; font-weight: 600; margin-bottom: 20px">Add Library</h2>
          <div class="field">
            <label>Name</label>
            <input v-model="newLib.name" placeholder="My Movies" required />
          </div>
          <div class="field">
            <label>Type</label>
            <select v-model="newLib.media_type" style="width: 100%; height: 40px; background: var(--bg-3); border: 1px solid var(--border); border-radius: var(--r-md); padding: 0 14px; color: var(--fg-0); font-size: 14px">
              <option value="movie">Movie</option>
              <option value="tv">TV Show</option>
              <option value="music">Music</option>
              <option value="book">Book</option>
            </select>
          </div>
          <div class="field">
            <label>Path</label>
            <input v-model="newLib.path" placeholder="/path/to/media" required />
          </div>
          <div v-if="addError" class="error-msg">{{ addError }}</div>
          <div style="display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px">
            <button type="button" class="btn btn-secondary" @click="showAdd = false">Cancel</button>
            <button type="submit" class="btn btn-primary">Add</button>
          </div>
        </form>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import type { Library } from '~~/shared/types'


const libraries = ref<Library[]>([])
const loading = ref(true)
const scanning = ref<number | null>(null)
const showAdd = ref(false)
const addError = ref('')
const newLib = ref({ name: '', media_type: 'movie', path: '' })

async function fetchLibraries() {
  try { libraries.value = await apiFetch<Library[]>('/api/libraries') } catch {}
  loading.value = false
}

async function scanLibrary(id: number) {
  scanning.value = id
  try { await apiFetch(`/api/libraries/${id}/scan`, { method: 'POST' }) } catch {}
  scanning.value = null
}

async function deleteLibrary(id: number) {
  if (!confirm('Delete this library?')) return
  try {
    await apiFetch(`/api/libraries/${id}`, { method: 'DELETE' })
    libraries.value = libraries.value.filter(l => l.id !== id)
  } catch {}
}

async function addLibrary() {
  addError.value = ''
  try {
    await apiFetch('/api/libraries', {
      method: 'POST',
      body: JSON.stringify({ name: newLib.value.name, media_type: newLib.value.media_type, paths: [newLib.value.path] }),
    })
    showAdd.value = false
    newLib.value = { name: '', media_type: 'movie', path: '' }
    await fetchLibraries()
  } catch (e: any) {
    addError.value = e?.data?.error || 'Failed to create library'
  }
}

onMounted(fetchLibraries)
</script>

<style scoped>
.lib-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 20px;
}
.lib-icon {
  width: 40px; height: 40px;
  border-radius: var(--r-md);
  display: flex; align-items: center; justify-content: center;
  background: var(--gold-soft);
  color: var(--gold);
}
.modal-overlay {
  position: fixed; inset: 0; z-index: 100;
  background: rgba(0,0,0,0.6);
  backdrop-filter: blur(8px);
  display: flex; align-items: center; justify-content: center;
  padding: 20px;
}
.modal-card {
  width: 100%; max-width: 440px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: 32px;
}
.field { margin-bottom: 16px; }
.field label {
  display: block; font-size: 11px; font-weight: 600; color: var(--fg-2);
  font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em; margin-bottom: 6px;
}
.field input {
  width: 100%; height: 40px; background: var(--bg-3); border: 1px solid var(--border);
  border-radius: var(--r-md); padding: 0 14px; color: var(--fg-0); font-size: 14px; outline: none;
}
.field input:focus { border-color: var(--gold); }
.field input::placeholder { color: var(--fg-3); }
.error-msg {
  background: rgba(217,107,107,0.1); border: 1px solid rgba(217,107,107,0.3);
  border-radius: var(--r-md); padding: 10px 14px; font-size: 13px; color: var(--bad); margin-bottom: 12px;
}
</style>
