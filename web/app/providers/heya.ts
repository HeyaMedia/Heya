import { defineProvider } from '@nuxt/image/runtime'

// Custom nuxt-image provider for Heya. Maps modifier props (width, height,
// quality, format) onto the Go image endpoint's query parameters (w, h, q, f).
// Preserves any pre-existing query string (e.g. ?sort=N&label=X on media
// images) by appending with the correct separator.
//
// Only our own `/api/...` image endpoints support server-side resizing, so
// those are the only URLs we rewrite. Everything else — absolute external
// URLs (radio favicons, podcast art, metadata-provider search thumbnails),
// bundled static assets (`/img/*` rating logos, `/favicon.svg`), and `data:`
// URIs — is passed through untouched. This lets every `<img>` in the app be a
// `<NuxtImg>` uniformly: internal media gets resized + disk-cached, external
// images render as-is (and still benefit from the SW image cache by URL).
export default defineProvider({
  getImage(src, { modifiers }) {
    // Passthrough anything we can't resize server-side.
    if (!src.startsWith('/api/')) return { url: src }

    const { width, height, quality, format } = modifiers as {
      width?: number | string
      height?: number | string
      quality?: number | string
      format?: string
    }

    const params: string[] = []
    if (width) params.push(`w=${width}`)
    if (height) params.push(`h=${height}`)
    if (quality) params.push(`q=${quality}`)
    if (format === 'jpeg' || format === 'jpg' || format === 'png') {
      params.push(`f=${format}`)
    }

    if (!params.length) return { url: src }
    const sep = src.includes('?') ? '&' : '?'
    return { url: src + sep + params.join('&') }
  },
})
