<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: ['admin', 'settings-activity-redirect'] })

withDefaults(defineProps<{ embedded?: boolean, showSummary?: boolean }>(), {
  embedded: false,
  showSummary: true,
})

import type { ActiveSession } from '~/composables/useActiveSessions'
import { transcodeSessionsQuery, type TranscodeSession } from '~/queries/admin'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
const { toast } = useToast()
const { sessions, isPending, formatTime, progressPct, transcodeLabel } = useActiveSessions()

// Live encoder telemetry, matched onto playback sessions by library file id
// (transcode keys are "fileID:aN:sid" and Heya admits one live transcode
// session per file). Matching by session presence — not playback_action — is
// deliberate: the Jellyfin playstate mirror reports direct_play even when the
// client picked the transcode URL, and a live ffmpeg head is the honest
// signal either way.
const transcodeData = useQuery(transcodeSessionsQuery())
let encTimer: ReturnType<typeof setInterval> | null = null

const transcodeByFile = computed(() => {
  const map = new Map<number, TranscodeSession>()
  for (const t of transcodeData.data.value?.sessions ?? []) {
    const fileId = Number.parseInt(t.key, 10)
    if (!Number.isNaN(fileId)) map.set(fileId, t)
  }
  return map
})

const sessionRows = computed(() =>
  sessions.value.map(s => ({ s, enc: transcodeByFile.value.get(s.file_id) })))

// Transcode sessions with no live playback session to attach to (a player
// whose session tracking lagged or dropped). Still shown — an active ffmpeg
// head must never be invisible on the dashboard.
const orphanTranscodes = computed(() => {
  const playingFiles = new Set(sessions.value.map(s => s.file_id))
  return (transcodeData.data.value?.sessions ?? []).filter(t => {
    const fileId = Number.parseInt(t.key, 10)
    return Number.isNaN(fileId) || !playingFiles.has(fileId)
  })
})

const ENC_STATE_LABELS: Record<string, string> = {
  running: 'encoding',
  throttled: 'buffered ahead',
  completed: 'encoded',
  killed: 'stopped',
  exited: 'exited',
  idle: 'idle',
}

function encStateLabel(t: TranscodeSession) {
  return ENC_STATE_LABELS[t.state] ?? t.state
}

function encAhead(s: ActiveSession, t: TranscodeSession) {
  return Math.max(0, t.encoder_pos_seconds - s.position_seconds)
}

function encPct(total: number | undefined, t: TranscodeSession) {
  const dur = total || t.duration_seconds
  if (!dur) return 0
  return Math.min(100, (t.encoder_pos_seconds / dur) * 100)
}

function fmtMbps(kbps?: number) {
  if (!kbps) return ''
  return kbps >= 1000 ? `${(kbps / 1000).toFixed(1)} Mbps` : `${Math.round(kbps)} kbps`
}

onMounted(() => {
  encTimer = setInterval(() => {
    if (document.hidden) return
    void transcodeData.refetch()
  }, 3000)
})

onUnmounted(() => {
  if (encTimer) clearInterval(encTimer)
})

const viewerCount = computed(() => new Set(sessions.value.map(s => s.user_id)).size)
const transcodingCount = computed(() => sessions.value.filter(s => s.playback_action === 'transcode').length)
const directCount = computed(() => sessions.value.filter(s => s.playback_action === 'direct_play' || s.playback_action === 'remux').length)

function mediaIcon(type: string): string {
  return type === 'movie' ? 'film' : type === 'tv' || type === 'anime' ? 'tv' : type === 'music' ? 'music' : 'book'
}

// Transcode badge tone: transcoding is the expensive path (amber); remux and
// direct play are cheap (green). The label text tells them apart.
function transcodeTone(s: ActiveSession): 'warn' | 'ok' | 'idle' {
  if (s.playback_action === 'transcode') return 'warn'
  if (s.playback_action === 'direct_play' || s.playback_action === 'remux') return 'ok'
  return 'idle'
}

function resLabel(h?: number): string {
  if (!h) return ''
  if (h >= 2160) return '4K'
  if (h >= 1440) return '1440p'
  if (h >= 1080) return '1080p'
  if (h >= 720) return '720p'
  if (h >= 480) return '480p'
  return `${h}p`
}

// Compact "H.264 · 1080p · 4.2 Mbps" line from the stream-info the client
// echoed back.
function qualityBits(s: ActiveSession): string[] {
  const bits: string[] = []
  if (s.video_codec) bits.push(s.video_codec.toUpperCase())
  const r = resLabel(s.height)
  if (r) bits.push(r)
  if (s.bitrate_kbps) {
    bits.push(s.bitrate_kbps >= 1000 ? `${(s.bitrate_kbps / 1000).toFixed(1)} Mbps` : `${s.bitrate_kbps} kbps`)
  }
  return bits
}

// Best-effort friendly client name from the user agent. Not exhaustive — just
// enough to tell "Safari on iPhone" from "Chrome on macOS" at a glance.
function clientLabel(ua?: string): string {
  if (!ua) return 'Unknown client'
  const browser =
    /Edg\//.test(ua) ? 'Edge'
    : /OPR\/|Opera/.test(ua) ? 'Opera'
    : /Chrome\//.test(ua) ? 'Chrome'
    : /Firefox\//.test(ua) ? 'Firefox'
    : /Safari\//.test(ua) ? 'Safari'
    : ''
  const os =
    /iPhone|iPad|iPod/.test(ua) ? 'iOS'
    : /Android/.test(ua) ? 'Android'
    : /Mac OS X/.test(ua) ? 'macOS'
    : /Windows/.test(ua) ? 'Windows'
    : /Linux/.test(ua) ? 'Linux'
    : ''
  return [browser, os].filter(Boolean).join(' · ') || 'Browser'
}

const busy = ref<string | null>(null)

async function stopSession(s: ActiveSession) {
  const ok = await confirm({
    title: 'Stop this stream?',
    message: `Tell ${s.username}'s player to stop “${s.media_title}”. Their app closes the video immediately.`,
    destructive: true,
    confirmLabel: 'Stop playback',
  })
  if (!ok) return
  busy.value = s.session_id
  try {
    await $heya('/api/sessions/{session_id}/command', {
      method: 'POST',
      path: { session_id: s.session_id },
      body: { action: 'stop' } as never,
    })
    toast.ok('Stop signal sent.')
  } catch (e: any) {
    toast.err(e?.data?.error || e?.message || 'Could not send stop.')
  } finally {
    busy.value = null
  }
}

// --- Message dialog ---
const msgTarget = ref<ActiveSession | null>(null)
const msgText = ref('')
const msgSending = ref(false)
const showMsg = computed({
  get: () => msgTarget.value !== null,
  set: (v: boolean) => { if (!v) msgTarget.value = null },
})

function openMsg(s: ActiveSession) {
  msgTarget.value = s
  msgText.value = ''
}

async function sendMsg() {
  const target = msgTarget.value
  if (!target || !msgText.value.trim()) return
  msgSending.value = true
  try {
    await $heya('/api/sessions/{session_id}/command', {
      method: 'POST',
      path: { session_id: target.session_id },
      body: { action: 'message', message: msgText.value.trim() } as never,
    })
    toast.ok(`Message sent to ${target.username}.`)
    msgTarget.value = null
  } catch (e: any) {
    toast.err(e?.data?.error || e?.message || 'Could not send message.')
  } finally {
    msgSending.value = false
  }
}
</script>

<template>
  <div>
    <header v-if="!embedded" class="sv2-page-head">
      <h2 class="sv2-page-title">Now Playing</h2>
      <p class="sv2-page-desc">
        Every live playback session across all users — what they're streaming,
        whether it's transcoding, and how far along it is. Stop a stream or send
        the player a message from here. Sessions vanish 30&nbsp;seconds after a
        player goes quiet.
      </p>
    </header>

    <div v-if="showSummary" class="tiles">
      <MetricTile label="Active streams" :value="sessions.length" icon="cast"
        :tone="sessions.length ? 'good' : 'neutral'" />
      <MetricTile label="Transcoding" :value="transcodingCount" icon="cpu"
        :tone="transcodingCount ? 'warn' : 'neutral'" />
      <MetricTile label="Direct / remux" :value="directCount" icon="lightning" />
      <MetricTile label="Viewers" :value="viewerCount" icon="users" />
    </div>

    <SettingsSection title="Live sessions" icon="pulse">
      <div v-if="isPending" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>

      <div v-else-if="sessions.length === 0 && orphanTranscodes.length === 0" class="empty-state">
        <div class="empty-icon"><Icon name="cast" :size="28" /></div>
        <div class="empty-title">Nothing playing right now</div>
        <p class="empty-desc">When someone starts watching or listening, their stream shows up here live.</p>
      </div>

      <div v-else class="stream-list">
        <div v-for="{ s, enc } in sessionRows" :key="s.session_id" class="stream-card">
          <div class="stream-icon" :class="`kind-${s.media_type}`">
            <Icon :name="mediaIcon(s.media_type)" :size="18" />
          </div>

          <div class="stream-body">
            <div class="stream-top">
              <span class="stream-title">{{ s.media_title || 'Unknown' }}</span>
              <StatusBadge v-if="transcodeLabel(s)" :state="transcodeTone(s)">{{ transcodeLabel(s) }}</StatusBadge>
              <StatusBadge v-if="s.paused" state="idle">paused</StatusBadge>
            </div>

            <div v-if="s.media_subtitle" class="stream-sub">{{ s.media_subtitle }}</div>

            <div class="stream-meta">
              <span class="meta-user"><Icon name="user" :size="11" /> {{ s.username }}</span>
              <span><Icon name="cpu" :size="11" /> {{ clientLabel(s.client_user_agent) }}</span>
              <span v-if="s.client_ip" class="mono">{{ s.client_ip }}</span>
              <span v-if="qualityBits(s).length" class="mono">{{ qualityBits(s).join(' · ') }}</span>
              <span>started {{ timeAgo(s.started_at) }}</span>
            </div>

            <div class="stream-progress">
              <div class="prog-track">
                <div v-if="enc" class="prog-encoded" :style="{ width: encPct(s.total_seconds, enc) + '%' }" />
                <div class="prog-fill" :class="{ paused: s.paused }" :style="{ width: progressPct(s) + '%' }" />
              </div>
              <div class="prog-time mono">
                {{ formatTime(s.position_seconds) }} <span class="prog-sep">/</span> {{ formatTime(s.total_seconds) }}
                <span class="prog-pct">· {{ progressPct(s) }}%</span>
              </div>
            </div>

            <div v-if="enc" class="stream-encoder mono">
              <span class="enc-state" :class="`st-${enc.state}`">
                <Icon name="cpu" :size="10" /> {{ encStateLabel(enc) }}
              </span>
              <span v-if="enc.running && enc.speed">{{ enc.speed.toFixed(2) }}×</span>
              <span v-if="enc.running && enc.fps">{{ Math.round(enc.fps) }} fps</span>
              <span v-if="enc.running && enc.bitrate_kbps > 0">{{ fmtMbps(enc.bitrate_kbps) }}</span>
              <span>+{{ formatTime(encAhead(s, enc)) }} buffered</span>
              <span v-if="enc.quality" class="dim">{{ enc.quality }} · {{ enc.container }}</span>
            </div>
          </div>

          <div class="stream-actions">
            <button class="sv2-btn ghost sm" :disabled="busy === s.session_id" @click="openMsg(s)">
              <Icon name="bell" :size="12" /> Message
            </button>
            <button class="sv2-btn danger sm" :disabled="busy === s.session_id" @click="stopSession(s)">
              <Icon :name="busy === s.session_id ? 'spinner' : 'stop'" :size="12" /> Stop
            </button>
          </div>
        </div>

        <div v-for="t in orphanTranscodes" :key="t.key" class="stream-card">
          <div class="stream-icon">
            <Icon name="cpu" :size="18" />
          </div>
          <div class="stream-body">
            <div class="stream-top">
              <span class="stream-title" :title="t.path">{{ t.file }}</span>
              <StatusBadge :state="t.running ? 'warn' : 'idle'">transcoding</StatusBadge>
            </div>
            <div class="stream-meta">
              <span class="mono">{{ t.video_codec === 'copy' ? 'remux' : t.video_codec.replace(/^lib/, '') }} · {{ t.container }}</span>
              <span>no linked player session</span>
            </div>
            <div class="stream-progress">
              <div class="prog-track">
                <div class="prog-encoded" :style="{ width: encPct(t.duration_seconds, t) + '%' }" />
              </div>
              <div class="prog-time mono">
                {{ formatTime(t.encoder_pos_seconds) }} <span class="prog-sep">/</span> {{ formatTime(t.duration_seconds) }}
              </div>
            </div>
            <div class="stream-encoder mono">
              <span class="enc-state" :class="`st-${t.state}`">
                <Icon name="cpu" :size="10" /> {{ encStateLabel(t) }}
              </span>
              <span v-if="t.running && t.speed">{{ t.speed.toFixed(2) }}×</span>
              <span v-if="t.running && t.fps">{{ Math.round(t.fps) }} fps</span>
              <span v-if="t.running && t.bitrate_kbps > 0">{{ fmtMbps(t.bitrate_kbps) }}</span>
            </div>
          </div>
        </div>
      </div>
    </SettingsSection>

    <AppDialog v-model="showMsg" :title="msgTarget ? `Message ${msgTarget.username}` : 'Send message'"
      description="Pops a toast on the target player. Handy for “dinner's ready” or “stop hogging the GPU”." size="md">
      <div class="dialog-form">
        <div class="msg-target" v-if="msgTarget">
          <Icon :name="mediaIcon(msgTarget.media_type)" :size="14" />
          <span>{{ msgTarget.media_title }}</span>
          <span class="mono dim">{{ clientLabel(msgTarget.client_user_agent) }}</span>
        </div>
        <div class="form-field">
          <label class="form-label" for="activity-msg-text">Message</label>
          <textarea id="activity-msg-text" v-model="msgText" class="sv2-input msg-input" maxlength="280" rows="3"
            placeholder="Type a short message…" @keydown.meta.enter="sendMsg" @keydown.ctrl.enter="sendMsg" />
        </div>
      </div>
      <template #footer="{ close }">
        <button class="sv2-btn ghost" @click="close()">Cancel</button>
        <button class="sv2-btn primary" :disabled="msgSending || !msgText.trim()" @click="sendMsg">
          <Icon :name="msgSending ? 'spinner' : 'bell'" :size="12" />
          {{ msgSending ? 'Sending…' : 'Send message' }}
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.empty-state {
  display: flex; flex-direction: column; align-items: center;
  padding: 40px 18px; text-align: center;
}
.empty-icon {
  width: 56px; height: 56px;
  border-radius: var(--r-md);
  background: var(--bg-3);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
  margin-bottom: 8px;
}
.empty-title { font-size: 14px; font-weight: 600; color: var(--fg-1); }
.empty-desc { margin: 4px 0 0; font-size: 12.5px; color: var(--fg-3); line-height: 1.4; }

.stream-list { display: flex; flex-direction: column; gap: 8px; }
.stream-card {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color 0.15s ease;
}
.stream-card:hover { border-color: var(--border-strong); }

.stream-icon {
  width: 40px; height: 40px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.stream-icon.kind-tv    { color: rgb(140, 160, 255); background: rgba(140, 160, 255, 0.10); }
.stream-icon.kind-music { color: rgb(200, 140, 255); background: rgba(200, 140, 255, 0.10); }
.stream-icon.kind-book  { color: rgb(140, 220, 180); background: rgba(140, 220, 180, 0.10); }

.stream-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 5px; }
.stream-top { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.stream-title { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.stream-sub { font-size: 12px; color: var(--fg-2); }

.stream-meta {
  display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px;
  font-size: 11px; color: var(--fg-3);
}
.stream-meta span { display: inline-flex; align-items: center; gap: 4px; }
.meta-user { color: var(--fg-2); }
.mono { font-family: var(--font-mono); }
.dim { color: var(--fg-3); }

.stream-progress { display: flex; align-items: center; gap: 10px; margin-top: 3px; }
.prog-track {
  position: relative;
  flex: 1; height: 4px; border-radius: 2px;
  background: rgb(var(--ink) / 0.08); overflow: hidden;
}
.prog-encoded {
  position: absolute; inset: 0 auto 0 0;
  background: color-mix(in srgb, var(--gold) 30%, transparent);
  transition: width 0.6s ease;
}
.prog-fill { position: relative; height: 100%; background: var(--gold); transition: width 0.5s ease; }
.prog-fill.paused { background: var(--fg-3); }

.stream-encoder {
  display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px;
  font-size: 10.5px; color: var(--fg-2);
  margin-top: 1px;
}
.enc-state {
  display: inline-flex; align-items: center; gap: 4px;
  font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em;
  font-size: 9.5px;
  color: var(--fg-2);
}
.enc-state.st-running   { color: var(--good); }
.enc-state.st-throttled { color: var(--gold); }
.enc-state.st-exited    { color: var(--bad); }
.prog-time { font-size: 11px; color: var(--fg-2); white-space: nowrap; }
.prog-sep { color: var(--fg-3); }
.prog-pct { color: var(--fg-3); }

.stream-actions { display: flex; flex-direction: column; gap: 6px; flex-shrink: 0; }
.sv2-btn.sm { padding: 6px 10px; font-size: 11.5px; }

.dialog-form { display: flex; flex-direction: column; gap: 12px; }
.msg-target {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 10px; border-radius: var(--r-sm);
  background: var(--bg-1); border: 1px solid var(--border);
  font-size: 12.5px; color: var(--fg-1);
}
.msg-target .mono { margin-left: auto; font-size: 11px; }
.form-field { display: flex; flex-direction: column; gap: 5px; }
.form-label {
  font-family: var(--font-mono);
  font-size: 10px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3);
}
.sv2-input {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  padding: 9px 12px;
  outline: none;
  transition: border-color 0.12s;
}
.sv2-input:focus { border-color: var(--gold); }
.msg-input { resize: vertical; font-family: inherit; line-height: 1.5; }
</style>
