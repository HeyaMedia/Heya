<script setup lang="ts">
const { user, logout } = useAuth()
const open = ref(false)
const dropdownRef = ref<HTMLElement>()
const showSettings = ref(false)

function toggle() { open.value = !open.value }

function handleClickOutside(e: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(e.target as Node)) {
    open.value = false
  }
}

onMounted(() => document.addEventListener('click', handleClickOutside))
onUnmounted(() => document.removeEventListener('click', handleClickOutside))
</script>

<template>
  <div class="ud-wrap" ref="dropdownRef">
    <button class="ud-trigger" @click.stop="toggle" :title="user?.username">
      <div class="ud-avatar">
        <span>{{ user?.username?.slice(0, 2).toUpperCase() }}</span>
      </div>
    </button>

    <Transition name="dropdown">
      <div v-if="open" class="ud-menu">
        <div class="ud-header">
          <div class="ud-header-avatar">
            <span>{{ user?.username?.slice(0, 2).toUpperCase() }}</span>
          </div>
          <div class="ud-header-info">
            <div class="ud-username">{{ user?.username }}</div>
            <div class="ud-email">{{ user?.email }}</div>
          </div>
        </div>

        <div class="ud-divider" />

        <button class="ud-item" @click="showSettings = true; open = false">
          <Icon name="settings" :size="15" />
          <span>Playback Settings</span>
        </button>

        <NuxtLink to="/settings" class="ud-item" @click="open = false">
          <Icon name="settings" :size="15" />
          <span>Server Settings</span>
        </NuxtLink>

        <div class="ud-divider" />

        <button class="ud-item ud-logout" @click="logout(); open = false">
          <Icon name="close" :size="15" />
          <span>Sign Out</span>
        </button>
      </div>
    </Transition>

    <UserSettingsModal v-if="showSettings" @close="showSettings = false" />
  </div>
</template>

<style scoped>
.ud-wrap { position: relative; }

.ud-trigger {
  display: flex; align-items: center; justify-content: center;
  cursor: pointer; padding: 0; margin-left: 6px;
}

.ud-avatar {
  width: 32px; height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
  font-size: 11px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  letter-spacing: 0.04em;
  transition: box-shadow 0.15s;
}
.ud-trigger:hover .ud-avatar { box-shadow: 0 0 0 2px rgba(230, 185, 74, 0.3); }

.ud-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 260px;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-3);
  overflow: hidden;
  z-index: 100;
}

.ud-header {
  display: flex; align-items: center; gap: 12px;
  padding: 16px 16px 12px;
}

.ud-header-avatar {
  width: 40px; height: 40px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
  font-size: 14px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}

.ud-header-info { min-width: 0; }
.ud-username { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.ud-email { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); margin-top: 1px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.ud-divider { height: 1px; background: var(--border); margin: 4px 0; }

.ud-item {
  display: flex; align-items: center; gap: 10px;
  width: 100%; padding: 10px 16px;
  font-size: 13px; font-weight: 500;
  color: var(--fg-1);
  text-decoration: none;
  transition: background 0.1s, color 0.1s;
}
.ud-item:hover { background: rgba(255,255,255,0.04); color: var(--fg-0); }

.ud-logout { color: var(--bad); }
.ud-logout:hover { background: rgba(255, 80, 80, 0.06); }

.dropdown-enter-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.dropdown-leave-active { transition: opacity 0.1s ease, transform 0.1s ease; }
.dropdown-enter-from { opacity: 0; transform: translateY(-4px) scale(0.98); }
.dropdown-leave-to { opacity: 0; transform: translateY(-2px); }
</style>
