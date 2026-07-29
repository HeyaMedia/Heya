<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { managerQualityProfilesQuery, type ManagerQualityItem, type ManagerQualityProfileView } from '~/queries/manager'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const profilesData = useQuery(managerQualityProfilesQuery())
const profiles = computed(() => profilesData.data.value ?? [])
const loading = computed(() => profilesData.isLoading.value)

useLiveRefresh([{
  events: ['manager.changed'],
  keys: [['manager', 'quality-profiles']],
  filter: event => (event.payload as { area?: string } | undefined)?.area === 'quality_profiles',
}])

const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

// ── Edit dialog ──────────────────────────────────────────────────────────
// Phase 1 editor: rename, toggle allowed qualities, pick the cutoff, flip
// upgrades. Ladder reordering and adding qualities arrive with the decision
// engine work (the quality vocabulary lives server-side).

const dialogOpen = ref(false)
const editingID = ref<number | null>(null)
const form = reactive({
  name: '',
  domain: 'video',
  items: [] as ManagerQualityItem[],
  cutoff: '',
  upgrades_enabled: true,
})
const savingForm = ref(false)
const formError = ref('')

const cutoffOptions = computed(() =>
  form.items.filter(item => item.allowed).map(item => ({ value: item.quality, label: item.quality })))

function openEdit(profile: ManagerQualityProfileView) {
  editingID.value = profile.id
  form.name = profile.name
  form.domain = profile.domain
  form.items = (profile.items ?? []).map(item => ({ ...item }))
  form.cutoff = profile.cutoff
  form.upgrades_enabled = profile.upgrades_enabled
  formError.value = ''
  dialogOpen.value = true
}

async function saveForm() {
  if (editingID.value == null) return
  savingForm.value = true
  formError.value = ''
  try {
    await $heya(`/api/manager/quality-profiles/${editingID.value}`, {
      method: 'PUT',
      body: {
        name: form.name,
        domain: form.domain,
        items: form.items,
        cutoff: form.cutoff,
        upgrades_enabled: form.upgrades_enabled,
      },
    })
    dialogOpen.value = false
    await profilesData.refetch()
  } catch (e: any) {
    formError.value = e?.data?.detail ?? e?.message ?? 'Save failed.'
  } finally {
    savingForm.value = false
  }
}

async function removeProfile(profile: ManagerQualityProfileView) {
  const ok = await confirm({
    title: 'Delete quality profile',
    message: `Delete ${profile.name}?`,
    destructive: true,
  })
  if (!ok) return
  try {
    await $heya(`/api/manager/quality-profiles/${profile.id}`, { method: 'DELETE' })
    await profilesData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Delete failed.' }
  }
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Quality profiles"
      icon="eq"
      eyebrow="Manager · System"
      description="What the decision engine is allowed to grab and when it stops upgrading. One profile set shared by every library — assign per library or per item."
    />

    <div v-if="flash" class="mgr-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" /> {{ flash.text }}
    </div>

    <SettingsSection
      title="Profiles"
      icon="eq"
      description="Qualities are ranked top-to-bottom; the cutoff is where searching stops. Custom-format scoring lands in the decision-engine phase."
    >
      <div v-if="loading && !profiles.length" class="mgr-loading">
        <Icon name="spinner" :size="16" /> Loading…
      </div>

      <div v-else class="qp-grid">
        <div v-for="profile in profiles" :key="profile.id" class="qp-card">
          <div class="qp-head">
            <div class="qp-name">{{ profile.name }}</div>
            <div class="qp-head-actions">
              <span class="qp-domain">{{ profile.domain }}</span>
              <AppTooltip label="Edit">
                <button type="button" class="mgr-btn-icon" @click="openEdit(profile)"><Icon name="pencil" :size="14" /></button>
              </AppTooltip>
              <AppTooltip :label="profile.in_use_count > 0 ? `In use by ${profile.in_use_count} items` : 'Delete'">
                <button type="button" class="mgr-btn-icon danger" :disabled="profile.in_use_count > 0" @click="removeProfile(profile)">
                  <Icon name="trash" :size="14" />
                </button>
              </AppTooltip>
            </div>
          </div>
          <div class="qp-meta">
            <span>cutoff <strong>{{ profile.cutoff }}</strong></span>
            <span>{{ profile.upgrades_enabled ? 'upgrades until cutoff met' : 'upgrades disabled' }}</span>
          </div>
          <ul class="qp-ladder">
            <li
              v-for="item in profile.items ?? []"
              :key="item.quality"
              class="qp-quality"
              :class="{ off: !item.allowed, cutoff: item.quality === profile.cutoff }"
            >
              <Icon :name="item.allowed ? 'check' : 'close'" :size="11" />
              <span>{{ item.quality }}</span>
              <span v-if="item.quality === profile.cutoff" class="qp-cutoff-tag">cutoff</span>
            </li>
          </ul>
          <div class="qp-foot">
            <span>{{ profile.in_use_count }} items assigned</span>
          </div>
        </div>
      </div>
    </SettingsSection>

    <AppDialog v-model="dialogOpen" title="Edit quality profile" size="sm">
      <div class="mgr-form">
        <label class="mgr-field">
          <span>Name</span>
          <input v-model="form.name" class="mgr-input">
        </label>
        <div class="mgr-field">
          <span>Qualities</span>
          <ul class="qp-edit-ladder">
            <li v-for="item in form.items" :key="item.quality" class="qp-edit-row">
              <AppSwitch v-model="item.allowed" size="sm" :aria-label="`Allow ${item.quality}`" />
              <span class="qp-edit-quality" :class="{ off: !item.allowed }">{{ item.quality }}</span>
            </li>
          </ul>
        </div>
        <label class="mgr-field">
          <span>Cutoff — stop searching once met</span>
          <AppSelect v-model="form.cutoff" :options="cutoffOptions" />
        </label>
        <div class="qp-edit-upgrades">
          <AppSwitch v-model="form.upgrades_enabled" size="sm" aria-label="Upgrades enabled" />
          <span>Upgrade existing files until the cutoff is met</span>
        </div>
        <p v-if="formError" class="mgr-form-error"><Icon name="warning" :size="12" /> {{ formError }}</p>
      </div>
      <template #footer>
        <button type="button" class="mgr-btn" @click="dialogOpen = false">Cancel</button>
        <button type="button" class="mgr-btn-gold" :disabled="savingForm" @click="saveForm">
          <Icon v-if="savingForm" name="spinner" :size="13" /> Save
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.qp-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 10px;
}
.qp-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.qp-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.qp-name { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.qp-head-actions { display: flex; align-items: center; gap: 6px; }
.qp-domain {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-3);
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: rgb(var(--ink) / 0.04);
}

.qp-meta {
  display: flex; flex-direction: column; gap: 2px;
  font-size: 11.5px; color: var(--fg-2);
}
.qp-meta strong { color: var(--gold); font-weight: 600; }

.qp-ladder {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.qp-quality {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.04);
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-1);
}
.qp-quality.off { color: var(--fg-3); background: transparent; border: 1px dashed var(--hair); }
.qp-quality.cutoff { background: var(--gold-soft); color: var(--gold-bright); }
.qp-cutoff-tag {
  margin-left: auto;
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--gold);
}

.qp-foot {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--hair);
  font-size: 11px;
  color: var(--fg-3);
}

.mgr-btn,
.mgr-btn-gold {
  display: inline-flex; align-items: center; gap: 7px;
  height: 32px; padding: 0 14px;
  border-radius: var(--r-sm);
  font-size: 12.5px; font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mgr-btn {
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-1);
}
.mgr-btn:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.mgr-btn-gold {
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  color: var(--gold-bright);
}
.mgr-btn-gold:hover { background: color-mix(in srgb, var(--gold) 18%, transparent); }
.mgr-btn-gold:disabled { opacity: 0.6; pointer-events: none; }

.mgr-btn-icon {
  width: 28px; height: 28px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mgr-btn-icon:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.mgr-btn-icon.danger:hover { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); }
.mgr-btn-icon:disabled { opacity: 0.4; pointer-events: none; }

.mgr-flash {
  display: flex; align-items: center; gap: 8px;
  margin-bottom: 14px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12.5px;
}
.mgr-flash.ok { background: color-mix(in srgb, var(--good) 10%, transparent); border: 1px solid color-mix(in srgb, var(--good) 30%, transparent); color: var(--good); }
.mgr-flash.err { background: color-mix(in srgb, var(--bad) 10%, transparent); border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent); color: var(--bad); }

.mgr-loading {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}
</style>

<!-- Portaled dialog additions (base .mgr-form styles live in the layout). -->
<style>
.qp-edit-ladder {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.qp-edit-row { display: flex; align-items: center; gap: 10px; }
.qp-edit-quality { font-family: var(--font-mono); font-size: 12px; color: var(--fg-0); }
.qp-edit-quality.off { color: var(--fg-3); text-decoration: line-through; }
.qp-edit-upgrades {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12.5px;
  color: var(--fg-1);
}
</style>
