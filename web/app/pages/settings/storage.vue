<script setup lang="ts">
import { timeAgo as timeAgoBase } from '~/composables/useFormat'
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminStorageQuery } from '~/queries/settings'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const storageData = useQuery(adminStorageQuery())
const storage = computed(() => storageData.data.value ?? null)
const loading = computed(() => storageData.isLoading.value)
const clearing = ref(false)
const scanning = ref(false)
const { flash } = useFlash()
const tick = ref(0)
let tickTimer: ReturnType<typeof setInterval> | null = null

async function load() {
  try {
    await storageData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load storage info.' }
  }
}

async function scanAll() {
  scanning.value = true
  try {
    await $heya('/api/admin/storage/scan', { method: 'POST', body: { library_id: 0 } as any })
    flash.value = { kind: 'ok', text: 'Disk-usage scan queued — refresh in a minute or two for results.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to queue scan.' }
  } finally {
    scanning.value = false
  }
}

async function clearCache() {
  if (!storage.value?.transcode_items) return
  const ok = await confirm({
    title: 'Clear transcode cache?',
    message: `Drops every cached HLS segment (${fmtMB(storage.value.transcode_used_mb)}, ${storage.value.transcode_items} items). Active sessions will need to re-transcode.`,
    destructive: true,
    confirmLabel: 'Clear cache',
  })
  if (!ok) return
  clearing.value = true
  try {
    await $heya('/api/transcode/cache', { method: 'DELETE' })
    flash.value = { kind: 'ok', text: 'Cache cleared.' }
    await load()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Clear failed.' }
  } finally {
    clearing.value = false
  }
}

function fmtMB(mb?: number) {
  if (!mb) return '0 MB'
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

function volumeTone(used: number | undefined): 'good' | 'warn' | 'bad' {
  if (used == null) return 'good'
  if (used >= 90) return 'bad'
  if (used >= 75) return 'warn'
  return 'good'
}

// Pull a usage row for a path; returns undefined when there's no scan yet.
function usageFor(path: string) {
  return storage.value?.library_disk_usage?.find(u => u.path === path)
}

function timeAgo(iso: string): string {
  // Read tick to keep "scanned 2m ago" current without remounting nodes.
  void tick.value
  return timeAgoBase(iso)
}

const totalScanned = computed(() => storage.value?.library_disk_usage?.length ?? 0)
const totalScannedBytes = computed(() =>
  (storage.value?.library_disk_usage ?? []).reduce((s, u) => s + (u.bytes || 0), 0),
)

onMounted(() => {
  tickTimer = setInterval(() => { tick.value++ }, 1000)
})
onBeforeUnmount(() => { if (tickTimer) clearInterval(tickTimer) })
</script>

<template>
  <div>
    <SettingsContextHero
      title="Storage"
      icon="hard-drives"
      eyebrow="Server · Data footprint"
      description="See where Heya stores its database, cache, models, and media roots without walking enormous libraries just to calculate usage."
    />

    <div v-if="loading && !storage" class="loading-state">
      <Icon name="spinner" :size="16" /> Probing filesystems…
    </div>

    <template v-else-if="storage">
      <SettingsSection title="Data directory" icon="folder"
        :description="`Heya's working directory (config, images, models, etc.). Configured via HEYA_DATA_DIR.`">
        <KVTable :rows="[
          { key: 'Path', value: storage.data_dir, mono: true, copy: true },
          { key: 'Exists', value: storage.data_dir_volume.exists ? 'yes' : 'no' },
          { key: 'Volume total', value: fmtBytes(storage.data_dir_volume.total_bytes) },
          { key: 'Volume used',  value: `${fmtBytes(storage.data_dir_volume.used_bytes)} (${storage.data_dir_volume.used_pct ?? 0}%)` },
          { key: 'Volume free',  value: fmtBytes(storage.data_dir_volume.free_bytes) },
          { key: 'Error',        value: storage.data_dir_volume.error ?? '' },
        ]" />
        <div class="cap-bar">
          <div class="cap-fill" :class="`tone-${volumeTone(storage.data_dir_volume.used_pct)}`"
            :style="{ width: (storage.data_dir_volume.used_pct ?? 0) + '%' }" />
        </div>
      </SettingsSection>

      <SettingsSection title="Transcode cache" icon="film"
        :description="`HLS segments produced by ffmpeg, evicted oldest-first when the cap is reached.`">
        <template #actions>
          <NuxtLink to="/settings/transcoding" class="link-arrow">
            Settings <Icon name="chevright" :size="11" />
          </NuxtLink>
          <button class="sv2-btn danger" :disabled="clearing || !storage.transcode_items" @click="clearCache">
            <Icon name="trash" :size="12" />
            {{ clearing ? 'Clearing…' : 'Clear cache' }}
          </button>
        </template>

        <div class="tiles inner">
          <MetricTile label="Used" :value="fmtMB(storage.transcode_used_mb)"
            icon="hard-drives" :sub="`cap ${storage.transcode_max_gb} GB`" />
          <MetricTile label="Segments" :value="storage.transcode_items" icon="film" />
          <MetricTile label="Volume free" :value="fmtBytes(storage.transcode_volume.free_bytes)"
            icon="cpu"
            :tone="volumeTone(storage.transcode_volume.used_pct)" />
        </div>

        <KVTable :rows="[
          { key: 'Path', value: storage.transcode_dir, mono: true, copy: true },
          { key: 'Volume total', value: fmtBytes(storage.transcode_volume.total_bytes) },
          { key: 'Volume used',  value: `${fmtBytes(storage.transcode_volume.used_bytes)} (${storage.transcode_volume.used_pct ?? 0}%)` },
          { key: 'Volume free',  value: fmtBytes(storage.transcode_volume.free_bytes) },
        ]" />
        <div class="cap-bar">
          <div class="cap-fill" :class="`tone-${volumeTone(storage.transcode_volume.used_pct)}`"
            :style="{ width: (storage.transcode_volume.used_pct ?? 0) + '%' }" />
        </div>
      </SettingsSection>

      <SettingsSection title="Library paths" icon="folder"
        :description="`Each path that any library reads from. Volume totals are cheap statfs reads; click 'Scan disk usage' to walk the tree and persist actual library bytes (background job — minutes on multi-TB sets).`">
        <template #actions>
          <NuxtLink to="/settings/libraries" class="link-arrow">
            Edit libraries <Icon name="chevright" :size="11" />
          </NuxtLink>
          <button class="sv2-btn ghost" :disabled="scanning" @click="scanAll">
            <Icon :name="scanning ? 'spinner' : 'refresh'" :size="12" />
            {{ scanning ? 'Queueing…' : 'Scan disk usage' }}
          </button>
          <button class="sv2-btn ghost" :disabled="loading" @click="load" title="Refresh cached results">
            <Icon name="refresh" :size="12" />
            Reload
          </button>
        </template>

        <div v-if="totalScanned > 0" class="scan-summary">
          <Icon name="check" :size="13" />
          <span>
            <strong>{{ fmtBytes(totalScannedBytes) }}</strong> across
            <strong>{{ totalScanned }}</strong> scanned {{ totalScanned === 1 ? 'path' : 'paths' }}.
          </span>
        </div>

        <div v-if="(storage.library_paths?.length ?? 0) === 0" class="empty-state">
          <Icon name="info" :size="14" /> No libraries configured yet.
        </div>
        <div v-else class="lib-list">
          <div v-for="(p, i) in (storage.library_paths ?? [])" :key="i" class="lib-card" :class="{ missing: !p.exists }">
            <div class="lib-icon" :class="{ missing: !p.exists }">
              <Icon :name="p.exists ? 'folder' : 'warning'" :size="16" />
            </div>
            <div class="lib-body">
              <div class="lib-row">
                <span class="lib-name">{{ p.label }}</span>
                <StatusBadge v-if="!p.exists" state="error">missing</StatusBadge>
                <StatusBadge v-else-if="(p.used_pct ?? 0) >= 90" state="error">{{ p.used_pct }}% full</StatusBadge>
                <StatusBadge v-else-if="(p.used_pct ?? 0) >= 75" state="warn">{{ p.used_pct }}% full</StatusBadge>
                <StatusBadge v-else state="ok">{{ p.used_pct ?? 0 }}% full</StatusBadge>
              </div>
              <div class="lib-path mono">{{ p.path }}</div>
              <div v-if="p.exists" class="lib-meta mono">
                Volume: {{ fmtBytes(p.free_bytes) }} free of {{ fmtBytes(p.total_bytes) }}
              </div>
              <div v-else-if="p.error" class="lib-err mono">{{ p.error }}</div>
              <div v-if="p.exists" class="lib-bar">
                <div class="lib-bar-fill" :class="`tone-${volumeTone(p.used_pct)}`"
                  :style="{ width: (p.used_pct ?? 0) + '%' }" />
              </div>
              <div v-if="usageFor(p.path)" class="lib-usage">
                <Icon name="hard-drives" :size="11" class="lib-usage-icon" />
                <span class="lib-usage-bytes">{{ fmtBytes(usageFor(p.path)?.bytes) }}</span>
                <span class="lib-usage-files">· {{ usageFor(p.path)?.file_count.toLocaleString() }} files</span>
                <span class="lib-usage-when">· scanned {{ timeAgo(usageFor(p.path)!.scanned_at) }}</span>
              </div>
              <div v-else class="lib-usage dim">
                <Icon name="info" :size="11" />
                <span>Not scanned yet — click "Scan disk usage" to populate.</span>
              </div>
            </div>
          </div>
        </div>
      </SettingsSection>
    </template>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.sv2-page-desc code { font-family: var(--font-mono); font-size: 12px; color: var(--fg-1); }

.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 8px;
}
.tiles.inner { margin-bottom: 12px; }

.cap-bar {
  margin-top: 12px;
  height: 6px;
  border-radius: 3px;
  background: var(--bg-0);
  overflow: hidden;
}
.cap-fill { height: 100%; transition: width 0.4s ease; }
.cap-fill.tone-good { background: var(--good); }
.cap-fill.tone-warn { background: var(--gold-deep); }
.cap-fill.tone-bad  { background: var(--bad); }

.lib-list { display: flex; flex-direction: column; gap: 8px; }
.lib-card {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.lib-card.missing { border-color: color-mix(in srgb, var(--bad) 30%, transparent); background: color-mix(in srgb, var(--bad) 4%, transparent); }

.lib-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--good);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.lib-icon.missing { color: var(--bad); }

.lib-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.lib-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.lib-name { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.lib-path { font-size: 11.5px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.lib-meta { font-size: 11px; color: var(--fg-3); }
.lib-err  { font-size: 11px; color: var(--bad); }
.lib-bar {
  margin-top: 4px;
  height: 3px;
  border-radius: 2px;
  background: var(--bg-0);
  overflow: hidden;
}
.lib-bar-fill { height: 100%; transition: width 0.4s ease; }
.lib-bar-fill.tone-good { background: var(--good); }
.lib-bar-fill.tone-warn { background: var(--gold-deep); }
.lib-bar-fill.tone-bad  { background: var(--bad); }

.lib-usage {
  display: flex; align-items: center; gap: 5px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
  margin-top: 4px;
}
.lib-usage.dim { color: var(--fg-4); font-style: italic; }
.lib-usage-icon { color: var(--gold); }
.lib-usage-bytes { font-weight: 600; color: var(--gold); }
.lib-usage-files, .lib-usage-when { color: var(--fg-3); }

.scan-summary {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px;
  background: color-mix(in srgb, var(--good) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--good) 25%, transparent);
  border-radius: var(--r-sm);
  font-size: 12px;
  color: var(--good);
  margin-bottom: 12px;
}
.scan-summary strong { color: var(--good); font-weight: 700; }

.mono { font-family: var(--font-mono); }

.link-arrow {
  display: inline-flex; align-items: center; gap: 2px;
  font-size: 11px; color: var(--fg-3); text-decoration: none;
}
.link-arrow:hover { color: var(--gold); }

@media (max-width: 520px) {
  .tiles { grid-template-columns: 1fr; }
  .lib-card { padding: 12px; }
  .lib-path { white-space: normal; overflow-wrap: anywhere; }
  .lib-usage { align-items: flex-start; flex-wrap: wrap; }
}

</style>
