export interface DeckEvents {
  onEnded: () => void
  onTimeUpdate: (currentTime: number, duration: number) => void
  onError: (error: Error) => void
}

// A Deck wraps one HTMLAudioElement plumbed into the Web Audio graph through
// a normGain → gain pair so we can apply per-track normalization and a
// per-deck volume independently. The DeckManager swaps two of these for
// gapless and crossfade transitions.
export class Deck {
  private audio: HTMLAudioElement
  private sourceNode: MediaElementAudioSourceNode | null = null
  readonly normGainNode: GainNode
  readonly gainNode: GainNode
  private events: Partial<DeckEvents> = {}
  private disposed = false

  constructor(private ctx: AudioContext) {
    this.audio = new Audio()
    this.audio.crossOrigin = 'use-credentials'
    this.audio.preload = 'auto'
    this.normGainNode = ctx.createGain()
    this.gainNode = ctx.createGain()

    this.audio.addEventListener('ended', () => this.events.onEnded?.())
    this.audio.addEventListener('timeupdate', () => {
      this.events.onTimeUpdate?.(this.audio.currentTime, this.audio.duration)
    })
    this.audio.addEventListener('error', () => {
      const msg = this.audio.error?.message ?? 'Unknown audio error'
      this.events.onError?.(new Error(msg))
    })
  }

  on<K extends keyof DeckEvents>(event: K, handler: DeckEvents[K]) {
    this.events[event] = handler
  }

  clearEvents() {
    this.events = {}
  }

  getOutputNode(): AudioNode {
    if (!this.sourceNode) {
      this.sourceNode = this.ctx.createMediaElementSource(this.audio)
      this.sourceNode.connect(this.normGainNode)
      this.normGainNode.connect(this.gainNode)
    }
    return this.gainNode
  }

  setNormGain(gainLinear: number) {
    this.normGainNode.gain.cancelScheduledValues(this.ctx.currentTime)
    this.normGainNode.gain.setValueAtTime(this.normGainNode.gain.value, this.ctx.currentTime)
    this.normGainNode.gain.linearRampToValueAtTime(gainLinear, this.ctx.currentTime + 0.1)
  }

  async load(url: string): Promise<void> {
    this.audio.src = url
    this.audio.load()
    await new Promise<void>((resolve, reject) => {
      const onCanPlay = () => { cleanup(); resolve() }
      const onError = () => { cleanup(); reject(new Error(this.audio.error?.message ?? 'Failed to load audio')) }
      const cleanup = () => {
        this.audio.removeEventListener('canplaythrough', onCanPlay)
        this.audio.removeEventListener('error', onError)
      }
      this.audio.addEventListener('canplaythrough', onCanPlay, { once: true })
      this.audio.addEventListener('error', onError, { once: true })
    })
  }

  async play(): Promise<void> {
    await this.audio.play()
  }

  pause() { this.audio.pause() }

  seek(time: number) {
    this.audio.currentTime = Math.max(0, Math.min(time, this.audio.duration || 0))
  }

  get currentTime(): number { return this.audio.currentTime }
  get duration(): number { return this.audio.duration || 0 }
  get paused(): boolean { return this.audio.paused }

  setVolume(value: number) {
    this.gainNode.gain.setValueAtTime(value, this.ctx.currentTime)
  }

  reset() {
    this.audio.pause()
    this.audio.removeAttribute('src')
    this.audio.load()
  }

  dispose() {
    if (this.disposed) return
    this.disposed = true
    this.audio.pause()
    this.audio.removeAttribute('src')
    this.sourceNode?.disconnect()
    this.normGainNode.disconnect()
    this.gainNode.disconnect()
  }
}
