<template>
  <aside class="flex w-56 flex-col border-r border-surface-border bg-surface-raised">
    <div class="flex h-14 items-center gap-2 px-4">
      <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-heya-primary text-sm font-bold text-white">
        H
      </div>
      <span class="text-lg font-semibold">Heya</span>
    </div>

    <nav class="flex-1 space-y-1 px-2 py-3">
      <NuxtLink
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-gray-400 transition-colors hover:bg-surface-overlay hover:text-gray-200"
        active-class="!bg-heya-primary/10 !text-heya-primary"
      >
        <span class="text-base" v-html="item.icon" />
        <span>{{ item.label }}</span>
      </NuxtLink>
    </nav>

    <div class="border-t border-surface-border p-3">
      <div v-if="user" class="flex items-center gap-2 rounded-lg px-2 py-1.5">
        <div class="flex h-7 w-7 items-center justify-center rounded-full bg-surface-overlay text-xs font-medium text-gray-300">
          {{ user.username[0].toUpperCase() }}
        </div>
        <div class="flex-1 truncate text-sm text-gray-400">{{ user.username }}</div>
        <button class="text-gray-500 hover:text-gray-300" @click="logout" title="Sign out">
          &times;
        </button>
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
const { user, logout } = useAuth()

const navItems = [
  { to: '/', label: 'Dashboard', icon: '&#9632;' },
  { to: '/movies', label: 'Movies', icon: '&#127916;' },
  { to: '/tv', label: 'TV Shows', icon: '&#128250;' },
  { to: '/music', label: 'Music', icon: '&#127925;' },
  { to: '/books', label: 'Books', icon: '&#128214;' },
  { to: '/libraries', label: 'Libraries', icon: '&#128194;' },
  { to: '/settings', label: 'Settings', icon: '&#9881;' },
]
</script>
