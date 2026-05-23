export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2025-05-19',
  devtools: { enabled: true },

  modules: ['@nuxtjs/tailwindcss'],

  components: [
    { path: '~/components', pathPrefix: false },
  ],

  css: [
    '@fontsource/inter/400.css',
    '@fontsource/inter/500.css',
    '@fontsource/inter/600.css',
    '@fontsource/inter/700.css',
    '@fontsource/jetbrains-mono/400.css',
    '@fontsource/jetbrains-mono/500.css',
    '@fontsource/jetbrains-mono/600.css',
    '@fontsource/jetbrains-mono/700.css',
    '~/assets/css/heya.css',
    '~/assets/css/main.css',
  ],

  runtimeConfig: {
    public: {
      apiBase: '/api',
    },
  },

  app: {
    head: {
      title: 'Heya',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'theme-color', content: '#0a0a12' },
      ],
      link: [
        { rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' },
      ],
      // Inline style so the document is dark from byte 0 — before any CSS file
      // loads and before the spa-loading-template renders. Kills the white
      // browser-default flash on cold loads / slow connections.
      style: [
        { innerHTML: 'html,body{background:#0a0a12;margin:0;color-scheme:dark}' },
      ],
    },
  },

  vite: {
    optimizeDeps: {
      exclude: ['@phosphor-icons/vue'],
    },
  },
})
