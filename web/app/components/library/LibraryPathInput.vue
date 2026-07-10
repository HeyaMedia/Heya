<template>
  <div class="lpi">
    <div class="lpi-tabs">
      <button
        type="button"
        class="lpi-tab"
        :class="{ active: mode === 'local' }"
        @click="switchMode('local')"
      >
        <Icon name="folder" :size="12" />
        Local
      </button>
      <button
        type="button"
        class="lpi-tab"
        :class="{ active: mode === 'smb' }"
        @click="switchMode('smb')"
      >
        <Icon name="globe" :size="12" />
        SMB
      </button>
    </div>

    <PathBrowser v-if="mode === 'local'" :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" />

    <div v-else class="smb-form">
      <div class="smb-row">
        <div class="smb-field" style="flex: 1">
          <label class="smb-label">Host</label>
          <input v-model="smb.host" class="smb-input" placeholder="192.168.1.10" @input="emitSmb" />
        </div>
        <div class="smb-field" style="width: 80px">
          <label class="smb-label">Port</label>
          <input v-model="smb.port" class="smb-input" placeholder="445" @input="emitSmb" />
        </div>
      </div>
      <div class="smb-row">
        <div class="smb-field" style="flex: 1">
          <label class="smb-label">Share</label>
          <input v-model="smb.share" class="smb-input" placeholder="media" @input="emitSmb" />
        </div>
        <div class="smb-field" style="flex: 1">
          <label class="smb-label">Path <span class="smb-optional">(optional)</span></label>
          <input v-model="smb.path" class="smb-input" placeholder="Movies" @input="emitSmb" />
        </div>
      </div>
      <div class="smb-row">
        <div class="smb-field" style="flex: 1">
          <label class="smb-label">Username <span class="smb-optional">(optional)</span></label>
          <input v-model="smb.user" class="smb-input" placeholder="guest" @input="emitSmb" />
        </div>
        <div class="smb-field" style="flex: 1">
          <label class="smb-label">Password <span class="smb-optional">(optional)</span></label>
          <input v-model="smb.pass" class="smb-input" type="password" placeholder="••••" @input="emitSmb" />
        </div>
      </div>
      <div class="smb-preview">
        <Icon name="globe" :size="11" />
        <span>{{ smbUrl || 'smb://host/share' }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  modelValue: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const mode = ref<'local' | 'smb'>(props.modelValue.startsWith('smb://') ? 'smb' : 'local')

const smb = reactive({ host: '', port: '', user: '', pass: '', share: '', path: '' })

if (props.modelValue.startsWith('smb://')) {
  parseSmbUrl(props.modelValue)
}

function parseSmbUrl(raw: string) {
  // Split the authority (user:pass@host:port) from the path at the first slash
  // and keep the path literal. Parsing the whole URL with `new URL` would
  // swallow a literal '#' or '?' in the path as a fragment/query and drop
  // everything after it — the same trap the Go ParseSMBURL had. SMB filenames
  // legitimately contain those characters, and the backend stores the path
  // verbatim, so the edit form must read it back verbatim too.
  const rest = raw.slice('smb://'.length)
  const slash = rest.indexOf('/')
  const authority = slash >= 0 ? rest.slice(0, slash) : rest
  const rawPath = slash >= 0 ? rest.slice(slash + 1) : ''
  try {
    const u = new URL(`smb://${authority}`)
    smb.host = u.hostname
    smb.port = u.port || ''
    smb.user = decodeURIComponent(u.username || '')
    smb.pass = decodeURIComponent(u.password || '')
  } catch {}
  const parts = rawPath.split('/')
  smb.share = parts[0] || ''
  smb.path = parts.slice(1).join('/')
}

const smbUrl = computed(() => {
  if (!smb.host || !smb.share) return ''
  let auth = ''
  if (smb.user) {
    auth = smb.pass ? `${encodeURIComponent(smb.user)}:${encodeURIComponent(smb.pass)}@` : `${encodeURIComponent(smb.user)}@`
  }
  const port = smb.port && smb.port !== '445' ? `:${smb.port}` : ''
  const path = smb.path ? `/${smb.path.replace(/^\//, '')}` : ''
  return `smb://${auth}${smb.host}${port}/${smb.share}${path}`
})

function emitSmb() {
  emit('update:modelValue', smbUrl.value)
}

function switchMode(m: 'local' | 'smb') {
  mode.value = m
  if (m === 'local' && props.modelValue.startsWith('smb://')) {
    emit('update:modelValue', '')
  }
  if (m === 'smb' && !props.modelValue.startsWith('smb://')) {
    emit('update:modelValue', smbUrl.value)
  }
}
</script>

<style scoped>
.lpi { flex: 1; display: flex; flex-direction: column; gap: 8px; }

.lpi-tabs {
  display: flex;
  gap: 2px;
  background: var(--bg-3);
  border-radius: var(--r-sm);
  padding: 2px;
  width: fit-content;
}

.lpi-tab {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 12px;
  border-radius: var(--r-xs);
  font-size: 11px;
  font-weight: 500;
  color: var(--fg-3);
  transition: all 0.12s;
}
.lpi-tab:hover { color: var(--fg-1); }
.lpi-tab.active {
  background: var(--bg-4);
  color: var(--fg-0);
  box-shadow: var(--shadow-1);
}

/* SMB form */
.smb-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.smb-row { display: flex; gap: 8px; }
.smb-field { display: flex; flex-direction: column; }

.smb-label {
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
  margin-bottom: 4px;
}
.smb-optional { font-weight: 400; opacity: 0.6; text-transform: none; letter-spacing: 0; }

.smb-input {
  height: 36px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 0 10px;
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-mono);
  outline: none;
  width: 100%;
  transition: border-color 0.12s;
}
.smb-input:focus { border-color: var(--gold); }
.smb-input::placeholder { color: var(--fg-4); }

.smb-preview {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  background: var(--bg-3);
  border-radius: var(--r-sm);
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
