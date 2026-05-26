<script setup lang="ts">
definePageMeta({ layout: 'settings' })

import type { components } from '#open-fetch-schemas/heya'
type UserList = components['schemas']['UserListView']

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const lists = ref<UserList[]>([])
const loading = ref(true)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)
const editing = ref<number | null>(null)
const draft = ref({ name: '', description: '' })

async function load() {
  loading.value = true
  try {
    lists.value = await $heya('/api/me/lists')
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load lists.' }
  } finally {
    loading.value = false
  }
}

function startEdit(l: UserList) {
  editing.value = l.id
  draft.value = { name: l.name, description: l.description ?? '' }
}

function cancelEdit() {
  editing.value = null
  draft.value = { name: '', description: '' }
}

async function saveEdit(l: UserList) {
  if (!draft.value.name.trim()) return
  try {
    const updated = await $heya('/api/me/lists/{id}', {
      method: 'PUT',
      path: { id: l.id },
      body: {
        name: draft.value.name.trim(),
        description: draft.value.description.trim(),
        filter_json: l.filter_json ?? null,
        icon: l.icon ?? '',
      },
    })
    const idx = lists.value.findIndex(x => x.id === l.id)
    if (idx >= 0) lists.value[idx] = updated
    flash.value = { kind: 'ok', text: 'List updated.' }
    cancelEdit()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to save list.' }
  }
}

async function remove(l: UserList) {
  const ok = await confirm({
    title: `Delete "${l.name}"?`,
    message: `This list and its ${l.item_count} ${l.item_count === 1 ? 'item' : 'items'} will be removed. The underlying media isn't deleted.`,
    destructive: true,
    confirmLabel: 'Delete',
  })
  if (!ok) return
  try {
    await $heya('/api/me/lists/{id}', { method: 'DELETE', path: { id: l.id } })
    lists.value = lists.value.filter(x => x.id !== l.id)
    flash.value = { kind: 'ok', text: 'List deleted.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to delete list.' }
  }
}

function timeAgo(ts: { Time?: string } | string | undefined): string {
  // Lists carry pgtype.Timestamptz which JSON-encodes as {Time, Valid}. Be
  // permissive in case the shape ever flattens to a string.
  const raw = typeof ts === 'string' ? ts : ts?.Time
  if (!raw) return '—'
  const t = new Date(raw).getTime()
  if (Number.isNaN(t)) return '—'
  const s = Math.floor((Date.now() - t) / 1000)
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d}d ago`
  return new Date(raw).toLocaleDateString()
}

function mediaTypeIcon(t: string): string {
  switch (t) {
    case 'movie': return 'film'
    case 'tv': return 'tv'
    case 'music': return 'music'
    case 'book': return 'book'
    default: return 'list'
  }
}

onMounted(load)
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">My lists</h2>
      <p class="sv2-page-desc">
        Lists you've created across every media type. Manage names and
        descriptions; add items from any media-detail page.
      </p>
    </header>

    <div v-if="loading" class="loading-state">
      <Icon name="spinner" :size="16" /> Loading…
    </div>

    <template v-else>
      <SettingsSection
        :title="`Your lists${lists.length ? ' (' + lists.length + ')' : ''}`"
        icon="bookmark"
      >
        <div v-if="lists.length === 0" class="empty-state">
          <Icon name="bookmark" :size="14" />
          You haven't created any lists yet. Browse any media page and hit "Add to list".
        </div>

        <div v-else class="list-grid">
          <div v-for="l in lists" :key="l.id" class="list-card">
            <template v-if="editing === l.id">
              <div class="list-edit">
                <SettingsField label="Name">
                  <input v-model="draft.name" class="sv2-input" maxlength="128" />
                </SettingsField>
                <SettingsField label="Description">
                  <textarea v-model="draft.description" class="sv2-textarea" rows="2" maxlength="2000" />
                </SettingsField>
                <div class="list-edit-actions">
                  <button class="sv2-btn ghost" @click="cancelEdit">Cancel</button>
                  <button class="sv2-btn primary" :disabled="!draft.name.trim()" @click="saveEdit(l)">Save</button>
                </div>
              </div>
            </template>
            <template v-else>
              <div class="list-icon"><Icon :name="mediaTypeIcon(l.media_type)" :size="16" /></div>
              <div class="list-body">
                <div class="list-name">
                  {{ l.name }}
                  <StatusBadge :state="l.list_type === 'smart' ? 'warn' : 'idle'">
                    {{ l.list_type }}
                  </StatusBadge>
                </div>
                <div v-if="l.description" class="list-desc">{{ l.description }}</div>
                <div class="list-meta">
                  <span>{{ l.item_count }} {{ l.item_count === 1 ? 'item' : 'items' }}</span>
                  <span>· {{ l.media_type }}</span>
                  <span>· updated {{ timeAgo(l.updated_at as any) }}</span>
                </div>
              </div>
              <div class="list-actions">
                <button class="list-btn" title="Rename" @click="startEdit(l)">
                  <Icon name="pencil" :size="13" />
                </button>
                <button class="list-btn danger" title="Delete" @click="remove(l)">
                  <Icon name="trash" :size="13" />
                </button>
              </div>
            </template>
          </div>
        </div>
      </SettingsSection>

      <div v-if="flash" class="sv2-flash" :class="flash.kind">
        <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
        {{ flash.text }}
      </div>
    </template>
  </div>
</template>

<style scoped>
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }

.loading-state, .empty-state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-3);
  font-size: 13px;
  padding: 20px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.list-grid { display: flex; flex-direction: column; gap: 8px; }
.list-card {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.list-icon {
  width: 36px;
  height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  flex-shrink: 0;
}
.list-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.list-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
}
.list-desc { font-size: 12px; color: var(--fg-2); line-height: 1.5; }
.list-meta { font-size: 11.5px; color: var(--fg-3); display: flex; flex-wrap: wrap; gap: 6px; }

.list-actions { display: flex; gap: 4px; flex-shrink: 0; }
.list-btn {
  width: 28px; height: 28px;
  border-radius: var(--r-sm);
  color: var(--fg-3);
  display: flex; align-items: center; justify-content: center;
  transition: background 0.12s, color 0.12s;
}
.list-btn:hover { background: rgba(255,255,255,0.04); color: var(--fg-1); }
.list-btn.danger:hover { background: rgba(217, 107, 107, 0.12); color: var(--bad); }

.list-edit { width: 100%; display: flex; flex-direction: column; gap: 8px; }
.list-edit-actions { display: flex; justify-content: flex-end; gap: 8px; padding-top: 4px; }

.sv2-input, .sv2-textarea {
  width: 100%;
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-sans);
  transition: border-color 0.12s;
}
.sv2-textarea { resize: vertical; min-height: 60px; }
.sv2-input:focus, .sv2-textarea:focus { outline: none; border-color: var(--gold); background: var(--bg-1); }

.sv2-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 7px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.sv2-btn.primary { background: var(--gold); color: #1a1408; }
.sv2-btn.primary:hover:not(:disabled) { background: var(--gold-deep); }
.sv2-btn.primary:disabled { opacity: 0.5; cursor: not-allowed; }
.sv2-btn.ghost { border: 1px solid var(--border); color: var(--fg-2); background: var(--bg-2); }
.sv2-btn.ghost:hover { color: var(--fg-0); }

.sv2-flash {
  margin-top: 16px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex; align-items: center; gap: 8px;
}
.sv2-flash.ok { background: rgba(111, 191, 124, 0.10); border: 1px solid rgba(111, 191, 124, 0.25); color: var(--good); }
.sv2-flash.err { background: rgba(217, 107, 107, 0.10); border: 1px solid rgba(217, 107, 107, 0.30); color: var(--bad); }
</style>
