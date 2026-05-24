// PrefetchQueue keeps a bounded LRU of preloaded <audio> elements so the
// next-in-queue track is already in the browser cache when the user (or the
// scheduler) calls for it. Cuts perceived latency on transitions.
export class PrefetchQueue {
  private pool = new Map<string, HTMLAudioElement>()
  private order: string[] = []

  constructor(private maxDepth: number = 5) {}

  setDepth(depth: number) {
    this.maxDepth = Math.max(1, depth)
    this.trim()
  }

  add(urls: string[]) {
    for (const url of urls) {
      if (this.pool.has(url)) continue
      const audio = new Audio()
      audio.preload = 'auto'
      audio.src = url
      this.pool.set(url, audio)
      this.order.push(url)
    }
    this.trim()
  }

  get(url: string): HTMLAudioElement | undefined { return this.pool.get(url) }

  dispose(url: string) {
    const audio = this.pool.get(url)
    if (audio) {
      audio.removeAttribute('src')
      audio.load()
      this.pool.delete(url)
      this.order = this.order.filter((u) => u !== url)
    }
  }

  clear() {
    for (const [url] of this.pool) this.dispose(url)
  }

  private trim() {
    while (this.order.length > this.maxDepth) {
      const oldest = this.order.shift()
      if (oldest) this.dispose(oldest)
    }
  }

  get size(): number { return this.pool.size }
}
