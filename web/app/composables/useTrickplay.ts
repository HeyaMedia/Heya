export interface TrickplayEntry {
  start: number
  end: number
  spriteUrl: string
  x: number
  y: number
  w: number
  h: number
}

export function useTrickplay(fileId: Ref<number>) {
  const entries = ref<TrickplayEntry[]>([])
  const loaded = ref(false)

  async function load(token: string) {
    try {
      const text = await $fetch<string>(`/api/stream/${fileId.value}/trickplay/index.vtt?token=${token}`, { responseType: 'text' })
      entries.value = parseWebVTT(text, fileId.value, token)
      loaded.value = entries.value.length > 0
    } catch {
      loaded.value = false
    }
  }

  function getThumbnail(time: number): TrickplayEntry | null {
    for (const e of entries.value) {
      if (time >= e.start && time < e.end) return e
    }
    return null
  }

  return { entries, loaded, load, getThumbnail }
}

function parseWebVTT(text: string, fileId: number, token: string): TrickplayEntry[] {
  const results: TrickplayEntry[] = []
  const lines = text.split('\n')
  let i = 0

  while (i < lines.length && !lines[i]!.includes('-->')) i++

  while (i < lines.length) {
    const line = lines[i]!.trim()
    if (!line.includes('-->')) { i++; continue }

    const [startStr, endStr] = line.split('-->').map(s => s.trim())
    const start = parseVTTTime(startStr!)
    const end = parseVTTTime(endStr!)

    i++
    if (i >= lines.length) break
    const urlLine = lines[i]!.trim()
    if (!urlLine) { i++; continue }

    const hashIdx = urlLine.indexOf('#xywh=')
    if (hashIdx < 0) { i++; continue }

    const spriteName = urlLine.slice(0, hashIdx)
    const coords = urlLine.slice(hashIdx + 6).split(',').map(Number)
    if (coords.length < 4) { i++; continue }

    results.push({
      start,
      end,
      spriteUrl: `/api/stream/${fileId}/trickplay/${spriteName}?token=${token}`,
      x: coords[0]!,
      y: coords[1]!,
      w: coords[2]!,
      h: coords[3]!,
    })
    i++
  }

  return results
}

function parseVTTTime(str: string): number {
  const parts = str.split(':')
  if (parts.length === 3) {
    const [h, m, rest] = parts
    const [s, ms] = rest!.split('.')
    return Number(h) * 3600 + Number(m) * 60 + Number(s) + Number(ms || 0) / 1000
  }
  if (parts.length === 2) {
    const [m, rest] = parts
    const [s, ms] = rest!.split('.')
    return Number(m) * 60 + Number(s) + Number(ms || 0) / 1000
  }
  return 0
}
