// Shared "quality label" formatter for a track file — "FLAC 24/96" (bit
// depth / kHz) when those fields are populated, "MP3 320" (bitrate) when
// they're not. Nuxt auto-imports `app/utils/**`, so pages/components call
// this directly with no import statement.
//
// Logic lifted verbatim from TrackQualityPicker's old inline `chipLabel` —
// that component now delegates to this function (see its script block) so
// TrackList phone rows and any other consumer render an identical label.
export interface TrackQualityInput {
  format?: string | null
  bitrate_kbps?: number | null
  sample_rate_hz?: number | null
  bit_depth?: number | null
}

export function formatTrackQuality(f: TrackQualityInput): string | null {
  if (!f.format) return null
  const parts: string[] = [f.format.toUpperCase()]
  const bitDepth = f.bit_depth ?? 0
  const sampleRateHz = f.sample_rate_hz ?? 0
  const bitrateKbps = f.bitrate_kbps ?? 0
  if (bitDepth > 0 && sampleRateHz > 0) {
    parts.push(`${bitDepth}/${Math.round(sampleRateHz / 1000)}`)
  } else if (bitrateKbps > 0) {
    parts.push(`${bitrateKbps}`)
  }
  return parts.join(' ')
}
