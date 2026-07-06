<template>
  <div v-if="!cast.length && !crew.length" class="mep-empty">
    <Icon name="users" :size="28" />
    <span>No cast or crew data available.</span>
  </div>
  <div v-else class="mf-split">
    <div v-if="cast.length" class="mf-col">
      <div class="mf-card mf-card-fill">
        <div class="mf-card-head">Cast</div>
        <div class="mep-list">
          <div v-for="c in cast" :key="`${c.id}-${c.character}`" class="mep-person">
            <NuxtImg
              v-if="c.profile_path"
              :src="c.profile_path.startsWith('http') ? c.profile_path : `/api/person/${c.id}/image`"
              class="mep-photo"
              @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
            />
            <div v-else class="mep-photo mep-photo-empty">
              <Icon name="user" :size="14" />
            </div>
            <div class="mep-info">
              <div class="mep-name">{{ c.name }}</div>
              <div class="mep-role">{{ c.character }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="crewDepts.length" class="mf-col">
      <div class="mf-card mf-card-fill">
        <div class="mf-card-head">Crew</div>
        <div class="mep-depts">
          <div v-for="dept in crewDepts" :key="dept.name" class="mep-dept">
            <div class="mep-dept-name">{{ dept.name }}</div>
            <div class="mep-dept-list">
              <div v-for="c in dept.members" :key="`${c.id}-${c.job}`" class="mep-crew-item">
                <span class="mep-crew-name">{{ c.name }}</span>
                <span class="mep-crew-job">{{ c.job }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{ detail: any }>()

const cast = computed(() => props.detail?.cast || [])
const crew = computed(() => props.detail?.crew || [])

const crewDepts = computed(() => {
  const depts = new Map<string, any[]>()
  for (const c of crew.value) {
    const dept = c.department || 'Other'
    if (!depts.has(dept)) depts.set(dept, [])
    depts.get(dept)!.push(c)
  }
  return Array.from(depts.entries()).map(([name, members]) => ({ name, members }))
})
</script>

<style scoped>
.mf-split {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  height: 100%;
  align-items: start;
}

.mf-col {
  display: flex;
  flex-direction: column;
  gap: 20px;
  min-width: 0;
}

.mf-card {
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 20px;
}

.mf-card-fill {
  flex: 1;
}

.mf-card-head {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
  margin-bottom: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.mep-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.mep-person {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  transition: background 0.12s;
}
.mep-person:hover {
  background: rgba(255, 255, 255, 0.03);
}

.mep-photo {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  object-fit: cover;
  flex-shrink: 0;
  background: var(--bg-3);
}

.mep-photo-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
}

.mep-info {
  flex: 1;
  min-width: 0;
}

.mep-name {
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mep-role {
  font-size: 11px;
  color: var(--fg-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mep-depts {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.mep-dept-name {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
  margin-bottom: 8px;
}

.mep-dept-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.mep-crew-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 0;
}

.mep-crew-name {
  font-size: 12px;
  color: var(--fg-1);
}

.mep-crew-job {
  font-size: 11px;
  color: var(--fg-3);
}

.mep-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 48px 0;
  color: var(--fg-3);
  font-size: 14px;
}
</style>
