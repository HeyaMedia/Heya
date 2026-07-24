// One gain stage after the deck mix owns user volume and mute. Per-deck gain
// nodes are reserved for transition automation, so a crossfade curve can
// never ramp an incoming track above the listener's selected level.
//
// A second stage after it owns the transport envelope (pause/resume). Keeping
// the two separate means a volume change mid-fade can't cancel the envelope,
// and the envelope never overwrites the level the listener chose.
export class MasterOutput {
  readonly inputNode: GainNode
  private readonly transportNode: GainNode

  constructor(private ctx: AudioContext, destination: AudioNode) {
    this.inputNode = ctx.createGain()
    this.transportNode = ctx.createGain()
    this.inputNode.connect(this.transportNode)
    this.transportNode.connect(destination)
  }

  setVolume(value: number) {
    const clamped = Math.max(0, Math.min(1, value))
    const gain = this.inputNode.gain
    gain.cancelScheduledValues(this.ctx.currentTime)
    gain.setValueAtTime(clamped, this.ctx.currentTime)
  }

  // Ramp the transport envelope, resolving when it lands. Pausing mid-waveform
  // is a step discontinuity — this is what removes the click it makes.
  fadeTransport(target: number, seconds: number): Promise<void> {
    const now = this.ctx.currentTime
    const gain = this.transportNode.gain
    gain.cancelScheduledValues(now)
    gain.setValueAtTime(gain.value, now)
    gain.linearRampToValueAtTime(target, now + seconds)
    return new Promise((resolve) => setTimeout(resolve, Math.ceil(seconds * 1000)))
  }

  setTransport(value: number) {
    const gain = this.transportNode.gain
    gain.cancelScheduledValues(this.ctx.currentTime)
    gain.setValueAtTime(value, this.ctx.currentTime)
  }

  dispose() {
    this.inputNode.disconnect()
    this.transportNode.disconnect()
  }
}
