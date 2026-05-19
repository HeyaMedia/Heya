export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2025-05-19',
  devtools: { enabled: true },

  modules: ['@nuxtjs/tailwindcss'],

  css: ['~/assets/css/main.css'],

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
    },
  },

  nitro: {
    devProxy: {
      '/api/': {
        target: 'http://localhost:8080/api/',
        changeOrigin: true,
      },
    },
  },
})
