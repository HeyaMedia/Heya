<template>
  <AppDialog v-model="open" title="Add to List" size="sm">
    <div v-if="!showCreateList">
      <button
        v-for="l in userLists" :key="l.id"
        class="list-option" :class="{ active: l.contains }"
        @click="toggleListItem(l)"
      >
        <Icon :name="l.contains ? 'check' : 'plus'" :size="14" />
        <span>{{ l.name }}</span>
        <span class="list-option-count">{{ l.item_count }}</span>
      </button>
      <div v-if="!userLists.length" style="padding: 16px 0; color: var(--fg-3); font-size: 13px; text-align: center">No lists yet</div>
      <button class="list-create-btn" @click="showCreateList = true">
        <Icon name="plus" :size="14" /> Create new list
      </button>
    </div>
    <div v-else>
      <input v-model="newListName" class="modal-input" placeholder="List name" @keydown.enter="createList" />
      <input v-model="newListDesc" class="modal-input" placeholder="Description (optional)" style="margin-top: 8px" />
      <div style="display: flex; gap: 8px; margin-top: 12px">
        <button class="btn btn-primary" @click="createList" :disabled="!newListName.trim()">Create</button>
        <button class="btn btn-secondary" @click="showCreateList = false">Cancel</button>
      </div>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
const props = defineProps<{ mediaItemId: number }>()
const open = defineModel<boolean>('open', { default: false })

const { userLists, loadLists, createList: submitList, toggleListItem } = useMediaLists(toRef(props, 'mediaItemId'))

const showCreateList = ref(false)
const newListName = ref('')
const newListDesc = ref('')

async function createList() {
  if (!newListName.value.trim()) return
  await submitList(newListName.value, newListDesc.value)
  newListName.value = ''
  newListDesc.value = ''
  showCreateList.value = false
}

watch(open, (v) => { if (v) loadLists() })
</script>

<style scoped>
/* AppDialog supplies the dialog chrome — only the row + input styles
   below are consumed by the list-add panel. Slot content keeps this
   component's scope id, so scoped rules do reach the portaled dialog. */
.modal-input {
  width: 100%; padding: 10px 14px; background: var(--bg-3); border: 1px solid var(--border);
  border-radius: var(--r-md); color: var(--fg-0); font-size: 14px; outline: none;
}
.modal-input:focus { border-color: var(--gold); }

.list-option {
  display: flex; align-items: center; gap: 10px; width: 100%;
  padding: 10px 12px; border-radius: var(--r-sm); font-size: 13px;
  color: var(--fg-1); transition: background 0.12s; text-align: left;
}
.list-option:hover { background: rgba(255,255,255,0.04); }
.list-option.active { color: var(--gold); }
.list-option-count { margin-left: auto; font-size: 10px; font-family: var(--font-mono); color: var(--fg-4); }

.list-create-btn {
  display: flex; align-items: center; gap: 8px; width: 100%;
  padding: 10px 12px; margin-top: 4px; border-top: 1px solid var(--border);
  font-size: 13px; color: var(--fg-2); transition: color 0.12s;
}
.list-create-btn:hover { color: var(--gold); }
</style>
