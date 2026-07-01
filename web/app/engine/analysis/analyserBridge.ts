// Pulls FFT + waveform data out of the signal chain so visualizers can read
// it without touching the audio routing.
export class AnalyserBridge {
  readonly analyserNode: AnalyserNode
  private frequencyData: Float32Array<ArrayBuffer>
  private timeDomainData: Float32Array<ArrayBuffer>

  // fftSize 8192 (audioMotion-analyzer's default) → ~5.4 Hz/bin at 44.1 kHz,
  // giving the 20–200 Hz region ~33 bins so a log-frequency spectrum can
  // actually resolve the bass instead of collapsing it onto 1–2 bins. The
  // −80…−20 dB window is the visible range for the byte-data path / a sane
  // reference; the spectrum reads float data and windows explicitly. These dB
  // knobs and smoothing only affect frequency data — the scope/VU read raw
  // time-domain, so bumping fftSize just gives them a finer/steadier read.
  constructor(ctx: AudioContext, fftSize = 8192, smoothing = 0.8) {
    this.analyserNode = ctx.createAnalyser()
    this.analyserNode.fftSize = fftSize
    this.analyserNode.smoothingTimeConstant = smoothing
    this.analyserNode.minDecibels = -80
    this.analyserNode.maxDecibels = -20
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
