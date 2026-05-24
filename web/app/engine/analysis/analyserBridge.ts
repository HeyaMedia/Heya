// Pulls FFT + waveform data out of the signal chain so visualizers can read
// it without touching the audio routing.
export class AnalyserBridge {
  readonly analyserNode: AnalyserNode
  private frequencyData: Float32Array<ArrayBuffer>
  private timeDomainData: Float32Array<ArrayBuffer>

  constructor(ctx: AudioContext, fftSize = 2048, smoothing = 0.8) {
    this.analyserNode = ctx.createAnalyser()
    this.analyserNode.fftSize = fftSize
    this.analyserNode.smoothingTimeConstant = smoothing
    this.frequencyData = new Float32Array(new ArrayBuffer(this.analyserNode.frequencyBinCount * 4))
    this.timeDomainData = new Float32Array(new ArrayBuffer(this.analyserNode.fftSize * 4))
  }

  connectFrom(source: AudioNode): AudioNode {
    source.connect(this.analyserNode)
    return this.analyserNode
  }

  getFrequencyData(): Float32Array<ArrayBuffer> {
    this.analyserNode.getFloatFrequencyData(this.frequencyData)
    return this.frequencyData
  }

  getTimeDomainData(): Float32Array<ArrayBuffer> {
    this.analyserNode.getFloatTimeDomainData(this.timeDomainData)
    return this.timeDomainData
  }

  dispose() { this.analyserNode.disconnect() }
}
