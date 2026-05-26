import { defineProvider } from '@nuxt/image/runtime'

// Custom nuxt-image provider for Heya. Maps modifier props (width, height,
// quality, format) onto the Go image endpoint's query parameters (w, h, q, f).
// Preserves any pre-existing query string (e.g. ?sort=N&label=X on media
// images) by appending with the correct separator.
export default defineProvider({
  getImage(src, { modifiers }) {
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
