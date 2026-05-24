<template>
  <div class="page-pad">
    <header class="podcasts-head">
      <div class="podcasts-head-art">
        <Icon name="mic" :size="36" />
      </div>
      <div class="podcasts-head-meta">
        <div class="m-kind">Podcasts</div>
        <h1 class="m-title">Listen to the things you actually care about</h1>
        <p class="m-sub">Subscribe via RSS to any podcast feed. Downloads cache locally; new episodes drop into Recent.</p>
        <div class="m-actions">
          <button class="btn btn-primary" disabled title="Coming later">
            <Icon name="plus" :size="14" /> Subscribe via RSS
          </button>
          <button class="btn" disabled title="Coming later">Browse Featured</button>
        </div>
      </div>
    </header>

    <div class="m-tabs">
      <button v-for="t in tabs" :key="t" class="m-tab" :class="{ active: tab === t }" @click="tab = t">{{ t }}</button>
    </div>

    <div v-if="tab === 'Subscribed'" class="m-empty-state">
      <Icon name="mic" :size="40" class="m-empty-icon" />
      <h3>No subscriptions yet</h3>
      <p>Paste an RSS URL or browse the Featured tab — every episode you've heard, and every one you haven't, lands here.</p>
    </div>

    <div v-else-if="tab === 'Featured'" class="m-empty-state">
      <Icon name="radio" :size="40" class="m-empty-icon" />
      <h3>Featured podcasts coming later</h3>
      <p>We'll surface curated picks here once the discovery feed ships. For now, subscribe directly via RSS.</p>
    </div>

    <div v-else class="m-empty-state">
      <Icon name="clock" :size="40" class="m-empty-icon" />
      <h3>Recent episodes will live here</h3>
      <p>Once you subscribe, the newest unplayed episodes show up at the top.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: 'default' })

const tabs = ['Subscribed', 'Featured', 'Recent'] as const
type Tab = (typeof tabs)[number]
const tab = ref<Tab>('Subscribed')
</script>

<style scoped>
.podcasts-head {
  display: flex;
  gap: 24px;
  align-items: flex-end;
  padding: 32px 0 24px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 24px;
}
.podcasts-head-art {
  width: 160px;
  height: 160px;
  border-radius: var(--r-md);
  background: linear-gradient(135deg, #2a1f3b, #1a0d2e);
  display: flex; align-items: center; justify-content: center;
  color: var(--gold);
  box-shadow: 0 16px 36px rgba(0,0,0,0.5);
  flex-shrink: 0;
}
.podcasts-head-meta { flex: 1; min-width: 0; }
.m-kind {
  font-size: 11px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.12em;
  color: var(--fg-2); margin-bottom: 6px;
}
.m-title {
  font-size: clamp(28px, 3.5vw, 44px);
  font-weight: 800;
  line-height: 1.05;
  margin-bottom: 8px;
  letter-spacing: -0.02em;
  color: var(--fg-0);
}
.m-sub { color: var(--fg-2); margin-bottom: 18px; max-width: 60ch; }
.m-actions { display: flex; gap: 10px; }
.m-actions :deep(.btn-primary) {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 0 18px; height: 40px;
  border-radius: 999px;
  font-weight: 600;
}

.m-tabs {
  display: flex;
  gap: 6px;
  margin-bottom: 24px;
  border-bottom: 1px solid var(--border);
}
.m-tab {
  padding: 10px 16px;
  background: transparent;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--fg-2);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s;
}
.m-tab:hover { color: var(--fg-0); }
.m-tab.active { color: var(--gold); border-bottom-color: var(--gold); }

.m-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 80px 24px;
  color: var(--fg-2);
}
.m-empty-icon { color: var(--fg-3); margin-bottom: 16px; }
.m-empty-state h3 { font-size: 18px; font-weight: 600; color: var(--fg-1); margin-bottom: 8px; }
.m-empty-state p { font-size: 13px; max-width: 50ch; color: var(--fg-2); }
</style>
