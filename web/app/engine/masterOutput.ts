// One gain stage after the deck mix owns user volume and mute. Per-deck gain
// nodes are reserved for transition automation, so a crossfade curve can
// never ramp an incoming track above the listener's selected level.
export class MasterOutput {
  readonly inputNode: GainNode

  constructor(private ctx: AudioContext, destination: AudioNode) {
    this.inputNode = ctx.createGain()
    this.inputNode.connect(destination)
  }

  setVolume(value: number) {
    const clamped = Math.max(0, Math.min(1, value))
    const gain = this.inputNode.gain
    gain.cancelScheduledValues(this.ctx.currentTime)
    gain.setValueAtTime(clamped, this.ctx.currentTime)
  }

  dispose() {
    this.inputNode.disconnect()
  }
}
