<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-semibold">Libraries</h1>
      <button class="btn-primary" @click="showAdd = true">Add Library</button>
    </div>

    <div v-if="loading" class="space-y-3">
      <div v-for="i in 3" :key="i" class="card animate-pulse p-4">
        <div class="h-6 w-1/3 rounded bg-surface-overlay" />
      </div>
    </div>

    <div v-else-if="libraries.length" class="space-y-3">
      <div v-for="lib in libraries" :key="lib.id" class="card p-4">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-lg text-lg" :class="mediaTypeBg(lib.media_type)">
              {{ mediaTypeLabel(lib.media_type)[0] }}
            </div>
            <div>
              <h3 class="font-medium">{{ lib.name }}</h3>
              <p class="text-xs text-gray-500">
                {{ mediaTypeLabel(lib.media_type) }} &middot;
                {{ lib.paths.join(', ') }}
              </p>
            </div>
          </div>
          <div class="flex gap-2">
            <button class="btn-ghost text-xs" @click="scanLibrary(lib.id)" :disabled="scanning === lib.id">
              {{ scanning === lib.id ? 'Scanning...' : 'Scan' }}
            </button>
            <button class="btn-ghost text-xs text-red-400 hover:text-red-300" @click="deleteLibrary(lib.id)">
              Delete
            </button>
          </div>
        </div>
      </div>
    </div>

    <div v-else class="card p-12 text-center text-gray-500">
      <p class="text-lg">No libraries configured</p>
      <p class="mt-1 text-sm">Add a library to start scanning your media</p>
    </div>

    <!-- Add Library Modal -->
    <Teleport to="body">
      <div v-if="showAdd" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" @click.self="showAdd = false">
        <form class="card w-full max-w-md space-y-4 p-6" @submit.prevent="addLibrary">
          <h2 class="text-lg font-semibold">Add Library</h2>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-400">Name</label>
            <input v-model="newLib.name" class="input" placeholder="My Movies" required />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-400">Type</label>
            <select v-model="newLib.media_type" class="input">
              <option value="movie">Movie</option>
              <option value="tv">TV Show</option>
              <option value="music">Music</option>
              <option value="book">Book</option>
            </select>
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-400">Path</label>
            <input v-model="newLib.path" class="input" placeholder="/path/to/media" required />
          </div>
          <div v-if="addError" class="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {{ addError }}
          </div>
          <div class="flex justify-end gap-2">
            <button type="button" class="btn-ghost" @click="showAdd = false">Cancel</button>
            <button type="submit" class="btn-primary">Add</button>
          </div>
        </form>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import type { Library } from '~~/shared/types'

const { isAuthenticated } = useAuth()
watchEffect(() => {
  if (!isAuthenticated.value) navigateTo('/login')
})

const libraries = ref<Library[]>([])
const loading = ref(true)
const scanning = ref<number | null>(null)
const showAdd = ref(false)
const addError = ref('')
const newLib = ref({ name: '', media_type: 'movie', path: '' })

async function fetchLibraries() {
  try {
    libraries.value = await apiFetch<Library[]>('/api/libraries')
  } catch { /* empty */ }
  loading.value = false
}

async function scanLibrary(id: number) {
  scanning.value = id
  try {
    await apiFetch(`/api/libraries/${id}/scan`, { method: 'POST' })
  } catch { /* empty */ }
  scanning.value = null
}

async function deleteLibrary(id: number) {
  if (!confirm('Delete this library? Media items will be preserved.')) return
  try {
    await apiFetch(`/api/libraries/${id}`, { method: 'DELETE' })
    libraries.value = libraries.value.filter(l => l.id !== id)
  } catch { /* empty */ }
}

async function addLibrary() {
  addError.value = ''
  try {
    await apiFetch('/api/libraries', {
      method: 'POST',
      body: JSON.stringify({
        name: newLib.value.name,
        media_type: newLib.value.media_type,
        paths: [newLib.value.path],
      }),
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
