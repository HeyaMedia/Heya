export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2025-05-19',
  devtools: { enabled: true },

  modules: ['@nuxtjs/tailwindcss', 'nuxt-open-fetch', 'nuxt-phosphor-icons', '@vueuse/nuxt', '@nuxt/image', '@vite-pwa/nuxt'],

  // PWA install support (Wave 4 of docs/responsive-plan.md). Self-hosted app
  // with frequent tagged releases, so `autoUpdate` — the new service worker
  // takes over silently on next load instead of nagging the user with an
  // "update available" prompt. `generateSW` (the module default) precaches
  // the built app shell. The ONLY `/api/*` requests the SW intercepts are
  // media images (see the `runtimeCaching` rule below, StaleWhileRevalidate) — auth,
  // streaming, and every other API route match no rule, so the SW leaves them
  // alone and they always hit the network fresh. `navigateFallback` covers
  // deep-link/SPA navigations the same way `spaHandler` does server-side
  // (internal/server/frontend.go always serves index.html for unknown paths);
  // the denylist keeps that fallback from ever answering a top-level
  // navigation to an API path (e.g. an image URL opened directly).
  pwa: {
    registerType: 'autoUpdate',
    manifest: {
      id: '/',
      name: 'Heya',
      short_name: 'Heya',
      description: 'Self-hosted media server for movies, TV, music, and books.',
      start_url: '/',
      display: 'standalone',
      background_color: '#0a0a12',
      theme_color: '#0a0a12',
      // No `orientation` lock: on foldables the portrait lock stops Chrome
      // from resizing the standalone window across a fold/unfold — the app
      // stays at the folded viewport (~70% height) until fully relaunched.
      // Unlocked, the window resizes live and the responsive breakpoints
      // (useViewport + CSS media queries) react without a restart.
      icons: [
        { src: '/pwa-192x192.png', sizes: '192x192', type: 'image/png' },
        { src: '/pwa-512x512.png', sizes: '512x512', type: 'image/png' },
        { src: '/pwa-maskable-512x512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
      ],
    },
    workbox: {
      // Defaults only glob js/css/html; add the icon + font formats that
      // make up the rest of the "app shell" so the standalone window has
      // something to paint from cache immediately. `akarisub` (libass WASM
      // + its font, ~3.5 MB) is the subtitle renderer for ASS tracks — only
      // needed when a video with ASS subs actually plays, not part of the
      // shell, so it's excluded from precache and fetched on demand instead.
      globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
      globIgnores: ['**/akarisub/**'],
      // The html glob above never actually matches the SPA shell: Nitro
      // writes index.html AFTER the client build where workbox's glob runs,
      // so without this explicit entry the built sw.js contained NO html in
      // its precache manifest while createHandlerBoundToURL('/index.html')
      // still pointed at it — a runtime `non-precached-url` error on every
      // SW-handled navigation. The Date.now revision busts the entry each
      // build; the shell is tiny and references content-hashed assets, so
      // always-refetch-on-update is the correct behavior anyway.
      additionalManifestEntries: [
        { url: '/index.html', revision: Date.now().toString(36) },
      ],
      navigateFallback: '/index.html',
      navigateFallbackDenylist: [/^\/api/],
      // The ONLY `/api/*` requests the SW may intercept: media images. Their
      // URLs are stable but the CONTENT is mutable — album covers especially
      // (re-identify/edit swaps the bytes behind the same
      // `/artists/{slug}/albums/{slug}/cover` URL). So StaleWhileRevalidate,
      // NOT CacheFirst: paint instantly from the SW cache (kills the
      // repeated-reload flicker on the media grids) while a background fetch
      // refreshes the entry, so edited art lands on the next view rather than
      // being pinned. `maxAgeSeconds` is capped at the server's own 7-day
      // `immutable` window so the SW never holds art staler than the browser's
      // HTTP cache already would. Auth, streaming, and every other `/api/*`
      // route match no rule here, so the SW leaves them alone — always
      // network-fresh. Covers `/image` variants (media/person/studio, incl.
      // `?w=…&q=…` resize params, keyed per size) and album `/cover`. 500-entry
      // LRU, quota-purge.
      runtimeCaching: [
        {
          urlPattern: /\/api\/(?:media|person|studio)\/[^/]+\/image|\/api\/music\/artists\/[^/]+\/albums\/[^/]+\/cover/,
          handler: 'StaleWhileRevalidate',
          options: {
            cacheName: 'heya-images',
            expiration: {
              maxEntries: 500,
              maxAgeSeconds: 60 * 60 * 24 * 7,
              purgeOnQuotaError: true,
            },
            cacheableResponse: { statuses: [0, 200] },
          },
        },
      ],
    },
    devOptions: {
      // Never register a SW against the Vite dev server — it would fight
      // HMR and the `heya dev-proxy` front door described in CLAUDE.md.
      enabled: false,
    },
  },

  image: {
    providers: {
      heya: {
        name: 'heya',
        provider: '~/providers/heya.ts',
      },
    },
    provider: 'heya',
  },

  // Typed OpenAPI client. The schema is regenerated by `make gen-api-client`
  // (lefthook + CI gate prevent drift). The module generates `useHeya` (real
  // useFetch wrapper) and `$heya` ($fetch wrapper); auth is wired through
  // openFetch hooks in plugins/heyaApi.client.ts.
  openFetch: {
    clients: {
      heya: {
        baseURL: '',
        schema: './shared/api.openapi.json',
      },
    },
  },

  components: [
    { path: '~/components', pathPrefix: false },
  ],

  // Vite ships `server.allowedHosts` defaulting to localhost-only, which
  // rejects any request with an external Host header — Tailscale MagicDNS
  // names, Funnel URLs, the laptop's LAN IP when probing from another
  // device. Caddy forwards the original Host header through, so Vite at
  // :3000 sees e.g. `mybox.tailnet.ts.net` and 403s with the "not allowed"
  // message. We allow the whole `.ts.net` suffix so any tailnet device can
  // hit the dev server without per-machine config; localhost stays implicit.
  // Dev-only — embedded SPA in prod never touches Vite.
  vite: {
    server: {
      allowedHosts: ['.ts.net'],
    },
  },

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
    '~/assets/css/surface.css',
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
        { name: 'viewport', content: 'width=device-width, initial-scale=1, viewport-fit=cover' },
        { name: 'theme-color', content: '#0a0a12' },
      ],
      link: [
        { rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' },
        // @vite-pwa/nuxt (unlike plain vite-plugin-pwa in a non-Nuxt app)
        // does NOT inject <link rel="manifest"> itself — Nuxt owns its own
        // head/document rendering instead of the raw index.html Vite
        // normally transforms, and this module's setup never touches
        // `app.head` (verified against its source — no head/link/meta
        // manipulation at all). Both this and apple-touch-icon are manual.
        { rel: 'manifest', href: '/manifest.webmanifest' },
        // iOS/iPadOS "Add to Home Screen" reads apple-touch-icon directly —
        // Apple never adopted the Web App Manifest icon list.
        { rel: 'apple-touch-icon', href: '/apple-touch-icon.png' },
      ],
      // Inline style so the document is dark from byte 0 — before any CSS file
      // loads and before the spa-loading-template renders. Kills the white
      // browser-default flash on cold loads / slow connections.
      style: [
        { innerHTML: 'html,body{background:#0a0a12;margin:0;color-scheme:dark}' },
      ],
    },
  },

})
