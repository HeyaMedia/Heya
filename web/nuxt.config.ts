export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: "2025-05-19",
  devtools: { enabled: false },

  modules: [
    "@pinia/nuxt",
    "@pinia/colada-nuxt",
    "@nuxtjs/tailwindcss",
    "@vueuse/nuxt",
    "@nuxt/image",
    "@vite-pwa/nuxt",
  ],

  hooks: {
    "build:manifest"(manifest) {
      // Nuxt normally emits rel=prefetch for every dynamic entry. That would
      // download all three playback payloads immediately after Home becomes
      // idle, defeating both the lazy imports and the Workbox exclusions.
      for (const resource of Object.values(manifest)) {
        if (/^feature-(?:hls|subtitles|visualizer)\./.test(resource.file || "")) {
          resource.prefetch = false
        }
      }
    },
    // Nuxt's Vite builder creates the final client environment after merging
    // `vite.build`, so output-level Rolldown settings placed there are
    // overwritten. Apply the production chunk policy to the final client
    // config instead. The app and all route/UI code become one cacheable
    // payload; the genuinely heavyweight playback engines stay lazy so Home
    // does not parse a video stack or visualizer it never uses.
    "vite:extendConfig"(config, { isClient }) {
      if (!isClient || process.env.NODE_ENV !== "production") return
      type RolldownOutput = Record<string, unknown> & { codeSplitting?: unknown }
      type ClientBuild = typeof config.build & {
        rolldownOptions?: { output?: RolldownOutput | RolldownOutput[] }
      }
      const environmentBuild = config.environments?.client?.build as ClientBuild | undefined
      const build = (environmentBuild ?? config.build) as ClientBuild
      if (!build) return
      build.rolldownOptions ??= {}
      const currentOutput = build.rolldownOptions.output
      const outputs = (Array.isArray(currentOutput) ? currentOutput : [currentOutput ?? {}])
      for (const output of outputs) {
        output.codeSplitting = {
          groups: [
            {
              // A single total partition avoids the catch-all recursively
              // reclaiming modules assigned by earlier groups. Rolldown treats
              // each returned name as its own manual group.
              name(id: string) {
                if (/[\\/]node_modules[\\/]butterchurn(?:-presets)?[\\/]/.test(id)) return "feature-visualizer"
                if (/[\\/]node_modules[\\/]hls\.js[\\/]/.test(id)) return "feature-hls"
                if (/[\\/]node_modules[\\/]akarisub[\\/]/.test(id)) return "feature-subtitles"
                return "app"
              },
              test: () => true,
              includeDependenciesRecursively: false,
            },
          ],
        }
        // Nuxt's default callback intentionally emits content-hash-only
        // filenames, which throws away the manual group name. Preserve it so
        // Workbox can exclude the three lazy feature payloads deterministically
        // without inspecting minified source code.
        output.chunkFileNames = "_nuxt/[name].[hash].js"
        // The catch-all deliberately does not pull dynamic dependencies into
        // itself. Preserve module execution order across the resulting manual
        // boundaries; Rolldown otherwise warns that circular chunks can run
        // side effects too early.
        output.strictExecutionOrder = true
      }
      build.rolldownOptions.output = Array.isArray(currentOutput) ? outputs : outputs[0]
    },
  },

  // PWA install support (Wave 4 of docs/responsive-plan.md). Self-hosted app
  // with frequent tagged releases. `generateSW` (the module default) precaches
  // the built app shell, so a freshly deployed version stays invisible until
  // the service worker is refreshed — and on an installed standalone PWA that
  // refresh almost never happens on its own (the app resumes an SPA session;
  // client-side routing never re-checks the SW), so updates would silently
  // stall on phones/tablets. `registerType: 'prompt'` (NOT 'autoUpdate') hands
  // the "new version waiting" signal to app/plugins/pwa-update.client.ts, which
  // polls for updates (client.periodicSyncForUpdates below, plus on foreground
  // + boot) and applies them SILENTLY — but only while nothing is playing, so a
  // song/video is never cut off mid-playback (autoUpdate would reload the
  // instant an update landed, interrupting playback).
  //
  // The ONLY `/api/*` requests the SW intercepts are media images (see the
  // `runtimeCaching` rule below, StaleWhileRevalidate) — auth, streaming, and
  // every other API route match no rule, so the SW leaves them alone and they
  // always hit the network fresh. `navigateFallback` covers deep-link/SPA
  // navigations the same way `spaHandler` does server-side
  // (internal/server/frontend.go always serves index.html for unknown paths);
  // the denylist keeps that fallback from ever answering a top-level
  // navigation to an API path (e.g. an image URL opened directly).
  pwa: {
    registerType: "prompt",
    // Poll for a new service worker hourly while the app is open; the plugin
    // layers on foreground + boot checks and applies the update when idle.
    client: {
      periodicSyncForUpdates: 3600,
    },
    manifest: {
      id: "/",
      name: "Heya",
      short_name: "Heya",
      description: "Self-hosted media server for movies, TV, music, and books.",
      start_url: "/",
      display: "standalone",
      background_color: "#0a0a12",
      theme_color: "#0a0a12",
      // No `orientation` lock: on foldables the portrait lock stops Chrome
      // from resizing the standalone window across a fold/unfold — the app
      // stays at the folded viewport (~70% height) until fully relaunched.
      // Unlocked, the window resizes live and the responsive breakpoints
      // (useViewport + CSS media queries) react without a restart.
      icons: [
        { src: "/pwa-192x192.png", sizes: "192x192", type: "image/png" },
        { src: "/pwa-512x512.png", sizes: "512x512", type: "image/png" },
        {
          src: "/pwa-maskable-512x512.png",
          sizes: "512x512",
          type: "image/png",
          purpose: "maskable",
        },
      ],
    },
    workbox: {
      // Adds only the click behavior for locally-triggered now-playing
      // notifications; Workbox continues to own generation and precaching.
      importScripts: ["/notification-click.js"],
      // Defaults only glob js/css/html; add the icon + font formats that
      // make up the rest of the "app shell" so the standalone window has
      // something to paint from cache immediately. Heavy playback-only
      // features stay out of the install/update path: libass, hls.js,
      // Butterchurn, and its presets are fetched on demand when somebody
      // actually plays that media.
      globPatterns: ["**/*.{js,css,html,svg,png,woff2}"],
      globIgnores: [
        "**/akarisub/**",
        "**/feature-hls.*.js",
        "**/feature-subtitles.*.js",
        "**/feature-visualizer.*.js",
      ],
      // The deliberately coarse app chunk is larger than Workbox's generic
      // 2 MiB safety default. Heya wants it installed as one atomic shell;
      // playback-only chunks above remain excluded and load on demand.
      maximumFileSizeToCacheInBytes: 8 * 1024 * 1024,
      // The html glob above never actually matches the SPA shell: Nitro
      // writes index.html AFTER the client build where workbox's glob runs,
      // so without this explicit entry the built sw.js contained NO html in
      // its precache manifest while createHandlerBoundToURL('/index.html')
      // still pointed at it — a runtime `non-precached-url` error on every
      // SW-handled navigation. The Date.now revision busts the entry each
      // build; the shell is tiny and references content-hashed assets, so
      // always-refetch-on-update is the correct behavior anyway.
      additionalManifestEntries: [
        { url: "/index.html", revision: Date.now().toString(36) },
      ],
      navigateFallback: "/index.html",
      // Compatibility APIs are separate protocol surfaces, not SPA routes.
      // Let their network responses through so an installed Heya PWA cannot
      // turn Jellyfin/Subsonic discovery URLs into the Nuxt 404 page.
      navigateFallbackDenylist: [
        /^\/api/,
        // Canonical Jellyfin routes are PascalCase; its lowercase legacy and
        // web/socket routes are enumerated separately. The server performs
        // exact case-insensitive route matching after the network request.
        /^\/[A-Z]/,
        /^\/(?:emby|socket|embywebsocket|web|api-docs|robots\.txt)(?:\/|$)/i,
        /^\/rest(?:\/|$)/i,
      ],
      // The ONLY `/api/*` requests the SW may intercept: media images. Generic
      // media/person/studio files carry the server's seven-day immutable
      // contract, and media objects add their durable updated-at revision to
      // the URL (useMedia.ts), so CacheFirst avoids running a background fetch
      // path on every remount. Album-cover DTOs do not yet all expose a durable
      // artwork revision; keep those on StaleWhileRevalidate so a refreshed
      // cover cannot stay pinned behind its stable slug URL. Auth, streaming,
      // and every other `/api/*` route match no rule here.
      runtimeCaching: [
        {
          urlPattern: /\/api\/(?:media|person|studio)\/[^/]+\/image/,
          handler: "CacheFirst",
          options: {
            cacheName: "heya-images",
            expiration: {
              maxEntries: 500,
              maxAgeSeconds: 60 * 60 * 24 * 7,
              purgeOnQuotaError: true,
            },
            cacheableResponse: { statuses: [0, 200] },
          },
        },
        {
          urlPattern: /\/api\/music\/artists\/[^/]+\/albums\/[^/]+\/cover/,
          handler: "StaleWhileRevalidate",
          options: {
            cacheName: "heya-album-covers",
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
        name: "heya",
        provider: "~/providers/heya.ts",
      },
    },
    provider: "heya",
  },

  components: [{ path: "~/components", pathPrefix: false }],

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
      allowedHosts: [".ts.net"],
    },
    build: {
      // Consolidate CSS into fewer files
      cssCodeSplit: false,
      minify: "terser",
      terserOptions: {
        compress: {
          drop_console: process.env.NODE_ENV === "production",
          drop_debugger: true,
          pure_funcs: ["console.log", "console.info", "console.debug"],
        },
      },
    },
    optimizeDeps: {
      include: [
        '@phosphor-icons/vue',
        '@pinia/colada-devtools',
        '@pinia/colada-plugin-auto-refetch',
        '@pinia/colada-plugin-retry',
        '@vue/devtools-core',
        '@vue/devtools-kit',
        'akarisub',
        'butterchurn', // CJS
        'butterchurn-presets', // CJS
        'butterchurn-presets/lib/butterchurnPresetsExtra.min.js', // CJS
        'butterchurn-presets/lib/butterchurnPresetsExtra2.min.js', // CJS
        'butterchurn-presets/lib/butterchurnPresetsMD1.min.js', // CJS
        'hls.js',
        'reka-ui',
        'vue-virtual-scroller',
      ],
    },
  },

  // Nitro
  nitro: {
    preset: "bun",
    minify: true,
    // The production SPA is embedded and served by Go's http.FileServerFS;
    // Heya's outer HTTP middleware already gzip-compresses text responses.
    // FileServerFS does not negotiate Nitro's sibling .gz/.br files, so
    // generating them only tripled the asset count and bloated the binary.
    compressPublicAssets: false,
    sourceMap: false,

    // Esbuild minification
    esbuild: {
      options: {
        minifySyntax: true,
        minifyWhitespace: true,
        minifyIdentifiers: true,
        treeShaking: true,
        target: "esnext",
      },
    },
  },

  // Experimental performance features
  experimental: {
    // Nuxt 4.5 can share Vite's watcher instead of opening a second watcher
    // tree, reducing file-descriptor pressure in Heya's large local workspace.
    // Production generation does not use this watcher.
    watcher: "builder",
    renderJsonPayloads: true, // Faster SSR JSON payloads via native JSON.parse
    writeEarlyHints: false, // No-op on nitro's bun preset (node-only feature)
    // View Transitions run the browser's default ~250ms full-root crossfade on
    // every client-side nav: the old page visibly fades away, then the new
    // page (snapshotted before its images decode) fades in. With home surfaces
    // painting synchronously from the persisted query cache, that crossfade IS
    // the perceived flicker — an instant swap reads as native. Re-enable only
    // together with custom ::view-transition CSS for targeted shared elements.
    viewTransition: false,
    payloadExtraction: false, // Disabled — conflicts with dynamic route caching on disk
  },

  css: [
    "@fontsource/inter/latin-400.css",
    "@fontsource/inter/latin-500.css",
    "@fontsource/inter/latin-600.css",
    "@fontsource/inter/latin-700.css",
    "@fontsource/jetbrains-mono/latin-400.css",
    "@fontsource/jetbrains-mono/latin-500.css",
    "@fontsource/jetbrains-mono/latin-600.css",
    "@fontsource/jetbrains-mono/latin-700.css",
    "@fontsource/jetbrains-mono/latin-800.css",
    // Display face (Heya 2.0). `standard.css` carries BOTH the weight and
    // width axes (font-stretch 62%–125%); the default `index.css` is
    // weight-only, so it would ignore `font-variation-settings: "wdth" …`.
    "@fontsource-variable/archivo/standard.css",
    // Optional type sets (Settings → Appearance → Type set). Registering all
    // @font-face blocks is cheap — each woff2 only downloads when a family is
    // actually painted (i.e. --font-display/--font-sans resolves to it via the
    // data-typeset override in heya.css). `standard.css` for Fraunces carries
    // the optical-size axis so `font-optical-sizing: auto` grades it at display
    // sizes; the rest are weight-only.
    "@fontsource-variable/fraunces/standard.css",
    "@fontsource-variable/source-serif-4/index.css",
    "@fontsource-variable/space-grotesk/index.css",
    "@fontsource-variable/nunito/index.css",
    "~/assets/css/heya.css",
    "~/assets/css/main.css",
    "~/assets/css/surface.css",
  ],

  runtimeConfig: {
    public: {
      apiBase: "/api",
      // Release builds receive the git tag from Docker/CI. Local Nuxt starts
      // get a unique identity so storage diagnostics and update logs never
      // ambiguously report a production release.
      heyaVersion: process.env.NUXT_PUBLIC_HEYA_VERSION || `dev-${Date.now().toString(36)}`,
    },
  },

  app: {
    head: {
      title: "Heya",
      // WCAG 3.1.1 (Language of Page): a lang on <html> lets screen readers
      // pick the right pronunciation/voice. The app ships English strings.
      htmlAttrs: { lang: "en" },
      meta: [
        { charset: "utf-8" },
        {
          name: "viewport",
          content: "width=device-width, initial-scale=1, viewport-fit=cover",
        },
        { name: "theme-color", content: "#0a0a12" },
      ],
      link: [
        { rel: "icon", type: "image/svg+xml", href: "/favicon.svg" },
        // @vite-pwa/nuxt (unlike plain vite-plugin-pwa in a non-Nuxt app)
        // does NOT inject <link rel="manifest"> itself — Nuxt owns its own
        // head/document rendering instead of the raw index.html Vite
        // normally transforms, and this module's setup never touches
        // `app.head` (verified against its source — no head/link/meta
        // manipulation at all). Both this and apple-touch-icon are manual.
        { rel: "manifest", href: "/manifest.webmanifest" },
        // iOS/iPadOS "Add to Home Screen" reads apple-touch-icon directly —
        // Apple never adopted the Web App Manifest icon list.
        { rel: "apple-touch-icon", href: "/apple-touch-icon.png" },
      ],
      // Inline style so the document is painted in the right theme from
      // byte 0 — before any CSS file loads and before the
      // spa-loading-template renders. Kills the white browser-default flash
      // on cold loads / slow connections. The boot script below stamps
      // data-theme/data-accent/data-density on <html> synchronously (from
      // the localStorage mirror that useAppearance maintains), so the
      // attribute-keyed rules here resolve before first paint.
      style: [
        {
          innerHTML:
            "html,body{background:#0a0a12;margin:0;color-scheme:dark}" +
            "html[data-theme=light],html[data-theme=light] body{background:#f1eee7;color-scheme:light}" +
            "html[data-theme=oled],html[data-theme=oled] body{background:#000}",
        },
      ],
      script: [
        {
          // Pre-paint attribute stamp from the localStorage mirror. Stays dumb:
          // the custom-accent branch replays the CACHED derived family verbatim
          // (never re-derives). Mirrors useAppearance.apply(); any default value
          // leaves its attribute off. Keep tiny + defensive (try/catch).
          innerHTML:
            "(function(){try{" +
            'var s=JSON.parse(localStorage.getItem("heya-appearance")||"{}");' +
            "var e=document.documentElement,d=e.dataset,st=e.style;" +
            'var t=s.theme||"dark";' +
            'if(t==="system"){t=window.matchMedia&&matchMedia("(prefers-color-scheme: light)").matches?"light":"dark"}' +
            'if(t!=="dark")d.theme=t;' +
            "var ac=s.accentCustomDerived;" +
            "if(s.accentCustom&&ac&&ac.accent){" +
            'st.setProperty("--accent",ac.accent);' +
            'st.setProperty("--accent-rgb",ac.rgb);' +
            'st.setProperty("--accent-bright",ac.bright);' +
            'st.setProperty("--accent-deep",ac.deep);' +
            'st.setProperty("--accent-ink",ac.ink);' +
            '}else if(s.accent&&s.accent!=="gold"){d.accent=s.accent}' +
            'if(s.density&&s.density!=="comfortable")d.density=s.density;' +
            'if(s.typeset&&s.typeset!=="heya")d.typeset=s.typeset;' +
            'if(s.fontScale&&s.fontScale!=="md")d.fontscale=s.fontScale;' +
            'if(s.lighting&&s.lighting!=="dramatic")d.lighting=s.lighting;' +
            'if(s.glass&&s.glass!=="rich")d.glass=s.glass;' +
            'if(s.radius&&s.radius!=="soft")d.radius=s.radius;' +
            'if(s.hero&&s.hero!=="standard")d.hero=s.hero;' +
            'if(s.motion&&s.motion!=="system")d.motion=s.motion;' +
            'if(s.scrollbar&&s.scrollbar!=="overlay")d.scrollbar=s.scrollbar;' +
            "}catch(e){}})()",
        },
      ],
    },
  },
});
