<template>
  <Teleport to="body">
    <div v-if="open" class="reader" :style="{ background: theme.bg, color: theme.fg }">
      <!-- Top bar -->
      <div class="reader-topbar" :style="{ background: theme.bar }">
        <button class="btn-icon" @click="$emit('close')" :style="{ color: theme.fg }">
          <Icon name="close" :size="20" />
        </button>
        <span class="reader-title">{{ title }}</span>
        <div style="display: flex; gap: 4px">
          <button class="btn-icon" @click="tocOpen = !tocOpen" :style="{ color: theme.fg }"><Icon name="list" :size="18" /></button>
          <button class="btn-icon" @click="settingsOpen = !settingsOpen" :style="{ color: theme.fg }"><Icon name="type" :size="18" /></button>
        </div>
      </div>

      <!-- Settings panel -->
      <div v-if="settingsOpen" class="reader-settings" :style="{ background: theme.bar, borderColor: theme.border }">
        <div class="rs-row">
          <span class="rs-label">Theme</span>
          <div style="display: flex; gap: 8px">
            <button
              v-for="t in themes"
              :key="t.name"
              class="rs-swatch"
              :class="{ active: activeTheme === t.name }"
              :style="{ background: t.bg, borderColor: activeTheme === t.name ? 'var(--gold)' : t.border }"
              @click="activeTheme = t.name"
            />
          </div>
        </div>
        <div class="rs-row">
          <span class="rs-label">Font Size</span>
          <div style="display: flex; gap: 6px; align-items: center">
            <button class="rs-btn" :style="{ color: theme.fg }" @click="fontSize = Math.max(14, fontSize - 2)">A−</button>
            <span style="font-size: 12px; font-family: var(--font-mono); min-width: 28px; text-align: center">{{ fontSize }}</span>
            <button class="rs-btn" :style="{ color: theme.fg }" @click="fontSize = Math.min(28, fontSize + 2)">A+</button>
          </div>
        </div>
        <div class="rs-row">
          <span class="rs-label">Width</span>
          <div style="display: flex; gap: 4px">
            <button v-for="w in widths" :key="w.label" class="rs-btn" :class="{ active: maxWidth === w.value }" :style="{ color: theme.fg }" @click="maxWidth = w.value">{{ w.label }}</button>
          </div>
        </div>
      </div>

      <!-- TOC overlay -->
      <div v-if="tocOpen" class="reader-toc" :style="{ background: theme.bar, borderColor: theme.border }">
        <div class="section-title" style="margin-bottom: 12px; padding: 0">Table of Contents</div>
        <div
          v-for="(ch, i) in chapters"
          :key="i"
          class="toc-item"
          :class="{ active: currentChapter === i }"
          :style="{ color: currentChapter === i ? 'var(--gold)' : theme.fg }"
          @click="currentChapter = i; tocOpen = false"
        >
          <span style="font-family: var(--font-mono); font-size: 11px; color: inherit; opacity: 0.5; min-width: 24px">{{ i + 1 }}</span>
          {{ ch }}
        </div>
      </div>

      <!-- Content -->
      <div class="reader-body" @click="tocOpen = false; settingsOpen = false">
        <div class="reader-content" :style="{ maxWidth: maxWidth + 'px', fontSize: fontSize + 'px' }">
          <div class="reader-chapter-num" :style="{ color: theme.fg, opacity: 0.3 }">Chapter {{ currentChapter + 1 }}</div>
          <h2 class="reader-chapter-title" :style="{ color: theme.fg }">{{ chapters[currentChapter] }}</h2>
          <div class="reader-text" :style="{ color: theme.fg, opacity: 0.85 }">
            <p v-for="(para, i) in paragraphs" :key="i">{{ para }}</p>
          </div>
        </div>
      </div>

      <!-- Bottom nav -->
      <div class="reader-bottombar" :style="{ background: theme.bar }">
        <button class="btn-icon" :style="{ color: theme.fg }" :disabled="currentChapter === 0" @click="currentChapter--">
          <Icon name="chevleft" :size="20" />
        </button>
        <div class="reader-progress">
          <div class="progress" style="flex: 1">
            <div :style="{ width: progressPct + '%' }" />
          </div>
          <span style="font-size: 10px; font-family: var(--font-mono); opacity: 0.5">{{ Math.round(progressPct) }}%</span>
        </div>
        <button class="btn-icon" :style="{ color: theme.fg }" :disabled="currentChapter >= chapters.length - 1" @click="currentChapter++">
          <Icon name="chevright" :size="20" />
        </button>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
defineProps<{ open: boolean; title: string }>()
defineEmits<{ close: [] }>()

const activeTheme = ref('dark')
const fontSize = ref(18)
const maxWidth = ref(680)
const currentChapter = ref(0)
const tocOpen = ref(false)
const settingsOpen = ref(false)

const themes = [
  { name: 'dark', bg: '#0c0c10', fg: '#d6d4cc', bar: 'rgba(12,12,16,0.95)', border: 'rgba(255,255,255,0.06)' },
  { name: 'sepia', bg: '#f4e8d0', fg: '#5c4b32', bar: 'rgba(244,232,208,0.95)', border: 'rgba(0,0,0,0.1)' },
  { name: 'paper', bg: '#f8f6f1', fg: '#2a2a2a', bar: 'rgba(248,246,241,0.95)', border: 'rgba(0,0,0,0.08)' },
]

const theme = computed(() => themes.find(t => t.name === activeTheme.value) ?? themes[0]!)

const widths = [
  { label: 'Narrow', value: 540 },
  { label: 'Medium', value: 680 },
  { label: 'Wide', value: 860 },
]

const chapters = [
  'The Boy Who Lived',
  'The Vanishing Glass',
  'The Letters from No One',
  'The Keeper of the Keys',
  'Diagon Alley',
  'The Journey from Platform Nine and Three-Quarters',
  'The Sorting Hat',
  'The Potions Master',
]

const paragraphs = [
  'The night was dark and full of whispers. Rain tapped against the windows of the old house like nervous fingers, and somewhere in the distance, an owl called out to the moon.',
  'He had been sitting in the chair for what felt like hours, the book open on his lap, but his eyes had long since stopped reading the words. Instead, they traced the patterns of light that danced across the ceiling, cast by the fire that was slowly dying in the hearth.',
  'There was a quality to the silence that pressed against the walls — not empty, but full. Full of memories, full of whispered conversations that had taken place in this room over decades, full of the weight of stories yet untold.',
  'She appeared in the doorway without a sound, and for a moment he wondered if she was real or merely another shadow conjured by his restless mind. But then she spoke, and her voice was warm and solid and unmistakably present.',
  '"You should sleep," she said, though they both knew that sleep was a distant country he could no longer visit. He smiled instead and gestured to the chair beside him.',
]

const progressPct = computed(() => ((currentChapter.value + 1) / chapters.length) * 100)
</script>

<style scoped>
.reader { position: fixed; inset: 0; z-index: 300; display: flex; flex-direction: column; transition: background 0.3s, color 0.3s; }
.reader-topbar { display: flex; align-items: center; justify-content: space-between; padding: 0 16px; height: 52px; backdrop-filter: blur(12px); z-index: 2; }
.reader-title { font-size: 13px; font-weight: 500; opacity: 0.7; }
.reader-settings { position: absolute; top: 52px; right: 16px; width: 280px; border: 1px solid; border-radius: var(--r-md); padding: 16px; z-index: 10; backdrop-filter: blur(12px); }
.rs-row { display: flex; align-items: center; justify-content: space-between; padding: 8px 0; }
.rs-label { font-size: 11px; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em; opacity: 0.5; }
.rs-swatch { width: 28px; height: 28px; border-radius: 50%; border: 2px solid; cursor: pointer; }
.rs-swatch.active { box-shadow: 0 0 0 2px var(--gold); }
.rs-btn { padding: 4px 10px; border-radius: var(--r-sm); font-size: 12px; font-weight: 600; background: rgba(128,128,128,0.1); }
.rs-btn.active { background: var(--gold-soft); color: var(--gold) !important; }
.reader-toc { position: absolute; top: 52px; left: 16px; width: 280px; max-height: 60vh; overflow-y: auto; border: 1px solid; border-radius: var(--r-md); padding: 16px; z-index: 10; backdrop-filter: blur(12px); }
.toc-item { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border-radius: var(--r-sm); cursor: pointer; font-size: 13px; }
.toc-item:hover { background: rgba(128,128,128,0.1); }
.reader-body { flex: 1; overflow-y: auto; display: flex; justify-content: center; padding: 48px 32px; }
.reader-content { width: 100%; transition: max-width 0.3s, font-size 0.3s; }
.reader-chapter-num { font-family: var(--font-mono); font-size: 11px; text-transform: uppercase; letter-spacing: 0.2em; margin-bottom: 8px; }
.reader-chapter-title { font-size: 32px; font-weight: 600; margin: 0 0 32px; }
.reader-text { font-family: 'Iowan Old Style', Georgia, 'Palatino Linotype', Palatino, serif; line-height: 1.85; }
.reader-text p { margin: 0 0 1.4em; text-indent: 1.5em; }
.reader-text p:first-child { text-indent: 0; }
.reader-bottombar { display: flex; align-items: center; gap: 16px; padding: 0 16px; height: 48px; backdrop-filter: blur(12px); }
.reader-progress { display: flex; align-items: center; gap: 10px; flex: 1; }
</style>
