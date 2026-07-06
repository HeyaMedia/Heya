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
    connectAudio(node: AudioNode): void
    disconnectAudio(node: AudioNode): void
    loadPreset(preset: object, blendTimeSeconds: number): void
    render(): void
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
