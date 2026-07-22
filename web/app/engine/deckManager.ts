import type { CrossfadeMode } from '~~/shared/types/audio'
import { generateFadeIn, generateFadeOut } from './crossfade/curves'
import type { TransitionPlan } from './crossfade/strategy'
import { alog } from './debug'
import { Deck } from './deck'

export interface DeckManagerEvents {
  onTrackEnded: () => void
  onTimeUpdate: (currentTime: number, duration: number) => void
  onError: (error: Error) => void
}

// Owns two Deck instances — one active, one pending — and orchestrates the
// hand-off between them. Gapless: pause-then-play instantly after the swap.
// Crossfade: gain automation curves on both decks for the overlap window,
// then promote the pending deck to active.
export class DeckManager {
  private activeDeck: Deck
  private pendingDeck: Deck
  private events: Partial<DeckManagerEvents> = {}
  private onSwap: (() => void) | null = null
  private transitioning = false
  private transitionTimer: ReturnType<typeof setTimeout> | null = null
  private transitionResolve: (() => void) | null = null

  constructor(private ctx: AudioContext) {
    this.activeDeck = new Deck(ctx)
    this.pendingDeck = new Deck(ctx)

    this.activeDeck.on('onEnded', () => this.events.onTrackEnded?.())
    this.activeDeck.on('onTimeUpdate', (time, dur) => this.events.onTimeUpdate?.(time, dur))
    this.activeDeck.on('onError', (err) => this.events.onError?.(err))
  }

  on<K extends keyof DeckManagerEvents>(event: K, handler: DeckManagerEvents[K]) {
    this.events[event] = handler
  }

  setOnSwap(callback: () => void) { this.onSwap = callback }

  getActiveOutput(): AudioNode { return this.activeDeck.getOutputNode() }

  async loadAndPlay(url: string, startPositionSeconds = 0): Promise<void> {
    // Cold loads enter silently and are faded to unity by the engine. The
    // shared master gain applies the user's actual volume after the deck mix.
    this.activeDeck.setTransitionGain(0)
    await this.activeDeck.load(url)
    if (startPositionSeconds > 0) this.activeDeck.seek(startPositionSeconds)
    await this.activeDeck.play()
  }

  async loadNext(url: string): Promise<void> {
    // A deck retired by a previous crossfade ends at zero. Reset it before it
    // becomes a gapless pending deck; crossfade curves explicitly start it at
    // zero again when overlap is requested.
    this.pendingDeck.setTransitionGain(1)
    await this.pendingDeck.load(url)
  }

  async transition(mode: CrossfadeMode | 'gapless', plan?: TransitionPlan): Promise<void> {
    if (mode === 'gapless') {
      alog('deck', 'gapless: pause active + swap to preloaded pending deck')
      this.activeDeck.pause()
      this.swapRoles()
      await this.activeDeck.play()
      return
    }

    const durationSeconds = plan?.durationSeconds ?? 3
    alog('deck', `crossfade: overlapping both decks for ${durationSeconds.toFixed(2)}s`)
    const fadeOutCurve = plan?.fadeOutCurve ?? generateFadeOut(durationSeconds * 100)
    const fadeInCurve = plan?.fadeInCurve ?? generateFadeIn(durationSeconds * 100)
    const now = this.ctx.currentTime

    this.activeDeck.transitionGainNode.gain.setValueCurveAtTime(new Float32Array(fadeOutCurve), now, durationSeconds)
    this.pendingDeck.transitionGainNode.gain.setValueCurveAtTime(new Float32Array(fadeInCurve), now, durationSeconds)

    this.transitioning = true
    await this.pendingDeck.play()

    await new Promise<void>((resolve) => {
      this.transitionResolve = resolve
      this.transitionTimer = setTimeout(() => {
        this.transitionTimer = null
        this.transitionResolve = null
        resolve()
      }, durationSeconds * 1000)
    })

    if (!this.transitioning) return

    this.transitioning = false
    this.activeDeck.pause()
    this.activeDeck.reset()
    this.swapRoles()
  }

  private cancelTransition() {
    if (this.transitionTimer) {
      clearTimeout(this.transitionTimer)
      this.transitionTimer = null
    }
    this.transitioning = false

    const now = this.ctx.currentTime
    this.activeDeck.transitionGainNode.gain.cancelScheduledValues(now)
    this.activeDeck.transitionGainNode.gain.setValueAtTime(1, now)
    this.pendingDeck.transitionGainNode.gain.cancelScheduledValues(now)
    this.pendingDeck.transitionGainNode.gain.setValueAtTime(1, now)

    this.activeDeck.pause()
    this.activeDeck.reset()
    this.swapRoles()

    this.transitionResolve?.()
    this.transitionResolve = null
  }

  private swapRoles() {
    const temp = this.activeDeck
    this.activeDeck = this.pendingDeck
    this.pendingDeck = temp

    // The retired deck's handlers would otherwise keep firing time updates
    // after the swap and confuse the scheduler. Detach them.
    this.pendingDeck.clearEvents()

    this.activeDeck.on('onEnded', () => this.events.onTrackEnded?.())
    this.activeDeck.on('onTimeUpdate', (time, dur) => this.events.onTimeUpdate?.(time, dur))
    this.activeDeck.on('onError', (err) => this.events.onError?.(err))

    this.onSwap?.()
  }

  get active(): Deck { return this.activeDeck }
  get pending(): Deck { return this.pendingDeck }
  get isTransitioning(): boolean { return this.transitioning }

  readClock() {
    return {
      positionSeconds: this.activeDeck.currentTime,
      durationSeconds: this.activeDeck.duration,
      paused: this.activeDeck.paused,
    }
  }

  pause() {
    this.activeDeck.pause()
    if (this.transitioning) {
      this.pendingDeck.pause()
      this.cancelTransition()
    }
  }

  async play(): Promise<void> { await this.activeDeck.play() }

  seek(time: number) { this.activeDeck.seek(time) }

  stopAll() {
    if (this.transitioning) {
      if (this.transitionTimer) {
        clearTimeout(this.transitionTimer)
        this.transitionTimer = null
      }
      this.transitioning = false

      const now = this.ctx.currentTime
      this.activeDeck.transitionGainNode.gain.cancelScheduledValues(now)
      this.activeDeck.transitionGainNode.gain.setValueAtTime(1, now)
      this.pendingDeck.transitionGainNode.gain.cancelScheduledValues(now)
      this.pendingDeck.transitionGainNode.gain.setValueAtTime(1, now)

      this.transitionResolve?.()
      this.transitionResolve = null
    }

    this.activeDeck.pause()
    this.activeDeck.reset()
    this.pendingDeck.pause()
    this.pendingDeck.reset()
  }

  setActiveNormalization(gainLinear: number) { this.activeDeck.setNormGain(gainLinear) }
  setPendingNormalization(gainLinear: number) { this.pendingDeck.setNormGain(gainLinear) }

  dispose() {
    if (this.transitioning) this.stopAll()
    this.activeDeck.dispose()
    this.pendingDeck.dispose()
  }
}
