// Ambient declarations for third-party modules that ship without their own
// TypeScript types.

declare module 'akarisub' {
  // The library exposes a default class. Type the shape loosely (its surface
  // is large and undocumented) so consumers can use it as both a value and a
  // type without us maintaining a parallel definition.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  export default class AkariSub {
    constructor(...args: any[])
    [key: string]: any
  }
}

declare module 'butterchurn' {
  // Milkdrop WebGL renderer. Only the handful of methods we drive are typed;
  // presets are opaque objects passed straight back into loadPreset.
  export interface Visualizer {
    // Internal AudioProcessor (see AudioProcessor in butterchurn's source):
    // allocated at construction time regardless of whether connectAudio() is
    // ever called, sized to its own fftSize. Read .length off these rather
    // than hardcoding when feeding render({ audioLevels }) — see
    // VisualizerMilkdrop.vue's native-backend branch.
    readonly audio: {
      readonly timeByteArray: Uint8Array
      readonly timeByteArrayL: Uint8Array
      readonly timeByteArrayR: Uint8Array
    }
    connectAudio(node: AudioNode): void
    disconnectAudio(node: AudioNode): void
    loadPreset(preset: object, blendTimeSeconds: number): void
    // `audioLevels`, when passed, is copied straight into the internal
    // AudioProcessor's buffers and the connected AnalyserNode (if any) is NOT
    // sampled that frame — used to feed native-backend PCM in place of a real
    // WebAudio analyser tap.
    render(opts?: {
      audioLevels?: {
        timeByteArray: Uint8Array
        timeByteArrayL: Uint8Array
        timeByteArrayR: Uint8Array
      }
    }): void
    setRendererSize(width: number, height: number): void
  }
  interface ButterchurnStatic {
    createVisualizer(
      ctx: AudioContext,
      canvas: HTMLCanvasElement,
      opts: { width: number; height: number; pixelRatio?: number; textureRatio?: number },
    ): Visualizer
  }
  const butterchurn: ButterchurnStatic
  export default butterchurn
}

declare module 'butterchurn-presets' {
  // Ships as either a flat Record of name→preset or an object exposing
  // getPresets(). We normalize both at the call site.
  const presets: Record<string, object> & { getPresets?: () => Record<string, object> }
  export default presets
}

declare module 'butterchurn-presets/lib/*' {
  // The extra preset packs (MD1, Extra, Extra2, …) live under lib/ and share
  // the base pack's shape. Deep-imported to expand the preset library.
  const presets: Record<string, object> & { getPresets?: () => Record<string, object> }
  export default presets
}
