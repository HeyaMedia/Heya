<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Server</h2>
      <p class="page-desc">Health monitoring, diagnostics, and API access</p>
    </div>

    <!-- Health -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="pulse" :size="14" />
        Health
      </h3>
      <div class="health-cards">
        <div class="hc">
          <div class="hc-dot" :class="health?.status === 'ok' ? 'good' : 'bad'" />
          <div class="hc-body">
            <div class="hc-label">Server</div>
            <div class="hc-val" :class="health?.status === 'ok' ? 'good' : 'bad'">
              {{ health?.status === 'ok' ? 'Online' : 'Offline' }}
            </div>
          </div>
        </div>
        <div class="hc">
          <div class="hc-dot" :class="health?.database === 'connected' ? 'good' : 'bad'" />
          <div class="hc-body">
            <div class="hc-label">PostgreSQL</div>
            <div class="hc-val" :class="health?.database === 'connected' ? 'good' : 'bad'">
              {{ health?.database === 'connected' ? 'Connected' : (health?.database ?? 'Unknown') }}
            </div>
          </div>
        </div>
        <div class="hc">
          <div class="hc-dot idle" />
          <div class="hc-body">
            <div class="hc-label">Version</div>
            <div class="hc-val">{{ health?.version || 'v1.0.0' }}</div>
          </div>
        </div>
      </div>
    </section>

    <!-- System info -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="cpu" :size="14" />
        System Information
      </h3>
      <div class="info-table">
        <div class="info-row">
          <span class="info-key">Backend</span>
          <span class="info-val">Go 1.26</span>
        </div>
        <div class="info-row">
          <span class="info-key">Frontend</span>
          <span class="info-val">Nuxt 4</span>
        </div>
        <div class="info-row">
          <span class="info-key">Database</span>
          <span class="info-val">PostgreSQL 17</span>
        </div>
        <div class="info-row">
          <span class="info-key">Job Queue</span>
          <span class="info-val">River (PG-backed)</span>
        </div>
        <div class="info-row">
          <span class="info-key">Media Processing</span>
          <span class="info-val">ffmpeg / ffprobe</span>
        </div>
      </div>
    </section>

    <!-- Quick glance: Jobs -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="timer" :size="14" />
        Background Jobs
        <span class="spacer" />
        <NuxtLink to="/settings/jobs" class="heading-link">
          View all <Icon name="arrow-right" :size="11" />
        </NuxtLink>
      </h3>
      <div v-if="jobSummary.length" class="summary-pills">
        <span v-for="s in jobSummary" :key="s.state" class="summary-pill" :class="s.state">
          <span class="pill-count">{{ s.count }}</span>
          {{ s.state }}
        </span>
      </div>
      <div v-else class="empty-hint">
        <Icon name="check" :size="14" />
        Job queue is empty
      </div>
    </section>

    <!-- Quick glance: Logs -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="list" :size="14" />
        Recent Logs
        <span class="spacer" />
        <NuxtLink to="/settings/logs" class="heading-link">
          View all <Icon name="arrow-right" :size="11" />
        </NuxtLink>
      </h3>
      <div v-if="recentLogs.length" class="mini-log-panel">
        <div v-for="(entry, i) in recentLogs" :key="i" class="mini-log-row" :class="entry.level">
          <span class="ml-level" :class="entry.level">{{ entry.level }}</span>
          <span class="ml-msg">{{ entry.message }}</span>
          <span class="ml-time">{{ formatLogTime(entry.time) }}</span>
        </div>
      </div>
      <div v-else class="empty-hint">
        <Icon name="info" :size="14" />
        No recent logs
      </div>
    </section>

    <!-- API -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="lightning" :size="14" />
        API Access
      </h3>
      <div class="api-cards">
        <a href="/api/openapi.json" target="_blank" class="api-card">
          <div class="api-card-icon">
            <Icon name="clipboard" :size="18" />
          </div>
          <div class="api-card-text">
            <div class="api-card-title">OpenAPI Spec</div>
            <div class="api-card-desc">Machine-readable API specification</div>
          </div>
          <Icon name="arrow-right" :size="14" class="api-card-arrow" />
        </a>
        <a href="/api/docs" target="_blank" class="api-card">
          <div class="api-card-icon">
            <Icon name="book" :size="18" />
          </div>
          <div class="api-card-text">
            <div class="api-card-title">Scalar Docs</div>
            <div class="api-card-desc">Interactive API documentation</div>
          </div>
          <Icon name="arrow-right" :size="14" class="api-card-arrow" />
        </a>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import type { HealthResponse } from '~~/shared/types'

interface JobSummary { state: string; count: number }
interface LogEntry { time: string; level: string; message: string; fields?: Record<string, any> }

const health = ref<HealthResponse | null>(null)
const jobSummary = ref<JobSummary[]>([])
const recentLogs = ref<LogEntry[]>([])

function formatLogTime(t: string) {
  try {
    return new Date(t).toLocaleTimeString('en-GB', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return '' }
}

onMounted(async () => {
  const [h, js, lg] = await Promise.allSettled([
    $fetch<HealthResponse>('/api/health'),
    apiFetch<JobSummary[]>('/api/jobs/summary'),
    apiFetch<LogEntry[]>('/api/logs?n=8'),
  ])
  if (h.status === 'fulfilled') health.value = h.value
  if (js.status === 'fulfilled') jobSummary.value = js.value
  if (lg.status === 'fulfilled') recentLogs.value = lg.value
})
</script>

<style scoped>
.page-header { margin-bottom: 32px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
.section-heading {
  display: flex; align-items: center; gap: 8px;
  font-size: 11px; font-weight: 600; color: var(--fg-3);
  font-family: var(--font-mono); text-transform: uppercase;
  letter-spacing: 0.1em; margin: 0 0 14px; padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}
.spacer { flex: 1; }

.heading-link {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 10px; font-weight: 600; color: var(--fg-3);
  text-decoration: none; text-transform: uppercase;
  letter-spacing: 0.06em; transition: color 0.12s ease;
}
.heading-link:hover { color: var(--gold); }

/* Health */
.health-cards { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
.hc { display: flex; align-items: center; gap: 12px; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); padding: 16px 18px; }
.hc-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.hc-dot.good { background: var(--good); box-shadow: 0 0 8px rgba(111, 191, 124, 0.4); }
.hc-dot.bad { background: var(--bad); box-shadow: 0 0 8px rgba(217, 107, 107, 0.4); }
.hc-dot.idle { background: var(--fg-4); }
.hc-body { min-width: 0; }
.hc-label { font-size: 10px; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.1em; color: var(--fg-3); }
.hc-val { font-size: 15px; font-weight: 600; margin-top: 2px; }
.hc-val.good { color: var(--good); }
.hc-val.bad { color: var(--bad); }

/* Info table */
.info-table { background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; }
.info-row { display: flex; justify-content: space-between; align-items: center; padding: 12px 18px; border-bottom: 1px solid var(--border); font-size: 13px; }
.info-row:last-child { border-bottom: none; }
.info-key { color: var(--fg-3); font-family: var(--font-mono); font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; }
.info-val { color: var(--fg-1); font-weight: 500; }

/* Job summary pills */
.summary-pills { display: flex; flex-wrap: wrap; gap: 6px; }
.summary-pill {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 12px; border-radius: 100px;
  font-size: 11px; font-family: var(--font-mono);
  background: var(--bg-3); border: 1px solid var(--border);
  color: var(--fg-2); text-transform: capitalize;
}
.pill-count { font-weight: 700; }
.summary-pill.running .pill-count { color: var(--good); }
.summary-pill.available .pill-count { color: var(--gold); }
.summary-pill.discarded .pill-count, .summary-pill.cancelled .pill-count { color: var(--bad); }
.summary-pill.completed .pill-count { color: var(--fg-3); }

/* Mini log panel */
.mini-log-panel {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 4px 0;
  font-family: var(--font-mono);
  font-size: 11px;
  overflow: hidden;
}

.mini-log-row {
  display: flex; gap: 8px; padding: 2px 14px; align-items: baseline;
}
.mini-log-row.error { background: rgba(217, 107, 107, 0.04); }
.mini-log-row.warn { background: rgba(230, 185, 74, 0.03); }

.ml-level {
  font-weight: 700; text-transform: uppercase; width: 42px; flex-shrink: 0;
  font-size: 10px; letter-spacing: 0.04em;
}
.ml-level.debug { color: var(--fg-3); }
.ml-level.info { color: rgb(140, 160, 255); }
.ml-level.warn { color: var(--gold); }
.ml-level.error { color: var(--bad); }

.ml-msg { color: var(--fg-1); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ml-time { color: var(--fg-4); flex-shrink: 0; }

/* API cards */
.api-cards { display: flex; flex-direction: column; gap: 6px; }
.api-card { display: flex; align-items: center; gap: 14px; padding: 16px 18px; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); text-decoration: none; transition: all 0.15s ease; }
.api-card:hover { border-color: var(--border-strong); background: var(--bg-3); }
.api-card-icon { width: 40px; height: 40px; border-radius: var(--r-sm); background: rgba(140, 160, 255, 0.1); color: rgb(140, 160, 255); display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.api-card-text { flex: 1; min-width: 0; }
.api-card-title { font-size: 13px; font-weight: 600; color: var(--fg-0); }
.api-card-desc { font-size: 12px; color: var(--fg-3); margin-top: 2px; }
.api-card-arrow { color: var(--fg-3); transition: transform 0.15s ease; }
.api-card:hover .api-card-arrow { transform: translateX(2px); color: var(--fg-1); }

.empty-hint {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 13px;
  padding: 14px 16px; background: var(--bg-2);
  border: 1px dashed var(--border); border-radius: var(--r-md);
}
</style>
