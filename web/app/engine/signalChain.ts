import type { DSPBlock } from '~~/shared/types/audio'

// SignalChain wires the deck output through an ordered list of DSP blocks
// to a single destination (typically the analyser → ctx.destination tail).
// Rebuilds the graph when blocks are toggled or reordered.
export class SignalChain {
  private blocks: DSPBlock[] = []
  private source: AudioNode | null = null
  private destination: AudioNode | null = null

  setBlocks(blocks: DSPBlock[]) {
    this.blocks = blocks
    this.rebuild()
  }

  setSource(source: AudioNode) {
    const oldSource = this.source
    this.source = source
    this.rebuildWithOldSource(oldSource)
  }

  setDestination(destination: AudioNode) {
    this.destination = destination
    this.rebuild()
  }

  rebuild() { this.rebuildWithOldSource(this.source) }

  private rebuildWithOldSource(oldSource: AudioNode | null) {
    if (!this.source || !this.destination) return
    this.disconnectOldSource(oldSource)

    let current: AudioNode = this.source
    for (const block of this.blocks) current = block.connect(current)
    current.connect(this.destination)
  }

  toggleBlock(name: string, enabled: boolean) {
    const block = this.blocks.find((b) => b.name === name)
    if (block) {
      block.enabled = enabled
      this.rebuild()
    }
  }

  getBlock<T extends DSPBlock>(name: string): T | undefined {
    return this.blocks.find((b) => b.name === name) as T | undefined
  }

  getBlocks(): DSPBlock[] { return this.blocks }

  // Feed an extra source into the same chain — used during crossfade so the
  // pending deck's output flows through EQ/limiter just like the active one.
  connectAdditionalSource(source: AudioNode) {
    if (this.blocks.length === 0) {
      if (this.destination) source.connect(this.destination)
      return
    }
    let current: AudioNode = source
    for (const block of this.blocks) current = block.connect(current)
    if (this.destination) current.connect(this.destination)
  }

  reorderBlocks(orderedMiddleNames: string[]) {
    if (this.blocks.length < 3) return

    const first = this.blocks[0]!
    const last = this.blocks[this.blocks.length - 1]!
    const middleBlocks = this.blocks.slice(1, -1)

    const reordered = orderedMiddleNames
      .map((name) => middleBlocks.find((b) => b.name === name))
      .filter((b): b is DSPBlock => b !== undefined)

    this.blocks = [first, ...reordered, last]
    this.rebuild()
  }

  private disconnectOldSource(oldSource: AudioNode | null) {
    oldSource?.disconnect()
    for (const block of this.blocks) {
      try { block.dispose() } catch { /* already disconnected */ }
    }
  }

  dispose() {
    this.disconnectOldSource(this.source)
    this.blocks = []
    this.source = null
    this.destination = null
  }
}
